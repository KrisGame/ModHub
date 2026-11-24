package modpacks

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"modhub/internal/domain"
	"modhub/internal/instances"
)

type exportCandidate struct {
	relPath, localPath, title, sourcePlatform, projectID, versionID, fileID, downloadURL, sha1, sha256, sha512 string
	category                                                                                                   ComponentCategory
	size                                                                                                       int64
	optional                                                                                                   bool
}

func (m *Manager) Export(ctx context.Context, req ExportModpackRequest) (ExportModpackResult, error) {
	format := ModpackFormat(strings.ToLower(strings.TrimSpace(string(req.Format))))
	if format != FormatMRPack && format != FormatCurseForge && format != FormatModHub {
		return ExportModpackResult{}, fmt.Errorf("неподдерживаемый формат экспорта %q (доступны mrpack, curseforge, modhub)", req.Format)
	}
	outPath := strings.TrimSpace(req.OutputPath)
	if outPath == "" {
		return ExportModpackResult{}, fmt.Errorf("не указан путь сохранения архива")
	}

	var name, author, desc, ver, mcVer, loader, loaderVer string
	var profSettings *ModHubPackProfileParams
	var cands []exportCandidate
	var warnings []string

	switch strings.ToLower(strings.TrimSpace(req.SourceType)) {
	case "instance":
		if m.instMgr == nil {
			return ExportModpackResult{}, fmt.Errorf("Instance Manager недоступен")
		}
		inst, err := m.instMgr.Get(req.SourceID)
		if err != nil {
			return ExportModpackResult{}, err
		}
		name, author, desc, ver = fallbackStr(req.Name, inst.Name), strings.TrimSpace(req.Author), fallbackStr(req.Description, inst.Description), fallbackStr(req.Version, "1.0.0")
		mcVer, loader, loaderVer = inst.MCVersion, string(inst.InstanceType), inst.LoaderVersion
		profSettings = &ModHubPackProfileParams{
			MemoryMinMB: inst.MemoryMinMB, MemoryMaxMB: inst.MemoryMaxMB, JVMArgs: inst.LaunchSettings.JVMArgs,
			WindowWidth: inst.LaunchSettings.WindowWidth, WindowHeight: inst.LaunchSettings.WindowHeight, Fullscreen: inst.LaunchSettings.Fullscreen,
		}
		cands, warnings = collectInstanceExportCandidates(inst, req, m.trackedFilesByInstance(inst.ID))

	case "pack":
		pk := m.findSavedPack(req.SourceID)
		if pk == nil {
			return ExportModpackResult{}, fmt.Errorf("сборка ModHub %q не найдена", req.SourceID)
		}
		name, author, desc, ver = fallbackStr(req.Name, pk.Name), strings.TrimSpace(req.Author), strings.TrimSpace(req.Description), fallbackStr(req.Version, "1.0.0")
		mcVer, loader = pk.MCVersion, fallbackStr(pk.Loader, "vanilla")
		cands = collectSavedPackExportCandidates(*pk, req)

	default:
		return ExportModpackResult{}, fmt.Errorf("неподдерживаемый источник экспорта %q", req.SourceType)
	}

	if len(cands) == 0 {
		return ExportModpackResult{}, fmt.Errorf("нет файлов для экспорта — выберите хотя бы одну категорию содержимого")
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return ExportModpackResult{}, err
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(outPath), "modhub-export-*.tmp")
	if err != nil {
		return ExportModpackResult{}, err
	}
	tmpPath := tmpFile.Name()
	cleanup := func() { _ = tmpFile.Close(); _ = os.Remove(tmpPath) }

	zw := zip.NewWriter(tmpFile)
	var writeErr error
	var fmtWarn []string
	switch format {
	case FormatMRPack:
		fmtWarn, writeErr = writeMRPackArchive(zw, name, desc, ver, mcVer, loader, loaderVer, cands)
	case FormatCurseForge:
		fmtWarn, writeErr = writeCurseForgeArchive(zw, name, author, ver, mcVer, loader, loaderVer, cands)
	case FormatModHub:
		fmtWarn, writeErr = writeModHubArchive(zw, name, author, desc, ver, mcVer, loader, loaderVer, profSettings, cands)
	}
	warnings = append(warnings, fmtWarn...)
	if writeErr != nil {
		_ = zw.Close()
		cleanup()
		return ExportModpackResult{}, writeErr
	}
	if err := zw.Close(); err != nil {
		cleanup()
		return ExportModpackResult{}, err
	}
	_ = tmpFile.Sync()
	fi, _ := tmpFile.Stat()
	var sizeBytes int64
	if fi != nil {
		sizeBytes = fi.Size()
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return ExportModpackResult{}, err
	}
	_ = os.Remove(outPath)
	if err := os.Rename(tmpPath, outPath); err != nil {
		_ = os.Remove(tmpPath)
		return ExportModpackResult{}, err
	}
	_ = ctx
	return ExportModpackResult{Format: format, OutputPath: outPath, TotalFiles: len(cands), SizeBytes: sizeBytes, Warnings: warnings}, nil
}

func (m *Manager) trackedFilesByInstance(instanceID string) map[string]domain.ModpackFileRecord {
	out := map[string]domain.ModpackFileRecord{}
	if m.store == nil || instanceID == "" {
		return out
	}
	mps, err := m.store.ListModpacks()
	if err != nil {
		return out
	}
	for _, mp := range mps {
		if mp.InstanceID != instanceID {
			continue
		}
		files, _ := m.store.ListModpackFiles(mp.ID)
		for _, f := range files {
			out[strings.ToLower(f.RelPath)] = f
		}
	}
	return out
}

func collectInstanceExportCandidates(inst instances.Instance, req ExportModpackRequest, tracked map[string]domain.ModpackFileRecord) ([]exportCandidate, []string) {
	var out []exportCandidate
	addDir := func(subdir string, cat ComponentCategory) {
		_ = filepath.WalkDir(filepath.Join(inst.GameDir, subdir), func(full string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			relFromGame, rerr := filepath.Rel(inst.GameDir, full)
			if rerr != nil {
				return nil
			}
			relSlash := filepath.ToSlash(relFromGame)
			if IsSensitiveSecretPath(relSlash) {
				return nil
			}
			cleanRel, verr := ValidateRelativeGamePath(relSlash)
			if verr != nil {
				return nil
			}
			hashes, herr := ComputeFileHashes(full)
			if herr != nil {
				return nil
			}
			cand := exportCandidate{relPath: cleanRel, localPath: full, category: cat, title: path.Base(cleanRel), sha1: hashes.SHA1, sha256: hashes.SHA256, sha512: hashes.SHA512, size: hashes.Size}
			if tr, ok := tracked[strings.ToLower(cleanRel)]; ok {
				cand.sourcePlatform, cand.projectID, cand.fileID, cand.downloadURL, cand.optional = tr.SourcePlatform, tr.SourceProjectID, tr.SourceFileID, tr.DownloadURL, tr.Optional
			}
			out = append(out, cand)
			return nil
		})
	}
	if req.IncludeMods {
		addDir("mods", CatMod)
	}
	if req.IncludeConfigs {
		addDir("config", CatConfig)
		addDir("defaultconfigs", CatConfig)
		for _, fn := range []string{"options.txt", "servers.dat"} {
			p := filepath.Join(inst.GameDir, fn)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				if h, herr := ComputeFileHashes(p); herr == nil {
					out = append(out, exportCandidate{relPath: fn, localPath: p, category: CatConfig, title: fn, sha1: h.SHA1, sha256: h.SHA256, sha512: h.SHA512, size: h.Size})
				}
			}
		}
	}
	if req.IncludeResourcePacks {
		addDir("resourcepacks", CatResourcePack)
	}
	if req.IncludeShaders {
		addDir("shaderpacks", CatShader)
	}
	if req.IncludeDatapacks {
		addDir("datapacks", CatDatapack)
	}
	if req.IncludeScreenshots {
		addDir("screenshots", CatUserFile)
	}
	if req.IncludeSaves {
		addDir("saves", CatUserFile)
	}
	if req.IncludeExtraFiles {
		entries, _ := os.ReadDir(inst.GameDir)
		skipTop := map[string]bool{"mods": true, "config": true, "defaultconfigs": true, "resourcepacks": true, "shaderpacks": true, "datapacks": true, "screenshots": true, "saves": true, "logs": true, "crash-reports": true, "versions": true, "libraries": true, "assets": true}
		for _, ent := range entries {
			low := strings.ToLower(ent.Name())
			if skipTop[low] || IsSensitiveSecretPath(ent.Name()) {
				continue
			}
			if ent.IsDir() {
				addDir(ent.Name(), CatUserFile)
			} else if low != "options.txt" && low != "servers.dat" {
				p := filepath.Join(inst.GameDir, ent.Name())
				if h, herr := ComputeFileHashes(p); herr == nil {
					out = append(out, exportCandidate{relPath: ent.Name(), localPath: p, category: CatUserFile, title: ent.Name(), sha1: h.SHA1, sha256: h.SHA256, sha512: h.SHA512, size: h.Size})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].relPath < out[j].relPath })
	return out, nil
}

func parseCartItemProjectID(it domain.CartItem) string {
	sk := strings.TrimSpace(it.SourceKey)
	if idx := strings.IndexByte(sk, ':'); idx >= 0 && idx+1 < len(sk) {
		return strings.TrimSpace(sk[idx+1:])
	}
	return sk
}

func collectSavedPackExportCandidates(pack domain.SavedPack, req ExportModpackRequest) []exportCandidate {
	var out []exportCandidate
	for _, it := range pack.Items {
		folder := fallbackStr(it.Folder, "mods")
		cat := ClassifyComponentPath(folder+"/"+it.Filename, 0, "")
		if ((cat == CatMod || cat == CatAddon) && !req.IncludeMods) ||
			(cat == CatResourcePack && !req.IncludeResourcePacks) ||
			(cat == CatShader && !req.IncludeShaders) ||
			(cat == CatDatapack && !req.IncludeDatapacks) {
			continue
		}
		cleanRel, err := ValidateRelativeGamePath(folder + "/" + filepath.Base(it.Filename))
		if err != nil {
			continue
		}
		pid := parseCartItemProjectID(it)
		out = append(out, exportCandidate{
			relPath: cleanRel, category: cat, title: it.Title, sourcePlatform: it.Platform,
			projectID: pid, downloadURL: it.DownloadURL, size: it.FileSize,
		})
	}
	return out
}

func writeZipJSON(zw *zip.Writer, name string, payload any) error {
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(raw)
	return err
}

func writeZipLocalFile(zw *zip.Writer, arcPath, localPath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := zw.Create(arcPath)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

func isAllowedModrinthCDN(rawURL string) bool {
	low := strings.ToLower(strings.TrimSpace(rawURL))
	return strings.HasPrefix(low, "https://cdn.modrinth.com/") || strings.HasPrefix(low, "https://cdn-raw.modrinth.com/")
}

func writeMRPackArchive(zw *zip.Writer, name, desc, ver, mcVer, loader, loaderVer string, cands []exportCandidate) ([]string, error) {
	deps := map[string]string{}
	if mcVer != "" {
		deps["minecraft"] = mcVer
	}
	switch strings.ToLower(loader) {
	case "fabric":
		deps["fabric-loader"] = fallbackStr(loaderVer, "latest")
	case "quilt":
		deps["quilt-loader"] = fallbackStr(loaderVer, "latest")
	case "forge":
		deps["forge"] = fallbackStr(loaderVer, "latest")
	case "neoforge":
		deps["neoforge"] = fallbackStr(loaderVer, "latest")
	}
	idx := ModrinthIndexJSON{FormatVersion: 1, Game: "minecraft", VersionID: ver, Name: name, Summary: desc, Files: make([]ModrinthIndexFile, 0, len(cands)), Dependencies: deps}
	var warnings []string
	overrideCount := 0
	for _, c := range cands {
		if c.downloadURL != "" && isAllowedModrinthCDN(c.downloadURL) && (c.sha1 != "" || c.sha512 != "") {
			hashes := map[string]string{}
			if c.sha1 != "" {
				hashes["sha1"] = c.sha1
			}
			if c.sha512 != "" {
				hashes["sha512"] = c.sha512
			}
			clientEnv := "required"
			if c.optional {
				clientEnv = "optional"
			}
			idx.Files = append(idx.Files, ModrinthIndexFile{Path: c.relPath, Hashes: hashes, Env: &ModrinthFileEnv{Client: clientEnv, Server: "optional"}, Downloads: []string{c.downloadURL}, FileSize: c.size})
			continue
		}
		if c.localPath != "" {
			if err := writeZipLocalFile(zw, "overrides/"+c.relPath, c.localPath); err != nil {
				return warnings, err
			}
			overrideCount++
		} else if c.downloadURL != "" {
			warnings = append(warnings, fmt.Sprintf("%s не имеет Modrinth CDN URL/хэшей и отсутствует локально — пропущен в .mrpack", c.relPath))
		}
	}
	if overrideCount > 0 {
		warnings = append(warnings, fmt.Sprintf("Файлов помещено в overrides/ (конфиги и локальные файлы без Modrinth CDN): %d", overrideCount))
	}
	return warnings, writeZipJSON(zw, "modrinth.index.json", idx)
}

func writeCurseForgeArchive(zw *zip.Writer, name, author, ver, mcVer, loader, loaderVer string, cands []exportCandidate) ([]string, error) {
	var loaders []CFManifestLoader
	if ld := strings.ToLower(strings.TrimSpace(loader)); ld != "" && ld != "vanilla" && ld != "empty" {
		loaders = append(loaders, CFManifestLoader{ID: ld + "-" + fallbackStr(loaderVer, "latest"), Primary: true})
	}
	mf := CurseForgeManifestJSON{Minecraft: CFManifestMinecraft{Version: mcVer, ModLoaders: loaders}, ManifestType: "minecraftModpack", ManifestVersion: 1, Name: name, Version: ver, Author: author, Files: make([]CFManifestFile, 0, len(cands)), Overrides: "overrides"}
	var warnings []string
	nonCFCount := 0
	for _, c := range cands {
		pid, errP := strconv.Atoi(strings.TrimSpace(c.projectID))
		fid, errF := strconv.Atoi(strings.TrimSpace(c.fileID))
		if strings.EqualFold(c.sourcePlatform, "curseforge") && errP == nil && errF == nil && pid > 0 && fid > 0 {
			mf.Files = append(mf.Files, CFManifestFile{ProjectID: pid, FileID: fid, Required: !c.optional})
			continue
		}
		if c.localPath != "" {
			if err := writeZipLocalFile(zw, "overrides/"+c.relPath, c.localPath); err != nil {
				return warnings, err
			}
			if c.category == CatMod || c.category == CatResourcePack || c.category == CatShader || c.category == CatDatapack {
				nonCFCount++
			}
		} else {
			nonCFCount++
			warnings = append(warnings, fmt.Sprintf("%s не имеет CurseForge projectID/fileID и не скачан локально — пропущен в manifest.json", c.relPath))
		}
	}
	if nonCFCount > 0 {
		warnings = append(warnings, fmt.Sprintf("Компонентов без CurseForge ID (помещены в overrides/ или пропущены): %d", nonCFCount))
	}
	return warnings, writeZipJSON(zw, "manifest.json", mf)
}

func writeModHubArchive(zw *zip.Writer, name, author, desc, ver, mcVer, loader, loaderVer string, profSettings *ModHubPackProfileParams, cands []exportCandidate) ([]string, error) {
	mf := ModHubPackManifest{
		FormatVersion: ModHubFormatVersionCurrent, Name: name, Author: author, Description: desc, Version: ver,
		MCVersion: mcVer, Loader: loader, LoaderVersion: loaderVer, CreatedAt: time.Now().Unix(),
		ProfileSettings: profSettings, Files: make([]ModHubPackFileEntry, 0, len(cands)), Overrides: make([]ModHubPackOverrideEntry, 0),
	}
	for _, c := range cands {
		if c.category == CatConfig || c.category == CatUserFile {
			if c.localPath != "" {
				arcEntry := "overrides/" + c.relPath
				if err := writeZipLocalFile(zw, arcEntry, c.localPath); err != nil {
					return nil, err
				}
				mf.Overrides = append(mf.Overrides, ModHubPackOverrideEntry{RelPath: c.relPath, ArchiveEntry: arcEntry, Category: c.category, SHA256: c.sha256, Size: c.size})
			}
			continue
		}
		envClient := "required"
		if c.optional {
			envClient = "optional"
		}
		entry := ModHubPackFileEntry{
			Path: c.relPath, Filename: path.Base(c.relPath), Title: c.title, Category: c.category,
			SourcePlatform: c.sourcePlatform, ProjectID: c.projectID, VersionID: c.versionID, FileID: c.fileID,
			DownloadURL: c.downloadURL, SHA1: c.sha1, SHA256: c.sha256, SHA512: c.sha512, Size: c.size, Optional: c.optional, EnvClient: envClient,
		}
		if c.localPath != "" {
			arcEntry := "files/" + c.relPath
			if err := writeZipLocalFile(zw, arcEntry, c.localPath); err != nil {
				return nil, err
			}
			entry.ArchiveEntry = arcEntry
		}
		mf.Files = append(mf.Files, entry)
	}
	return nil, writeZipJSON(zw, "modhub.pack.json", mf)
}

func fallbackStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return strings.TrimSpace(b)
}
