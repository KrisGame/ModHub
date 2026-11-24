package modpacks

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"modhub/internal/catalog"
	"modhub/internal/domain"
	"modhub/internal/instances"
	"modhub/internal/security"
)

var (
	ErrUnknownFormat        = errors.New("неподдерживаемый формат сборки (ожидается .mrpack, CurseForge .zip или .modhub)")
	ErrUnsupportedFormatVer = errors.New("неподдерживаемая версия формата сборки")
	ErrInvalidManifest      = errors.New("повреждённый или неполный манифест сборки")
)

func readZipFileEntry(zr *zip.ReadCloser, name string, maxBytes int64) ([]byte, bool, error) {
	want := strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "/"))
	for _, f := range zr.File {
		norm := strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(f.Name, "\\", "/"), "/"))
		if norm == want {
			rc, err := f.Open()
			if err != nil {
				return nil, true, err
			}
			defer rc.Close()
			data, err := io.ReadAll(io.LimitReader(rc, maxBytes+1))
			if err != nil {
				return nil, true, err
			}
			if int64(len(data)) > maxBytes {
				return nil, true, fmt.Errorf("%w: манифест %s превышает лимит размера", ErrInvalidManifest, name)
			}
			return data, true, nil
		}
	}
	return nil, false, nil
}

func sha256HexBytes(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func parseLoaderFromCFID(id string) (string, string) {
	s := strings.TrimSpace(id)
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "neoforge-"):
		return "neoforge", strings.TrimSpace(s[len("neoforge-"):])
	case strings.HasPrefix(low, "forge-"):
		return "forge", strings.TrimSpace(s[len("forge-"):])
	case strings.HasPrefix(low, "fabric-"):
		return "fabric", strings.TrimSpace(s[len("fabric-"):])
	case strings.HasPrefix(low, "quilt-"):
		return "quilt", strings.TrimSpace(s[len("quilt-"):])
	default:
		return "vanilla", ""
	}
}

func parseLoaderFromMRDeps(deps map[string]string) (string, string, string) {
	mcVer := strings.TrimSpace(deps["minecraft"])
	for k, v := range deps {
		kl := strings.ToLower(strings.TrimSpace(k))
		val := strings.TrimSpace(v)
		switch kl {
		case "neoforge":
			return mcVer, "neoforge", val
		case "fabric-loader":
			return mcVer, "fabric", val
		case "quilt-loader":
			return mcVer, "quilt", val
		case "forge":
			return mcVer, "forge", val
		}
	}
	return mcVer, "vanilla", ""
}

func computeCounts(files []ModpackFilePlan, serverSkipped int) (ImportCounts, int64) {
	var c ImportCounts
	c.ServerSkipped = serverSkipped
	var estBytes int64
	for _, f := range files {
		c.TotalFiles++
		if f.Optional {
			c.OptionalFiles++
		}
		if !f.Available {
			c.Unavailable++
		} else if f.Size > 0 {
			estBytes += f.Size
		}
		switch f.Category {
		case CatMod:
			c.Mods++
		case CatAddon:
			c.Addons++
		case CatResourcePack:
			c.ResourcePacks++
		case CatShader:
			c.Shaders++
		case CatDatapack:
			c.Datapacks++
		case CatConfig:
			c.Configs++
		default:
			c.UserFiles++
		}
	}
	return c, estBytes
}

func collectZipOverrides(zr *zip.ReadCloser, prefixes []string, skipPrefixes []string) ([]ModpackFilePlan, error) {
	var out []ModpackFilePlan
	seenRel := map[string]int{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		norm := strings.TrimPrefix(strings.ReplaceAll(f.Name, "\\", "/"), "/")
		low := strings.ToLower(norm)
		skip := false
		for _, sp := range skipPrefixes {
			if strings.HasPrefix(low, strings.ToLower(sp)) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		matchedPrefix := ""
		for _, pref := range prefixes {
			if strings.HasPrefix(low, strings.ToLower(pref)) {
				matchedPrefix = pref
				break
			}
		}
		if matchedPrefix == "" {
			continue
		}
		relRaw := norm[len(matchedPrefix):]
		if strings.TrimSpace(relRaw) == "" {
			continue
		}
		cleanRel, err := ValidateRelativeGamePath(relRaw)
		if err != nil {
			return nil, err
		}
		susp, suspReason := CheckSuspiciousFile(cleanRel)
		plan := ModpackFilePlan{
			RelPath: cleanRel, Title: path.Base(cleanRel), Filename: path.Base(cleanRel),
			Category: ClassifyComponentPath(cleanRel, 0, ""), SourceKind: "override",
			ArchiveEntry: f.Name, Size: int64(f.UncompressedSize64),
			SelectedByDefault: true, Available: true, Suspicious: susp, SuspiciousReason: suspReason,
		}
		lowKey := strings.ToLower(cleanRel)
		if prevIdx, exists := seenRel[lowKey]; exists {
			out[prevIdx] = plan
		} else {
			seenRel[lowKey] = len(out)
			out = append(out, plan)
		}
	}
	return out, nil
}

func (m *Manager) ParseArchivePreview(ctx context.Context, archivePath, sourceType, sourceRef, srcProjID, srcVerID string) (ImportPreview, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return ImportPreview{}, fmt.Errorf("%w: %v", ErrArchiveCorrupted, err)
	}
	defer zr.Close()

	m.mu.Lock()
	limits := m.limits
	m.mu.Unlock()

	safetyWarnings, err := InspectArchiveSafety(zr, limits)
	if err != nil {
		return ImportPreview{}, err
	}

	if rawMH, found, err := readZipFileEntry(zr, "modhub.pack.json", DefaultMaxManifestBytes); err != nil {
		return ImportPreview{}, err
	} else if found {
		prev, err := m.parseModHubArchive(zr, rawMH, archivePath, sourceType, sourceRef, srcProjID, srcVerID)
		if err != nil {
			return ImportPreview{}, err
		}
		prev.Warnings = append(prev.Warnings, safetyWarnings...)
		return prev, nil
	}

	if rawMR, found, err := readZipFileEntry(zr, "modrinth.index.json", DefaultMaxManifestBytes); err != nil {
		return ImportPreview{}, err
	} else if found {
		prev, err := m.parseModrinthArchive(zr, rawMR, archivePath, sourceType, sourceRef, srcProjID, srcVerID)
		if err != nil {
			return ImportPreview{}, err
		}
		prev.Warnings = append(prev.Warnings, safetyWarnings...)
		return prev, nil
	}

	if rawCF, found, err := readZipFileEntry(zr, "manifest.json", DefaultMaxManifestBytes); err != nil {
		return ImportPreview{}, err
	} else if found {
		prev, err := m.parseCurseForgeArchive(ctx, zr, rawCF, archivePath, sourceType, sourceRef, srcProjID, srcVerID)
		if err != nil {
			return ImportPreview{}, err
		}
		prev.Warnings = append(prev.Warnings, safetyWarnings...)
		return prev, nil
	}

	return ImportPreview{}, ErrUnknownFormat
}

func (m *Manager) parseModrinthArchive(zr *zip.ReadCloser, rawManifest []byte, archivePath, sourceType, sourceRef, srcProjID, srcVerID string) (ImportPreview, error) {
	var idx ModrinthIndexJSON
	if err := json.Unmarshal(rawManifest, &idx); err != nil {
		return ImportPreview{}, fmt.Errorf("%w (modrinth.index.json): %v", ErrInvalidManifest, err)
	}
	if idx.FormatVersion != 1 {
		return ImportPreview{}, fmt.Errorf("%w: Modrinth formatVersion=%d (поддерживается версия 1)", ErrUnsupportedFormatVer, idx.FormatVersion)
	}
	if !strings.EqualFold(strings.TrimSpace(idx.Game), "minecraft") {
		return ImportPreview{}, fmt.Errorf("%w: поддерживается только игра minecraft (получено %q)", ErrInvalidManifest, idx.Game)
	}
	if strings.TrimSpace(idx.Name) == "" {
		return ImportPreview{}, fmt.Errorf("%w: отсутствует название сборки в modrinth.index.json", ErrInvalidManifest)
	}
	mcVer, loader, loaderVer := parseLoaderFromMRDeps(idx.Dependencies)
	if mcVer == "" {
		return ImportPreview{}, fmt.Errorf("%w: в dependencies отсутствует версия minecraft", ErrInvalidManifest)
	}

	var files, unavailable, optionalList []ModpackFilePlan
	var conflicts []ImportConflict
	var warnings []string
	seenPaths := map[string]bool{}
	serverSkipped := 0

	for _, mf := range idx.Files {
		isOptional := false
		if mf.Env != nil {
			clientEnv := strings.ToLower(strings.TrimSpace(mf.Env.Client))
			if clientEnv == "unsupported" {
				serverSkipped++
				continue
			}
			if clientEnv == "optional" {
				isOptional = true
			}
		}
		cleanRel, err := ValidateRelativeGamePath(mf.Path)
		if err != nil {
			return ImportPreview{}, err
		}
		lowPath := strings.ToLower(cleanRel)
		if seenPaths[lowPath] {
			conflicts = append(conflicts, ImportConflict{Code: "duplicate_path", Path: cleanRel, Message: "Дублирующийся путь в манифесте: " + cleanRel})
			continue
		}
		seenPaths[lowPath] = true

		validURLs := make([]string, 0, len(mf.Downloads))
		var urlErr string
		for _, u := range mf.Downloads {
			u = strings.TrimSpace(u)
			if u == "" {
				continue
			}
			if _, vErr := m.httpClient.ValidateLauncherURL(u); vErr == nil {
				validURLs = append(validURLs, u)
			} else if urlErr == "" {
				urlErr = vErr.Error()
			}
		}
		susp, suspReason := CheckSuspiciousFile(cleanRel)
		plan := ModpackFilePlan{
			RelPath: cleanRel, Title: path.Base(cleanRel), Filename: path.Base(cleanRel),
			Category: ClassifyComponentPath(cleanRel, 0, ""), SourceKind: "download", SourcePlatform: "modrinth",
			DownloadURLs: validURLs, SHA1: strings.ToLower(strings.TrimSpace(mf.Hashes["sha1"])), SHA512: strings.ToLower(strings.TrimSpace(mf.Hashes["sha512"])),
			Size: mf.FileSize, Optional: isOptional, SelectedByDefault: !isOptional, Available: len(validURLs) > 0,
			Suspicious: susp, SuspiciousReason: suspReason,
		}
		if !plan.Available {
			if urlErr != "" {
				plan.UnavailableReason = "URL загрузки отклонён политикой безопасности: " + urlErr
			} else {
				plan.UnavailableReason = "В манифесте отсутствуют ссылки на скачивание файла"
			}
			unavailable = append(unavailable, plan)
		}
		if plan.Optional {
			optionalList = append(optionalList, plan)
		}
		if susp {
			conflicts = append(conflicts, ImportConflict{Code: "suspicious_file", Path: cleanRel, Message: suspReason})
		}
		files = append(files, plan)
	}

	overrides, err := collectZipOverrides(zr, []string{"overrides/", "client-overrides/"}, []string{"server-overrides/"})
	if err != nil {
		return ImportPreview{}, err
	}
	for _, ov := range overrides {
		lowPath := strings.ToLower(ov.RelPath)
		if seenPaths[lowPath] {
			conflicts = append(conflicts, ImportConflict{Code: "duplicate_path", Path: ov.RelPath, Message: "Override перекрывает загружаемый файл: " + ov.RelPath})
		}
		seenPaths[lowPath] = true
		if ov.Suspicious {
			conflicts = append(conflicts, ImportConflict{Code: "suspicious_file", Path: ov.RelPath, Message: ov.SuspiciousReason})
		}
		files = append(files, ov)
	}
	counts, estBytes := computeCounts(files, serverSkipped)
	if serverSkipped > 0 {
		warnings = append(warnings, fmt.Sprintf("Пропущено серверных файлов (env.client=unsupported): %d", serverSkipped))
	}
	return ImportPreview{
		Format: FormatMRPack, SourceType: sourceType, SourceRef: sourceRef, SourceProjectID: srcProjID, SourceVersionID: srcVerID,
		ResolvedArchivePath: archivePath, Name: strings.TrimSpace(idx.Name), Description: strings.TrimSpace(idx.Summary),
		Version: strings.TrimSpace(idx.VersionID), MCVersion: mcVer, Loader: loader, LoaderVersion: loaderVer,
		ManifestHash: sha256HexBytes(rawManifest), EstimatedBytes: estBytes, Counts: counts, Files: files,
		UnavailableFiles: unavailable, OptionalFiles: optionalList, Conflicts: conflicts, Warnings: warnings,
	}, nil
}

type cfBatchFileItem struct {
	ID          int    `json:"id"`
	ModID       int    `json:"modId"`
	DisplayName string `json:"displayName"`
	FileName    string `json:"fileName"`
	FileLength  int64  `json:"fileLength"`
	DownloadURL string `json:"downloadUrl"`
	Hashes      []struct {
		Value string `json:"value"`
		Algo  int    `json:"algo"`
	} `json:"hashes"`
}

func (m *Manager) parseCurseForgeArchive(ctx context.Context, zr *zip.ReadCloser, rawManifest []byte, archivePath, sourceType, sourceRef, srcProjID, srcVerID string) (ImportPreview, error) {
	var mf CurseForgeManifestJSON
	if err := json.Unmarshal(rawManifest, &mf); err != nil {
		return ImportPreview{}, fmt.Errorf("%w (manifest.json): %v", ErrInvalidManifest, err)
	}
	if mf.ManifestType != "" && !strings.EqualFold(mf.ManifestType, "minecraftModpack") {
		return ImportPreview{}, fmt.Errorf("%w: неизвестный manifestType=%q", ErrInvalidManifest, mf.ManifestType)
	}
	if mf.ManifestVersion != 0 && mf.ManifestVersion != 1 {
		return ImportPreview{}, fmt.Errorf("%w: CurseForge manifestVersion=%d", ErrUnsupportedFormatVer, mf.ManifestVersion)
	}
	mcVer := strings.TrimSpace(mf.Minecraft.Version)
	if strings.TrimSpace(mf.Name) == "" || mcVer == "" {
		return ImportPreview{}, fmt.Errorf("%w: отсутствует имя сборки или версия Minecraft в manifest.json", ErrInvalidManifest)
	}

	loader, loaderVer := "vanilla", ""
	for _, ml := range mf.Minecraft.ModLoaders {
		if ml.Primary || loader == "vanilla" {
			loader, loaderVer = parseLoaderFromCFID(ml.ID)
			if ml.Primary {
				break
			}
		}
	}

	cfKey := ""
	if m.cfKeyFn != nil {
		cfKey = security.SanitizeCFKey(m.cfKeyFn())
	}

	var warnings []string
	var conflicts []ImportConflict
	resolvedFiles := map[int]cfBatchFileItem{}
	modMeta := map[int]catalog.CFMod{}

	if len(mf.Files) > 0 {
		if cfKey == "" {
			warnings = append(warnings, "API-ключ CurseForge не задан в Настройках — файлы из CurseForge API не могут быть загружены автоматически.")
		} else {
			var fileIDs, modIDs []int
			seenFID, seenMID := map[int]bool{}, map[int]bool{}
			for _, f := range mf.Files {
				if f.FileID > 0 && !seenFID[f.FileID] {
					seenFID[f.FileID] = true
					fileIDs = append(fileIDs, f.FileID)
				}
				if f.ProjectID > 0 && !seenMID[f.ProjectID] {
					seenMID[f.ProjectID] = true
					modIDs = append(modIDs, f.ProjectID)
				}
			}
			hc := m.httpClient.WithContext(ctx)
			hdr := map[string]string{"x-api-key": cfKey}
			for start := 0; start < len(fileIDs); start += 100 {
				end := start + 100
				if end > len(fileIDs) {
					end = len(fileIDs)
				}
				var resp struct {
					Data []cfBatchFileItem `json:"data"`
				}
				if err := hc.PostJSON(domain.CurseForgeAPI+"/mods/files", map[string]any{"fileIds": fileIDs[start:end]}, hdr, &resp); err != nil {
					warnings = append(warnings, "Ошибка обращения к CurseForge API (/mods/files): "+security.SanitizeLogText(err.Error()))
				} else {
					for _, item := range resp.Data {
						resolvedFiles[item.ID] = item
					}
				}
			}
			for mid, cm := range catalog.GetCFModsBatch(hc, cfKey, modIDs) {
				modMeta[mid] = cm
			}
		}
	}

	var files, unavailable, optionalList []ModpackFilePlan
	seenPaths := map[string]bool{}
	for _, entry := range mf.Files {
		if entry.ProjectID <= 0 || entry.FileID <= 0 {
			continue
		}
		cm := modMeta[entry.ProjectID]
		cat := ClassifyComponentPath("", cm.ClassID, "")
		folder := FolderForCategory(cat)
		cfFile, hasFile := resolvedFiles[entry.FileID]
		filename := fallbackStr(cfFile.FileName, fmt.Sprintf("cf-%d-%d.jar", entry.ProjectID, entry.FileID))
		title := fallbackStr(cm.Name, fallbackStr(cfFile.DisplayName, fmt.Sprintf("CurseForge #%d (файл %d)", entry.ProjectID, entry.FileID)))

		cleanRel, err := ValidateRelativeGamePath(folder + "/" + filepath.Base(filename))
		if err != nil {
			cleanRel = fmt.Sprintf("%s/cf-%d-%d.jar", folder, entry.ProjectID, entry.FileID)
		}
		lowPath := strings.ToLower(cleanRel)
		if seenPaths[lowPath] {
			conflicts = append(conflicts, ImportConflict{Code: "duplicate_mod", Path: cleanRel, Message: "Дублирующийся файл в манифесте CurseForge: " + cleanRel})
			continue
		}
		seenPaths[lowPath] = true

		sha1Val := ""
		for _, h := range cfFile.Hashes {
			if h.Algo == 1 && strings.TrimSpace(h.Value) != "" {
				sha1Val = strings.ToLower(strings.TrimSpace(h.Value))
				break
			}
		}
		var validURLs []string
		unavailReason := ""
		if cfKey == "" {
			unavailReason = "Не указан API-ключ CurseForge в Настройках"
		} else if !hasFile {
			unavailReason = fmt.Sprintf("Файл #%d (проект #%d) не найден или удалён на CurseForge", entry.FileID, entry.ProjectID)
		} else if strings.TrimSpace(cfFile.DownloadURL) == "" {
			unavailReason = "Автор запретил автоматическое скачивание через сторонние лаунчеры (CurseForge third-party distribution disabled)"
		} else if _, vErr := m.httpClient.ValidateLauncherURL(cfFile.DownloadURL); vErr != nil {
			unavailReason = "URL файла отклонён проверкой безопасности: " + vErr.Error()
		} else {
			validURLs = []string{strings.TrimSpace(cfFile.DownloadURL)}
		}

		plan := ModpackFilePlan{
			RelPath: cleanRel, Title: title, Filename: path.Base(cleanRel), Category: cat, SourceKind: "download",
			SourcePlatform: "curseforge", SourceProjectID: strconv.Itoa(entry.ProjectID), SourceFileID: strconv.Itoa(entry.FileID),
			DownloadURLs: validURLs, SHA1: sha1Val, Size: cfFile.FileLength, Optional: !entry.Required,
			SelectedByDefault: entry.Required, Available: len(validURLs) > 0, UnavailableReason: unavailReason,
		}
		if !plan.Available {
			unavailable = append(unavailable, plan)
		}
		if plan.Optional {
			optionalList = append(optionalList, plan)
		}
		files = append(files, plan)
	}

	ovPrefix := fallbackStr(strings.Trim(strings.ReplaceAll(mf.Overrides, "\\", "/"), "/"), "overrides")
	overrides, err := collectZipOverrides(zr, []string{ovPrefix + "/"}, nil)
	if err != nil {
		return ImportPreview{}, err
	}
	for _, ov := range overrides {
		lowPath := strings.ToLower(ov.RelPath)
		if seenPaths[lowPath] {
			conflicts = append(conflicts, ImportConflict{Code: "duplicate_path", Path: ov.RelPath, Message: "Override перекрывает файл манифеста: " + ov.RelPath})
		}
		seenPaths[lowPath] = true
		files = append(files, ov)
	}
	counts, estBytes := computeCounts(files, 0)
	if len(unavailable) > 0 {
		warnings = append(warnings, fmt.Sprintf("Недоступно для автозагрузки с CurseForge: %d файл(ов). Ознакомьтесь со списком перед установкой.", len(unavailable)))
	}
	return ImportPreview{
		Format: FormatCurseForge, SourceType: sourceType, SourceRef: sourceRef, SourceProjectID: srcProjID, SourceVersionID: srcVerID,
		ResolvedArchivePath: archivePath, Name: strings.TrimSpace(mf.Name), Author: strings.TrimSpace(mf.Author),
		Version: strings.TrimSpace(mf.Version), MCVersion: mcVer, Loader: loader, LoaderVersion: loaderVer,
		ManifestHash: sha256HexBytes(rawManifest), EstimatedBytes: estBytes, Counts: counts, Files: files,
		UnavailableFiles: unavailable, OptionalFiles: optionalList, Conflicts: conflicts, Warnings: warnings,
	}, nil
}

func (m *Manager) parseModHubArchive(zr *zip.ReadCloser, rawManifest []byte, archivePath, sourceType, sourceRef, srcProjID, srcVerID string) (ImportPreview, error) {
	var mf ModHubPackManifest
	if err := json.Unmarshal(rawManifest, &mf); err != nil {
		return ImportPreview{}, fmt.Errorf("%w (modhub.pack.json): %v", ErrInvalidManifest, err)
	}
	if mf.FormatVersion <= 0 || mf.FormatVersion > ModHubFormatVersionCurrent {
		return ImportPreview{}, fmt.Errorf("%w: версия формата .modhub = %d (поддерживается до v%d)", ErrUnsupportedFormatVer, mf.FormatVersion, ModHubFormatVersionCurrent)
	}
	if strings.TrimSpace(mf.Name) == "" {
		return ImportPreview{}, fmt.Errorf("%w: отсутствует название сборки в modhub.pack.json", ErrInvalidManifest)
	}
	zipEntries := map[string]int64{}
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() {
			zipEntries[strings.TrimPrefix(strings.ReplaceAll(f.Name, "\\", "/"), "/")] = int64(f.UncompressedSize64)
		}
	}
	var files, unavailable, optionalList []ModpackFilePlan
	var conflicts []ImportConflict
	seenPaths := map[string]bool{}
	for _, fe := range mf.Files {
		cleanRel, err := ValidateRelativeGamePath(fe.Path)
		if err != nil {
			return ImportPreview{}, err
		}
		if strings.EqualFold(fe.EnvClient, "unsupported") {
			continue
		}
		lowPath := strings.ToLower(cleanRel)
		if seenPaths[lowPath] {
			conflicts = append(conflicts, ImportConflict{Code: "duplicate_path", Path: cleanRel, Message: "Дублирующийся путь в .modhub: " + cleanRel})
			continue
		}
		seenPaths[lowPath] = true
		cat := fe.Category
		if cat == "" {
			cat = ClassifyComponentPath(cleanRel, 0, "")
		}
		susp, suspReason := CheckSuspiciousFile(cleanRel)
		plan := ModpackFilePlan{
			RelPath: cleanRel, Title: fallbackStr(fe.Title, path.Base(cleanRel)), Filename: path.Base(cleanRel),
			Category: cat, SourcePlatform: fe.SourcePlatform, SourceProjectID: fe.ProjectID, SourceFileID: fe.FileID,
			SHA1: strings.ToLower(strings.TrimSpace(fe.SHA1)), SHA256: strings.ToLower(strings.TrimSpace(fe.SHA256)), SHA512: strings.ToLower(strings.TrimSpace(fe.SHA512)),
			Size: fe.Size, Optional: fe.Optional, SelectedByDefault: !fe.Optional, Suspicious: susp, SuspiciousReason: suspReason,
		}
		archEntry := strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(fe.ArchiveEntry), "\\", "/"), "/")
		if archEntry != "" {
			if sz, exists := zipEntries[archEntry]; exists {
				plan.SourceKind = "archive_bundle"
				plan.ArchiveEntry = archEntry
				if plan.Size == 0 {
					plan.Size = sz
				}
				plan.Available = true
			}
		}
		if !plan.Available && strings.TrimSpace(fe.DownloadURL) != "" {
			if _, vErr := m.httpClient.ValidateLauncherURL(fe.DownloadURL); vErr == nil {
				plan.SourceKind = "download"
				plan.DownloadURLs = []string{strings.TrimSpace(fe.DownloadURL)}
				plan.Available = true
			} else {
				plan.SourceKind = "download"
				plan.UnavailableReason = "URL загрузки отклонён: " + vErr.Error()
			}
		}
		if !plan.Available {
			plan.UnavailableReason = fallbackStr(plan.UnavailableReason, "Файл отсутствует внутри архива и не имеет ссылки для скачивания")
			unavailable = append(unavailable, plan)
		}
		if plan.Optional {
			optionalList = append(optionalList, plan)
		}
		if susp {
			conflicts = append(conflicts, ImportConflict{Code: "suspicious_file", Path: cleanRel, Message: suspReason})
		}
		files = append(files, plan)
	}
	overrides, err := collectZipOverrides(zr, []string{"overrides/"}, nil)
	if err != nil {
		return ImportPreview{}, err
	}
	for _, ov := range overrides {
		if !seenPaths[strings.ToLower(ov.RelPath)] {
			seenPaths[strings.ToLower(ov.RelPath)] = true
			files = append(files, ov)
		}
	}
	counts, estBytes := computeCounts(files, 0)
	return ImportPreview{
		Format: FormatModHub, SourceType: sourceType, SourceRef: sourceRef, SourceProjectID: srcProjID, SourceVersionID: srcVerID,
		ResolvedArchivePath: archivePath, Name: strings.TrimSpace(mf.Name), Author: strings.TrimSpace(mf.Author),
		Description: strings.TrimSpace(mf.Description), Version: strings.TrimSpace(mf.Version),
		MCVersion: strings.TrimSpace(mf.MCVersion), Loader: strings.TrimSpace(mf.Loader), LoaderVersion: strings.TrimSpace(mf.LoaderVersion),
		ManifestHash: sha256HexBytes(rawManifest), EstimatedBytes: estBytes, Counts: counts, Files: files,
		UnavailableFiles: unavailable, OptionalFiles: optionalList, Conflicts: conflicts, ProfileSettings: mf.ProfileSettings,
	}, nil
}

func (m *Manager) buildPreviewFromSavedPack(pack domain.SavedPack) (ImportPreview, error) {
	var files, unavailable []ModpackFilePlan
	seen := map[string]bool{}
	for _, it := range pack.Items {
		folder := fallbackStr(it.Folder, "mods")
		if strings.TrimSpace(it.Filename) == "" {
			continue
		}
		cleanRel, err := ValidateRelativeGamePath(folder + "/" + filepath.Base(it.Filename))
		if err != nil || seen[strings.ToLower(cleanRel)] {
			continue
		}
		seen[strings.ToLower(cleanRel)] = true
		var validURLs []string
		reason := ""
		if strings.TrimSpace(it.DownloadURL) != "" {
			if _, err := m.httpClient.ValidateLauncherURL(it.DownloadURL); err == nil {
				validURLs = []string{strings.TrimSpace(it.DownloadURL)}
			} else {
				reason = "URL отклонён проверкой безопасности: " + err.Error()
			}
		} else {
			reason = "Отсутствует прямая ссылка для скачивания (возможно, отключена автором на CurseForge)"
		}
		plan := ModpackFilePlan{
			RelPath: cleanRel, Title: it.Title, Filename: path.Base(cleanRel),
			Category: ClassifyComponentPath(cleanRel, 0, ""), SourceKind: "download",
			SourcePlatform: it.Platform, SourceProjectID: parseCartItemProjectID(it),
			DownloadURLs: validURLs, Size: it.FileSize, SelectedByDefault: true, Available: len(validURLs) > 0, UnavailableReason: reason,
		}
		if !plan.Available {
			unavailable = append(unavailable, plan)
		}
		files = append(files, plan)
	}
	rawJSON, _ := json.Marshal(pack)
	counts, estBytes := computeCounts(files, 0)
	return ImportPreview{
		Format: FormatModHub, SourceType: "pack", SourceRef: pack.ID, Name: pack.Name, Version: "1.0.0",
		MCVersion: pack.MCVersion, Loader: fallbackStr(pack.Loader, "vanilla"), ManifestHash: sha256HexBytes(rawJSON),
		EstimatedBytes: estBytes, Counts: counts, Files: files, UnavailableFiles: unavailable,
	}, nil
}

func (m *Manager) buildPreviewFromInstance(inst instances.Instance) (ImportPreview, error) {
	tmpPath := filepath.Join(m.cacheDir, "inst_export_previews", fmt.Sprintf("%s.modhub", inst.ID))
	_, err := m.Export(context.Background(), ExportModpackRequest{
		SourceType: "instance", SourceID: inst.ID, Format: FormatModHub, OutputPath: tmpPath,
		Name: inst.Name, Description: inst.Description, Version: "1.0.0",
		IncludeMods: true, IncludeConfigs: true, IncludeResourcePacks: true, IncludeShaders: true, IncludeDatapacks: true,
	})
	if err != nil {
		return ImportPreview{}, err
	}
	return m.ParseArchivePreview(context.Background(), tmpPath, "instance", inst.ID, "", "")
}
