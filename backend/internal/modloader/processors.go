package modloader

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"modhub/internal/downloads"
	"modhub/internal/mcversion"
	javart "modhub/internal/runtime"
	"modhub/internal/storage"
)

type OSJavaProcessRunner struct{}

func (OSJavaProcessRunner) RunJavaProcessor(ctx context.Context, javaExe, workDir string, classpath []string, mainClass string, args []string) (string, error) {
	verifiedExe, err := javart.ResolveJavaExecutable(javaExe)
	if err != nil {
		return "", fmt.Errorf("некорректный исполняемый файл Java для процессора: %w", err)
	}
	if strings.TrimSpace(mainClass) == "" || strings.ContainsAny(mainClass, " \t\r\n;&|<>") {
		return "", fmt.Errorf("недопустимый Main-Class процессора: %q", mainClass)
	}
	pCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmdArgs := make([]string, 0, 4+len(args))
	cmdArgs = append(cmdArgs, "-cp", strings.Join(classpath, string(os.PathListSeparator)), mainClass)
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.CommandContext(pCtx, verifiedExe, cmdArgs...)
	cmd.Dir = workDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		snippet := strings.TrimSpace(out.String())
		if len(snippet) > 600 {
			snippet = snippet[len(snippet)-600:]
		}
		return snippet, fmt.Errorf("процессор %s завершился с ошибкой (%w): %s", mainClass, err, snippet)
	}
	return out.String(), nil
}

type InstallProfileJSON struct {
	Spec       int                        `json:"spec"`
	Profile    string                     `json:"profile"`
	Version    string                     `json:"version"`
	Minecraft  string                     `json:"minecraft"`
	JSONPath   string                     `json:"json"`
	Path       string                     `json:"path,omitempty"`
	Data       map[string]InstallDataPair `json:"data,omitempty"`
	Processors []InstallProcessorSpec     `json:"processors,omitempty"`
	Libraries  []mcversion.Library        `json:"libraries,omitempty"`
	Install    *struct {
		ProfileName string `json:"profileName"`
		Target      string `json:"target"`
		Path        string `json:"path"`
		Version     string `json:"version"`
		FilePath    string `json:"filePath"`
		Minecraft   string `json:"minecraft"`
	} `json:"install,omitempty"`
	VersionInfo *mcversion.VersionMetadata `json:"versionInfo,omitempty"`
}

type InstallDataPair struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

type InstallProcessorSpec struct {
	Sides     []string          `json:"sides,omitempty"`
	Jar       string            `json:"jar"`
	Classpath []string          `json:"classpath"`
	Args      []string          `json:"args"`
	Outputs   map[string]string `json:"outputs,omitempty"`
}

func runInstallerPipeline(
	ctx context.Context,
	loader LoaderType,
	defaultMavenBase string,
	installerJarPath string,
	req LoaderInstallContext,
	dlMgr *downloads.Manager,
	mcMgr *mcversion.Manager,
	procRunner ProcessRunner,
) (LoaderInstallResult, error) {
	if procRunner == nil {
		procRunner = OSJavaProcessRunner{}
	}
	zr, err := zip.OpenReader(installerJarPath)
	if err != nil {
		return LoaderInstallResult{}, fmt.Errorf("не удалось открыть установщик %s: %w", filepath.Base(installerJarPath), err)
	}
	defer zr.Close()

	profBytes, err := readZipEntryBytes(zr, "install_profile.json", 8*1024*1024)
	if err != nil {
		return LoaderInstallResult{}, fmt.Errorf("в установщике %s отсутствует install_profile.json: %w", loader, err)
	}
	var prof InstallProfileJSON
	if err := json.Unmarshal(profBytes, &prof); err != nil {
		return LoaderInstallResult{}, fmt.Errorf("некорректный install_profile.json: %w", err)
	}
	if prof.Spec == 0 && prof.VersionInfo != nil && prof.Install != nil {
		return runLegacyForgePipeline(ctx, zr, &prof, defaultMavenBase, req, dlMgr, mcMgr)
	}

	verJSONEntry := strings.TrimSpace(prof.JSONPath)
	if verJSONEntry == "" {
		verJSONEntry = "/version.json"
	}
	verBytes, err := readZipEntryBytes(zr, verJSONEntry, 8*1024*1024)
	if err != nil {
		return LoaderInstallResult{}, fmt.Errorf("в установщике %s отсутствует %s: %w", loader, verJSONEntry, err)
	}
	var verMeta mcversion.VersionMetadata
	if err := json.Unmarshal(verBytes, &verMeta); err != nil {
		return LoaderInstallResult{}, fmt.Errorf("некорректный version.json внутри установщика %s: %w", loader, err)
	}
	if strings.TrimSpace(verMeta.ID) == "" {
		verMeta.ID = strings.TrimSpace(prof.Version)
	}
	if err := mcversion.ValidateVersionID(verMeta.ID); err != nil {
		return LoaderInstallResult{}, err
	}
	if verMeta.InheritsFrom == "" {
		verMeta.InheritsFrom = req.MCVersion
	}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		norm := strings.ReplaceAll(f.Name, "\\", "/")
		if strings.HasPrefix(norm, "maven/") {
			if rel, rerr := sanitizeRelativeArchivePath(strings.TrimPrefix(norm, "maven/")); rerr == nil {
				dest := filepath.Join(mcMgr.LibrariesDir(), filepath.FromSlash(rel))
				if _, stErr := os.Stat(dest); os.IsNotExist(stErr) {
					_ = extractZipEntrySafe(zr, norm, dest, 128*1024*1024)
				}
			}
		}
	}

	fillMavenURLs := func(libs []mcversion.Library) []mcversion.Library {
		out := make([]mcversion.Library, len(libs))
		for i, l := range libs {
			if l.Downloads == nil && strings.TrimSpace(l.URL) == "" {
				l.URL = defaultMavenBase
			}
			out[i] = l
		}
		return out
	}
	instMeta := &mcversion.VersionMetadata{Libraries: fillMavenURLs(prof.Libraries)}
	verMeta.Libraries = fillMavenURLs(verMeta.Libraries)
	plat := mcMgr.Platform()
	allLibs := append(mcversion.FilterLibrariesForPlatform(instMeta, plat), mcversion.FilterLibrariesForPlatform(&verMeta, plat)...)
	if err := downloadLibrariesBatch(ctx, dlMgr, mcMgr.LibrariesDir(), allLibs, req.GroupID, "modloader", "modloader", req.InstanceID, req.MCVersion, string(loader), req.LoaderVersion, req.OnProgress); err != nil {
		return LoaderInstallResult{}, err
	}

	workDir, err := os.MkdirTemp("", fmt.Sprintf("modhub-%s-proc-*", loader))
	if err != nil {
		return LoaderInstallResult{}, fmt.Errorf("не удалось создать рабочую папку процессоров: %w", err)
	}
	defer os.RemoveAll(workDir)

	dataVars := map[string]string{
		"SIDE": "client", "MINECRAFT_JAR": mcMgr.ClientJARPath(req.MCVersion),
		"ROOT": mcMgr.SharedDir(), "INSTALLER": installerJarPath, "LIBRARY_DIR": mcMgr.LibrariesDir(),
	}
	for key, pair := range prof.Data {
		val := strings.TrimSpace(pair.Client)
		if val == "" {
			continue
		}
		if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
			if rel, rerr := mcversion.MavenCoordToRelPath(val[1 : len(val)-1]); rerr == nil {
				dataVars[key] = filepath.Join(mcMgr.LibrariesDir(), filepath.FromSlash(rel))
			}
		} else if strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'") && len(val) >= 2 {
			dataVars[key] = val[1 : len(val)-1]
		} else if strings.HasPrefix(val, "/") {
			cleanRel, rerr := sanitizeRelativeArchivePath(val)
			if rerr != nil {
				return LoaderInstallResult{}, rerr
			}
			extracted := filepath.Join(workDir, filepath.FromSlash(cleanRel))
			if err := extractZipEntrySafe(zr, val, extracted, 128*1024*1024); err != nil {
				return LoaderInstallResult{}, fmt.Errorf("ошибка извлечения данных установщика %s: %w", val, err)
			}
			dataVars[key] = extracted
		} else {
			dataVars[key] = val
		}
	}

	resolveToken := func(tok string) (string, error) {
		s := strings.TrimSpace(tok)
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			rel, err := mcversion.MavenCoordToRelPath(s[1 : len(s)-1])
			if err != nil {
				return "", err
			}
			return filepath.Join(mcMgr.LibrariesDir(), filepath.FromSlash(rel)), nil
		}
		if strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'") && len(s) >= 2 {
			return s[1 : len(s)-1], nil
		}
		for k, v := range dataVars {
			s = strings.ReplaceAll(s, "{"+k+"}", v)
		}
		return s, nil
	}

	var clientProcs []InstallProcessorSpec
	for _, proc := range prof.Processors {
		if processorAppliesToClient(proc.Sides) {
			clientProcs = append(clientProcs, proc)
		}
	}

	for idx, proc := range clientProcs {
		if ctx.Err() != nil {
			return LoaderInstallResult{}, ctx.Err()
		}
		allOutputsValid := len(proc.Outputs) > 0
		resolvedOutputs := make(map[string]string, len(proc.Outputs))
		for outKey, outHashExpr := range proc.Outputs {
			outPath, err := resolveToken(outKey)
			if err != nil {
				return LoaderInstallResult{}, err
			}
			expHash, err := resolveToken(outHashExpr)
			if err != nil {
				return LoaderInstallResult{}, err
			}
			expHash = strings.Trim(strings.TrimSpace(expHash), "'\"")
			resolvedOutputs[outPath] = expHash
			if !mcversion.FileMatchesSHA1(outPath, expHash, 0) {
				allOutputsValid = false
			}
		}
		if allOutputsValid {
			continue
		}
		if strings.TrimSpace(req.JavaExePath) == "" {
			return LoaderInstallResult{}, fmt.Errorf("файлы Minecraft и библиотеки загрузчика скачаны; для завершения процессоров %s установите Java в Настройках → Java Runtime", loader)
		}
		jarRel, err := mcversion.MavenCoordToRelPath(proc.Jar)
		if err != nil {
			return LoaderInstallResult{}, fmt.Errorf("некорректный JAR процессора %s: %w", proc.Jar, err)
		}
		procJarPath := filepath.Join(mcMgr.LibrariesDir(), filepath.FromSlash(jarRel))
		if _, err := os.Stat(procJarPath); err != nil {
			return LoaderInstallResult{}, fmt.Errorf("отсутствует JAR процессора %s", jarRel)
		}
		mainClass, err := readJarMainClass(procJarPath)
		if err != nil {
			return LoaderInstallResult{}, err
		}
		cpList := make([]string, 0, len(proc.Classpath)+1)
		cpList = append(cpList, procJarPath)
		for _, cpCoord := range proc.Classpath {
			cpRel, err := mcversion.MavenCoordToRelPath(cpCoord)
			if err != nil {
				return LoaderInstallResult{}, err
			}
			cpList = append(cpList, filepath.Join(mcMgr.LibrariesDir(), filepath.FromSlash(cpRel)))
		}
		resolvedArgs := make([]string, 0, len(proc.Args))
		for _, a := range proc.Args {
			ra, err := resolveToken(a)
			if err != nil {
				return LoaderInstallResult{}, err
			}
			resolvedArgs = append(resolvedArgs, ra)
		}
		if req.OnProgress != nil {
			req.OnProgress(mcversion.InstallProgress{
				InstanceID: req.InstanceID, MCVersion: req.MCVersion, Loader: string(loader), LoaderVersion: req.LoaderVersion,
				Stage: "processors", Message: fmt.Sprintf("Выполнение процессора %s (%d из %d): %s", loader, idx+1, len(clientProcs), filepath.Base(procJarPath)),
				Component: filepath.Base(procJarPath), CompletedFiles: idx, TotalFiles: len(clientProcs), Progress: float64(idx) * 100 / float64(len(clientProcs)),
			})
		}
		if _, err := procRunner.RunJavaProcessor(ctx, req.JavaExePath, workDir, cpList, mainClass, resolvedArgs); err != nil {
			return LoaderInstallResult{}, err
		}
		for outPath, expHash := range resolvedOutputs {
			if !mcversion.FileMatchesSHA1(outPath, expHash, 0) {
				return LoaderInstallResult{}, fmt.Errorf("выходной файл процессора %s не создан или не совпал по SHA-1", filepath.Base(outPath))
			}
		}
	}

	verDir := mcMgr.VersionDir(verMeta.ID)
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		return LoaderInstallResult{}, err
	}
	if err := storage.WriteJSONFileAtomic(verDir, verMeta.ID+".json", verMeta, 0o644); err != nil {
		return LoaderInstallResult{}, fmt.Errorf("не удалось сохранить %s.json: %w", verMeta.ID, err)
	}
	javaMajor := 0
	if verMeta.JavaVersion != nil {
		javaMajor = verMeta.JavaVersion.MajorVersion
	}
	return LoaderInstallResult{VersionID: verMeta.ID, LoaderVersion: req.LoaderVersion, MetadataPath: mcMgr.VersionJSONPath(verMeta.ID), JavaMajor: javaMajor, Libraries: len(verMeta.Libraries)}, nil
}

func runLegacyForgePipeline(ctx context.Context, zr *zip.ReadCloser, prof *InstallProfileJSON, defaultMavenBase string, req LoaderInstallContext, dlMgr *downloads.Manager, mcMgr *mcversion.Manager) (LoaderInstallResult, error) {
	verMeta := *prof.VersionInfo
	if strings.TrimSpace(verMeta.ID) == "" {
		verMeta.ID = strings.TrimSpace(prof.Install.Target)
	}
	if err := mcversion.ValidateVersionID(verMeta.ID); err != nil {
		return LoaderInstallResult{}, err
	}
	if verMeta.InheritsFrom == "" {
		verMeta.InheritsFrom = req.MCVersion
	}
	if prof.Install.FilePath != "" && prof.Install.Path != "" {
		rel, err := mcversion.MavenCoordToRelPath(prof.Install.Path)
		if err != nil {
			return LoaderInstallResult{}, err
		}
		if err := extractZipEntrySafe(zr, prof.Install.FilePath, filepath.Join(mcMgr.LibrariesDir(), filepath.FromSlash(rel)), 128*1024*1024); err != nil {
			return LoaderInstallResult{}, fmt.Errorf("ошибка извлечения legacy Forge universal JAR: %w", err)
		}
	}
	for i := range verMeta.Libraries {
		if verMeta.Libraries[i].Downloads == nil && strings.TrimSpace(verMeta.Libraries[i].URL) == "" {
			verMeta.Libraries[i].URL = defaultMavenBase
		}
	}
	libs := mcversion.FilterLibrariesForPlatform(&verMeta, mcMgr.Platform())
	if err := downloadLibrariesBatch(ctx, dlMgr, mcMgr.LibrariesDir(), libs, req.GroupID, "modloader", "modloader", req.InstanceID, req.MCVersion, string(LoaderForge), req.LoaderVersion, req.OnProgress); err != nil {
		return LoaderInstallResult{}, err
	}
	verDir := mcMgr.VersionDir(verMeta.ID)
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		return LoaderInstallResult{}, err
	}
	if err := storage.WriteJSONFileAtomic(verDir, verMeta.ID+".json", verMeta, 0o644); err != nil {
		return LoaderInstallResult{}, err
	}
	return LoaderInstallResult{VersionID: verMeta.ID, LoaderVersion: req.LoaderVersion, MetadataPath: mcMgr.VersionJSONPath(verMeta.ID), Libraries: len(libs)}, nil
}

func processorAppliesToClient(sides []string) bool {
	if len(sides) == 0 {
		return true
	}
	for _, s := range sides {
		if strings.EqualFold(strings.TrimSpace(s), "client") {
			return true
		}
	}
	return false
}
