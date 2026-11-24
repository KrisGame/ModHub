package desktop

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"modhub/internal/accounts"
	"modhub/internal/app"
	"modhub/internal/domain"
	"modhub/internal/downloads"
	"modhub/internal/events"
	"modhub/internal/instances"
	"modhub/internal/launcher"
	"modhub/internal/mcversion"
	"modhub/internal/modloader"
	"modhub/internal/modpacks"
	"modhub/internal/packio"
	javart "modhub/internal/runtime"
)

const AppVersion = "0.9.0"

type RuntimeAdapter interface {
	Emit(ctx context.Context, eventName string, data any)
	OpenDirectoryDialog(ctx context.Context, title string) (string, error)
	OpenJavaFileDialog(ctx context.Context, title string) (string, error)
	OpenModpackFileDialog(ctx context.Context, title string) (string, error)
	SaveModpackDialog(ctx context.Context, title, defaultFilename, extPattern, extLabel string) (string, error)
	SaveFileDialog(ctx context.Context, title, defaultFilename string) (string, error)
	BrowserOpenURL(ctx context.Context, rawURL string)
	WindowMinimise(ctx context.Context)
	WindowHide(ctx context.Context)
	WindowShow(ctx context.Context)
	WindowToggleMaximise(ctx context.Context)
	Quit(ctx context.Context)
}

type noopRuntime struct{}

func (noopRuntime) Emit(context.Context, string, any) {}
func (noopRuntime) OpenDirectoryDialog(context.Context, string) (string, error) {
	return "", fmt.Errorf("диалог выбора папки недоступен вне desktop-режима")
}
func (noopRuntime) OpenJavaFileDialog(context.Context, string) (string, error) {
	return "", fmt.Errorf("диалог выбора файла недоступен вне desktop-режима")
}
func (noopRuntime) OpenModpackFileDialog(context.Context, string) (string, error) {
	return "", fmt.Errorf("диалог выбора архива недоступен вне desktop-режима")
}
func (noopRuntime) SaveModpackDialog(context.Context, string, string, string, string) (string, error) {
	return "", fmt.Errorf("диалог сохранения архива недоступен вне desktop-режима")
}
func (noopRuntime) SaveFileDialog(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("диалог сохранения файла недоступен вне desktop-режима")
}
func (noopRuntime) BrowserOpenURL(context.Context, string) {}
func (noopRuntime) WindowMinimise(context.Context)         {}
func (noopRuntime) WindowHide(context.Context)             {}
func (noopRuntime) WindowShow(context.Context)             {}
func (noopRuntime) WindowToggleMaximise(context.Context)   {}
func (noopRuntime) Quit(context.Context)                   {}

// AppInfo exposes desktop environment metadata to the React UI.
type AppInfo struct {
	Version       string      `json:"version"`
	Runtime       string      `json:"runtime"` // wails-desktop | web
	OS            string      `json:"os"`
	Arch          string      `json:"arch"`
	SchemaVersion int         `json:"schema_version"`
	DataDir       DataDirInfo `json:"data_dir"`
}

// ConfigUpdateRequest is the Wails-bound payload for saving settings.
type ConfigUpdateRequest struct {
	CurseForgeAPIKey       *string `json:"curseforge_api_key,omitempty"`
	MergeThreshold         *int    `json:"merge_threshold,omitempty"`
	ProxyURL               *string `json:"proxy_url,omitempty"`
	AutoInstallMediumDeps  *bool   `json:"auto_install_medium_deps,omitempty"`
	MaxConcurrentDownloads *int    `json:"max_concurrent_downloads,omitempty"`
	MicrosoftClientID      *string `json:"microsoft_client_id,omitempty"`
}

// JavaRecommendationResponse bundles compatibility requirements and the best installed match.
type JavaRecommendationResponse struct {
	Requirement javart.RequirementResult `json:"requirement"`
	Recommended *javart.JavaRuntime      `json:"recommended,omitempty"`
}

// TestCFResult is returned by TestCurseForgeKey.
type TestCFResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// SavePackZIPResult describes the outcome of streaming a modpack directly to a chosen file on disk.
type SavePackZIPResult struct {
	Cancelled     bool     `json:"cancelled"`
	Path          string   `json:"path,omitempty"`
	Downloaded    int      `json:"downloaded"`
	Total         int      `json:"total"`
	Failed        int      `json:"failed"`
	Status        string   `json:"status"` // complete | partial | cancelled
	FailedReasons []string `json:"failed_reasons,omitempty"`
}

// ScanInstalledResult describes the outcome of scanning a local mods folder via native dialog.
type ScanInstalledResult struct {
	Cancelled    bool           `json:"cancelled"`
	Folder       string         `json:"folder,omitempty"`
	ScannedFiles int            `json:"scanned_files"`
	Installed    map[string]any `json:"installed"`
	MatchedCount int            `json:"matched_count"`
}

var reSafeZipStem = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// Bridge exposes curated Go methods to the React frontend via Wails v2 Bindings.
// Every method delegates directly to app.Container (zero duplicated business logic).
type Bridge struct {
	container *app.Container
	dataInfo  DataDirInfo
	rt        RuntimeAdapter

	mu      sync.Mutex
	ctx     context.Context
	unsubEv func()
}

func NewBridge(container *app.Container, dataInfo DataDirInfo, rt RuntimeAdapter) *Bridge {
	if rt == nil {
		rt = noopRuntime{}
	}
	return &Bridge{
		container: container,
		dataInfo:  dataInfo,
		rt:        rt,
	}
}

func (b *Bridge) activeCtx() context.Context {
	b.mu.Lock()
	ctx := b.ctx
	b.mu.Unlock()
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

// Startup is wired to Wails options.App.OnStartup.
func (b *Bridge) Startup(ctx context.Context) {
	b.mu.Lock()
	b.ctx = ctx
	if b.container != nil && b.container.Accounts != nil {
		// Ensure Microsoft OAuth opens the real OS system browser via Wails runtime (never embedded WebView).
		b.container.Accounts.SetBrowserOpener(func(rawURL string) error {
			b.rt.BrowserOpenURL(ctx, rawURL)
			return nil
		})
	}
	if b.unsubEv != nil {
		b.unsubEv()
	}
	b.unsubEv = b.container.Events.SubscribeAll(func(ev events.Event) {
		wirePayload := map[string]any{
			"name":      string(ev.Name),
			"timestamp": ev.Timestamp.UnixMilli(),
			"payload":   ev.Payload,
		}
		b.rt.Emit(ctx, "modhub:event", wirePayload)
		b.rt.Emit(ctx, string(ev.Name), ev.Payload)
	})
	b.mu.Unlock()
}

// BeforeClose is wired to Wails options.App.OnBeforeClose.
func (b *Bridge) BeforeClose(_ context.Context) bool {
	return false
}

// Shutdown is wired to Wails options.App.OnShutdown.
func (b *Bridge) Shutdown(_ context.Context) {
	b.mu.Lock()
	if b.unsubEv != nil {
		b.unsubEv()
		b.unsubEv = nil
	}
	b.mu.Unlock()
}

// --- Desktop / OS Bindings ---

func (b *Bridge) GetAppInfo() AppInfo {
	schemaVer := 0
	if b.container != nil && b.container.DB != nil {
		schemaVer = b.container.DB.SchemaVersion()
	}
	return AppInfo{
		Version:       AppVersion,
		Runtime:       "wails-desktop",
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		SchemaVersion: schemaVer,
		DataDir:       b.dataInfo,
	}
}

func (b *Bridge) OpenDataDir() error {
	dir := b.dataInfo.Dir
	if dir == "" && b.container != nil {
		dir = b.container.DataDir
	}
	if dir == "" {
		return fmt.Errorf("директория данных не определена")
	}
	return openPathInOS(dir)
}

func (b *Bridge) OpenExternalURL(rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("разрешены только HTTP/HTTPS-ссылки")
	}
	b.rt.BrowserOpenURL(b.activeCtx(), u.String())
	return nil
}

// ScanInstalledFolder opens a native OS directory picker, computes SHA-1 hashes of .jar/.zip files in Go,
// and resolves installed mods via Modrinth API without browser webkitdirectory limitations.
func (b *Bridge) ScanInstalledFolder(mcVersion, loader string) (ScanInstalledResult, error) {
	folder, err := b.rt.OpenDirectoryDialog(b.activeCtx(), "Выберите папку mods (или с .jar/.zip файлами)")
	if err != nil {
		return ScanInstalledResult{}, err
	}
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return ScanInstalledResult{Cancelled: true, Installed: map[string]any{}}, nil
	}
	return b.ScanInstalledFolderAt(folder, mcVersion, loader)
}

// ScanInstalledFolderAt scans a specific directory path (reusable by tests & future Instance Manager).
func (b *Bridge) ScanInstalledFolderAt(folder, mcVersion, loader string) (ScanInstalledResult, error) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return ScanInstalledResult{}, fmt.Errorf("не удалось прочитать папку: %w", err)
	}
	hashes := make([]string, 0, 64)
	scanned := 0
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		low := strings.ToLower(ent.Name())
		if !strings.HasSuffix(low, ".jar") && !strings.HasSuffix(low, ".zip") {
			continue
		}
		if scanned >= 400 {
			break
		}
		h, err := sha1File(filepath.Join(folder, ent.Name()))
		if err != nil {
			continue
		}
		scanned++
		hashes = append(hashes, h)
	}
	if scanned == 0 {
		return ScanInstalledResult{Folder: folder, ScannedFiles: 0, Installed: map[string]any{}, MatchedCount: 0}, nil
	}
	payload, err := b.container.CheckInstalledHashes(b.activeCtx(), hashes, mcVersion, loader)
	if err != nil {
		return ScanInstalledResult{}, err
	}
	inst, _ := payload["installed"].(map[string]any)
	if inst == nil {
		inst = map[string]any{}
	}
	matched, _ := payload["matched_count"].(int)
	return ScanInstalledResult{
		Folder:       folder,
		ScannedFiles: scanned,
		Installed:    inst,
		MatchedCount: matched,
	}, nil
}

func sha1File(path string) (string, error) {
	f, err := os.Open(path)
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

// SavePackZIP opens a native Save File dialog and streams the modpack archive directly to disk,
// avoiding passing large binary blobs over IPC or browser memory.
func (b *Bridge) SavePackZIP(packName string, items []domain.CartItem) (SavePackZIPResult, error) {
	if err := packio.ValidatePackItems(items, b.container.HTTP.ValidateDLURL); err != nil {
		return SavePackZIPResult{}, err
	}
	stem := strings.Trim(reSafeZipStem.ReplaceAllString(strings.TrimSpace(packName), "-"), "-")
	if stem == "" {
		stem = "modhub-pack"
	}
	targetPath, err := b.rt.SaveFileDialog(b.activeCtx(), "Сохранить сборку (.ZIP)", stem+".zip")
	if err != nil {
		return SavePackZIPResult{}, err
	}
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return SavePackZIPResult{Cancelled: true, Status: "cancelled"}, nil
	}
	if !strings.HasSuffix(strings.ToLower(targetPath), ".zip") {
		targetPath += ".zip"
	}
	return b.WritePackZIPToPath(targetPath, items)
}

// WritePackZIPToPath atomically streams a validated pack ZIP directly into targetPath.
func (b *Bridge) WritePackZIPToPath(targetPath string, items []domain.CartItem) (SavePackZIPResult, error) {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return SavePackZIPResult{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(targetPath), "modhub-pack-*.tmp")
	if err != nil {
		return SavePackZIPResult{}, fmt.Errorf("не удалось создать временный файл: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	summary, err := packio.StreamPackZIPWithReport(b.activeCtx(), tmp, b.container.HTTP.WithContext(b.activeCtx()), items)
	if err != nil {
		cleanup()
		return SavePackZIPResult{}, err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return SavePackZIPResult{}, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return SavePackZIPResult{}, err
	}
	_ = os.Remove(targetPath)
	if err := os.Rename(tmpName, targetPath); err != nil {
		_ = os.Remove(tmpName)
		return SavePackZIPResult{}, err
	}

	status := "complete"
	if len(summary.FailedFiles) > 0 {
		status = "partial"
	}
	return SavePackZIPResult{
		Path:          targetPath,
		Downloaded:    summary.WrittenCount,
		Total:         summary.TotalRequested,
		Failed:        len(summary.FailedFiles),
		Status:        status,
		FailedReasons: summary.FailedFiles,
	}, nil
}

// --- Core Container Bindings (direct Go <-> React IPC, zero business logic duplication) ---

func (b *Bridge) GetConfig() map[string]any {
	return b.container.PublicConfigPayload()
}

func (b *Bridge) UpdateConfig(req ConfigUpdateRequest) (map[string]any, error) {
	return b.container.UpdateConfig(app.ConfigUpdatePatch{
		CurseForgeAPIKey:       req.CurseForgeAPIKey,
		MergeThreshold:         req.MergeThreshold,
		ProxyURL:               req.ProxyURL,
		AutoInstallMediumDeps:  req.AutoInstallMediumDeps,
		MaxConcurrentDownloads: req.MaxConcurrentDownloads,
		MicrosoftClientID:      req.MicrosoftClientID,
	})
}

func (b *Bridge) TestCurseForgeKey(candidateKey string) TestCFResult {
	key := strings.TrimSpace(candidateKey)
	if key == "" {
		key = b.container.GetConfig().CurseForgeAPIKey
	}
	if err := b.container.TestCurseForgeKey(key); err != nil {
		return TestCFResult{OK: false, Error: err.Error()}
	}
	return TestCFResult{OK: true, Message: "Ключ CurseForge действителен"}
}

func (b *Bridge) CheckNetwork() domain.NetworkCheckResult {
	return b.container.CheckNetwork()
}

func (b *Bridge) ClearAllCaches() {
	b.container.ClearAllCaches()
}

func (b *Bridge) GetIndexStatus() map[string]any {
	return b.container.IndexStatusPayload()
}

func (b *Bridge) TriggerIndexSync(full bool) map[string]any {
	started := b.container.TriggerBackgroundSync(full)
	return map[string]any{
		"ok":      true,
		"started": started,
		"status":  b.container.IndexStatusPayload(),
	}
}

func (b *Bridge) GetMCVersions() map[string]any {
	return b.container.GetMCVersions()
}

func (b *Bridge) GetFilters(ct string) map[string]any {
	return b.container.GetFilters(ct)
}

func (b *Bridge) ResolveURL(rawURL string) (domain.ProjectCard, error) {
	return b.container.ResolveURL(b.activeCtx(), rawURL)
}

func (b *Bridge) GetPacks() []domain.SavedPack {
	return b.container.GetPacks()
}

func (b *Bridge) SavePack(pack domain.SavedPack) ([]domain.SavedPack, error) {
	return b.container.SavePack(pack)
}

func (b *Bridge) DeletePack(id string) ([]domain.SavedPack, error) {
	return b.container.DeletePack(id)
}

func (b *Bridge) GetOverrides() domain.OverridesData {
	return b.container.GetOverrides()
}

func (b *Bridge) AddMergeOverride(sourceKeys []string) (domain.OverridesData, error) {
	return b.container.AddMergeOverride(sourceKeys)
}

func (b *Bridge) AddSplitOverride(sourceKeys []string) (domain.OverridesData, error) {
	return b.container.AddSplitOverride(sourceKeys)
}

func (b *Bridge) DeleteOverride(kind string, index int) (domain.OverridesData, error) {
	return b.container.DeleteOverride(kind, index)
}

func (b *Bridge) ResetOverrides() (domain.OverridesData, error) {
	return b.container.ResetOverrides()
}

// --- Centralized Download Manager Bindings ---

func (b *Bridge) GetDownloads() downloads.Snapshot {
	return b.container.Downloads.GetSnapshot()
}

func (b *Bridge) AddDownload(req downloads.DownloadRequest) (downloads.TaskView, error) {
	return b.container.Downloads.Enqueue(req)
}

func (b *Bridge) PauseDownload(id string) (downloads.TaskView, error) {
	return b.container.Downloads.Pause(id)
}

func (b *Bridge) ResumeDownload(id string) (downloads.TaskView, error) {
	return b.container.Downloads.Resume(id)
}

func (b *Bridge) CancelDownload(id string) (downloads.TaskView, error) {
	return b.container.Downloads.Cancel(id)
}

func (b *Bridge) RetryDownload(id string) (downloads.TaskView, error) {
	return b.container.Downloads.Retry(id)
}

func (b *Bridge) RemoveDownload(id string) error {
	return b.container.Downloads.Remove(id)
}

func (b *Bridge) ClearFinishedDownloads() int {
	return b.container.Downloads.ClearFinished()
}

func (b *Bridge) SetMaxConcurrentDownloads(n int) (map[string]any, error) {
	return b.container.UpdateConfig(app.ConfigUpdatePatch{MaxConcurrentDownloads: &n})
}

// --- Java Runtime Manager Bindings ---

func (b *Bridge) GetJavaRuntimes(refreshRemote bool) javart.StateSnapshot {
	return b.container.Java.GetSnapshot(b.activeCtx(), refreshRemote)
}

func (b *Bridge) ScanJavaRuntimes() javart.StateSnapshot {
	b.container.Java.ScanInstalled(b.activeCtx())
	_, _ = b.container.Java.RefreshAvailable(b.activeCtx())
	return b.container.Java.GetSnapshot(b.activeCtx(), false)
}

func (b *Bridge) AddCustomJavaPath(path string) (javart.JavaRuntime, error) {
	return b.container.Java.AddCustomPath(b.activeCtx(), path)
}

func (b *Bridge) BrowseCustomJavaPath() (javart.JavaRuntime, error) {
	p, err := b.rt.OpenJavaFileDialog(b.activeCtx(), "Выберите исполняемый файл Java (java.exe)")
	if err != nil {
		return javart.JavaRuntime{}, err
	}
	p = strings.TrimSpace(p)
	if p == "" {
		return javart.JavaRuntime{}, nil
	}
	return b.container.Java.AddCustomPath(b.activeCtx(), p)
}

func (b *Bridge) VerifyJavaRuntime(id string) (javart.JavaRuntime, error) {
	return b.container.Java.VerifyRuntime(b.activeCtx(), id)
}

func (b *Bridge) InstallJavaRuntime(component string) error {
	return b.container.Java.InstallRuntimeAsync(component)
}

func (b *Bridge) CancelJavaInstall(component string) {
	b.container.Java.CancelInstall(component)
}

func (b *Bridge) ReinstallJavaRuntime(id string) error {
	return b.container.Java.ReinstallRuntimeAsync(id)
}

func (b *Bridge) RemoveJavaRuntime(id string) error {
	return b.container.Java.RemoveRuntime(id)
}

func (b *Bridge) OpenJavaRuntimeDir(id string) error {
	var dir string
	var err error
	if strings.TrimSpace(id) == "" {
		dir = b.container.Java.RuntimesDir()
		_ = os.MkdirAll(dir, 0o755)
	} else {
		dir, err = b.container.Java.GetRuntimeDir(id)
		if err != nil {
			return err
		}
	}
	return openPathInOS(dir)
}

func (b *Bridge) RecommendJavaForMC(mcVersion, loader string) JavaRecommendationResponse {
	req, rt := b.container.Java.RecommendForMC(javart.RequirementInput{
		MCVersion: mcVersion,
		Loader:    loader,
	})
	return JavaRecommendationResponse{Requirement: req, Recommended: rt}
}

// --- Instance Manager Bindings ---

func (b *Bridge) GetInstances() instances.ListSnapshot {
	return b.container.Instances.List()
}

func (b *Bridge) GetInstance(id string) (instances.Instance, error) {
	return b.container.Instances.Get(id)
}

func (b *Bridge) CreateInstance(req instances.CreateInstanceRequest) (instances.Instance, error) {
	return b.container.CreateInstanceAndPrepare(req)
}

func (b *Bridge) UpdateInstance(req instances.UpdateInstanceRequest) (instances.Instance, error) {
	return b.container.Instances.Update(req)
}

func (b *Bridge) DuplicateInstance(req instances.DuplicateInstanceRequest) (instances.Instance, error) {
	return b.container.Instances.Duplicate(req)
}

func (b *Bridge) DeleteInstance(req instances.DeleteInstanceRequest) error {
	return b.container.Instances.Delete(req)
}

func (b *Bridge) VerifyInstance(id string) (instances.IntegrityReport, error) {
	return b.container.Instances.Verify(id)
}

func (b *Bridge) RepairInstance(id string) (instances.IntegrityReport, error) {
	return b.container.Instances.Repair(id)
}

func (b *Bridge) GetInstanceFiles(id string) (instances.InstanceFilesSummary, error) {
	return b.container.Instances.GetFilesSummary(id)
}

func (b *Bridge) ListInstanceMods(id string) ([]instances.InstalledModRef, error) {
	return b.container.Mods.List(id)
}

func (b *Bridge) SetInstanceModEnabled(req instances.ModActionRequest) ([]instances.InstalledModRef, error) {
	if req.Enabled == nil {
		return nil, instances.ErrInvalidInput
	}
	return b.container.Mods.SetEnabled(req.InstanceID, req.Filename, *req.Enabled)
}

func (b *Bridge) DeleteInstanceMod(req instances.ModActionRequest) ([]instances.InstalledModRef, error) {
	return b.container.Mods.Delete(req.InstanceID, req.Filename)
}

func (b *Bridge) InstallInstanceMod(req instances.InstallModRequest) (instances.InstallModResult, error) {
	return b.container.Mods.Install(req)
}

func (b *Bridge) SetAllInstanceModsEnabled(instanceID string, enabled bool) ([]instances.InstalledModRef, error) {
	return b.container.Mods.SetAllEnabled(instanceID, enabled)
}

func (b *Bridge) DeleteMultipleInstanceMods(req instances.BatchModActionRequest) ([]instances.InstalledModRef, error) {
	return b.container.Mods.DeleteBatch(req.InstanceID, req.Filenames)
}

func (b *Bridge) CheckInstanceModUpdates(instanceID string) ([]instances.ModUpdateInfo, error) {
	return b.container.Mods.CheckUpdates(instanceID)
}

func (b *Bridge) ApplyInstanceModUpdate(req instances.ApplyModUpdateRequest) (instances.InstallModResult, error) {
	return b.container.Mods.ApplyUpdate(req)
}

// --- Instance Content Management (Resourcepacks, Shaderpacks, Worlds) ---

func (b *Bridge) ListInstanceResourcePacks(id string) ([]instances.ResourcePackRef, error) {
	return b.container.Content.ListResourcePacks(id)
}

func (b *Bridge) SetInstanceResourcePackEnabled(req instances.ContentActionRequest) ([]instances.ResourcePackRef, error) {
	if req.Enabled == nil {
		return nil, instances.ErrInvalidInput
	}
	return b.container.Content.SetResourcePackEnabled(req.InstanceID, req.Filename, *req.Enabled)
}

func (b *Bridge) DeleteInstanceResourcePack(req instances.ContentActionRequest) ([]instances.ResourcePackRef, error) {
	return b.container.Content.DeleteResourcePack(req.InstanceID, req.Filename)
}

func (b *Bridge) ListInstanceShaderPacks(id string) ([]instances.ShaderPackRef, error) {
	return b.container.Content.ListShaderPacks(id)
}

func (b *Bridge) SetInstanceShaderPackEnabled(req instances.ContentActionRequest) ([]instances.ShaderPackRef, error) {
	if req.Enabled == nil {
		return nil, instances.ErrInvalidInput
	}
	return b.container.Content.SetShaderPackEnabled(req.InstanceID, req.Filename, *req.Enabled)
}

func (b *Bridge) DeleteInstanceShaderPack(req instances.ContentActionRequest) ([]instances.ShaderPackRef, error) {
	return b.container.Content.DeleteShaderPack(req.InstanceID, req.Filename)
}

func (b *Bridge) ListInstanceWorlds(id string) ([]instances.WorldRef, error) {
	return b.container.Content.ListWorlds(id)
}

func (b *Bridge) ExportInstanceWorld(id, folder string) (instances.ExportWorldResult, error) {
	return b.container.Content.ExportWorld(id, folder)
}

func (b *Bridge) OpenInstanceDir(id, subdir string) error {
	target, err := b.container.Instances.ResolveOpenDir(id, subdir)
	if err != nil {
		return err
	}
	return openPathInOS(target)
}

func (b *Bridge) BrowseInstanceFolder() (string, error) {
	return b.rt.OpenDirectoryDialog(b.activeCtx(), "Выберите директорию для игрового профиля")
}

// --- Minecraft Version Manager & Modloader Manager Bindings ---

func (b *Bridge) GetMinecraftManifest(forceRefresh bool) (mcversion.VersionManifestResult, error) {
	return b.container.MCVersions.FetchManifest(b.activeCtx(), forceRefresh)
}

func (b *Bridge) GetModloaderVersions(loader string, mcVersion string) (modloader.LoaderVersionsResult, error) {
	return b.container.Modloaders.ListLoaderVersions(b.activeCtx(), modloader.LoaderType(loader), mcVersion)
}

func (b *Bridge) InstallProfileVersion(req modloader.InstallProfileVersionRequest) error {
	return b.container.Modloaders.InstallForInstanceAsync(req)
}

func (b *Bridge) CancelProfileInstall(instanceID string) bool {
	return b.container.Modloaders.CancelInstall(instanceID)
}

func (b *Bridge) GetActiveProfileInstall(instanceID string) *mcversion.InstallProgress {
	return b.container.Modloaders.GetActiveInstall(instanceID)
}

func (b *Bridge) ListProfileInstalledVersions(instanceID string) ([]modloader.InstalledVersion, error) {
	return b.container.Modloaders.ListInstalledVersions(instanceID)
}

func (b *Bridge) ActivateProfileVersion(instanceID, installedID string, createBackup bool) (modloader.InstalledVersion, error) {
	return b.container.Modloaders.ActivateInstalledVersion(instanceID, installedID, createBackup)
}

func (b *Bridge) VerifyProfileVersion(instanceID, installedID string) (modloader.FullVerificationReport, error) {
	return b.container.Modloaders.VerifyInstalledVersion(b.activeCtx(), instanceID, installedID)
}

func (b *Bridge) DeleteProfileInstalledVersion(instanceID, installedID string) error {
	return b.container.Modloaders.DeleteInstalledVersion(instanceID, installedID)
}

// --- Microsoft Account Manager Bindings (token-free views only!) ---

func (b *Bridge) GetAccounts() accounts.AccountsSnapshot {
	return b.container.Accounts.GetSnapshot()
}

func (b *Bridge) StartMicrosoftLogin(reauthAccountID string) error {
	return b.container.Accounts.StartMicrosoftLoginAsync(reauthAccountID)
}

func (b *Bridge) AddOfflineAccount(username string) (accounts.AccountView, error) {
	return b.container.Accounts.AddOfflineAccount(username)
}

func (b *Bridge) CancelMicrosoftLogin() bool {
	return b.container.Accounts.CancelAuth()
}

func (b *Bridge) SetActiveAccount(id string) (accounts.AccountView, error) {
	return b.container.Accounts.SetActive(id)
}

func (b *Bridge) RefreshAccountAuth(id string, force bool) (accounts.AccountView, error) {
	return b.container.Accounts.RefreshAccount(b.activeCtx(), id, force)
}

func (b *Bridge) CheckAccountLicense(id string) (accounts.AccountView, error) {
	return b.container.Accounts.CheckAccountLicense(b.activeCtx(), id)
}

func (b *Bridge) SignOutAccount(id string) (accounts.AccountView, error) {
	return b.container.Accounts.SignOut(id)
}

func (b *Bridge) DeleteAccount(id string) error {
	return b.container.Accounts.DeleteAccount(id)
}

func (b *Bridge) UpdateAccountSkin(id, skinURL, skinVariant, capeURL, capeAlias string) (accounts.AccountView, error) {
	return b.container.Accounts.UpdateAccountSkin(id, skinURL, skinVariant, capeURL, capeAlias)
}

// --- Game Launcher & Process Monitoring Bindings ---

func (b *Bridge) GetLauncherSnapshot() launcher.LauncherSnapshot {
	return b.container.Launcher.GetSnapshot()
}

func (b *Bridge) PreflightLaunch(req launcher.LaunchRequest) launcher.PreflightReport {
	return b.container.Launcher.Preflight(b.activeCtx(), req)
}

func (b *Bridge) LaunchInstance(req launcher.LaunchRequest) (launcher.GameSession, error) {
	return b.container.Launcher.LaunchAsync(req)
}

func (b *Bridge) StopInstance(instanceID string, force bool) error {
	return b.container.Launcher.Stop(instanceID, force)
}

func (b *Bridge) GetInstanceSession(instanceID string) *launcher.GameSession {
	return b.container.Launcher.GetInstanceSession(instanceID)
}

func (b *Bridge) GetInstanceSessionHistory(instanceID string, limit int) []launcher.GameSession {
	return b.container.Launcher.GetSessionHistory(instanceID, limit)
}

func (b *Bridge) GetInstanceLogs(instanceID string, limit int) []launcher.LogLine {
	return b.container.Launcher.GetInstanceLogs(instanceID, limit)
}

func (b *Bridge) ListInstanceCrashReports(instanceID string) ([]launcher.CrashReportInfo, error) {
	return b.container.Launcher.ListCrashReports(instanceID)
}

func (b *Bridge) AnalyzeInstanceCrash(instanceID string) (*launcher.CrashDiagnosis, error) {
	return b.container.Launcher.AnalyzeInstanceCrash(instanceID)
}

func (b *Bridge) OpenInstanceLogFile(instanceID string) error {
	sess := b.container.Launcher.GetInstanceSession(instanceID)
	if sess != nil && strings.TrimSpace(sess.LogFilePath) != "" {
		if _, err := os.Stat(sess.LogFilePath); err == nil {
			return openPathInOS(sess.LogFilePath)
		}
	}
	return b.OpenInstanceDir(instanceID, "logs")
}

func (b *Bridge) ListModpacks() ([]modpacks.ModpackView, error) {
	return b.container.Modpacks.ListModpacks()
}
func (b *Bridge) DeleteModpackRecord(id string) error {
	return b.container.Modpacks.DeleteModpackRecord(id)
}
func (b *Bridge) BrowseModpackArchive() (string, error) {
	return b.rt.OpenModpackFileDialog(b.activeCtx(), "Выберите архив сборки (.mrpack, .zip, .modhub)")
}
func (b *Bridge) PreviewModpackImport(req modpacks.PreviewImportRequest) (modpacks.ImportPreview, error) {
	return b.container.Modpacks.PreviewImport(b.activeCtx(), req)
}
func (b *Bridge) ExecuteModpackImport(req modpacks.ExecuteImportRequest) error {
	return b.container.Modpacks.ExecuteImportAsync(req)
}
func (b *Bridge) GetActiveModpackProgress() *modpacks.ImportProgress {
	return b.container.Modpacks.GetActiveProgress()
}
func (b *Bridge) ExportModpack(req modpacks.ExportModpackRequest) (modpacks.ExportModpackResult, error) {
	if strings.TrimSpace(req.OutputPath) == "" {
		stem := strings.Trim(reSafeZipStem.ReplaceAllString(strings.TrimSpace(req.Name), "-"), "-")
		if stem == "" {
			stem = "modhub-pack"
		}
		ext, pattern, label := ".modhub", "*.modhub", "ModHub Modpack (*.modhub)"
		switch req.Format {
		case modpacks.FormatMRPack:
			ext, pattern, label = ".mrpack", "*.mrpack", "Modrinth Modpack (*.mrpack)"
		case modpacks.FormatCurseForge:
			ext, pattern, label = ".zip", "*.zip", "CurseForge Modpack (*.zip)"
		}
		chosen, err := b.rt.SaveModpackDialog(b.activeCtx(), "Экспорт сборки", stem+ext, pattern, label)
		if err != nil {
			return modpacks.ExportModpackResult{}, err
		}
		chosen = strings.TrimSpace(chosen)
		if chosen == "" {
			return modpacks.ExportModpackResult{Cancelled: true, Format: req.Format}, nil
		}
		if !strings.HasSuffix(strings.ToLower(chosen), ext) {
			chosen += ext
		}
		req.OutputPath = chosen
	}
	return b.container.Modpacks.Export(b.activeCtx(), req)
}
func (b *Bridge) PreviewModpackUpdate(modpackID, newArchivePath string) (modpacks.UpdateDiffPreview, error) {
	return b.container.Modpacks.PreviewUpdate(b.activeCtx(), modpackID, newArchivePath)
}
func (b *Bridge) ApplyModpackUpdate(req modpacks.ApplyUpdateRequest) (modpacks.ModpackView, error) {
	return b.container.Modpacks.ApplyUpdate(b.activeCtx(), req)
}

func (b *Bridge) OpenCrashReport(instanceID, filename string) error {
	fn := filepath.Base(strings.TrimSpace(filename))
	if fn == "" || fn == "." || fn == ".." || !strings.HasSuffix(strings.ToLower(fn), ".txt") {
		return fmt.Errorf("некорректное имя файла crash report")
	}
	inst, err := b.container.Instances.Get(instanceID)
	if err != nil {
		return err
	}
	full := filepath.Join(inst.GameDir, "crash-reports", fn)
	if _, err := os.Stat(full); err != nil {
		return fmt.Errorf("файл crash report не найден")
	}
	return openPathInOS(full)
}

func (b *Bridge) MinimizeWindow() {
	b.rt.WindowMinimise(b.activeCtx())
}

func (b *Bridge) HideWindow() {
	b.rt.WindowHide(b.activeCtx())
}

func (b *Bridge) ShowWindow() {
	b.rt.WindowShow(b.activeCtx())
}

func (b *Bridge) ToggleMaximizeWindow() {
	b.rt.WindowToggleMaximise(b.activeCtx())
}

func (b *Bridge) CloseWindow() {
	b.rt.Quit(b.activeCtx())
}

func (b *Bridge) TrimMemory() {
	TrimProcessWorkingSet()
}

func (b *Bridge) EnterPhantomMode() {
	b.rt.WindowHide(b.activeCtx())
	TrimProcessWorkingSet()
}
