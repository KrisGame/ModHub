package modloader

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"modhub/internal/downloads"
	"modhub/internal/mcversion"
	"modhub/internal/netx"
	"modhub/internal/storage"
)

const (
	OfficialQuiltMetaURL  = "https://meta.quiltmc.org/v3"
	OfficialQuiltMavenURL = "https://maven.quiltmc.org/repository/release/"
)

type QuiltProvider struct {
	metaBaseURL string
	mavenBase   string
	httpClient  *netx.HTTPClient
	dlMgr       *downloads.Manager
	mcMgr       *mcversion.Manager
}

func NewQuiltProvider(httpClient *netx.HTTPClient, dlMgr *downloads.Manager, mcMgr *mcversion.Manager) *QuiltProvider {
	return &QuiltProvider{metaBaseURL: OfficialQuiltMetaURL, mavenBase: OfficialQuiltMavenURL, httpClient: httpClient, dlMgr: dlMgr, mcMgr: mcMgr}
}

func (p *QuiltProvider) SetBaseURLs(metaBase, mavenBase string) {
	if strings.TrimSpace(metaBase) != "" {
		p.metaBaseURL = strings.TrimRight(strings.TrimSpace(metaBase), "/")
	}
	if strings.TrimSpace(mavenBase) != "" {
		p.mavenBase = strings.TrimRight(strings.TrimSpace(mavenBase), "/") + "/"
	}
}

func (p *QuiltProvider) Type() LoaderType { return LoaderQuilt }

type quiltLoaderEntry struct {
	Loader struct {
		Version string `json:"version"`
		Maven   string `json:"maven"`
	} `json:"loader"`
}

func isQuiltVersionStable(v string) bool {
	low := strings.ToLower(v)
	return !strings.Contains(low, "beta") && !strings.Contains(low, "alpha") && !strings.Contains(low, "rc") && !strings.Contains(low, "pre")
}

func (p *QuiltProvider) ListVersions(ctx context.Context, mcVersion string) (LoaderVersionsResult, error) {
	mcVer := strings.TrimSpace(mcVersion)
	if err := mcversion.ValidateVersionID(mcVer); err != nil {
		return LoaderVersionsResult{}, err
	}
	endpoint := fmt.Sprintf("%s/versions/loader/%s", p.metaBaseURL, url.PathEscape(mcVer))
	if _, err := p.httpClient.ValidateLauncherURL(endpoint); err != nil {
		return LoaderVersionsResult{}, err
	}
	var raw []quiltLoaderEntry
	if err := p.httpClient.WithContext(ctx).GetJSON(endpoint, nil, 600, &raw); err != nil {
		return LoaderVersionsResult{}, fmt.Errorf("не удалось получить версии Quilt для MC %s: %w", mcVer, err)
	}
	if len(raw) == 0 {
		return LoaderVersionsResult{Loader: LoaderQuilt, MCVersion: mcVer, Compatible: false, Versions: []LoaderVersionInfo{}, Message: fmt.Sprintf("Quilt не поддерживает Minecraft %s", mcVer)}, nil
	}
	out := make([]LoaderVersionInfo, 0, len(raw))
	foundRec := false
	for i, item := range raw {
		v := strings.TrimSpace(item.Loader.Version)
		if v == "" {
			continue
		}
		stable := isQuiltVersionStable(v)
		rec := false
		if stable && !foundRec {
			rec = true
			foundRec = true
		}
		profURL := fmt.Sprintf("%s/versions/loader/%s/%s/profile/json", p.metaBaseURL, url.PathEscape(mcVer), url.PathEscape(v))
		out = append(out, LoaderVersionInfo{Loader: LoaderQuilt, MCVersion: mcVer, Version: v, Stable: stable, Recommended: rec, Latest: i == 0, MetadataURL: profURL})
	}
	return LoaderVersionsResult{Loader: LoaderQuilt, MCVersion: mcVer, Compatible: len(out) > 0, Versions: out}, nil
}

func (p *QuiltProvider) Install(ctx context.Context, req LoaderInstallContext) (LoaderInstallResult, error) {
	mcVer := strings.TrimSpace(req.MCVersion)
	loaderVer := strings.TrimSpace(req.LoaderVersion)
	if err := mcversion.ValidateVersionID(mcVer); err != nil {
		return LoaderInstallResult{}, err
	}
	if loaderVer == "" {
		lr, err := p.ListVersions(ctx, mcVer)
		if err != nil {
			return LoaderInstallResult{}, err
		}
		if !lr.Compatible || len(lr.Versions) == 0 {
			return LoaderInstallResult{}, fmt.Errorf("Quilt не поддерживает Minecraft %s", mcVer)
		}
		loaderVer = lr.Versions[0].Version
		for _, v := range lr.Versions {
			if v.Recommended {
				loaderVer = v.Version
				break
			}
		}
	}
	profURL := fmt.Sprintf("%s/versions/loader/%s/%s/profile/json", p.metaBaseURL, url.PathEscape(mcVer), url.PathEscape(loaderVer))
	if _, err := p.httpClient.ValidateLauncherURL(profURL); err != nil {
		return LoaderInstallResult{}, err
	}
	if req.OnProgress != nil {
		req.OnProgress(mcversion.InstallProgress{InstanceID: req.InstanceID, MCVersion: mcVer, Loader: string(LoaderQuilt), LoaderVersion: loaderVer, Stage: "modloader", Message: fmt.Sprintf("Получение профиля Quilt %s для MC %s...", loaderVer, mcVer), Progress: -1})
	}
	var profile mcversion.VersionMetadata
	if err := p.httpClient.WithContext(ctx).GetJSON(profURL, nil, 0, &profile); err != nil {
		return LoaderInstallResult{}, fmt.Errorf("не удалось загрузить профиль Quilt %s: %w", loaderVer, err)
	}
	if strings.TrimSpace(profile.ID) == "" {
		profile.ID = fmt.Sprintf("quilt-loader-%s-%s", loaderVer, mcVer)
	}
	if err := mcversion.ValidateVersionID(profile.ID); err != nil {
		return LoaderInstallResult{}, err
	}
	if strings.TrimSpace(profile.MainClass) == "" {
		return LoaderInstallResult{}, fmt.Errorf("профиль Quilt %s некорректен (отсутствует mainClass)", loaderVer)
	}
	if profile.InheritsFrom == "" {
		profile.InheritsFrom = mcVer
	}
	for i := range profile.Libraries {
		if profile.Libraries[i].Downloads == nil && strings.TrimSpace(profile.Libraries[i].URL) == "" {
			profile.Libraries[i].URL = p.mavenBase
		}
	}
	libs := mcversion.FilterLibrariesForPlatform(&profile, p.mcMgr.Platform())
	if err := downloadLibrariesBatch(ctx, p.dlMgr, p.mcMgr.LibrariesDir(), libs, req.GroupID, "modloader", "modloader", req.InstanceID, mcVer, string(LoaderQuilt), loaderVer, req.OnProgress); err != nil {
		return LoaderInstallResult{}, err
	}
	verDir := p.mcMgr.VersionDir(profile.ID)
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		return LoaderInstallResult{}, err
	}
	if err := storage.WriteJSONFileAtomic(verDir, profile.ID+".json", profile, 0o644); err != nil {
		return LoaderInstallResult{}, fmt.Errorf("не удалось сохранить метаданные Quilt: %w", err)
	}
	if vRes := p.Verify(ctx, profile.ID); !vRes.Valid {
		return LoaderInstallResult{}, fmt.Errorf("проверка установленного Quilt не пройдена: %s", strings.Join(vRes.Errors, "; "))
	}
	javaMajor := 0
	if profile.JavaVersion != nil {
		javaMajor = profile.JavaVersion.MajorVersion
	}
	return LoaderInstallResult{VersionID: profile.ID, LoaderVersion: loaderVer, MetadataPath: p.mcMgr.VersionJSONPath(profile.ID), JavaMajor: javaMajor, Libraries: len(libs)}, nil
}

func (p *QuiltProvider) Verify(_ context.Context, versionID string) LoaderVerifyResult {
	return verifyLoaderVersionJSON(p.mcMgr, versionID)
}
