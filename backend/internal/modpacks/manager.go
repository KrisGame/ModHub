package modpacks

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"modhub/internal/domain"
	"modhub/internal/downloads"
	"modhub/internal/events"
	"modhub/internal/instances"
	"modhub/internal/modloader"
	"modhub/internal/netx"
	"modhub/internal/security"
)

var (
	ErrModpackNotFound = errors.New("сборка не найдена")
	ErrPreviewNotFound = errors.New("предварительный просмотр импорта устарел или не найден — повторите анализ")
	ErrNoUpdateSource  = errors.New("у этой сборки нет подключённого онлайн-источника обновлений (укажите новый файл архива вручную)")
	reMRModpackURL     = regexp.MustCompile(`(?i)modrinth\.com/modpack/([a-zA-Z0-9._-]+)`)
	reCFModpackURL     = regexp.MustCompile(`(?i)curseforge\.com/minecraft/modpacks/([a-zA-Z0-9._-]+)`)
)

type CFKeyProvider func() string

type cachedPreview struct {
	preview   ImportPreview
	createdAt time.Time
}

type cachedUpdatePlan struct {
	diff            UpdateDiffPreview
	newPrev         ImportPreview
	oldManifestHash string
	createdAt       time.Time
}

type Manager struct {
	cacheDir   string
	httpClient *netx.HTTPClient
	dlMgr      *downloads.Manager
	instMgr    *instances.Manager
	mlMgr      *modloader.Manager
	store      Store
	bus        *events.Bus
	cfKeyFn    CFKeyProvider
	packLookup instances.PackLookup

	mu          sync.Mutex
	applyMu     sync.Mutex
	limits      ArchiveLimits
	previews    map[string]cachedPreview
	updatePlans map[string]cachedUpdatePlan
	activeProg  *ImportProgress
	closeOnce   sync.Once
	wg          sync.WaitGroup
}

func NewManager(cacheDir string, httpClient *netx.HTTPClient, dlMgr *downloads.Manager, instMgr *instances.Manager, mlMgr *modloader.Manager, store Store, bus *events.Bus, cfKeyFn CFKeyProvider, packLookup instances.PackLookup) *Manager {
	m := &Manager{
		cacheDir: filepath.Clean(cacheDir), httpClient: httpClient, dlMgr: dlMgr, instMgr: instMgr, mlMgr: mlMgr,
		store: store, bus: bus, cfKeyFn: cfKeyFn, packLookup: packLookup, limits: DefaultArchiveLimits(),
		previews: make(map[string]cachedPreview), updatePlans: make(map[string]cachedUpdatePlan),
	}
	_ = os.MkdirAll(m.cacheDir, 0o755)
	return m
}

func (m *Manager) SetArchiveLimits(lim ArchiveLimits) { m.mu.Lock(); m.limits = lim; m.mu.Unlock() }
func (m *Manager) RemoveExport(targetPath string) error {
	exportDir := filepath.Join(filepath.Dir(m.cacheDir), "exports")
	rel, err := filepath.Rel(exportDir, targetPath)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("путь выходит за каталог экспорта")
	}
	if resolved, e := filepath.EvalSymlinks(targetPath); e == nil {
		base, be := filepath.EvalSymlinks(exportDir)
		if be == nil {
			rel, err = filepath.Rel(base, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("путь выходит за каталог экспорта")
			}
		}
	}
	return os.Remove(targetPath)
}

func (m *Manager) Close() {
	if m != nil {
		m.closeOnce.Do(func() { m.wg.Wait() })
	}
}

func (m *Manager) GetActiveProgress() *ImportProgress {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeProg == nil {
		return nil
	}
	cp := *m.activeProg
	return &cp
}

func genID(prefix string) string {
	var b [5]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(b[:]))
}

func (m *Manager) findSavedPack(id string) *domain.SavedPack {
	if m.packLookup == nil || strings.TrimSpace(id) == "" {
		return nil
	}
	for _, p := range m.packLookup() {
		if p.ID == id {
			cp := p
			return &cp
		}
	}
	return nil
}

func (m *Manager) ListModpacks() ([]ModpackView, error) {
	if m.store == nil {
		return []ModpackView{}, nil
	}
	recs, err := m.store.ListModpacks()
	if err != nil {
		return nil, err
	}
	statsMap, _ := m.store.ListModpackFileStats()
	updMap, _ := m.store.ListRecentModpackUpdatesMap(20)
	out := make([]ModpackView, 0, len(recs))
	for _, r := range recs {
		st := statsMap[r.ID]
		out = append(out, ModpackView{
			ID: r.ID, Name: r.Name, Author: r.Author, Description: r.Description, Version: r.Version,
			Format: ModpackFormat(r.Format), SourceType: r.SourceType, SourceRef: r.SourceRef,
			SourceProjectID: r.SourceProjectID, SourceVersionID: r.SourceVersionID,
			MCVersion: r.MCVersion, Loader: r.Loader, LoaderVersion: r.LoaderVersion, InstanceID: r.InstanceID,
			Status: r.Status, LastError: r.LastError, FilesCount: st[0], ManagedCount: st[1],
			CanCheckRemote: (r.SourceType == "modrinth" || r.SourceType == "curseforge") && strings.TrimSpace(r.SourceProjectID) != "",
			CreatedAt:      r.CreatedAt, UpdatedAt: r.UpdatedAt, InstalledAt: r.InstalledAt, UpdateHistory: updMap[r.ID],
		})
	}
	return out, nil
}

func (m *Manager) DeleteModpackRecord(id string) error {
	if m.store == nil || strings.TrimSpace(id) == "" {
		return ErrModpackNotFound
	}
	if err := m.store.DeleteModpack(id); err != nil {
		return err
	}
	if m.bus != nil {
		m.bus.Publish(events.EventModpackDeleted, map[string]any{"id": id})
	}
	return nil
}

func (m *Manager) recordToView(r domain.ModpackRecord) ModpackView {
	var filesCount, managedCount int
	var hist []domain.ModpackUpdateRecord
	if m.store != nil {
		if files, err := m.store.ListModpackFiles(r.ID); err == nil {
			filesCount = len(files)
			for _, f := range files {
				if f.Managed {
					managedCount++
				}
			}
		}
		hist, _ = m.store.ListModpackUpdates(r.ID)
	}
	return ModpackView{
		ID: r.ID, Name: r.Name, Author: r.Author, Description: r.Description, Version: r.Version,
		Format: ModpackFormat(r.Format), SourceType: r.SourceType, SourceRef: r.SourceRef,
		SourceProjectID: r.SourceProjectID, SourceVersionID: r.SourceVersionID,
		MCVersion: r.MCVersion, Loader: r.Loader, LoaderVersion: r.LoaderVersion, InstanceID: r.InstanceID,
		Status: r.Status, LastError: r.LastError, FilesCount: filesCount, ManagedCount: managedCount,
		CanCheckRemote: (r.SourceType == "modrinth" || r.SourceType == "curseforge") && strings.TrimSpace(r.SourceProjectID) != "",
		CreatedAt:      r.CreatedAt, UpdatedAt: r.UpdatedAt, InstalledAt: r.InstalledAt, UpdateHistory: hist,
	}
}

func (m *Manager) PreviewImport(ctx context.Context, req PreviewImportRequest) (ImportPreview, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	srcType, srcRef := strings.ToLower(strings.TrimSpace(req.SourceType)), strings.TrimSpace(req.SourceRef)
	if srcRef == "" {
		return ImportPreview{}, errors.New("не указан источник сборки (файл, URL или идентификатор)")
	}
	if srcType == "url" {
		if mm := reMRModpackURL.FindStringSubmatch(srcRef); len(mm) == 2 {
			srcType, srcRef = "modrinth", mm[1]
		} else if cm := reCFModpackURL.FindStringSubmatch(srcRef); len(cm) == 2 {
			srcType, srcRef = "curseforge", cm[1]
		}
	}

	var prev ImportPreview
	var err error
	switch srcType {
	case "file", "":
		absPath, aerr := filepath.Abs(srcRef)
		if aerr != nil {
			return ImportPreview{}, aerr
		}
		prev, err = m.ParseArchivePreview(ctx, absPath, "file", absPath, "", "")
	case "url":
		dlPath, derr := m.downloadArchiveViaManager(ctx, srcRef, "url-modpack", "", "")
		if derr != nil {
			return ImportPreview{}, derr
		}
		prev, err = m.ParseArchivePreview(ctx, dlPath, "url", srcRef, "", "")
	case "modrinth":
		archivePath, projID, verID, rerr := m.fetchModrinthModpackArchive(ctx, srcRef, req.VersionID)
		if rerr != nil {
			return ImportPreview{}, rerr
		}
		prev, err = m.ParseArchivePreview(ctx, archivePath, "modrinth", srcRef, projID, verID)
	case "curseforge":
		archivePath, projID, fileID, rerr := m.fetchCurseForgeModpackArchive(ctx, srcRef, req.VersionID)
		if rerr != nil {
			return ImportPreview{}, rerr
		}
		prev, err = m.ParseArchivePreview(ctx, archivePath, "curseforge", srcRef, projID, fileID)
	case "pack":
		pk := m.findSavedPack(srcRef)
		if pk == nil {
			return ImportPreview{}, fmt.Errorf("сборка Packs %q не найдена", srcRef)
		}
		prev, err = m.buildPreviewFromSavedPack(*pk)
	case "instance":
		if m.instMgr == nil {
			return ImportPreview{}, errors.New("Instance Manager недоступен")
		}
		inst, ierr := m.instMgr.Get(srcRef)
		if ierr != nil {
			return ImportPreview{}, ierr
		}
		prev, err = m.buildPreviewFromInstance(inst)
	default:
		return ImportPreview{}, fmt.Errorf("неизвестный тип источника %q", req.SourceType)
	}
	if err != nil {
		return ImportPreview{}, err
	}

	prev.PreviewID = genID("prev")
	m.mu.Lock()
	cut := time.Now().Add(-2 * time.Hour)
	for k, cp := range m.previews {
		if cp.createdAt.Before(cut) {
			delete(m.previews, k)
		}
	}
	m.previews[prev.PreviewID] = cachedPreview{preview: prev, createdAt: time.Now()}
	m.mu.Unlock()
	return prev, nil
}

func (m *Manager) downloadArchiveViaManager(ctx context.Context, rawURL, label, expectedSHA1, groupID string) (string, error) {
	if m.dlMgr == nil {
		return "", errors.New("Download Manager не инициализирован")
	}
	if _, err := m.httpClient.ValidateLauncherURL(rawURL); err != nil {
		return "", fmt.Errorf("URL архива отклонён политикой безопасности: %w", err)
	}
	ext := filepath.Ext(rawURL)
	if q := strings.IndexByte(ext, '?'); q >= 0 {
		ext = ext[:q]
	}
	if ext == "" || len(ext) > 10 {
		ext = ".zip"
	}
	destDir := filepath.Join(m.cacheDir, "archives")
	_ = os.MkdirAll(destDir, 0o755)
	destPath := filepath.Join(destDir, genID("arch")+ext)
	algo := downloads.ChecksumNone
	if strings.TrimSpace(expectedSHA1) != "" {
		algo = downloads.ChecksumSHA1
	}
	tv, err := m.dlMgr.Enqueue(downloads.DownloadRequest{Name: label, Category: "modpack", URL: rawURL, DestPath: destPath, ChecksumAlgo: algo, ChecksumValue: expectedSHA1, GroupID: groupID})
	if err != nil {
		return "", err
	}
	fin, err := m.dlMgr.Wait(ctx, tv.ID)
	if err != nil {
		return "", err
	}
	if fin.Status != downloads.StatusCompleted {
		return "", fmt.Errorf("не удалось скачать архив сборки (%s): %s", fin.Status, fin.Error)
	}
	return destPath, nil
}

func (m *Manager) fetchModrinthModpackArchive(ctx context.Context, slugOrID, targetVerID string) (string, string, string, error) {
	idOrSlug := strings.TrimSpace(slugOrID)
	if mm := reMRModpackURL.FindStringSubmatch(idOrSlug); len(mm) == 2 {
		idOrSlug = mm[1]
	}
	hc := m.httpClient.WithContext(ctx)
	var proj struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := hc.GetJSON(fmt.Sprintf("%s/project/%s", domain.ModrinthAPI, url.PathEscape(idOrSlug)), nil, 180, &proj); err != nil || proj.ID == "" {
		return "", "", "", fmt.Errorf("модпак Modrinth %q не найден", idOrSlug)
	}
	var versions []struct {
		ID            string `json:"id"`
		VersionNumber string `json:"version_number"`
		Files         []struct {
			URL      string            `json:"url"`
			Filename string            `json:"filename"`
			Primary  bool              `json:"primary"`
			Hashes   map[string]string `json:"hashes"`
		} `json:"files"`
	}
	if err := hc.GetJSON(fmt.Sprintf("%s/project/%s/version", domain.ModrinthAPI, url.PathEscape(proj.ID)), nil, 120, &versions); err != nil || len(versions) == 0 {
		return "", "", "", fmt.Errorf("не найдены версии модпака Modrinth %s", proj.Title)
	}
	chosen := versions[0]
	if strings.TrimSpace(targetVerID) != "" {
		for _, v := range versions {
			if v.ID == targetVerID || v.VersionNumber == targetVerID {
				chosen = v
				break
			}
		}
	}
	if len(chosen.Files) == 0 {
		return "", "", "", fmt.Errorf("версия %s модпака Modrinth не содержит файлов", chosen.VersionNumber)
	}
	f := chosen.Files[0]
	for _, cand := range chosen.Files {
		if cand.Primary || strings.HasSuffix(strings.ToLower(cand.Filename), ".mrpack") {
			f = cand
			break
		}
	}
	dlPath, err := m.downloadArchiveViaManager(ctx, f.URL, fmt.Sprintf("%s (%s)", proj.Title, chosen.VersionNumber), f.Hashes["sha1"], "mp:mr:"+proj.ID)
	if err != nil {
		return "", "", "", err
	}
	return dlPath, proj.ID, chosen.ID, nil
}

func (m *Manager) fetchCurseForgeModpackArchive(ctx context.Context, slugOrID, targetFileID string) (string, string, string, error) {
	cfKey := ""
	if m.cfKeyFn != nil {
		cfKey = security.SanitizeCFKey(m.cfKeyFn())
	}
	if cfKey == "" {
		return "", "", "", errors.New("для импорта модпаков с CurseForge укажите API-ключ CurseForge в Настройках")
	}
	ref := strings.TrimSpace(slugOrID)
	if cm := reCFModpackURL.FindStringSubmatch(ref); len(cm) == 2 {
		ref = cm[1]
	}
	hc := m.httpClient.WithContext(ctx)
	hdr := map[string]string{"x-api-key": cfKey}
	modID := ref
	if _, err := strconv.Atoi(modID); err != nil {
		var searchResp struct {
			Data []struct {
				ID int `json:"id"`
			} `json:"data"`
		}
		q := url.Values{}
		q.Set("gameId", strconv.Itoa(domain.CurseForgeGameID))
		q.Set("classId", "4471")
		q.Set("slug", ref)
		if err := hc.GetJSON(domain.CurseForgeAPI+"/mods/search?"+q.Encode(), hdr, 180, &searchResp); err != nil || len(searchResp.Data) == 0 {
			return "", "", "", fmt.Errorf("модпак CurseForge %q не найден", ref)
		}
		modID = strconv.Itoa(searchResp.Data[0].ID)
	}
	var filesResp struct {
		Data []cfBatchFileItem `json:"data"`
	}
	if err := hc.GetJSON(fmt.Sprintf("%s/mods/%s/files?pageSize=20", domain.CurseForgeAPI, url.PathEscape(modID)), hdr, 120, &filesResp); err != nil || len(filesResp.Data) == 0 {
		return "", "", "", fmt.Errorf("не найдены файлы модпака CurseForge #%s", modID)
	}
	chosen := filesResp.Data[0]
	if strings.TrimSpace(targetFileID) != "" {
		for _, f := range filesResp.Data {
			if strconv.Itoa(f.ID) == targetFileID {
				chosen = f
				break
			}
		}
	}
	if strings.TrimSpace(chosen.DownloadURL) == "" {
		return "", "", "", fmt.Errorf("архив модпака CurseForge #%s (%s) недоступен для автоматического скачивания через API (ограничение автора)", modID, chosen.FileName)
	}
	sha1Val := ""
	for _, h := range chosen.Hashes {
		if h.Algo == 1 {
			sha1Val = h.Value
			break
		}
	}
	dlPath, err := m.downloadArchiveViaManager(ctx, chosen.DownloadURL, fallbackStr(chosen.DisplayName, chosen.FileName), sha1Val, "mp:cf:"+modID)
	if err != nil {
		return "", "", "", err
	}
	return dlPath, modID, strconv.Itoa(chosen.ID), nil
}

func (m *Manager) ExecuteImportAsync(req ExecuteImportRequest) error {
	m.mu.Lock()
	cp, ok := m.previews[req.PreviewID]
	m.mu.Unlock()
	if !ok || time.Since(cp.createdAt) > 2*time.Hour {
		return ErrPreviewNotFound
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		_, _ = m.ExecuteImportSync(context.Background(), req)
	}()
	return nil
}

func (m *Manager) ExecuteImportSync(ctx context.Context, req ExecuteImportRequest) (ImportResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	cp, ok := m.previews[req.PreviewID]
	m.mu.Unlock()
	if !ok || time.Since(cp.createdAt) > 2*time.Hour {
		return ImportResult{}, ErrPreviewNotFound
	}
	prev := cp.preview
	profName := fallbackStr(req.ProfileName, prev.Name)
	modpackID := genID("mp")
	groupID := "modpack:" + modpackID
	conflictPolicy := req.ConflictPolicy
	if conflictPolicy == "" {
		conflictPolicy = ConflictKeepExisting
	}

	emit := func(stage, msg string, done, total int, pct float64, errStr, instID string) {
		p := ImportProgress{ModpackID: modpackID, InstanceID: instID, Name: profName, Stage: stage, Message: msg, CompletedFiles: done, TotalFiles: total, Progress: pct, Error: errStr, UpdatedAt: time.Now().Unix()}
		m.mu.Lock()
		if stage == "completed" || stage == "partial" || stage == "error" {
			m.activeProg = nil
		} else {
			m.activeProg = &p
		}
		m.mu.Unlock()
		if m.bus != nil {
			switch stage {
			case "error":
				m.bus.Publish(events.EventModpackImportError, p)
			case "completed", "partial":
				m.bus.Publish(events.EventModpackImportCompleted, p)
			default:
				m.bus.Publish(events.EventModpackImportProgress, p)
			}
		}
	}

	if len(prev.UnavailableFiles) > 0 && !req.SkipUnavailable {
		err := fmt.Errorf("сборка содержит недоступные файлы (%d шт.); подтвердите импорт с пропуском недоступных или проверьте настройки API", len(prev.UnavailableFiles))
		emit("error", err.Error(), 0, 0, -1, err.Error(), strings.TrimSpace(req.TargetInstanceID))
		return ImportResult{}, err
	}

	if strings.TrimSpace(req.TargetInstanceID) != "" {
		emit("validating_target", "Проверка целевого профиля...", 0, len(prev.Files), -1, "", req.TargetInstanceID)
	}
	emit("creating_profile", "Создание изолированного игрового профиля...", 0, len(prev.Files), -1, "", "")
	var inst instances.Instance
	var err error
	installMinecraft := req.InstallMinecraft && strings.TrimSpace(req.TargetInstanceID) == ""
	if strings.TrimSpace(req.TargetInstanceID) != "" {
		inst, err = m.instMgr.Get(req.TargetInstanceID)
		if err == nil && inst.InUse {
			err = instances.ErrInstanceBusy
		}
	} else {
		createReq := instances.CreateInstanceRequest{
			Name: profName, Description: fallbackStr(req.Description, prev.Description), Icon: req.Icon,
			MCVersion: prev.MCVersion, InstanceType: instances.InstanceType(fallbackStr(prev.Loader, "vanilla")),
			LoaderVersion: prev.LoaderVersion, CustomDir: req.CustomDir, MemoryMaxMB: req.MemoryMaxMB,
		}
		if prev.ProfileSettings != nil {
			if createReq.MemoryMaxMB == 0 {
				createReq.MemoryMaxMB = prev.ProfileSettings.MemoryMaxMB
			}
			createReq.MemoryMinMB = prev.ProfileSettings.MemoryMinMB
			createReq.LaunchSettings = instances.LaunchSettings{
				JVMArgs: prev.ProfileSettings.JVMArgs, WindowWidth: prev.ProfileSettings.WindowWidth,
				WindowHeight: prev.ProfileSettings.WindowHeight, Fullscreen: prev.ProfileSettings.Fullscreen,
			}
		}
		inst, err = m.instMgr.Create(createReq)
	}
	if err != nil {
		emit("error", "Не удалось создать профиль: "+err.Error(), 0, 0, -1, err.Error(), strings.TrimSpace(req.TargetInstanceID))
		return ImportResult{}, err
	}

	var warnings []string
	if installMinecraft && prev.MCVersion != "" && m.mlMgr != nil {
		emit("installing_mc", fmt.Sprintf("Установка Minecraft %s (%s)...", prev.MCVersion, fallbackStr(prev.Loader, "vanilla")), 0, len(prev.Files), -1, "", inst.ID)
		if _, merr := m.mlMgr.InstallForInstanceSync(ctx, modloader.InstallProfileVersionRequest{
			InstanceID: inst.ID, MCVersion: prev.MCVersion, Loader: modloader.LoaderType(fallbackStr(prev.Loader, "vanilla")), LoaderVersion: prev.LoaderVersion,
		}); merr != nil {
			warnings = append(warnings, "Не удалось автоматически установить Minecraft/загрузчик (вы можете установить во вкладке «Версия и загрузчик»): "+merr.Error())
		}
	}

	optSelected := map[string]bool{}
	for _, p := range req.SelectedOptional {
		optSelected[strings.ToLower(strings.TrimSpace(p))] = true
	}
	var toDownload, toExtract, unavailable []ModpackFilePlan
	skippedOptional := 0
	for _, f := range prev.Files {
		if f.Optional && !optSelected[strings.ToLower(f.RelPath)] {
			skippedOptional++
			continue
		}
		if !f.Available {
			unavailable = append(unavailable, f)
			continue
		}
		if f.SourceKind == "download" {
			toDownload = append(toDownload, f)
		} else {
			toExtract = append(toExtract, f)
		}
	}

	totalOps := len(toDownload) + len(toExtract)
	doneOps, downloadedCount, appliedOverrides := 0, 0, 0
	var failedFiles []string
	now := time.Now().Unix()
	fileRecs := make([]domain.ModpackFileRecord, 0, totalOps)

	if len(toDownload) > 0 && m.dlMgr != nil {
		const batchSize = 20
		for start := 0; start < len(toDownload); start += batchSize {
			end := start + batchSize
			if end > len(toDownload) {
				end = len(toDownload)
			}
			type qJob struct {
				plan     ModpackFilePlan
				destPath string
				taskID   string
			}
			jobs := make([]qJob, 0, end-start)
			for _, f := range toDownload[start:end] {
				destPath, derr := ResolveSafeDestPath(inst.GameDir, f.RelPath)
				if derr != nil {
					failedFiles = append(failedFiles, fmt.Sprintf("%s: %v", f.RelPath, derr))
					continue
				}
				if !shouldWriteTarget(destPath, f.SHA1, f.SHA256, conflictPolicy) {
					doneOps++
					continue
				}
				_ = backupIfNeeded(destPath, conflictPolicy)
				algo, chkVal := downloads.ChecksumNone, ""
				if f.SHA256 != "" {
					algo, chkVal = downloads.ChecksumSHA256, f.SHA256
				} else if f.SHA1 != "" {
					algo, chkVal = downloads.ChecksumSHA1, f.SHA1
				}
				tv, qerr := m.dlMgr.Enqueue(downloads.DownloadRequest{
					Name: fallbackStr(f.Title, f.Filename), Category: string(f.Category), URL: f.DownloadURLs[0],
					DestPath: destPath, ExpectedSize: f.Size, ChecksumAlgo: algo, ChecksumValue: chkVal, GroupID: groupID,
				})
				if qerr != nil {
					failedFiles = append(failedFiles, fmt.Sprintf("%s: %v", f.RelPath, qerr))
					continue
				}
				jobs = append(jobs, qJob{plan: f, destPath: destPath, taskID: tv.ID})
			}
			for _, jb := range jobs {
				fin, werr := m.dlMgr.Wait(ctx, jb.taskID)
				doneOps++
				emit("downloading", fmt.Sprintf("Загружено %d из %d: %s", doneOps, totalOps, jb.plan.Filename), doneOps, totalOps, float64(doneOps)*100/float64(max(totalOps, 1)), "", inst.ID)
				if werr != nil || fin.Status != downloads.StatusCompleted {
					msg := fin.Error
					if werr != nil {
						msg = werr.Error()
					}
					failedFiles = append(failedFiles, fmt.Sprintf("%s: %s", jb.plan.RelPath, msg))
					continue
				}
				hashes, herr := ComputeFileHashes(jb.destPath)
				if herr == nil && jb.plan.SHA512 != "" && !strings.EqualFold(hashes.SHA512, jb.plan.SHA512) {
					_ = os.Remove(jb.destPath)
					failedFiles = append(failedFiles, fmt.Sprintf("%s: несовпадение контрольной суммы SHA-512", jb.plan.RelPath))
					continue
				}
				downloadedCount++
				primaryURL := ""
				if len(jb.plan.DownloadURLs) > 0 {
					primaryURL = jb.plan.DownloadURLs[0]
				}
				fileRecs = append(fileRecs, domain.ModpackFileRecord{
					ModpackID: modpackID, InstanceID: inst.ID, RelPath: jb.plan.RelPath, Category: string(jb.plan.Category),
					SourcePlatform: jb.plan.SourcePlatform, SourceProjectID: jb.plan.SourceProjectID, SourceFileID: jb.plan.SourceFileID,
					SHA1: hashes.SHA1, SHA256: hashes.SHA256, SHA512: hashes.SHA512, Size: hashes.Size,
					Managed: true, Optional: jb.plan.Optional, Enabled: true, DownloadURL: primaryURL, UpdatedAt: now,
				})
			}
		}
	}

	if len(toExtract) > 0 && prev.ResolvedArchivePath != "" {
		if zr, zerr := zip.OpenReader(prev.ResolvedArchivePath); zerr != nil {
			failedFiles = append(failedFiles, "не удалось повторно открыть архив для извлечения overrides: "+zerr.Error())
		} else {
			entryMap := map[string]*zip.File{}
			for _, zf := range zr.File {
				entryMap[zf.Name] = zf
			}
			for _, f := range toExtract {
				doneOps++
				emit("applying_overrides", fmt.Sprintf("Применение файлов %d из %d: %s", doneOps, totalOps, f.Filename), doneOps, totalOps, float64(doneOps)*100/float64(max(totalOps, 1)), "", inst.ID)
				zf, exists := entryMap[f.ArchiveEntry]
				if !exists {
					failedFiles = append(failedFiles, fmt.Sprintf("%s: запись %q не найдена в архиве", f.RelPath, f.ArchiveEntry))
					continue
				}
				destPath, derr := ResolveSafeDestPath(inst.GameDir, f.RelPath)
				if derr != nil {
					failedFiles = append(failedFiles, fmt.Sprintf("%s: %v", f.RelPath, derr))
					continue
				}
				if !shouldWriteTarget(destPath, f.SHA1, f.SHA256, conflictPolicy) {
					continue
				}
				_ = backupIfNeeded(destPath, conflictPolicy)
				if err := extractZipFileBounded(zf, destPath, DefaultMaxSingleEntryBytes); err != nil {
					failedFiles = append(failedFiles, fmt.Sprintf("%s: %v", f.RelPath, err))
					continue
				}
				hashes, _ := ComputeFileHashes(destPath)
				appliedOverrides++
				fileRecs = append(fileRecs, domain.ModpackFileRecord{
					ModpackID: modpackID, InstanceID: inst.ID, RelPath: f.RelPath, Category: string(f.Category),
					SourcePlatform: f.SourcePlatform, SourceProjectID: f.SourceProjectID, SourceFileID: f.SourceFileID,
					SHA1: hashes.SHA1, SHA256: hashes.SHA256, SHA512: hashes.SHA512, Size: hashes.Size,
					Managed: true, Optional: f.Optional, Enabled: true, UpdatedAt: now,
				})
			}
			_ = zr.Close()
		}
	}

	emit("verifying", "Проверка целостности созданного профиля...", totalOps, totalOps, 98, "", inst.ID)
	status, lastErr := "installed", ""
	if len(failedFiles) > 0 || len(unavailable) > 0 {
		status = "partial"
		if len(failedFiles) > 0 {
			lastErr = strings.Join(failedFiles[:min(len(failedFiles), 3)], "; ")
		} else {
			lastErr = fmt.Sprintf("Пропущено недоступных файлов: %d", len(unavailable))
		}
	}
	mpRec := domain.ModpackRecord{
		ID: modpackID, Name: profName, Author: prev.Author, Description: fallbackStr(req.Description, prev.Description),
		Version: fallbackStr(prev.Version, "1.0.0"), Format: string(prev.Format), SourceType: prev.SourceType,
		SourceRef: prev.SourceRef, SourceProjectID: prev.SourceProjectID, SourceVersionID: prev.SourceVersionID,
		MCVersion: prev.MCVersion, Loader: prev.Loader, LoaderVersion: prev.LoaderVersion, InstanceID: inst.ID,
		ManifestHash: prev.ManifestHash, Status: status, LastError: lastErr, CreatedAt: now, UpdatedAt: now, InstalledAt: now,
	}
	if m.store != nil {
		_ = m.store.UpsertModpack(mpRec)
		_ = m.store.ReplaceModpackFiles(modpackID, fileRecs)
	}
	if m.instMgr != nil {
		_, _ = m.instMgr.Verify(inst.ID)
	}
	view := m.recordToView(mpRec)
	stage := "completed"
	if status == "partial" {
		stage = "partial"
	}
	emit(stage, fmt.Sprintf("Импорт «%s» завершён (загружено: %d, overrides: %d)", profName, downloadedCount, appliedOverrides), totalOps, totalOps, 100, lastErr, inst.ID)
	if m.bus != nil {
		m.bus.Publish(events.EventModpackUpdated, view)
	}
	return ImportResult{Modpack: view, InstanceID: inst.ID, DownloadedFiles: downloadedCount, AppliedOverrides: appliedOverrides, SkippedOptional: skippedOptional, Unavailable: unavailable, FailedFiles: failedFiles, Warnings: warnings, Status: status}, nil
}

func (m *Manager) PreviewUpdate(ctx context.Context, modpackID, newArchivePath string) (UpdateDiffPreview, error) {
	if m.store == nil {
		return UpdateDiffPreview{}, ErrModpackNotFound
	}
	recs, err := m.store.ListModpacks()
	if err != nil {
		return UpdateDiffPreview{}, err
	}
	var target *domain.ModpackRecord
	for i := range recs {
		if recs[i].ID == modpackID {
			cp := recs[i]
			target = &cp
			break
		}
	}
	if target == nil {
		return UpdateDiffPreview{}, ErrModpackNotFound
	}

	var newPrev ImportPreview
	if strings.TrimSpace(newArchivePath) != "" {
		newPrev, err = m.PreviewImport(ctx, PreviewImportRequest{SourceType: "file", SourceRef: newArchivePath})
	} else if target.SourceType == "modrinth" && target.SourceProjectID != "" {
		newPrev, err = m.PreviewImport(ctx, PreviewImportRequest{SourceType: "modrinth", SourceRef: target.SourceProjectID})
	} else if target.SourceType == "curseforge" && target.SourceProjectID != "" {
		newPrev, err = m.PreviewImport(ctx, PreviewImportRequest{SourceType: "curseforge", SourceRef: target.SourceProjectID})
	} else {
		return UpdateDiffPreview{
			ModpackID: target.ID, CurrentVersion: target.Version, HasUpdate: false, SourceSupported: false,
			SourceNote: "Сборка была импортирована из локального файла без онлайн-источника обновлений. Выберите новый файл архива (.mrpack / .zip / .modhub) для обновления.",
		}, nil
	}
	if err != nil {
		return UpdateDiffPreview{}, err
	}

	oldFiles, _ := m.store.ListModpackFiles(target.ID)
	oldByRel := map[string]domain.ModpackFileRecord{}
	for _, f := range oldFiles {
		oldByRel[strings.ToLower(f.RelPath)] = f
	}
	var gameDir string
	if m.instMgr != nil && target.InstanceID != "" {
		if inst, ierr := m.instMgr.Get(target.InstanceID); ierr == nil {
			gameDir = inst.GameDir
		}
	}

	diff := UpdateDiffPreview{
		ModpackID: target.ID, PreviewID: newPrev.PreviewID, CurrentVersion: target.Version,
		NewVersion: fallbackStr(newPrev.Version, target.Version), SourceSupported: true,
		Added: []UpdateDiffEntry{}, Updated: []UpdateDiffEntry{}, Removed: []UpdateDiffEntry{},
		ConfigConflicts: []UpdateDiffEntry{}, PreservedUser: []UpdateDiffEntry{},
		UnavailableFiles: newPrev.UnavailableFiles, Warnings: newPrev.Warnings,
	}
	newByRel := map[string]ModpackFilePlan{}
	for _, nf := range newPrev.Files {
		if nf.Optional && !nf.SelectedByDefault {
			continue
		}
		low := strings.ToLower(nf.RelPath)
		newByRel[low] = nf
		newHash := firstNonEmpty(nf.SHA256, nf.SHA1)
		oldRec, existed := oldByRel[low]
		if !existed {
			if gameDir != "" {
				if full, rerr := ResolveSafeDestPath(gameDir, nf.RelPath); rerr == nil {
					if diskH, herr := ComputeFileHashes(full); herr == nil {
						if newHash != "" && (strings.EqualFold(diskH.SHA256, nf.SHA256) || strings.EqualFold(diskH.SHA1, nf.SHA1)) {
							continue
						}
						diff.ConfigConflicts = append(diff.ConfigConflicts, UpdateDiffEntry{RelPath: nf.RelPath, Title: nf.Title, Category: nf.Category, Action: "config_conflict", OldHash: diskH.SHA256, NewHash: newHash, UserEdited: true, Reason: "Файл уже существует и добавлен пользователем вручную"})
						continue
					}
				}
			}
			diff.Added = append(diff.Added, UpdateDiffEntry{RelPath: nf.RelPath, Title: nf.Title, Category: nf.Category, Action: "add", NewHash: newHash})
			continue
		}
		oldHash := firstNonEmpty(oldRec.SHA256, oldRec.SHA1)
		if newHash == "" && nf.ArchiveEntry != "" && newPrev.ResolvedArchivePath != "" {
			newHash = computeArchiveEntrySHA256(newPrev.ResolvedArchivePath, nf.ArchiveEntry)
		}
		if (nf.SHA256 != "" && strings.EqualFold(oldRec.SHA256, nf.SHA256)) || (nf.SHA1 != "" && strings.EqualFold(oldRec.SHA1, nf.SHA1)) || (newHash != "" && strings.EqualFold(oldHash, newHash)) {
			continue
		}
		userEdited := false
		if gameDir != "" && oldRec.SHA256 != "" {
			if full, rerr := ResolveSafeDestPath(gameDir, nf.RelPath); rerr == nil {
				if diskH, herr := ComputeFileHashes(full); herr == nil && !strings.EqualFold(diskH.SHA256, oldRec.SHA256) {
					userEdited = true
				}
			}
		}
		if userEdited {
			diff.ConfigConflicts = append(diff.ConfigConflicts, UpdateDiffEntry{RelPath: nf.RelPath, Title: nf.Title, Category: nf.Category, Action: "config_conflict", OldHash: oldHash, NewHash: newHash, UserEdited: true, Reason: "Файл изменён пользователем локально и также обновлён в новой версии сборки"})
		} else {
			diff.Updated = append(diff.Updated, UpdateDiffEntry{RelPath: nf.RelPath, Title: nf.Title, Category: nf.Category, Action: "update", OldHash: oldHash, NewHash: newHash})
		}
	}

	for low, oldRec := range oldByRel {
		if !oldRec.Managed {
			continue
		}
		if _, stillInNew := newByRel[low]; stillInNew {
			continue
		}
		userEdited := false
		if gameDir != "" && oldRec.SHA256 != "" {
			if full, rerr := ResolveSafeDestPath(gameDir, oldRec.RelPath); rerr == nil {
				if diskH, herr := ComputeFileHashes(full); herr == nil && !strings.EqualFold(diskH.SHA256, oldRec.SHA256) {
					userEdited = true
				}
			}
		}
		if userEdited {
			diff.PreservedUser = append(diff.PreservedUser, UpdateDiffEntry{RelPath: oldRec.RelPath, Category: ComponentCategory(oldRec.Category), Action: "preserve_user", UserEdited: true, Reason: "Файл был изменён пользователем и будет сохранён"})
		} else {
			diff.Removed = append(diff.Removed, UpdateDiffEntry{RelPath: oldRec.RelPath, Category: ComponentCategory(oldRec.Category), Action: "remove", OldHash: firstNonEmpty(oldRec.SHA256, oldRec.SHA1)})
		}
	}

	if gameDir != "" {
		for _, sub := range []string{"mods", "config", "resourcepacks", "shaderpacks", "datapacks"} {
			_ = filepath.WalkDir(filepath.Join(gameDir, sub), func(full string, d os.DirEntry, werr error) error {
				if werr != nil || d.IsDir() {
					return nil
				}
				rel, rerr := filepath.Rel(gameDir, full)
				if rerr != nil {
					return nil
				}
				relSlash := filepath.ToSlash(rel)
				low := strings.ToLower(relSlash)
				if _, isOld := oldByRel[low]; !isOld {
					if _, isNew := newByRel[low]; !isNew {
						diff.PreservedUser = append(diff.PreservedUser, UpdateDiffEntry{RelPath: relSlash, Category: ClassifyComponentPath(relSlash, 0, ""), Action: "preserve_user", Reason: "Добавлено пользователем вручную — не будет удалено"})
					}
				}
				return nil
			})
		}
	}

	diff.HasUpdate = len(diff.Added) > 0 || len(diff.Updated) > 0 || len(diff.Removed) > 0 || len(diff.ConfigConflicts) > 0 || (target.ManifestHash != "" && newPrev.ManifestHash != "" && target.ManifestHash != newPrev.ManifestHash)
	m.mu.Lock()
	cut := time.Now().Add(-2 * time.Hour)
	for k, up := range m.updatePlans {
		if up.createdAt.Before(cut) {
			delete(m.updatePlans, k)
		}
	}
	m.updatePlans[newPrev.PreviewID] = cachedUpdatePlan{diff: diff, newPrev: newPrev, oldManifestHash: target.ManifestHash, createdAt: time.Now()}
	m.mu.Unlock()
	return diff, nil
}

func (m *Manager) ApplyUpdate(ctx context.Context, req ApplyUpdateRequest) (ModpackView, error) {
	return m.applyUpdateSafe(ctx, req)
}

func shouldWriteTarget(destPath, wantSHA1, wantSHA256 string, policy ConflictResolution) bool {
	fi, err := os.Stat(destPath)
	if err != nil || fi.IsDir() {
		return true
	}
	if hashes, herr := ComputeFileHashes(destPath); herr == nil {
		if (wantSHA256 != "" && strings.EqualFold(hashes.SHA256, wantSHA256)) || (wantSHA1 != "" && strings.EqualFold(hashes.SHA1, wantSHA1)) {
			return false
		}
	}
	return policy != ConflictKeepExisting
}

func backupIfNeeded(destPath string, policy ConflictResolution) error {
	if policy != ConflictBackupAndOverwrite {
		return nil
	}
	if fi, err := os.Stat(destPath); err != nil || fi.IsDir() {
		return nil
	}
	in, err := os.Open(destPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destPath + ".bak." + time.Now().Format("20060102_150405"))
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	_ = out.Close()
	return err
}

func extractZipFileBounded(zf *zip.File, destPath string, maxBytes int64) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	tmp, err := os.CreateTemp(filepath.Dir(destPath), ".mpextract-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	n, err := io.Copy(tmp, io.LimitReader(rc, maxBytes+1))
	if err != nil || n > maxBytes {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		if n > maxBytes {
			return ErrArchiveTooLarge
		}
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	_ = os.Remove(destPath)
	return os.Rename(tmpPath, destPath)
}

func computeArchiveEntrySHA256(archivePath, entryName string) string {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return ""
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == entryName {
			rc, err := f.Open()
			if err != nil {
				return ""
			}
			h, _, _ := ComputeReaderSHA256(rc)
			_ = rc.Close()
			return h
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
