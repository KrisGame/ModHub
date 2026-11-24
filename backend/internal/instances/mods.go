package instances

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"modhub/internal/downloads"
	"modhub/internal/events"
	"modhub/internal/netx"
)

type ModActionRequest struct {
	InstanceID string `json:"id"`
	Filename   string `json:"filename"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

type InstallModRequest struct {
	InstanceID  string `json:"id"`
	Title       string `json:"title,omitempty"`
	Filename    string `json:"filename"`
	URL         string `json:"url"`
	FallbackURL string `json:"fallback_url,omitempty"`
	Size        int64  `json:"size,omitempty"`
	SHA1        string `json:"sha1,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Category    string `json:"category,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	VersionID   string `json:"version_id,omitempty"`
	GroupID     string `json:"group_id,omitempty"`
	Replace     bool   `json:"replace,omitempty"`
}

type InstallModResult struct {
	TaskID   string `json:"task_id"`
	Filename string `json:"filename"`
	DestPath string `json:"dest_path"`
}

type Mods struct {
	mgr  *Manager
	dl   *downloads.Manager
	bus  *events.Bus
	http *netx.HTTPClient
}

func NewMods(mgr *Manager, dl *downloads.Manager, bus *events.Bus, http ...*netx.HTTPClient) *Mods {
	var httpClient *netx.HTTPClient
	if len(http) > 0 {
		httpClient = http[0]
	}
	return &Mods{mgr: mgr, dl: dl, bus: bus, http: httpClient}
}

func (m *Mods) List(instanceID string) ([]InstalledModRef, error) {
	inst, err := m.mgr.Get(instanceID)
	if err != nil {
		return nil, err
	}
	return scanInstalledModFiles(filepath.Join(inst.GameDir, "mods")), nil
}

func (m *Mods) SetEnabled(instanceID, filename string, enabled bool) ([]InstalledModRef, error) {
	name, err := sanitizeModFilename(filename, true)
	if err != nil {
		return nil, err
	}
	inst, err := m.mutableInstance(instanceID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(inst.GameDir, "mods")
	source := filepath.Join(dir, name)
	if err := validateModTarget(dir, source); err != nil {
		return nil, err
	}
	info, err := os.Lstat(source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("файл не найден")
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("файл мода не является обычным файлом")
	}
	targetName := name
	if enabled && strings.HasSuffix(name, ".jar.disabled") {
		targetName = strings.TrimSuffix(name, ".disabled")
	} else if !enabled && strings.HasSuffix(name, ".jar") {
		targetName = name + ".disabled"
	}
	if targetName != name {
		target := filepath.Join(dir, targetName)
		if err := validateModTarget(dir, target); err != nil {
			return nil, err
		}
		if _, err := os.Lstat(target); err == nil {
			return nil, fmt.Errorf("файл %q уже существует", targetName)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := os.Rename(source, target); err != nil {
			return nil, err
		}
	}
	if err := m.mgr.RefreshMods(instanceID); err != nil {
		return nil, err
	}
	return m.List(instanceID)
}

func (m *Mods) Delete(instanceID, filename string) ([]InstalledModRef, error) {
	name, err := sanitizeModFilename(filename, true)
	if err != nil {
		return nil, err
	}
	inst, err := m.mutableInstance(instanceID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(inst.GameDir, "mods")
	target := filepath.Join(dir, name)
	if err := validateModTarget(dir, target); err != nil {
		return nil, err
	}
	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("файл не найден")
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("файл мода не является обычным файлом")
	}
	if err := os.Remove(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("файл не найден")
		}
		return nil, err
	}
	if err := m.mgr.RefreshMods(instanceID); err != nil {
		return nil, err
	}
	return m.List(instanceID)
}

func (m *Mods) Install(req InstallModRequest) (InstallModResult, error) {
	category := strings.ToLower(strings.TrimSpace(req.Category))
	if category == "" {
		category = "mod"
	}

	var name string
	var err error
	switch category {
	case "resourcepack", "resource_pack":
		name, err = sanitizePackFilename(req.Filename)
		if err != nil {
			return InstallModResult{}, err
		}
		if !strings.HasSuffix(strings.ToLower(name), ".zip") {
			return InstallModResult{}, fmt.Errorf("ресурспак должен иметь расширение .zip")
		}
	case "shader", "shaderpack", "shader_pack":
		name, err = sanitizePackFilename(req.Filename)
		if err != nil {
			return InstallModResult{}, err
		}
		if !strings.HasSuffix(strings.ToLower(name), ".zip") {
			return InstallModResult{}, fmt.Errorf("шейдерпак должен иметь расширение .zip")
		}
	default:
		category = "mod"
		name, err = sanitizeModFilename(req.Filename, false)
		if err != nil {
			return InstallModResult{}, err
		}
	}

	if strings.TrimSpace(req.URL) == "" {
		return InstallModResult{}, fmt.Errorf("не указан URL загрузки")
	}
	inst, err := m.mutableInstance(req.InstanceID)
	if err != nil {
		return InstallModResult{}, err
	}

	var dir string
	switch category {
	case "resourcepack", "resource_pack":
		dir = filepath.Join(inst.GameDir, "resourcepacks")
	case "shader", "shaderpack", "shader_pack":
		dir = filepath.Join(inst.GameDir, "shaderpacks")
	default:
		dir = filepath.Join(inst.GameDir, "mods")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return InstallModResult{}, err
	}
	target := filepath.Join(dir, name)
	if err := validateModTarget(dir, target); err != nil {
		return InstallModResult{}, err
	}
	disabled := filepath.Join(dir, name+".disabled")
	if err := validateModTarget(dir, disabled); err != nil {
		return InstallModResult{}, err
	}
	for _, candidate := range []string{target, disabled} {
		if info, statErr := os.Lstat(candidate); statErr == nil {
			if !req.Replace {
				return InstallModResult{}, fmt.Errorf("мод с таким именем уже установлен в профиль")
			}
			if !info.Mode().IsRegular() {
				return InstallModResult{}, fmt.Errorf("путь назначения занят не файлом мода")
			}
			if removeErr := os.Remove(candidate); removeErr != nil {
				return InstallModResult{}, removeErr
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return InstallModResult{}, statErr
		}
	}
	if m.dl == nil {
		return InstallModResult{}, fmt.Errorf("менеджер загрузок недоступен")
	}
	groupID := strings.TrimSpace(req.GroupID)
	if groupID == "" {
		groupID = "modinstall:" + req.InstanceID
	}
	checksumAlgo, checksum := downloads.ChecksumNone, ""
	if strings.TrimSpace(req.SHA256) != "" {
		checksumAlgo, checksum = downloads.ChecksumSHA256, strings.TrimSpace(req.SHA256)
	} else if strings.TrimSpace(req.SHA1) != "" {
		checksumAlgo, checksum = downloads.ChecksumSHA1, strings.TrimSpace(req.SHA1)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = name
	}
	tv, err := m.dl.Enqueue(downloads.DownloadRequest{
		Name: title, Category: "mod", URL: strings.TrimSpace(req.URL), DestPath: target,
		ExpectedSize: req.Size, ChecksumAlgo: checksumAlgo, ChecksumValue: checksum, GroupID: groupID,
	})
	if err != nil {
		return InstallModResult{}, err
	}
	go func() {
		_, _ = m.dl.Wait(context.Background(), tv.ID)
		_ = m.mgr.RefreshMods(req.InstanceID)
	}()
	return InstallModResult{TaskID: tv.ID, Filename: name, DestPath: target}, nil
}

func (m *Mods) mutableInstance(instanceID string) (Instance, error) {
	if err := ValidateInstanceID(instanceID); err != nil {
		return Instance{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	inst, err := m.mgr.Get(instanceID)
	if err != nil {
		return Instance{}, err
	}
	if inst.InUse {
		return Instance{}, ErrInstanceBusy
	}
	return inst, nil
}

func sanitizeModFilename(name string, allowDisabled bool) (string, error) {
	if name == "" || len(name) > 200 || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\:`) {
		return "", fmt.Errorf("некорректное имя файла мода")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("некорректное имя файла мода")
		}
	}
	if allowDisabled {
		if !strings.HasSuffix(name, ".jar") && !strings.HasSuffix(name, ".jar.disabled") {
			return "", fmt.Errorf("поддерживаются только файлы .jar и .jar.disabled")
		}
	} else if !strings.HasSuffix(name, ".jar") {
		return "", fmt.Errorf("имя файла должно заканчиваться на .jar")
	}
	return name, nil
}

func validateModTarget(modsDir, target string) error {
	rel, err := filepath.Rel(filepath.Clean(modsDir), filepath.Clean(target))
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("путь файла мода выходит за пределы папки mods")
	}
	return nil
}

func (m *Mods) SetAllEnabled(instanceID string, enabled bool) ([]InstalledModRef, error) {
	inst, err := m.mutableInstance(instanceID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(inst.GameDir, "mods")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []InstalledModRef{}, nil
	}
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		low := strings.ToLower(name)
		if enabled && strings.HasSuffix(low, ".jar.disabled") {
			targetName := strings.TrimSuffix(name, ".disabled")
			_ = os.Rename(filepath.Join(dir, name), filepath.Join(dir, targetName))
		} else if !enabled && strings.HasSuffix(low, ".jar") {
			targetName := name + ".disabled"
			_ = os.Rename(filepath.Join(dir, name), filepath.Join(dir, targetName))
		}
	}
	_ = m.mgr.RefreshMods(instanceID)
	return m.List(instanceID)
}

func (m *Mods) DeleteBatch(instanceID string, filenames []string) ([]InstalledModRef, error) {
	inst, err := m.mutableInstance(instanceID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(inst.GameDir, "mods")
	for _, fn := range filenames {
		name, err := sanitizeModFilename(fn, true)
		if err != nil {
			continue
		}
		target := filepath.Join(dir, name)
		if err := validateModTarget(dir, target); err != nil {
			continue
		}
		_ = os.Remove(target)
	}
	_ = m.mgr.RefreshMods(instanceID)
	return m.List(instanceID)
}

type modrinthVersionFile struct {
	Hashes struct {
		SHA1   string `json:"sha1"`
		SHA512 string `json:"sha512"`
	} `json:"hashes"`
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
	Size     int64  `json:"size"`
}

type modrinthVersion struct {
	ID            string                `json:"id"`
	ProjectID     string                `json:"project_id"`
	Name          string                `json:"name"`
	VersionNumber string                `json:"version_number"`
	VersionType   string                `json:"version_type"`
	Files         []modrinthVersionFile `json:"files"`
}

func (m *Mods) CheckUpdates(instanceID string) ([]ModUpdateInfo, error) {
	inst, err := m.mgr.Get(instanceID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(inst.GameDir, "mods")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []ModUpdateInfo{}, nil
	}

	type modFileHash struct {
		filename string
		sha1     string
	}
	fileHashes := make([]modFileHash, 0, len(entries))
	hashesList := make([]string, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		low := strings.ToLower(name)
		if !strings.HasSuffix(low, ".jar") && !strings.HasSuffix(low, ".jar.disabled") {
			continue
		}
		h, err := fileSHA1(filepath.Join(dir, name))
		if err == nil && len(h) == 40 {
			fileHashes = append(fileHashes, modFileHash{filename: name, sha1: h})
			hashesList = append(hashesList, h)
		}
	}

	if len(hashesList) == 0 {
		return []ModUpdateInfo{}, nil
	}

	client := m.http
	if client == nil {
		client = netx.NewHTTPClient(nil)
	}

	// 1. Query Modrinth for current version information
	matched := make(map[string]modrinthVersion)
	_ = client.PostJSON("https://api.modrinth.com/v2/version_files", map[string]any{
		"hashes":    hashesList,
		"algorithm": "sha1",
	}, nil, &matched)

	// 2. Query Modrinth for updates matching the instance MC version and loader
	loaders := []string{}
	if inst.InstanceType != TypeVanilla && inst.InstanceType != TypeEmpty {
		loaders = append(loaders, strings.ToLower(string(inst.InstanceType)))
	}
	gameVersions := []string{}
	if inst.MCVersion != "" {
		gameVersions = append(gameVersions, inst.MCVersion)
	}

	latest := make(map[string]modrinthVersion)
	if len(gameVersions) > 0 {
		updateReq := map[string]any{
			"hashes":        hashesList,
			"algorithm":     "sha1",
			"game_versions": gameVersions,
		}
		if len(loaders) > 0 {
			updateReq["loaders"] = loaders
		}
		_ = client.PostJSON("https://api.modrinth.com/v2/version_files/update", updateReq, nil, &latest)
	}

	updates := make([]ModUpdateInfo, 0, len(fileHashes))
	for _, fh := range fileHashes {
		curVer, hasCur := matched[fh.sha1]
		latVer, hasLat := latest[fh.sha1]

		info := ModUpdateInfo{
			CurrentFilename: fh.filename,
			CurrentSHA1:     fh.sha1,
		}

		if hasCur {
			info.ProjectID = curVer.ProjectID
			info.CurrentVersion = curVer.VersionNumber
			info.ProjectTitle = curVer.Name
		}

		if hasLat {
			info.NewVersionID = latVer.ID
			info.NewVersionNumber = latVer.VersionNumber
			info.ReleaseType = latVer.VersionType
			if latVer.ProjectID != "" {
				info.ProjectID = latVer.ProjectID
			}

			var bestFile *modrinthVersionFile
			for i := range latVer.Files {
				f := &latVer.Files[i]
				if f.Primary || bestFile == nil {
					bestFile = f
				}
			}
			if bestFile != nil {
				info.NewFilename = bestFile.Filename
				info.NewDownloadURL = bestFile.URL
				info.NewSize = bestFile.Size
				info.NewSHA1 = bestFile.Hashes.SHA1
			}

			if bestFile != nil && bestFile.URL != "" {
				if hasCur && latVer.ID != curVer.ID {
					info.HasUpdate = true
				} else if !hasCur && strings.ToLower(bestFile.Filename) != strings.ToLower(fh.filename) {
					info.HasUpdate = true
				}
			}
		}

		updates = append(updates, info)
	}

	sort.Slice(updates, func(i, j int) bool {
		if updates[i].HasUpdate != updates[j].HasUpdate {
			return updates[i].HasUpdate
		}
		return strings.ToLower(updates[i].CurrentFilename) < strings.ToLower(updates[j].CurrentFilename)
	})

	return updates, nil
}

func (m *Mods) ApplyUpdate(req ApplyModUpdateRequest) (InstallModResult, error) {
	if strings.TrimSpace(req.NewURL) == "" || strings.TrimSpace(req.NewFilename) == "" {
		return InstallModResult{}, fmt.Errorf("не указаны параметры новой версии мода")
	}
	inst, err := m.mutableInstance(req.InstanceID)
	if err != nil {
		return InstallModResult{}, err
	}
	dir := filepath.Join(inst.GameDir, "mods")

	cleanNewName, err := sanitizeModFilename(req.NewFilename, false)
	if err != nil {
		return InstallModResult{}, err
	}

	target := filepath.Join(dir, cleanNewName)
	if err := validateModTarget(dir, target); err != nil {
		return InstallModResult{}, err
	}

	if m.dl == nil {
		return InstallModResult{}, fmt.Errorf("менеджер загрузок недоступен")
	}

	checksumAlgo, checksum := downloads.ChecksumNone, ""
	if strings.TrimSpace(req.NewSHA1) != "" {
		checksumAlgo, checksum = downloads.ChecksumSHA1, strings.TrimSpace(req.NewSHA1)
	}

	groupID := fmt.Sprintf("modupdate:%s:%s", req.InstanceID, cleanNewName)
	tv, err := m.dl.Enqueue(downloads.DownloadRequest{
		Name:          cleanNewName,
		Category:      "mod",
		URL:           strings.TrimSpace(req.NewURL),
		DestPath:      target,
		ExpectedSize:  req.NewSize,
		ChecksumAlgo:  checksumAlgo,
		ChecksumValue: checksum,
		GroupID:       groupID,
	})
	if err != nil {
		return InstallModResult{}, err
	}

	oldFilename := strings.TrimSpace(req.CurrentFilename)
	go func() {
		task, _ := m.dl.Wait(context.Background(), tv.ID)
		if task.Status == downloads.StatusCompleted {
			if oldFilename != "" && !strings.EqualFold(oldFilename, cleanNewName) {
				cleanOld, err := sanitizeModFilename(oldFilename, true)
				if err == nil {
					oldTarget := filepath.Join(dir, cleanOld)
					if validateModTarget(dir, oldTarget) == nil {
						_ = os.Remove(oldTarget)
					}
				}
			}
			_ = m.mgr.RefreshMods(req.InstanceID)
		}
	}()

	return InstallModResult{TaskID: tv.ID, Filename: cleanNewName, DestPath: target}, nil
}

func fileSHA1(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
