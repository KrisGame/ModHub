package modloader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"modhub/internal/domain"
	"modhub/internal/downloads"
	"modhub/internal/mcversion"
	"modhub/internal/netx"
)

const (
	OfficialNeoForgeMavenMetaURL       = "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml"
	OfficialNeoForgeLegacyMavenMetaURL = "https://maven.neoforged.net/releases/net/neoforged/forge/maven-metadata.xml"
	OfficialNeoForgeMavenBaseURL       = "https://maven.neoforged.net/releases/"
)

type NeoForgeProvider struct {
	mavenMetaURL       string
	legacyMavenMetaURL string
	mavenBaseURL       string
	httpClient         *netx.HTTPClient
	dlMgr              *downloads.Manager
	mcMgr              *mcversion.Manager
	procRunner         ProcessRunner
}

func NewNeoForgeProvider(httpClient *netx.HTTPClient, dlMgr *downloads.Manager, mcMgr *mcversion.Manager, procRunner ProcessRunner) *NeoForgeProvider {
	if procRunner == nil {
		procRunner = OSJavaProcessRunner{}
	}
	return &NeoForgeProvider{mavenMetaURL: OfficialNeoForgeMavenMetaURL, legacyMavenMetaURL: OfficialNeoForgeLegacyMavenMetaURL, mavenBaseURL: OfficialNeoForgeMavenBaseURL, httpClient: httpClient, dlMgr: dlMgr, mcMgr: mcMgr, procRunner: procRunner}
}

func (p *NeoForgeProvider) SetURLs(mavenMetaURL, legacyMavenMetaURL, mavenBaseURL string) {
	if strings.TrimSpace(mavenMetaURL) != "" {
		p.mavenMetaURL = strings.TrimSpace(mavenMetaURL)
	}
	if strings.TrimSpace(legacyMavenMetaURL) != "" {
		p.legacyMavenMetaURL = strings.TrimSpace(legacyMavenMetaURL)
	}
	if strings.TrimSpace(mavenBaseURL) != "" {
		p.mavenBaseURL = strings.TrimRight(strings.TrimSpace(mavenBaseURL), "/") + "/"
	}
}

func (p *NeoForgeProvider) SetProcessRunner(r ProcessRunner) {
	if r != nil {
		p.procRunner = r
	}
}

func (p *NeoForgeProvider) Type() LoaderType { return LoaderNeoForge }

func NeoForgeMatchesMCVersion(nfVer, mcVersion string) bool {
	nf, mc := strings.TrimSpace(nfVer), strings.TrimSpace(mcVersion)
	if nf == "" || mc == "" {
		return false
	}
	if strings.HasPrefix(nf, "0.") {
		return strings.Contains(strings.ToLower(nf), strings.ToLower(mc))
	}
	t := domain.ParseMCTuple(mc)
	if t[0] >= 26 {
		return strings.HasPrefix(nf, fmt.Sprintf("%d.%d.", t[0], t[1]))
	}
	if t[0] == 1 && t[1] >= 20 {
		parts := strings.Split(mc, ".")
		patch := 0
		if len(parts) >= 3 {
			patch, _ = strconv.Atoi(parts[2])
		}
		return strings.HasPrefix(nf, fmt.Sprintf("%d.%d.", t[1], patch))
	}
	return false
}

func isNeoForgeStable(v string) bool {
	low := strings.ToLower(v)
	return !strings.Contains(low, "beta") && !strings.Contains(low, "alpha") && !strings.Contains(low, "snapshot")
}

func (p *NeoForgeProvider) ListVersions(ctx context.Context, mcVersion string) (LoaderVersionsResult, error) {
	mcVer := strings.TrimSpace(mcVersion)
	if err := mcversion.ValidateVersionID(mcVer); err != nil {
		return LoaderVersionsResult{}, err
	}
	if mcVer == "1.20.1" {
		xmlBody, err := p.httpClient.WithContext(ctx).GetText(p.legacyMavenMetaURL, 600)
		if err != nil {
			return LoaderVersionsResult{}, fmt.Errorf("не удалось получить версии NeoForge 1.20.1: %w", err)
		}
		allVers, _, _, err := parseMavenMetadataVersions(xmlBody)
		if err != nil {
			return LoaderVersionsResult{}, err
		}
		out := make([]LoaderVersionInfo, 0, len(allVers))
		for i := len(allVers) - 1; i >= 0; i-- {
			full := allVers[i]
			if !strings.HasPrefix(full, "1.20.1-") {
				continue
			}
			short := strings.TrimPrefix(full, "1.20.1-")
			instURL := fmt.Sprintf("%snet/neoforged/forge/%s/forge-%s-installer.jar", p.mavenBaseURL, full, full)
			out = append(out, LoaderVersionInfo{Loader: LoaderNeoForge, MCVersion: mcVer, Version: short, FullVersion: full, Stable: true, Recommended: len(out) == 0, Latest: len(out) == 0, InstallerURL: instURL})
		}
		return LoaderVersionsResult{Loader: LoaderNeoForge, MCVersion: mcVer, Compatible: len(out) > 0, Versions: out}, nil
	}

	xmlBody, err := p.httpClient.WithContext(ctx).GetText(p.mavenMetaURL, 600)
	if err != nil {
		return LoaderVersionsResult{}, fmt.Errorf("не удалось получить версии NeoForge для MC %s: %w", mcVer, err)
	}
	allVers, _, _, err := parseMavenMetadataVersions(xmlBody)
	if err != nil {
		return LoaderVersionsResult{}, err
	}
	out := make([]LoaderVersionInfo, 0, 24)
	foundRec := false
	for i := len(allVers) - 1; i >= 0; i-- {
		v := allVers[i]
		if !NeoForgeMatchesMCVersion(v, mcVer) {
			continue
		}
		stable := isNeoForgeStable(v)
		rec := false
		if stable && !foundRec {
			rec = true
			foundRec = true
		}
		instURL := fmt.Sprintf("%snet/neoforged/neoforge/%s/neoforge-%s-installer.jar", p.mavenBaseURL, v, v)
		out = append(out, LoaderVersionInfo{Loader: LoaderNeoForge, MCVersion: mcVer, Version: v, FullVersion: v, Stable: stable, Recommended: rec, Latest: len(out) == 0, InstallerURL: instURL})
	}
	if len(out) > 0 && !foundRec {
		out[0].Recommended = true
	}
	if len(out) == 0 {
		return LoaderVersionsResult{Loader: LoaderNeoForge, MCVersion: mcVer, Compatible: false, Versions: []LoaderVersionInfo{}, Message: fmt.Sprintf("NeoForge не поддерживает Minecraft %s", mcVer)}, nil
	}
	return LoaderVersionsResult{Loader: LoaderNeoForge, MCVersion: mcVer, Compatible: true, Versions: out}, nil
}

func (p *NeoForgeProvider) Install(ctx context.Context, req LoaderInstallContext) (LoaderInstallResult, error) {
	mcVer := strings.TrimSpace(req.MCVersion)
	if err := mcversion.ValidateVersionID(mcVer); err != nil {
		return LoaderInstallResult{}, err
	}
	lr, err := p.ListVersions(ctx, mcVer)
	if err != nil {
		return LoaderInstallResult{}, err
	}
	if !lr.Compatible || len(lr.Versions) == 0 {
		return LoaderInstallResult{}, fmt.Errorf("NeoForge не поддерживает Minecraft %s", mcVer)
	}
	chosen := lr.Versions[0]
	if strings.TrimSpace(req.LoaderVersion) == "" {
		for _, v := range lr.Versions {
			if v.Recommended {
				chosen = v
				break
			}
		}
	} else {
		found := false
		want := strings.TrimSpace(req.LoaderVersion)
		for _, v := range lr.Versions {
			if v.Version == want || v.FullVersion == want {
				chosen = v
				found = true
				break
			}
		}
		if !found {
			return LoaderInstallResult{}, fmt.Errorf("версия NeoForge %q несовместима с Minecraft %s", want, mcVer)
		}
	}
	req.LoaderVersion = chosen.Version
	if req.OnProgress != nil {
		req.OnProgress(mcversion.InstallProgress{InstanceID: req.InstanceID, MCVersion: mcVer, Loader: string(LoaderNeoForge), LoaderVersion: chosen.Version, Stage: "modloader", Message: fmt.Sprintf("Загрузка установщика NeoForge %s...", chosen.FullVersion), Component: fmt.Sprintf("neoforge-%s-installer.jar", chosen.FullVersion), Progress: -1})
	}
	instDir := filepath.Join(p.mcMgr.SharedDir(), "installers")
	_ = os.MkdirAll(instDir, 0o755)
	installerPath := filepath.Join(instDir, fmt.Sprintf("neoforge-%s-installer.jar", chosen.FullVersion))
	tv, err := p.dlMgr.Enqueue(downloads.DownloadRequest{Name: fmt.Sprintf("NeoForge Installer %s", chosen.FullVersion), Category: "modloader", URL: chosen.InstallerURL, DestPath: installerPath, GroupID: req.GroupID})
	if err != nil {
		return LoaderInstallResult{}, fmt.Errorf("ошибка постановки установщика NeoForge в очередь: %w", err)
	}
	fin, err := p.dlMgr.Wait(ctx, tv.ID)
	if err != nil {
		p.dlMgr.CancelGroup(req.GroupID)
		return LoaderInstallResult{}, err
	}
	if fin.Status != downloads.StatusCompleted {
		return LoaderInstallResult{}, fmt.Errorf("не удалось скачать установщик NeoForge (%s): %s", fin.Status, fin.Error)
	}
	res, err := runInstallerPipeline(ctx, LoaderNeoForge, p.mavenBaseURL, installerPath, req, p.dlMgr, p.mcMgr, p.procRunner)
	if err != nil {
		return LoaderInstallResult{}, err
	}
	if vRes := p.Verify(ctx, res.VersionID); !vRes.Valid {
		return LoaderInstallResult{}, fmt.Errorf("проверка установленного NeoForge не пройдена: %s", strings.Join(vRes.Errors, "; "))
	}
	return res, nil
}

func (p *NeoForgeProvider) Verify(_ context.Context, versionID string) LoaderVerifyResult {
	return verifyLoaderVersionJSON(p.mcMgr, versionID)
}
