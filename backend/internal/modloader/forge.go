package modloader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"modhub/internal/downloads"
	"modhub/internal/mcversion"
	"modhub/internal/netx"
)

const (
	OfficialForgePromotionsURL = "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json"
	OfficialForgeMavenMetaURL  = "https://maven.minecraftforge.net/net/minecraftforge/forge/maven-metadata.xml"
	OfficialForgeMavenBaseURL  = "https://maven.minecraftforge.net/"
)

type ForgeProvider struct {
	promotionsURL string
	mavenMetaURL  string
	mavenBaseURL  string
	httpClient    *netx.HTTPClient
	dlMgr         *downloads.Manager
	mcMgr         *mcversion.Manager
	procRunner    ProcessRunner
}

func NewForgeProvider(httpClient *netx.HTTPClient, dlMgr *downloads.Manager, mcMgr *mcversion.Manager, procRunner ProcessRunner) *ForgeProvider {
	if procRunner == nil {
		procRunner = OSJavaProcessRunner{}
	}
	return &ForgeProvider{promotionsURL: OfficialForgePromotionsURL, mavenMetaURL: OfficialForgeMavenMetaURL, mavenBaseURL: OfficialForgeMavenBaseURL, httpClient: httpClient, dlMgr: dlMgr, mcMgr: mcMgr, procRunner: procRunner}
}

func (p *ForgeProvider) SetURLs(promotionsURL, mavenMetaURL, mavenBaseURL string) {
	if strings.TrimSpace(promotionsURL) != "" {
		p.promotionsURL = strings.TrimSpace(promotionsURL)
	}
	if strings.TrimSpace(mavenMetaURL) != "" {
		p.mavenMetaURL = strings.TrimSpace(mavenMetaURL)
	}
	if strings.TrimSpace(mavenBaseURL) != "" {
		p.mavenBaseURL = strings.TrimRight(strings.TrimSpace(mavenBaseURL), "/") + "/"
	}
}

func (p *ForgeProvider) SetProcessRunner(r ProcessRunner) {
	if r != nil {
		p.procRunner = r
	}
}

func (p *ForgeProvider) Type() LoaderType { return LoaderForge }

type forgePromotionsJSON struct {
	Promos map[string]string `json:"promos"`
}

func (p *ForgeProvider) ListVersions(ctx context.Context, mcVersion string) (LoaderVersionsResult, error) {
	mcVer := strings.TrimSpace(mcVersion)
	if err := mcversion.ValidateVersionID(mcVer); err != nil {
		return LoaderVersionsResult{}, err
	}
	var promos forgePromotionsJSON
	if _, err := p.httpClient.ValidateLauncherURL(p.promotionsURL); err == nil {
		_ = p.httpClient.WithContext(ctx).GetJSON(p.promotionsURL, nil, 600, &promos)
	}
	recVer := strings.TrimSpace(promos.Promos[mcVer+"-recommended"])
	latVer := strings.TrimSpace(promos.Promos[mcVer+"-latest"])

	xmlBody, err := p.httpClient.WithContext(ctx).GetText(p.mavenMetaURL, 600)
	var allMaven []string
	if err == nil {
		allMaven, _, _, _ = parseMavenMetadataVersions(xmlBody)
	}
	prefix := mcVer + "-"
	seen := map[string]struct{}{}
	matched := make([]LoaderVersionInfo, 0, 16)
	addVersion := func(shortVer, fullVer string) {
		shortVer, fullVer = strings.TrimSpace(shortVer), strings.TrimSpace(fullVer)
		if shortVer == "" || fullVer == "" {
			return
		}
		if _, dup := seen[fullVer]; dup {
			return
		}
		seen[fullVer] = struct{}{}
		instURL := fmt.Sprintf("%snet/minecraftforge/forge/%s/forge-%s-installer.jar", p.mavenBaseURL, fullVer, fullVer)
		matched = append(matched, LoaderVersionInfo{
			Loader: LoaderForge, MCVersion: mcVer, Version: shortVer, FullVersion: fullVer,
			Stable: shortVer == recVer || recVer == "", Recommended: recVer != "" && shortVer == recVer, Latest: latVer != "" && shortVer == latVer, InstallerURL: instURL,
		})
	}
	for i := len(allMaven) - 1; i >= 0; i-- {
		full := allMaven[i]
		if !strings.HasPrefix(full, prefix) {
			continue
		}
		addVersion(strings.TrimSuffix(strings.TrimPrefix(full, prefix), "-"+mcVer), full)
	}
	if recVer != "" {
		addVersion(recVer, mcVer+"-"+recVer)
	}
	if latVer != "" {
		addVersion(latVer, mcVer+"-"+latVer)
	}
	if len(matched) == 0 && err != nil && len(promos.Promos) == 0 {
		return LoaderVersionsResult{}, fmt.Errorf("не удалось получить список версий Forge для MC %s: %w", mcVer, err)
	}
	if len(matched) == 0 {
		return LoaderVersionsResult{Loader: LoaderForge, MCVersion: mcVer, Compatible: false, Versions: []LoaderVersionInfo{}, Message: fmt.Sprintf("Forge не поддерживает Minecraft %s", mcVer)}, nil
	}
	return LoaderVersionsResult{Loader: LoaderForge, MCVersion: mcVer, Compatible: true, Versions: matched}, nil
}

func (p *ForgeProvider) Install(ctx context.Context, req LoaderInstallContext) (LoaderInstallResult, error) {
	mcVer := strings.TrimSpace(req.MCVersion)
	if err := mcversion.ValidateVersionID(mcVer); err != nil {
		return LoaderInstallResult{}, err
	}
	lr, err := p.ListVersions(ctx, mcVer)
	if err != nil {
		return LoaderInstallResult{}, err
	}
	if !lr.Compatible || len(lr.Versions) == 0 {
		return LoaderInstallResult{}, fmt.Errorf("Forge не поддерживает Minecraft %s", mcVer)
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
			return LoaderInstallResult{}, fmt.Errorf("версия Forge %q несовместима с Minecraft %s", want, mcVer)
		}
	}
	req.LoaderVersion = chosen.Version
	if req.OnProgress != nil {
		req.OnProgress(mcversion.InstallProgress{InstanceID: req.InstanceID, MCVersion: mcVer, Loader: string(LoaderForge), LoaderVersion: chosen.Version, Stage: "modloader", Message: fmt.Sprintf("Загрузка установщика Forge %s...", chosen.FullVersion), Component: fmt.Sprintf("forge-%s-installer.jar", chosen.FullVersion), Progress: -1})
	}
	instDir := filepath.Join(p.mcMgr.SharedDir(), "installers")
	_ = os.MkdirAll(instDir, 0o755)
	installerPath := filepath.Join(instDir, fmt.Sprintf("forge-%s-installer.jar", chosen.FullVersion))
	tv, err := p.dlMgr.Enqueue(downloads.DownloadRequest{Name: fmt.Sprintf("Forge Installer %s", chosen.FullVersion), Category: "modloader", URL: chosen.InstallerURL, DestPath: installerPath, GroupID: req.GroupID})
	if err != nil {
		return LoaderInstallResult{}, fmt.Errorf("ошибка постановки установщика Forge в очередь: %w", err)
	}
	fin, err := p.dlMgr.Wait(ctx, tv.ID)
	if err != nil {
		p.dlMgr.CancelGroup(req.GroupID)
		return LoaderInstallResult{}, err
	}
	if fin.Status != downloads.StatusCompleted {
		return LoaderInstallResult{}, fmt.Errorf("не удалось скачать установщик Forge (%s): %s", fin.Status, fin.Error)
	}
	res, err := runInstallerPipeline(ctx, LoaderForge, p.mavenBaseURL, installerPath, req, p.dlMgr, p.mcMgr, p.procRunner)
	if err != nil {
		return LoaderInstallResult{}, err
	}
	if vRes := p.Verify(ctx, res.VersionID); !vRes.Valid {
		return LoaderInstallResult{}, fmt.Errorf("проверка установленного Forge не пройдена: %s", strings.Join(vRes.Errors, "; "))
	}
	return res, nil
}

func (p *ForgeProvider) Verify(_ context.Context, versionID string) LoaderVerifyResult {
	return verifyLoaderVersionJSON(p.mcMgr, versionID)
}
