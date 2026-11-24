package desktop

import (
	"archive/zip"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"modhub/internal/app"
	"modhub/internal/domain"
	"modhub/internal/events"
	"modhub/internal/settings"
	"modhub/internal/storage"
)

type mockRuntime struct {
	mu        sync.Mutex
	emitted   []string
	openDir   string
	savePath  string
	openedURL string
	minimized bool
	hidden    bool
	shown     bool
	maximized bool
	quit      bool
}

func (m *mockRuntime) Emit(_ context.Context, name string, _ any) {
	m.mu.Lock()
	m.emitted = append(m.emitted, name)
	m.mu.Unlock()
}

func (m *mockRuntime) OpenDirectoryDialog(_ context.Context, _ string) (string, error) {
	return m.openDir, nil
}

func (m *mockRuntime) OpenJavaFileDialog(_ context.Context, _ string) (string, error) { return "", nil }
func (m *mockRuntime) OpenModpackFileDialog(_ context.Context, _ string) (string, error) { return "", nil }
func (m *mockRuntime) SaveModpackDialog(_ context.Context, _, _, _, _ string) (string, error) { return m.savePath, nil }

func (m *mockRuntime) SaveFileDialog(_ context.Context, _, _ string) (string, error) {
	return m.savePath, nil
}

func (m *mockRuntime) BrowserOpenURL(_ context.Context, rawURL string) {
	m.mu.Lock()
	m.openedURL = rawURL
	m.mu.Unlock()
}

func (m *mockRuntime) WindowMinimise(_ context.Context) {
	m.mu.Lock()
	m.minimized = true
	m.mu.Unlock()
}

func (m *mockRuntime) WindowHide(_ context.Context) {
	m.mu.Lock()
	m.hidden = true
	m.mu.Unlock()
}

func (m *mockRuntime) WindowShow(_ context.Context) {
	m.mu.Lock()
	m.shown = true
	m.mu.Unlock()
}

func (m *mockRuntime) WindowToggleMaximise(_ context.Context) {
	m.mu.Lock()
	m.maximized = !m.maximized
	m.mu.Unlock()
}

func (m *mockRuntime) Quit(_ context.Context) {
	m.mu.Lock()
	m.quit = true
	m.mu.Unlock()
}

func TestResolveDataDirCleanInstallAndExistingPreserved(t *testing.T) {
	root := t.TempDir()
	stable := filepath.Join(root, "AppData", "ModHub")
	exeDir := filepath.Join(root, "bin")
	_ = os.MkdirAll(exeDir, 0o755)

	// 1. Clean install creates stable dir
	info, err := ResolveDataDirWithTarget(stable, exeDir, exeDir)
	if err != nil || info.Dir != stable || info.Mode != "standard" || info.MigratedFrom != "" {
		t.Fatalf("unexpected clean install result: info=%+v err=%v", info, err)
	}

	// 2. Seed existing config in stable dir & legacy dir; stable MUST NOT be overwritten
	cfg := settings.DefaultAppConfig()
	cfg.MergeThreshold = 88
	if err := settings.SaveAppConfig(stable, cfg); err != nil {
		t.Fatalf("SaveAppConfig: %v", err)
	}
	legacyDir := filepath.Join(exeDir, "data")
	_ = os.MkdirAll(legacyDir, 0o755)
	_ = os.WriteFile(filepath.Join(legacyDir, "config.json"), []byte(`{"port":9999,"merge_threshold":50}`), 0o600)

	info2, err := ResolveDataDirWithTarget(stable, exeDir, exeDir)
	if err != nil || info2.MigratedFrom != "" {
		t.Fatalf("expected existing stable dir to be kept untouched: %+v err=%v", info2, err)
	}
	reloaded := settings.LoadAppConfig(stable)
	if reloaded.MergeThreshold != 88 {
		t.Fatalf("existing config overwritten: got %+v", reloaded)
	}
}

func TestResolveDataDirMigratesLegacyWithBackupAndPreservesSQLite(t *testing.T) {
	root := t.TempDir()
	stable := filepath.Join(root, "AppData", "ModHub")
	exeDir := filepath.Join(root, "old_modhub")
	legacyDir := filepath.Join(exeDir, "data")
	_ = os.MkdirAll(legacyDir, 0o755)

	// Create populated legacy config, packs, and SQLite database
	cfg := settings.DefaultAppConfig()
	cfg.CurseForgeAPIKey = "$2a$10$mocklegacykey1234567890"
	cfg.ProxyURL = "direct"
	if err := settings.SaveAppConfig(legacyDir, cfg); err != nil {
		t.Fatalf("seed legacy config: %v", err)
	}
	packs := []domain.SavedPack{{ID: "p_legacy", Name: "LegacyPack", Items: []domain.CartItem{{Title: "Sodium", Folder: "mods", Filename: "sodium.jar"}}}}
	if err := settings.SavePacksCompat(legacyDir, packs); err != nil {
		t.Fatalf("seed legacy packs: %v", err)
	}
	legacyDB, err := storage.OpenDatabase(filepath.Join(legacyDir, "modhub.db"))
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	legacyDB.UpsertCatalogItems([]domain.RawProjectCard{{
		Platform: "modrinth", SourceKey: "modrinth:sod", ID: "sod", ContentType: "mod", Title: "Sodium", Downloads: 999,
	}})
	_ = legacyDB.Close()

	info, err := ResolveDataDirWithTarget(stable, exeDir, exeDir)
	if err != nil {
		t.Fatalf("ResolveDataDirWithTarget: %v", err)
	}
	if info.MigratedFrom != legacyDir || info.BackupPath == "" {
		t.Fatalf("expected migration with backup, got %+v", info)
	}

	// Verify backup exists
	if _, err := os.Stat(filepath.Join(info.BackupPath, "config.json")); err != nil {
		t.Fatalf("backup config.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(info.BackupPath, "modhub.db")); err != nil {
		t.Fatalf("backup modhub.db missing: %v", err)
	}

	// Verify migrated data in stable dir
	migCfg := settings.LoadAppConfig(stable)
	if migCfg.CurseForgeAPIKey != cfg.CurseForgeAPIKey || migCfg.ProxyURL != "direct" {
		t.Fatalf("migrated config mismatch: %+v", migCfg)
	}
	migPacks := settings.LoadPacksCompat(stable)
	if len(migPacks) != 1 || migPacks[0].ID != "p_legacy" {
		t.Fatalf("migrated packs mismatch: %+v", migPacks)
	}
	migDB, err := storage.OpenDatabase(filepath.Join(stable, "modhub.db"))
	if err != nil {
		t.Fatalf("open migrated db: %v", err)
	}
	defer migDB.Close()
	if card, ok := migDB.GetCatalogItem("modrinth:sod"); !ok || card.Title != "Sodium" {
		t.Fatalf("migrated SQLite data lost: ok=%v card=%+v", ok, card)
	}
}

func TestResolveDataDirPortableMarker(t *testing.T) {
	root := t.TempDir()
	stable := filepath.Join(root, "AppData", "ModHub")
	exeDir := filepath.Join(root, "portable_app")
	_ = os.MkdirAll(exeDir, 0o755)
	_ = os.WriteFile(filepath.Join(exeDir, ".portable"), []byte("1"), 0o644)

	info, err := ResolveDataDirWithTarget(stable, exeDir, exeDir)
	if err != nil || info.Mode != "portable" || info.Dir != filepath.Join(exeDir, "data") {
		t.Fatalf("expected portable mode, got info=%+v err=%v", info, err)
	}
}

func TestBridgeBindingsEventsAndSavePackZIP(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.OpenDatabase(filepath.Join(dir, "bridge.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer db.Close()

	container := app.NewContainer(dir, db)
	defer container.Close()

	rt := &mockRuntime{savePath: filepath.Join(dir, "exports", "my-pack.zip")}
	b := NewBridge(container, DataDirInfo{Dir: dir, Mode: "standard"}, rt)
	b.Startup(context.Background())
	defer b.Shutdown(context.Background())

	info := b.GetAppInfo()
	if info.Runtime != "wails-desktop" || info.SchemaVersion <= 0 || info.DataDir.Dir != dir {
		t.Fatalf("unexpected GetAppInfo: %+v", info)
	}

	// Config update emits Go -> React event
	th := 80
	if _, err := b.UpdateConfig(ConfigUpdateRequest{MergeThreshold: &th}); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	b.ClearAllCaches()

	rt.mu.Lock()
	emitted := append([]string(nil), rt.emitted...)
	rt.mu.Unlock()
	foundCfg, foundCache := false, false
	for _, ev := range emitted {
		if ev == string(events.EventConfigUpdated) {
			foundCfg = true
		}
		if ev == string(events.EventCacheCleared) {
			foundCache = true
		}
	}
	if !foundCfg || !foundCache {
		t.Fatalf("expected EventBus forwarding to Wails runtime, got: %+v", emitted)
	}

	// Pack save/delete round-trip via Bridge
	packs, err := b.SavePack(domain.SavedPack{
		ID: "p1", Name: "DesktopPack",
		Items: []domain.CartItem{{Title: "Sodium", Folder: "mods", Filename: "sodium.jar"}},
	})
	if err != nil || len(packs) != 1 {
		t.Fatalf("SavePack via bridge failed: %+v err=%v", packs, err)
	}

	// Direct-to-disk SavePackZIP via native dialog mock
	dlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("MOCK-JAR-BYTES"))
	}))
	defer dlSrv.Close()
	container.HTTP.SetDownloadClient(dlSrv.Client())
	container.HTTP.SetDownloadValidator(func(raw string) (*url.URL, error) { return url.Parse(raw) })

	zipRes, err := b.SavePackZIP("DesktopPack", []domain.CartItem{{
		Title: "Sodium", Folder: "mods", Filename: "sodium.jar", DownloadURL: dlSrv.URL + "/sodium.jar",
	}})
	if err != nil || zipRes.Cancelled || zipRes.Downloaded != 1 || zipRes.Status != "complete" {
		t.Fatalf("SavePackZIP failed: res=%+v err=%v", zipRes, err)
	}
	zr, err := zip.OpenReader(zipRes.Path)
	if err != nil {
		t.Fatalf("open exported zip: %v", err)
	}
	_ = zr.Close()

	// OpenExternalURL rejects non-http schemes
	if err := b.OpenExternalURL("javascript:alert(1)"); err == nil {
		t.Fatal("expected non-http URL rejected")
	}
	if err := b.OpenExternalURL("https://modrinth.com"); err != nil || rt.openedURL != "https://modrinth.com" {
		t.Fatalf("expected https URL opened, got err=%v url=%q", err, rt.openedURL)
	}
}

func TestBridgeWindowAndPhantomMode(t *testing.T) {
	dir := t.TempDir()
	dataInfo := DataDirInfo{Dir: dir, Mode: "test"}
	db, err := storage.OpenDatabase(filepath.Join(dir, "modhub.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer db.Close()

	container := app.NewContainer(dir, db)
	defer container.Close()

	rt := &mockRuntime{}
	b := NewBridge(container, dataInfo, rt)

	b.MinimizeWindow()
	b.ToggleMaximizeWindow()
	b.ShowWindow()
	b.EnterPhantomMode()
	b.TrimMemory()
	b.CloseWindow()

	rt.mu.Lock()
	min := rt.minimized
	max := rt.maximized
	sh := rt.shown
	hid := rt.hidden
	q := rt.quit
	rt.mu.Unlock()

	if !min || !max || !sh || !hid || !q {
		t.Fatalf("expected all window actions invoked: min=%v max=%v sh=%v hid=%v q=%v", min, max, sh, hid, q)
	}
}
