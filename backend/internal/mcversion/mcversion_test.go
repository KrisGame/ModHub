package mcversion

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"modhub/internal/downloads"
	"modhub/internal/events"
	"modhub/internal/netx"
	"modhub/internal/storage"
)

func sha1Bytes(b []byte) string {
	s := sha1.Sum(b)
	return hex.EncodeToString(s[:])
}

func makeMockNativeJAR(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for k, v := range files {
		w, err := zw.Create(k)
		if err != nil {
			t.Fatalf("zip create %s: %v", k, err)
		}
		_, _ = w.Write([]byte(v))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func TestManifestFetchTypesInvalidAndOfflineStaleCache(t *testing.T) {
	var failRemote, returnCorrupt atomic.Bool
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failRemote.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if returnCorrupt.Load() {
			_, _ = w.Write([]byte(`{"latest":{},"versions":[]}`))
			return
		}
		switch r.URL.Path {
		case "/manifest.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"latest": map[string]any{"release": "1.21.1", "snapshot": "24w36a"},
				"versions": []map[string]any{
					{"id": "1.21.1", "type": "release", "url": srv.URL + "/v/1.21.1.json", "releaseTime": "2024-08-08T12:00:00Z"},
					{"id": "24w36a", "type": "snapshot", "url": srv.URL + "/v/24w36a.json", "releaseTime": "2024-09-04T12:00:00Z"},
					{"id": "b1.7.3", "type": "old_beta", "url": srv.URL + "/v/b1.7.3.json", "releaseTime": "2011-07-08T12:00:00Z"},
				},
			})
		case "/v/1.21.1.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "1.21.1", "type": "release", "mainClass": "net.minecraft.client.main.Main",
				"javaVersion": map[string]any{"component": "java-runtime-delta", "majorVersion": 21},
				"arguments": map[string]any{
					"game": []any{"--username", "${auth_player_name}"},
					"jvm":  []any{"-Xmx2G", map[string]any{"rules": []any{map[string]any{"action": "allow", "os": map[string]any{"name": "windows"}}}, "value": "-XX:HeapDumpPath=Mojang"}},
				},
			})
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	db, _ := storage.OpenDatabase(filepath.Join(dir, "mc.db"))
	defer db.Close()
	hc := netx.NewHTTPClient(db)
	hc.SetAPIClient(srv.Client())
	hc.SetLauncherDownloadValidator(func(raw string) (*url.URL, error) { return url.Parse(raw) })
	dlMgr := downloads.NewManager(hc, db, nil, 2)
	defer dlMgr.Close()
	mgr := NewManager(filepath.Join(dir, "shared"), hc, dlMgr, nil, nil)
	mgr.SetManifestURL(srv.URL + "/manifest.json")

	returnCorrupt.Store(true)
	if _, err := mgr.FetchManifest(context.Background(), true); err == nil {
		t.Fatal("expected error for corrupt empty manifest")
	}
	returnCorrupt.Store(false)

	res, err := mgr.FetchManifest(context.Background(), true)
	if err != nil || res.FromCache || res.Stale || len(res.Versions) != 3 {
		t.Fatalf("FetchManifest live mismatch: res=%+v err=%v", res, err)
	}
	meta, err := mgr.FetchVersionMetadata(context.Background(), "1.21.1", true)
	if err != nil || meta.MainClass != "net.minecraft.client.main.Main" || meta.JavaVersion == nil || meta.JavaVersion.MajorVersion != 21 {
		t.Fatalf("FetchVersionMetadata mismatch: meta=%+v err=%v", meta, err)
	}
	failRemote.Store(true)
	offRes, err := mgr.FetchManifest(context.Background(), true)
	if err != nil || !offRes.FromCache || !offRes.Stale || offRes.Warning == "" {
		t.Fatalf("expected honest stale offline cache, got res=%+v err=%v", offRes, err)
	}
}

func TestLibraryRulesFilteringVanillaInstallAndReinstall(t *testing.T) {
	clientData := []byte("MOCK-MINECRAFT-CLIENT-JAR")
	winLibData := []byte("MOCK-WIN-X64-LIB")
	nativeJarData := makeMockNativeJAR(t, map[string]string{"META-INF/MANIFEST.MF": "1.0", "lwjgl.dll": "DLL"})
	logCfgData := []byte(`<Configuration/>`)
	assetData := []byte(`sound`)
	assetHash := sha1Bytes(assetData)
	idxBytes, _ := json.Marshal(AssetIndex{Objects: map[string]AssetObject{"minecraft/sounds/step.ogg": {Hash: assetHash, Size: int64(len(assetData))}}})

	var dlHits atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"latest": map[string]any{"release": "1.21.1"}, "versions": []map[string]any{{"id": "1.21.1", "type": "release", "url": srv.URL + "/v/1.21.1.json"}}})
		case "/v/1.21.1.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "1.21.1", "type": "release", "mainClass": "net.minecraft.client.main.Main",
				"javaVersion": map[string]any{"component": "java-runtime-delta", "majorVersion": 21},
				"downloads":   map[string]any{"client": map[string]any{"url": srv.URL + "/files/client.jar", "size": len(clientData), "sha1": sha1Bytes(clientData)}},
				"assetIndex":  map[string]any{"id": "17", "url": srv.URL + "/files/17.json", "size": len(idxBytes), "sha1": sha1Bytes(idxBytes)},
				"logging":     map[string]any{"client": map[string]any{"argument": "-Dlog4j", "type": "log4j2-xml", "file": map[string]any{"id": "client-1.12.xml", "url": srv.URL + "/files/log.xml", "size": len(logCfgData), "sha1": sha1Bytes(logCfgData)}}},
				"libraries": []map[string]any{
					{"name": "com.mojang:winlib:1.0", "rules": []map[string]any{{"action": "allow", "os": map[string]any{"name": "windows", "arch": "x64"}}}, "downloads": map[string]any{"artifact": map[string]any{"path": "com/mojang/winlib/1.0/winlib-1.0.jar", "url": srv.URL + "/files/winlib.jar", "size": len(winLibData), "sha1": sha1Bytes(winLibData)}}},
					{"name": "com.mojang:linuxonly:1.0", "rules": []map[string]any{{"action": "allow", "os": map[string]any{"name": "linux"}}}, "downloads": map[string]any{"artifact": map[string]any{"path": "com/mojang/linuxonly/1.0/linuxonly-1.0.jar", "url": srv.URL + "/files/NO.jar", "size": 10, "sha1": "0"}}},
					{"name": "com.mojang:armonly:1.0", "rules": []map[string]any{{"action": "allow", "os": map[string]any{"name": "windows", "arch": "arm64"}}}, "downloads": map[string]any{"artifact": map[string]any{"path": "com/mojang/armonly/1.0/armonly-1.0.jar", "url": srv.URL + "/files/NO2.jar", "size": 10, "sha1": "0"}}},
					{"name": "org.lwjgl:lwjgl-platform:3.3.3", "natives": map[string]any{"windows": "natives-windows"}, "extract": map[string]any{"exclude": []string{"META-INF/"}}, "downloads": map[string]any{"classifiers": map[string]any{"natives-windows": map[string]any{"path": "org/lwjgl/lwjgl-platform/3.3.3/lwjgl-platform-3.3.3-natives-windows.jar", "url": srv.URL + "/files/natives.jar", "size": len(nativeJarData), "sha1": sha1Bytes(nativeJarData)}}}},
				},
			})
		case "/files/client.jar":
			dlHits.Add(1); _, _ = w.Write(clientData)
		case "/files/winlib.jar":
			dlHits.Add(1); _, _ = w.Write(winLibData)
		case "/files/natives.jar":
			dlHits.Add(1); _, _ = w.Write(nativeJarData)
		case "/files/log.xml":
			dlHits.Add(1); _, _ = w.Write(logCfgData)
		case "/files/17.json":
			dlHits.Add(1); _, _ = w.Write(idxBytes)
		case "/assets/" + assetHash[:2] + "/" + assetHash:
			dlHits.Add(1); _, _ = w.Write(assetData)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	db, _ := storage.OpenDatabase(filepath.Join(dir, "vanilla.db"))
	defer db.Close()
	hc := netx.NewHTTPClient(db)
	hc.SetAPIClient(srv.Client())
	hc.SetLauncherDownloadClient(srv.Client())
	hc.SetLauncherDownloadValidator(func(raw string) (*url.URL, error) { return url.Parse(raw) })
	bus := events.NewBus()
	defer bus.Close()
	dlMgr := downloads.NewManager(hc, db, bus, 4)
	dlMgr.SetRetryBaseDelay(10 * time.Millisecond)
	defer dlMgr.Close()

	mgr := NewManager(filepath.Join(dir, "shared"), hc, dlMgr, nil, bus)
	mgr.SetManifestURL(srv.URL + "/manifest.json")
	mgr.SetAssetsBaseURL(srv.URL + "/assets")
	mgr.SetPlatform(PlatformFor("windows", "amd64"))

	meta, rep, err := mgr.InstallVanilla(context.Background(), "inst_1", "1.21.1", "mc:1.21.1", nil)
	if err != nil || meta == nil || !rep.Valid || rep.LibrariesTotal != 2 || rep.LibrariesOK != 2 {
		t.Fatalf("InstallVanilla failed: rep=%+v err=%v", rep, err)
	}
	if _, err := os.Stat(filepath.Join(mgr.VersionDir("1.21.1"), "natives", "lwjgl.dll")); err != nil {
		t.Fatal("expected lwjgl.dll extracted")
	}
	firstHits := dlHits.Load()
	_, rep2, err := mgr.InstallVanilla(context.Background(), "inst_1", "1.21.1", "mc:1.21.1:re", nil)
	if err != nil || !rep2.Valid || dlHits.Load() != firstHits {
		t.Fatalf("expected 0 redundant downloads on re-install: err=%v extra=%d", err, dlHits.Load()-firstHits)
	}
}
