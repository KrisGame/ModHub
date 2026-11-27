package instances

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"modhub/internal/downloads"
	"modhub/internal/events"
)

// Content manages resource packs, shader packs, and world saves for instances.
type Content struct {
	mgr *Manager
	dl  *downloads.Manager
	bus *events.Bus
}

func NewContent(mgr *Manager, dl *downloads.Manager, bus *events.Bus) *Content {
	return &Content{mgr: mgr, dl: dl, bus: bus}
}

// ============================================================================
// RESOURCE PACKS
// ============================================================================

func (c *Content) ListResourcePacks(instanceID string) ([]ResourcePackRef, error) {
	inst, err := c.mgr.Get(instanceID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(inst.GameDir, "resourcepacks")
	_ = os.MkdirAll(dir, 0o755)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return []ResourcePackRef{}, nil
	}

	packs := make([]ResourcePackRef, 0, len(entries))
	for _, ent := range entries {
		name := ent.Name()
		low := strings.ToLower(name)
		if ent.IsDir() {
			if strings.HasPrefix(name, ".") {
				continue
			}
		} else {
			if !strings.HasSuffix(low, ".zip") && !strings.HasSuffix(low, ".zip.disabled") {
				continue
			}
		}

		enabled := !strings.HasSuffix(low, ".disabled")
		fullPath := filepath.Join(dir, name)

		ref := ResourcePackRef{
			Filename: name,
			Name:     cleanPackDisplayName(name),
			Enabled:  enabled,
		}

		if fi, err := ent.Info(); err == nil {
			if ent.IsDir() {
				ref.Size = calcDirSize(fullPath)
			} else {
				ref.Size = fi.Size()
			}
		}

		// Read pack.mcmeta and pack.png metadata
		if ent.IsDir() {
			readFolderPackMeta(fullPath, &ref)
		} else {
			readZipPackMeta(fullPath, &ref)
		}

		packs = append(packs, ref)
	}

	sort.Slice(packs, func(i, j int) bool {
		if packs[i].Enabled != packs[j].Enabled {
			return packs[i].Enabled // Enabled first
		}
		return strings.ToLower(packs[i].Name) < strings.ToLower(packs[j].Name)
	})

	return packs, nil
}

func (c *Content) SetResourcePackEnabled(instanceID, filename string, enabled bool) ([]ResourcePackRef, error) {
	inst, err := c.mutableInstance(instanceID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(inst.GameDir, "resourcepacks")
	name, err := sanitizePackFilename(filename)
	if err != nil {
		return nil, err
	}

	source := filepath.Join(dir, name)
	if err := validateModTarget(dir, source); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(source); err != nil {
		return nil, fmt.Errorf("ресурспак не найден: %w", err)
	}

	targetName := name
	if enabled && strings.HasSuffix(name, ".disabled") {
		targetName = strings.TrimSuffix(name, ".disabled")
	} else if !enabled && !strings.HasSuffix(name, ".disabled") {
		targetName = name + ".disabled"
	}

	if targetName != name {
		target := filepath.Join(dir, targetName)
		if err := validateModTarget(dir, target); err != nil {
			return nil, err
		}
		if _, err := os.Lstat(target); err == nil {
			return nil, fmt.Errorf("целевой файл %q уже существует", targetName)
		}
		if err := os.Rename(source, target); err != nil {
			return nil, fmt.Errorf("не удалось переименовать ресурспак: %w", err)
		}
	}

	return c.ListResourcePacks(instanceID)
}

func (c *Content) DeleteResourcePack(instanceID, filename string) ([]ResourcePackRef, error) {
	inst, err := c.mutableInstance(instanceID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(inst.GameDir, "resourcepacks")
	name, err := sanitizePackFilename(filename)
	if err != nil {
		return nil, err
	}

	target := filepath.Join(dir, name)
	if err := validateModTarget(dir, target); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(target); err != nil {
		return nil, fmt.Errorf("ресурспак не найден")
	}

	if err := os.RemoveAll(target); err != nil {
		return nil, fmt.Errorf("не удалось удалить ресурспак: %w", err)
	}

	return c.ListResourcePacks(instanceID)
}

// ============================================================================
// SHADER PACKS
// ============================================================================

func (c *Content) ListShaderPacks(instanceID string) ([]ShaderPackRef, error) {
	inst, err := c.mgr.Get(instanceID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(inst.GameDir, "shaderpacks")
	_ = os.MkdirAll(dir, 0o755)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return []ShaderPackRef{}, nil
	}

	shaders := make([]ShaderPackRef, 0, len(entries))
	for _, ent := range entries {
		name := ent.Name()
		low := strings.ToLower(name)
		if ent.IsDir() {
			if strings.HasPrefix(name, ".") {
				continue
			}
		} else {
			if !strings.HasSuffix(low, ".zip") && !strings.HasSuffix(low, ".zip.disabled") {
				continue
			}
		}

		enabled := !strings.HasSuffix(low, ".disabled")
		fullPath := filepath.Join(dir, name)

		ref := ShaderPackRef{
			Filename: name,
			Name:     cleanPackDisplayName(name),
			Enabled:  enabled,
		}

		if fi, err := ent.Info(); err == nil {
			if ent.IsDir() {
				ref.Size = calcDirSize(fullPath)
			} else {
				ref.Size = fi.Size()
			}
		}

		shaders = append(shaders, ref)
	}

	sort.Slice(shaders, func(i, j int) bool {
		if shaders[i].Enabled != shaders[j].Enabled {
			return shaders[i].Enabled
		}
		return strings.ToLower(shaders[i].Name) < strings.ToLower(shaders[j].Name)
	})

	return shaders, nil
}

func (c *Content) SetShaderPackEnabled(instanceID, filename string, enabled bool) ([]ShaderPackRef, error) {
	inst, err := c.mutableInstance(instanceID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(inst.GameDir, "shaderpacks")
	name, err := sanitizePackFilename(filename)
	if err != nil {
		return nil, err
	}

	source := filepath.Join(dir, name)
	if err := validateModTarget(dir, source); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(source); err != nil {
		return nil, fmt.Errorf("шейдерпак не найден: %w", err)
	}

	targetName := name
	if enabled && strings.HasSuffix(name, ".disabled") {
		targetName = strings.TrimSuffix(name, ".disabled")
	} else if !enabled && !strings.HasSuffix(name, ".disabled") {
		targetName = name + ".disabled"
	}

	if targetName != name {
		target := filepath.Join(dir, targetName)
		if err := validateModTarget(dir, target); err != nil {
			return nil, err
		}
		if _, err := os.Lstat(target); err == nil {
			return nil, fmt.Errorf("целевой файл %q уже существует", targetName)
		}
		if err := os.Rename(source, target); err != nil {
			return nil, fmt.Errorf("не удалось переименовать шейдерпак: %w", err)
		}
	}

	return c.ListShaderPacks(instanceID)
}

func (c *Content) DeleteShaderPack(instanceID, filename string) ([]ShaderPackRef, error) {
	inst, err := c.mutableInstance(instanceID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(inst.GameDir, "shaderpacks")
	name, err := sanitizePackFilename(filename)
	if err != nil {
		return nil, err
	}

	target := filepath.Join(dir, name)
	if err := validateModTarget(dir, target); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(target); err != nil {
		return nil, fmt.Errorf("шейдерпак не найден")
	}

	if err := os.RemoveAll(target); err != nil {
		return nil, fmt.Errorf("не удалось удалить шейдерпак: %w", err)
	}

	return c.ListShaderPacks(instanceID)
}

// ============================================================================
// WORLDS / SAVES
// ============================================================================

func (c *Content) ListWorlds(instanceID string) ([]WorldRef, error) {
	inst, err := c.mgr.Get(instanceID)
	if err != nil {
		return nil, err
	}
	savesDir := filepath.Join(inst.GameDir, "saves")
	_ = os.MkdirAll(savesDir, 0o755)

	entries, err := os.ReadDir(savesDir)
	if err != nil {
		return []WorldRef{}, nil
	}

	worlds := make([]WorldRef, 0, len(entries))
	for _, ent := range entries {
		if !ent.IsDir() || strings.HasPrefix(ent.Name(), ".") {
			continue
		}

		worldFolder := ent.Name()
		fullPath := filepath.Join(savesDir, worldFolder)

		ref := WorldRef{
			Folder:   worldFolder,
			Name:     worldFolder,
			GameType: "survival",
		}

		if fi, err := ent.Info(); err == nil {
			ref.LastPlayed = fi.ModTime().Unix()
		}

		// Calculate world directory size
		ref.Size = calcDirSize(fullPath)

		// Parse level.dat
		levelDatPath := filepath.Join(fullPath, "level.dat")
		if data, err := os.ReadFile(levelDatPath); err == nil {
			parseLevelDat(data, &ref)
		}

		// Read icon.png if present
		iconPath := filepath.Join(fullPath, "icon.png")
		if iconBytes, err := readLimitedFile(iconPath, 1*1024*1024); err == nil && len(iconBytes) > 0 {
			ref.IconDataURL = "data:image/png;base64," + base64.StdEncoding.EncodeToString(iconBytes)
		}

		worlds = append(worlds, ref)
	}

	// Sort most recently played first
	sort.Slice(worlds, func(i, j int) bool {
		return worlds[i].LastPlayed > worlds[j].LastPlayed
	})

	return worlds, nil
}

func (c *Content) ExportWorld(instanceID, folder string) (ExportWorldResult, error) {
	inst, err := c.mgr.Get(instanceID)
	if err != nil {
		return ExportWorldResult{}, err
	}
	name, err := sanitizeWorldFolderName(folder)
	if err != nil {
		return ExportWorldResult{}, err
	}

	savesDir := filepath.Join(inst.GameDir, "saves")
	worldPath := filepath.Join(savesDir, name)
	if err := validateModTarget(savesDir, worldPath); err != nil {
		return ExportWorldResult{}, err
	}

	fi, err := os.Stat(worldPath)
	if err != nil || !fi.IsDir() {
		return ExportWorldResult{}, fmt.Errorf("папка сохранения мира не найдена")
	}

	if err := validateWorldSize(worldPath); err != nil {
		return ExportWorldResult{}, err
	}
	backupsDir := filepath.Join(inst.RootDir, "backups")
	if err := os.MkdirAll(backupsDir, 0o700); err != nil {
		return ExportWorldResult{}, fmt.Errorf("не удалось создать папку backups: %w", err)
	}
	timestamp := time.Now().Format("20060102-150405")
	archiveName := fmt.Sprintf("%s-backup-%s.zip", name, timestamp)
	return c.exportWorldToPath(context.Background(), worldPath, filepath.Join(backupsDir, archiveName), archiveName)
}

func validateWorldSize(worldPath string) error {
	const maxWorldFiles = 100000
	const maxWorldBytes = int64(8) << 30
	files := 0
	var totalBytes int64
	return filepath.Walk(worldPath, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("символические ссылки в мире не поддерживаются")
		}
		if info.IsDir() {
			return nil
		}
		files++
		if files > maxWorldFiles {
			return fmt.Errorf("мир содержит слишком много файлов (лимит %d)", maxWorldFiles)
		}
		if info.Size() < 0 || totalBytes > int64(^uint64(0)>>1)-info.Size() {
			return fmt.Errorf("размер мира превышает допустимый предел")
		}
		totalBytes += info.Size()
		if totalBytes > maxWorldBytes {
			return fmt.Errorf("размер мира превышает лимит 8 ГБ")
		}
		return nil
	})
}

type worldExportReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r worldExportReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (c *Content) exportWorldToPath(ctx context.Context, worldPath, archivePath, archiveName string) (ExportWorldResult, error) {
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o700); err != nil {
		return ExportWorldResult{}, fmt.Errorf("не удалось создать папку экспорта: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(archivePath), ".world-backup-*.tmp")
	if err != nil {
		return ExportWorldResult{}, fmt.Errorf("не удалось создать архив: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanup := func() { _ = tmpFile.Close(); _ = os.Remove(tmpPath) }

	zw := zip.NewWriter(tmpFile)
	const maxWorldBytes = int64(8) << 30
	const maxWorldFiles = 100000
	var streamedBytes int64
	streamedFiles := 0

	err = filepath.Walk(worldPath, func(path string, info os.FileInfo, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("символические ссылки в мире не поддерживаются")
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("в архив мира можно добавить только обычные файлы")
		}
		streamedFiles++
		if streamedFiles > maxWorldFiles || info.Size() < 0 || info.Size() > maxWorldBytes-streamedBytes {
			return fmt.Errorf("world exceeds export limits")
		}
		rel, relErr := filepath.Rel(worldPath, path)
		if relErr != nil {
			return relErr
		}
		// In zip, path separators must be forward slashes
		zipEntryPath := filepath.ToSlash(rel)

		hdr, hdrErr := zip.FileInfoHeader(info)
		if hdrErr != nil {
			return hdrErr
		}
		hdr.Name = zipEntryPath
		hdr.Method = zip.Deflate

		w, createErr := zw.CreateHeader(hdr)
		if createErr != nil {
			return createErr
		}

		rf, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		// Recheck bytes while streaming: files can grow after the initial scan.
		written, copyErr := io.Copy(w, io.LimitReader(worldExportReader{ctx: ctx, reader: rf}, maxWorldBytes-streamedBytes+1))
		_ = rf.Close()
		streamedBytes += written
		if streamedBytes > maxWorldBytes {
			return fmt.Errorf("world exceeds export size limit")
		}
		return copyErr
	})

	if err != nil {
		_ = zw.Close()
		cleanup()
		return ExportWorldResult{}, fmt.Errorf("ошибка при архивации мира: %w", err)
	}

	if err := zw.Close(); err != nil {
		cleanup()
		return ExportWorldResult{}, err
	}
	if err := tmpFile.Sync(); err != nil {
		cleanup()
		return ExportWorldResult{}, err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return ExportWorldResult{}, err
	}
	if err := os.Rename(tmpPath, archivePath); err != nil {
		_ = os.Remove(tmpPath)
		return ExportWorldResult{}, err
	}

	stat, _ := os.Stat(archivePath)
	var sz int64
	if stat != nil {
		sz = stat.Size()
	}

	return ExportWorldResult{
		ArchivePath: archivePath,
		Filename:    archiveName,
		Size:        sz,
	}, nil
}

// ExportWorldTo writes a safe world archive to a caller-selected destination.
// HTTP callers must constrain the destination to their own temporary export area.
func (c *Content) ExportWorldTo(ctx context.Context, instanceID, folder, targetPath string) (ExportWorldResult, error) {
	inst, err := c.mgr.Get(instanceID)
	if err != nil {
		return ExportWorldResult{}, err
	}
	name, err := sanitizeWorldFolderName(folder)
	if err != nil {
		return ExportWorldResult{}, err
	}
	worldPath := filepath.Join(inst.GameDir, "saves", name)
	if err := validateModTarget(filepath.Join(inst.GameDir, "saves"), worldPath); err != nil {
		return ExportWorldResult{}, err
	}
	fi, err := os.Stat(worldPath)
	if err != nil || !fi.IsDir() {
		return ExportWorldResult{}, fmt.Errorf("папка сохранения мира не найдена")
	}
	if strings.TrimSpace(targetPath) == "" {
		return ExportWorldResult{}, fmt.Errorf("не указан путь скачивания")
	}
	return c.exportWorldToPath(ctx, worldPath, targetPath, name+".zip")
}

// RemoveExport removes an export after the HTTP download finishes.
func (c *Content) RemoveExport(exportDir, targetPath string) error {
	if err := validateModTarget(exportDir, targetPath); err != nil {
		return err
	}
	return os.Remove(targetPath)
}

func (c *Content) mutableInstance(instanceID string) (Instance, error) {
	if err := ValidateInstanceID(instanceID); err != nil {
		return Instance{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	inst, err := c.mgr.Get(instanceID)
	if err != nil {
		return Instance{}, err
	}
	if inst.InUse {
		return Instance{}, ErrInstanceBusy
	}
	return inst, nil
}

// ============================================================================
// HELPERS & NBT PARSING
// ============================================================================

func cleanPackDisplayName(filename string) string {
	res := filename
	res = strings.TrimSuffix(res, ".disabled")
	res = strings.TrimSuffix(res, ".zip")
	res = strings.TrimSuffix(res, ".ZIP")
	return res
}

func sanitizePackFilename(name string) (string, error) {
	if name == "" || len(name) > 200 || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\:`) {
		return "", fmt.Errorf("некорректное имя файла")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("некорректные символы в имени файла")
		}
	}
	return name, nil
}

func sanitizeWorldFolderName(name string) (string, error) {
	if name == "" || len(name) > 120 || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\:`) {
		return "", fmt.Errorf("некорректное имя папки сохранения")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("некорректные символы в имени папки")
		}
	}
	return name, nil
}

func calcDirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

func readLimitedFile(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxBytes))
}

type packMcmetaStructure struct {
	Pack struct {
		Description any `json:"description"`
		PackFormat  int `json:"pack_format"`
	} `json:"pack"`
}

func extractPackDescription(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	if m, ok := v.(map[string]any); ok {
		if text, ok := m["text"].(string); ok {
			return strings.TrimSpace(text)
		}
	}
	b, err := json.Marshal(v)
	if err == nil {
		return strings.Trim(string(b), `"`)
	}
	return ""
}

func readFolderPackMeta(folderPath string, ref *ResourcePackRef) {
	metaPath := filepath.Join(folderPath, "pack.mcmeta")
	if data, err := readLimitedFile(metaPath, 256*1024); err == nil {
		var meta packMcmetaStructure
		if err := json.Unmarshal(data, &meta); err == nil {
			ref.Description = extractPackDescription(meta.Pack.Description)
			ref.PackFormat = meta.Pack.PackFormat
		}
	}

	iconPath := filepath.Join(folderPath, "pack.png")
	if iconBytes, err := readLimitedFile(iconPath, 1*1024*1024); err == nil && len(iconBytes) > 0 {
		ref.IconDataURL = "data:image/png;base64," + base64.StdEncoding.EncodeToString(iconBytes)
	}
}

func readZipPackMeta(zipPath string, ref *ResourcePackRef) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return
	}
	defer zr.Close()

	for _, f := range zr.File {
		cleanName := strings.TrimPrefix(filepath.ToSlash(f.Name), "./")
		if strings.EqualFold(cleanName, "pack.mcmeta") {
			if rc, err := f.Open(); err == nil {
				data, _ := io.ReadAll(io.LimitReader(rc, 256*1024))
				_ = rc.Close()
				var meta packMcmetaStructure
				if json.Unmarshal(data, &meta) == nil {
					ref.Description = extractPackDescription(meta.Pack.Description)
					ref.PackFormat = meta.Pack.PackFormat
				}
			}
		} else if strings.EqualFold(cleanName, "pack.png") {
			if rc, err := f.Open(); err == nil {
				iconBytes, _ := io.ReadAll(io.LimitReader(rc, 1*1024*1024))
				_ = rc.Close()
				if len(iconBytes) > 0 {
					ref.IconDataURL = "data:image/png;base64," + base64.StdEncoding.EncodeToString(iconBytes)
				}
			}
		}
	}
}

// parseLevelDat extracts basic metadata (Name, GameType, Hardcore, LastPlayed, Version) from Minecraft level.dat.
func parseLevelDat(raw []byte, ref *WorldRef) {
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return
	}
	defer gz.Close()

	decompressed, err := io.ReadAll(io.LimitReader(gz, 10*1024*1024))
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return
	}

	// Minimal NBT tag scanner
	scanLevelDatNBT(decompressed, ref)
}

func scanLevelDatNBT(data []byte, ref *WorldRef) {
	if len(data) < 3 || data[0] != 10 { // Root must be TAG_Compound (10)
		return
	}

	r := bytes.NewReader(data)
	t, _, val, err := readNBTTag(r)
	if err != nil || t != 10 {
		return
	}
	rootMap, ok := val.(map[string]any)
	if !ok {
		return
	}
	dataMap, ok := rootMap["Data"].(map[string]any)
	if !ok {
		dataMap = rootMap
	}

	if s, ok := dataMap["LevelName"].(string); ok && s != "" {
		ref.Name = s
	}
	if gt, ok := dataMap["GameType"].(int32); ok {
		switch gt {
		case 0:
			ref.GameType = "survival"
		case 1:
			ref.GameType = "creative"
		case 2:
			ref.GameType = "adventure"
		case 3:
			ref.GameType = "spectator"
		}
	}
	if hc, ok := dataMap["hardcore"].(byte); ok && hc == 1 {
		ref.Hardcore = true
		ref.GameType = "hardcore"
	}
	if lp, ok := dataMap["LastPlayed"].(int64); ok && lp > 0 {
		ref.LastPlayed = lp / 1000
	}
	if vMap, ok := dataMap["Version"].(map[string]any); ok {
		if vName, ok := vMap["Name"].(string); ok && vName != "" {
			ref.Version = vName
		}
	}
}

func readUint16(r *bytes.Reader) (uint16, error) {
	var v uint16
	err := binary.Read(r, binary.BigEndian, &v)
	return v, err
}

func readString(r *bytes.Reader) (string, error) {
	l, err := readUint16(r)
	if err != nil {
		return "", err
	}
	buf := make([]byte, l)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func readNBTTag(r *bytes.Reader) (byte, string, any, error) {
	tagType, err := r.ReadByte()
	if err != nil || tagType == 0 {
		return 0, "", nil, err
	}
	name, err := readString(r)
	if err != nil {
		return 0, "", nil, err
	}
	val, err := readNBTValue(r, tagType)
	return tagType, name, val, err
}

func readNBTValue(r *bytes.Reader, tagType byte) (any, error) {
	switch tagType {
	case 1: // Byte
		return r.ReadByte()
	case 2: // Short
		var v int16
		err := binary.Read(r, binary.BigEndian, &v)
		return v, err
	case 3: // Int
		var v int32
		err := binary.Read(r, binary.BigEndian, &v)
		return v, err
	case 4: // Long
		var v int64
		err := binary.Read(r, binary.BigEndian, &v)
		return v, err
	case 5: // Float
		var v float32
		err := binary.Read(r, binary.BigEndian, &v)
		return v, err
	case 6: // Double
		var v float64
		err := binary.Read(r, binary.BigEndian, &v)
		return v, err
	case 7: // Byte array
		var l int32
		if err := binary.Read(r, binary.BigEndian, &l); err != nil {
			return nil, err
		}
		if l < 0 {
			return nil, errors.New("invalid byte array length")
		}
		buf := make([]byte, l)
		_, err := io.ReadFull(r, buf)
		return buf, err
	case 8: // String
		return readString(r)
	case 9: // List
		elemType, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		var count int32
		if err := binary.Read(r, binary.BigEndian, &count); err != nil {
			return nil, err
		}
		if count <= 0 {
			return []any{}, nil
		}
		list := make([]any, 0, count)
		for i := int32(0); i < count; i++ {
			elem, err := readNBTValue(r, elemType)
			if err != nil {
				return nil, err
			}
			list = append(list, elem)
		}
		return list, nil
	case 10: // Compound
		m := make(map[string]any)
		for {
			t, n, v, err := readNBTTag(r)
			if err != nil || t == 0 {
				break
			}
			m[n] = v
		}
		return m, nil
	case 11: // Int array
		var l int32
		if err := binary.Read(r, binary.BigEndian, &l); err != nil {
			return nil, err
		}
		if l < 0 {
			return nil, errors.New("invalid int array length")
		}
		buf := make([]int32, l)
		err := binary.Read(r, binary.BigEndian, buf)
		return buf, err
	case 12: // Long array
		var l int32
		if err := binary.Read(r, binary.BigEndian, &l); err != nil {
			return nil, err
		}
		if l < 0 {
			return nil, errors.New("invalid long array length")
		}
		buf := make([]int64, l)
		err := binary.Read(r, binary.BigEndian, buf)
		return buf, err
	default:
		return nil, fmt.Errorf("unknown tag type %d", tagType)
	}
}
