package modpacks

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"modhub/internal/domain"
	"modhub/internal/downloads"
	"modhub/internal/events"
	"modhub/internal/instances"
	"modhub/internal/netx"
	"modhub/internal/storage"
)

func sha1Of(b []byte) string   { h := sha1.Sum(b); return hex.EncodeToString(h[:]) }
func sha512Of(b []byte) string { h := sha512.Sum512(b); return hex.EncodeToString(h[:]) }

func writeTestZIP(t *testing.T, dest string, entries map[string][]byte) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(dest), 0o755)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		_, _ = w.Write(data)
	}
	_ = zw.Close()
	if err := os.WriteFile(dest, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write zip %s: %v", dest, err)
	}
}

func setupTestModpackEnv(t *testing.T, srv *httptest.Server, cfKey string, savedPacks []domain.SavedPack) (*Manager, *instances.Manager, *storage.Database, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.OpenDatabase(filepath.Join(dir, "mp.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	hc := netx.NewHTTPClient(db)
	if srv != nil {
		hc.SetAPIClient(srv.Client())
		hc.SetLauncherDownloadClient(srv.Client())
		hc.SetLauncherDownloadValidator(func(raw string) (*url.URL, error) { return url.Parse(raw) })
	}
	bus := events.NewBus()
	t.Cleanup(func() { bus.Close() })
	dlMgr := downloads.NewManager(hc, db, bus, 4)
	dlMgr.SetRetryBaseDelay(10 * time.Millisecond)
	t.Cleanup(func() { dlMgr.Close() })
	instMgr := instances.NewManager(dir, filepath.Join(dir, "instances"), filepath.Join(dir, "shared"), filepath.Join(dir, "runtimes"), filepath.Join(dir, "media"), db, nil, bus, func() []domain.SavedPack { return savedPacks })
	mgr := NewManager(filepath.Join(dir, "mp_cache"), hc, dlMgr, instMgr, nil, db, bus, func() string { return cfKey }, func() []domain.SavedPack { return savedPacks })
	t.Cleanup(func() { mgr.Close() })
	return mgr, instMgr, db, dir
}

func TestFormatsAndImportModrinthCurseForgeAndErrors(t *testing.T) {
	sodiumBytes, optModBytes := []byte("SODIUM-JAR-BYTES"), []byte("OPTIONAL-ZOOM-JAR")
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cdn/sodium.jar":
			_, _ = w.Write(sodiumBytes)
		case "/cdn/zoom.jar":
			_, _ = w.Write(optModBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	mgr, instMgr, _, dir := setupTestModpackEnv(t, srv, "", nil)
	mrIdx, _ := json.Marshal(ModrinthIndexJSON{
		FormatVersion: 1, Game: "minecraft", VersionID: "1.0.0", Name: "Test MR Pack", Summary: "RPG Pack",
		Dependencies: map[string]string{"minecraft": "1.21.1", "fabric-loader": "0.16.9"},
		Files: []ModrinthIndexFile{
			{Path: "mods/sodium.jar", Hashes: map[string]string{"sha1": sha1Of(sodiumBytes), "sha512": sha512Of(sodiumBytes)}, Env: &ModrinthFileEnv{Client: "required", Server: "unsupported"}, Downloads: []string{srv.URL + "/cdn/sodium.jar"}, FileSize: int64(len(sodiumBytes))},
			{Path: "mods/zoom.jar", Hashes: map[string]string{"sha1": sha1Of(optModBytes)}, Env: &ModrinthFileEnv{Client: "optional", Server: "unsupported"}, Downloads: []string{srv.URL + "/cdn/zoom.jar"}, FileSize: int64(len(optModBytes))},
			{Path: "mods/server-only.jar", Hashes: map[string]string{"sha1": "deadbeef"}, Env: &ModrinthFileEnv{Client: "unsupported", Server: "required"}, Downloads: []string{srv.URL + "/cdn/NO.jar"}, FileSize: 100},
		},
	})
	mrPath := filepath.Join(dir, "sample.mrpack")
	writeTestZIP(t, mrPath, map[string][]byte{
		"modrinth.index.json":           mrIdx,
		"overrides/config/mod.toml":     []byte("a = 1"),
		"client-overrides/options.txt":  []byte("fov:90"),
		"server-overrides/server.props": []byte("motd=no"),
	})

	prev, err := mgr.PreviewImport(context.Background(), PreviewImportRequest{SourceType: "file", SourceRef: mrPath})
	if err != nil || prev.Format != FormatMRPack || prev.Counts.ServerSkipped != 1 || prev.Counts.OptionalFiles != 1 {
		t.Fatalf("PreviewImport .mrpack mismatch: prev=%+v err=%v", prev, err)
	}
	res, err := mgr.ExecuteImportSync(context.Background(), ExecuteImportRequest{PreviewID: prev.PreviewID, SelectedOptional: []string{"mods/zoom.jar"}})
	if err != nil || res.Status != "installed" || res.DownloadedFiles != 2 || res.AppliedOverrides != 2 {
		t.Fatalf("ExecuteImportSync .mrpack failed: res=%+v err=%v", res, err)
	}
	inst, _ := instMgr.Get(res.InstanceID)
	if _, err := os.Stat(filepath.Join(inst.GameDir, "mods", "sodium.jar")); err != nil {
		t.Fatal("expected sodium.jar installed")
	}
	if _, err := os.Stat(filepath.Join(inst.GameDir, "mods", "server-only.jar")); !os.IsNotExist(err) {
		t.Fatal("server-only mod must not be installed")
	}
	if _, err := os.Stat(filepath.Join(inst.GameDir, "server.props")); !os.IsNotExist(err) {
		t.Fatal("server-overrides must not be applied")
	}

	cfMF, _ := json.Marshal(CurseForgeManifestJSON{
		Minecraft:    CFManifestMinecraft{Version: "1.20.1", ModLoaders: []CFManifestLoader{{ID: "forge-47.3.0", Primary: true}}},
		ManifestType: "minecraftModpack", ManifestVersion: 1, Name: "CF Sample", Version: "2.0", Author: "Tester",
		Files: []CFManifestFile{{ProjectID: 328085, FileID: 555001, Required: true}}, Overrides: "overrides",
	})
	cfPath := filepath.Join(dir, "cf-pack.zip")
	writeTestZIP(t, cfPath, map[string][]byte{"manifest.json": cfMF, "overrides/config/cf.toml": []byte("x=1")})
	if cfPrev, err := mgr.PreviewImport(context.Background(), PreviewImportRequest{SourceType: "file", SourceRef: cfPath}); err != nil || cfPrev.Format != FormatCurseForge || len(cfPrev.UnavailableFiles) != 1 {
		t.Fatalf("unexpected CF preview: prev=%+v err=%v", cfPrev, err)
	}

	badVer := filepath.Join(dir, "future.modhub")
	writeTestZIP(t, badVer, map[string][]byte{"modhub.pack.json": []byte(`{"format_version":99,"name":"Future"}`)})
	if _, err := mgr.PreviewImport(context.Background(), PreviewImportRequest{SourceType: "file", SourceRef: badVer}); !errors.Is(err, ErrUnsupportedFormatVer) {
		t.Fatalf("expected ErrUnsupportedFormatVer, got %v", err)
	}
	badJSON := filepath.Join(dir, "broken.mrpack")
	writeTestZIP(t, badJSON, map[string][]byte{"modrinth.index.json": []byte(`{corrupt`)})
	if _, err := mgr.PreviewImport(context.Background(), PreviewImportRequest{SourceType: "file", SourceRef: badJSON}); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("expected ErrInvalidManifest, got %v", err)
	}
}

func TestExecuteImportIntoExistingInstance(t *testing.T) {
	modBytes := []byte("saved-pack-mod")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cdn/example.jar" {
			_, _ = w.Write(modBytes)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	pack := domain.SavedPack{
		ID: "pack_existing", Name: "Existing pack", MCVersion: "1.21.1", Loader: "fabric",
		Items: []domain.CartItem{{Title: "Example", Platform: "modrinth", SourceKey: "modrinth:example", Folder: "mods", Filename: "example.jar", DownloadURL: srv.URL + "/cdn/example.jar", FileSize: int64(len(modBytes)), IsAvailable: true}},
	}
	mgr, instMgr, _, _ := setupTestModpackEnv(t, srv, "", []domain.SavedPack{pack})
	inst, err := instMgr.Create(instances.CreateInstanceRequest{Name: "Target", MCVersion: "1.21.1", InstanceType: instances.TypeFabric})
	if err != nil {
		t.Fatalf("create target instance: %v", err)
	}
	preview, err := mgr.PreviewImport(context.Background(), PreviewImportRequest{SourceType: "pack", SourceRef: pack.ID})
	if err != nil {
		t.Fatalf("preview saved pack: %v", err)
	}
	result, err := mgr.ExecuteImportSync(context.Background(), ExecuteImportRequest{PreviewID: preview.PreviewID, TargetInstanceID: inst.ID, InstallMinecraft: true})
	if err != nil {
		t.Fatalf("import into existing instance: %v", err)
	}
	if result.InstanceID != inst.ID || result.Status != "installed" {
		t.Fatalf("unexpected import result: %+v", result)
	}
	after, err := instMgr.Get(inst.ID)
	if err != nil {
		t.Fatalf("reload target instance: %v", err)
	}
	if after.MCVersion != "1.21.1" || after.InstanceType != instances.TypeFabric {
		t.Fatalf("target profile settings changed: MC=%s loader=%s", after.MCVersion, after.InstanceType)
	}
	if _, err := os.Stat(filepath.Join(after.GameDir, "mods", "example.jar")); err != nil {
		t.Fatalf("saved-pack mod not installed in existing profile: %v", err)
	}

	if err := instMgr.AcquireBusy(inst.ID); err != nil {
		t.Fatalf("mark target instance busy: %v", err)
	}
	defer instMgr.ReleaseBusy(inst.ID)
	busyPreview, err := mgr.PreviewImport(context.Background(), PreviewImportRequest{SourceType: "pack", SourceRef: pack.ID})
	if err != nil {
		t.Fatalf("preview saved pack for busy target: %v", err)
	}
	if _, err := mgr.ExecuteImportSync(context.Background(), ExecuteImportRequest{PreviewID: busyPreview.PreviewID, TargetInstanceID: inst.ID}); !errors.Is(err, instances.ErrInstanceBusy) {
		t.Fatalf("expected running target profile to be rejected, got %v", err)
	}
}

func TestExportRoundTripAndUserPreservingUpdate(t *testing.T) {
	mgr, instMgr, _, dir := setupTestModpackEnv(t, nil, "", nil)
	inst, _ := instMgr.Create(instances.CreateInstanceRequest{Name: "OriginPack", Description: "Rich", MCVersion: "1.21.1", InstanceType: instances.TypeNeoForge, LoaderVersion: "21.1.77", MemoryMaxMB: 6144})
	_ = os.WriteFile(filepath.Join(inst.GameDir, "mods", "alpha.jar"), []byte("ALPHA-V1"), 0o644)
	_ = os.WriteFile(filepath.Join(inst.GameDir, "mods", "beta.jar"), []byte("BETA-V1"), 0o644)
	_ = os.WriteFile(filepath.Join(inst.GameDir, "config", "app.toml"), []byte("setting=v1"), 0o644)
	_ = os.WriteFile(filepath.Join(inst.GameDir, "resourcepacks", "rp.zip"), []byte("RP1"), 0o644)
	_ = os.WriteFile(filepath.Join(inst.GameDir, "shaderpacks", "sh.zip"), []byte("SH1"), 0o644)
	_ = os.WriteFile(filepath.Join(inst.GameDir, "saves", "world1.dat"), []byte("HEAVY-WORLD"), 0o644)
	_ = os.WriteFile(filepath.Join(inst.GameDir, "accounts.json"), []byte("SECRET_SHOULD_NEVER_EXPORT"), 0o600)

	mhOut := filepath.Join(dir, "export-v1.modhub")
	resMH, err := mgr.Export(context.Background(), ExportModpackRequest{
		SourceType: "instance", SourceID: inst.ID, Format: FormatModHub, OutputPath: mhOut, Version: "1.0.0",
		IncludeMods: true, IncludeConfigs: true, IncludeResourcePacks: true, IncludeShaders: true, IncludeDatapacks: true, IncludeExtraFiles: true,
	})
	if err != nil || resMH.TotalFiles != 5 || resMH.EmbeddedFiles != 5 || resMH.LinkedFiles != 0 || resMH.SkippedFiles != 0 {
		t.Fatalf("Export .modhub failed: res=%+v err=%v", resMH, err)
	}
	zr, _ := zip.OpenReader(mhOut)
	for _, f := range zr.File {
		if strings.Contains(strings.ToLower(f.Name), "accounts.json") || strings.Contains(strings.ToLower(f.Name), "world1.dat") {
			t.Fatalf("forbidden file exported: %s", f.Name)
		}
	}
	_ = zr.Close()

	if _, err := mgr.Export(context.Background(), ExportModpackRequest{SourceType: "instance", SourceID: inst.ID, Format: FormatMRPack, OutputPath: filepath.Join(dir, "e.mrpack"), IncludeMods: true, IncludeConfigs: true}); err != nil {
		t.Fatalf("Export .mrpack: %v", err)
	}
	if cfRes, err := mgr.Export(context.Background(), ExportModpackRequest{SourceType: "instance", SourceID: inst.ID, Format: FormatCurseForge, OutputPath: filepath.Join(dir, "e-cf.zip"), IncludeMods: true, IncludeConfigs: true}); err != nil || len(cfRes.Warnings) == 0 {
		t.Fatalf("Export CF expected warning: res=%+v err=%v", cfRes, err)
	}

	prevV1, err := mgr.PreviewImport(context.Background(), PreviewImportRequest{SourceType: "file", SourceRef: mhOut})
	if err != nil || prevV1.ProfileSettings == nil || prevV1.ProfileSettings.MemoryMaxMB != 6144 {
		t.Fatalf("Re-import .modhub preview mismatch: %+v err=%v", prevV1, err)
	}
	impV1, err := mgr.ExecuteImportSync(context.Background(), ExecuteImportRequest{PreviewID: prevV1.PreviewID, ProfileName: "ImportedV1"})
	if err != nil || impV1.Status != "installed" {
		t.Fatalf("ExecuteImportSync .modhub failed: %+v err=%v", impV1, err)
	}
	impInst, _ := instMgr.Get(impV1.InstanceID)
	_ = os.WriteFile(filepath.Join(impInst.GameDir, "mods", "my-custom-user-mod.jar"), []byte("USER-MOD"), 0o644)
	_ = os.WriteFile(filepath.Join(impInst.GameDir, "config", "app.toml"), []byte("setting=USER-EDITED"), 0o644)

	_ = os.WriteFile(filepath.Join(inst.GameDir, "mods", "alpha.jar"), []byte("ALPHA-V2-UPDATED"), 0o644)
	_ = os.Remove(filepath.Join(inst.GameDir, "mods", "beta.jar"))
	_ = os.WriteFile(filepath.Join(inst.GameDir, "mods", "gamma.jar"), []byte("GAMMA-NEW"), 0o644)
	_ = os.WriteFile(filepath.Join(inst.GameDir, "config", "app.toml"), []byte("setting=v2"), 0o644)
	mhOutV2 := filepath.Join(dir, "export-v2.modhub")
	_, _ = mgr.Export(context.Background(), ExportModpackRequest{SourceType: "instance", SourceID: inst.ID, Format: FormatModHub, OutputPath: mhOutV2, Version: "2.0.0", IncludeMods: true, IncludeConfigs: true, IncludeResourcePacks: true, IncludeShaders: true})

	diff, err := mgr.PreviewUpdate(context.Background(), impV1.Modpack.ID, mhOutV2)
	if err != nil || !diff.HasUpdate || len(diff.Added) != 1 || len(diff.Updated) != 1 || len(diff.Removed) != 1 || len(diff.ConfigConflicts) != 1 || len(diff.PreservedUser) != 1 {
		t.Fatalf("unexpected update diff: +%d ~%d -%d !%d u%d err=%v", len(diff.Added), len(diff.Updated), len(diff.Removed), len(diff.ConfigConflicts), len(diff.PreservedUser), err)
	}
	updView, err := mgr.ApplyUpdate(context.Background(), ApplyUpdateRequest{ModpackID: impV1.Modpack.ID, PreviewID: diff.PreviewID, ConfigConflictMode: ConflictKeepExisting})
	if err != nil || updView.Version != "2.0.0" {
		t.Fatalf("ApplyUpdate failed: %+v err=%v", updView, err)
	}
	if b, _ := os.ReadFile(filepath.Join(impInst.GameDir, "mods", "alpha.jar")); string(b) != "ALPHA-V2-UPDATED" {
		t.Fatalf("alpha not updated: %q", string(b))
	}
	if _, err := os.Stat(filepath.Join(impInst.GameDir, "mods", "my-custom-user-mod.jar")); err != nil {
		t.Fatal("user-added mod deleted!")
	}
	if cfgB, _ := os.ReadFile(filepath.Join(impInst.GameDir, "config", "app.toml")); string(cfgB) != "setting=USER-EDITED" {
		t.Fatalf("user config overwritten: %q", string(cfgB))
	}
}

func TestArchiveSecurityZipSlipLimitsAndMaliciousURLs(t *testing.T) {
	mgr, _, _, dir := setupTestModpackEnv(t, nil, "", nil)

	zipSlip := filepath.Join(dir, "slip.mrpack")
	writeTestZIP(t, zipSlip, map[string][]byte{
		"modrinth.index.json":  []byte(`{"formatVersion":1,"game":"minecraft","name":"Evil","dependencies":{"minecraft":"1.21.1"},"files":[]}`),
		"overrides/../../x.sh": []byte("evil"),
	})
	if _, err := mgr.PreviewImport(context.Background(), PreviewImportRequest{SourceType: "file", SourceRef: zipSlip}); !errors.Is(err, ErrZipSlipDetected) {
		t.Fatalf("expected ErrZipSlipDetected, got %v", err)
	}

	manifestSlip := filepath.Join(dir, "mslip.mrpack")
	writeTestZIP(t, manifestSlip, map[string][]byte{
		"modrinth.index.json": []byte(`{"formatVersion":1,"game":"minecraft","name":"Evil2","dependencies":{"minecraft":"1.21.1"},"files":[{"path":"../evil.jar","hashes":{"sha1":"abc"},"downloads":["https://cdn.modrinth.com/a.jar"],"fileSize":10}]}`),
	})
	if _, err := mgr.PreviewImport(context.Background(), PreviewImportRequest{SourceType: "file", SourceRef: manifestSlip}); !errors.Is(err, ErrZipSlipDetected) {
		t.Fatalf("expected ErrZipSlipDetected for manifest path, got %v", err)
	}

	corrupt := filepath.Join(dir, "corrupt.zip")
	_ = os.WriteFile(corrupt, []byte("not a zip"), 0o644)
	if _, err := mgr.PreviewImport(context.Background(), PreviewImportRequest{SourceType: "file", SourceRef: corrupt}); !errors.Is(err, ErrArchiveCorrupted) {
		t.Fatalf("expected ErrArchiveCorrupted, got %v", err)
	}

	mgr.SetArchiveLimits(ArchiveLimits{MaxFiles: 2, MaxUncompressedBytes: 512, MaxSingleEntryBytes: 256})
	bigZip := filepath.Join(dir, "big.mrpack")
	writeTestZIP(t, bigZip, map[string][]byte{
		"modrinth.index.json": []byte(`{"formatVersion":1,"game":"minecraft","name":"Big","dependencies":{"minecraft":"1.21.1"},"files":[]}`),
		"overrides/a.bin":     bytes.Repeat([]byte("A"), 300),
	})
	if _, err := mgr.PreviewImport(context.Background(), PreviewImportRequest{SourceType: "file", SourceRef: bigZip}); !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("expected ErrArchiveTooLarge, got %v", err)
	}
	mgr.SetArchiveLimits(DefaultArchiveLimits())

	evilURLPack := filepath.Join(dir, "evilurl.mrpack")
	writeTestZIP(t, evilURLPack, map[string][]byte{
		"modrinth.index.json": []byte(`{"formatVersion":1,"game":"minecraft","name":"EvilURL","dependencies":{"minecraft":"1.21.1"},"files":[{"path":"mods/ssrf.jar","hashes":{"sha1":"abc"},"downloads":["http://127.0.0.1/secret.jar","https://evil.example.com/a.jar"],"fileSize":10}]}`),
	})
	if prevEvil, err := mgr.PreviewImport(context.Background(), PreviewImportRequest{SourceType: "file", SourceRef: evilURLPack}); err != nil || len(prevEvil.UnavailableFiles) != 1 || prevEvil.UnavailableFiles[0].Available {
		t.Fatalf("expected SSRF URLs rejected, got prev=%+v err=%v", prevEvil, err)
	}
}
