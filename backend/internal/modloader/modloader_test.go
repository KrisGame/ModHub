package modloader

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"modhub/internal/instances"
	"modhub/internal/mcversion"
	"modhub/internal/netx"
	javart "modhub/internal/runtime"
	"modhub/internal/storage"
)

func sha1Hex(b []byte) string {
	s := sha1.Sum(b)
	return hex.EncodeToString(s[:])
}

func buildTestZIP(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
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
	return buf.Bytes()
}

type fakeProcRunner struct {
	calls      atomic.Int32
	shouldFail atomic.Bool
	writeOut   map[string][]byte
}

func (f *fakeProcRunner) RunJavaProcessor(ctx context.Context, _ string, _ string, _ []string, _ string, args []string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	f.calls.Add(1)
	if f.shouldFail.Load() {
		return "simulated processor crash", errors.New("processor exit status 1")
	}
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--output" {
			outPath := args[i+1]
			data := f.writeOut[filepath.Base(outPath)]
			if len(data) == 0 {
				data = []byte("PATCHED-CLIENT-OUTPUT")
			}
			_ = os.MkdirAll(filepath.Dir(outPath), 0o755)
			_ = os.WriteFile(outPath, data, 0o644)
		}
	}
	return "OK", nil
}

func TestFabricAndQuiltInstallVerifyReinstallAndError(t *testing.T) {
	fabLib, quiltLib := []byte("FABRIC-LOADER-JAR"), []byte("QUILT-LOADER-JAR")
	var failQuiltLib atomic.Bool
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fabric/versions/loader/1.21.1":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"loader": map[string]any{"version": "0.16.9", "stable": true}}})
		case "/fabric/versions/loader/1.12.2":
			_, _ = w.Write([]byte(`[]`))
		case "/fabric/versions/loader/1.21.1/0.16.9/profile/json":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "fabric-loader-0.16.9-1.21.1", "inheritsFrom": "1.21.1", "mainClass": "net.fabricmc.loader.impl.launch.knot.KnotClient", "libraries": []map[string]any{{"name": "net.fabricmc:fabric-loader:0.16.9", "url": srv.URL + "/maven/"}}})
		case "/maven/net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar":
			_, _ = w.Write(fabLib)
		case "/quilt/versions/loader/1.21.1":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"loader": map[string]any{"version": "0.27.1"}}})
		case "/quilt/versions/loader/1.21.1/0.27.1/profile/json":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "quilt-loader-0.27.1-1.21.1", "inheritsFrom": "1.21.1", "mainClass": "org.quiltmc.loader.impl.launch.knot.KnotClient", "libraries": []map[string]any{{"name": "org.quiltmc:quilt-loader:0.27.1", "url": srv.URL + "/qmaven/"}}})
		case "/qmaven/org/quiltmc/quilt-loader/0.27.1/quilt-loader-0.27.1.jar":
			if failQuiltLib.Load() {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(quiltLib)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	db, _ := storage.OpenDatabase(filepath.Join(dir, "fq.db"))
	defer db.Close()
	hc := netx.NewHTTPClient(db)
	hc.SetAPIClient(srv.Client())
	hc.SetLauncherDownloadClient(srv.Client())
	hc.SetLauncherDownloadValidator(func(raw string) (*url.URL, error) { return url.Parse(raw) })
	dlMgr := downloads.NewManager(hc, db, nil, 4)
	dlMgr.SetRetryBaseDelay(10 * time.Millisecond)
	defer dlMgr.Close()
	mcMgr := mcversion.NewManager(filepath.Join(dir, "shared"), hc, dlMgr, nil, nil)

	fab := NewFabricProvider(hc, dlMgr, mcMgr)
	fab.SetBaseURLs(srv.URL+"/fabric", srv.URL+"/maven")
	quilt := NewQuiltProvider(hc, dlMgr, mcMgr)
	quilt.SetBaseURLs(srv.URL+"/quilt", srv.URL+"/qmaven")

	if r, err := fab.ListVersions(context.Background(), "1.12.2"); err != nil || r.Compatible {
		t.Fatalf("expected Fabric incompatible with 1.12.2, got %+v err=%v", r, err)
	}
	resFab, err := fab.Install(context.Background(), LoaderInstallContext{InstanceID: "i1", MCVersion: "1.21.1", LoaderVersion: "0.16.9", GroupID: "fab1"})
	if err != nil || !fab.Verify(context.Background(), resFab.VersionID).Valid {
		t.Fatalf("Fabric install/verify failed: res=%+v err=%v", resFab, err)
	}
	if _, err := fab.Install(context.Background(), LoaderInstallContext{InstanceID: "i1", MCVersion: "1.21.1", LoaderVersion: "0.16.9", GroupID: "fab2"}); err != nil {
		t.Fatalf("Fabric reinstall failed: %v", err)
	}

	failQuiltLib.Store(true)
	if _, err := quilt.Install(context.Background(), LoaderInstallContext{InstanceID: "i1", MCVersion: "1.21.1", LoaderVersion: "0.27.1", GroupID: "qfail"}); err == nil {
		t.Fatal("expected Quilt install error on 404")
	}
	failQuiltLib.Store(false)
	resQ, err := quilt.Install(context.Background(), LoaderInstallContext{InstanceID: "i1", MCVersion: "1.21.1", LoaderVersion: "0.27.1", GroupID: "qok"})
	if err != nil || !quilt.Verify(context.Background(), resQ.VersionID).Valid {
		t.Fatalf("Quilt install/verify failed: res=%+v err=%v", resQ, err)
	}
}

func TestForgeAndNeoForgeInstallProcessorsAndFailureHandling(t *testing.T) {
	patchedOut := []byte("PATCHED-CLIENT-OUTPUT")
	patchedHash := sha1Hex(patchedOut)
	procJarBytes := buildTestZIP(t, map[string][]byte{"META-INF/MANIFEST.MF": []byte("Manifest-Version: 1.0\r\nMain-Class: net.minecraftforge.installertools.ConsoleTool\r\n")})
	verLibBytes := []byte("FORGE-MODLAUNCHER-JAR")

	makeInstaller := func(srvURL, verID, mcVer string) []byte {
		profJSON, _ := json.Marshal(map[string]any{
			"spec": 1, "version": verID, "minecraft": mcVer, "json": "/version.json",
			"data": map[string]any{"PATCHED": map[string]any{"client": "[net.custom:patched:1.0:client]"}, "PATCHED_SHA": map[string]any{"client": "'" + patchedHash + "'"}, "BINPATCH": map[string]any{"client": "/data/client.lzma"}},
			"libraries": []map[string]any{{"name": "net.custom:proc:1.0", "downloads": map[string]any{"artifact": map[string]any{"path": "net/custom/proc/1.0/proc-1.0.jar", "url": srvURL + "/libs/proc.jar", "size": len(procJarBytes), "sha1": sha1Hex(procJarBytes)}}}},
			"processors": []map[string]any{{"sides": []string{"client"}, "jar": "net.custom:proc:1.0", "classpath": []string{}, "args": []string{"--input", "{MINECRAFT_JAR}", "--patch", "{BINPATCH}", "--output", "{PATCHED}"}, "outputs": map[string]string{"{PATCHED}": "{PATCHED_SHA}"}}},
		})
		verJSON, _ := json.Marshal(map[string]any{
			"id": verID, "inheritsFrom": mcVer, "mainClass": "cpw.mods.bootstraplauncher.BootstrapLauncher",
			"libraries": []map[string]any{{"name": "cpw.mods:modlauncher:10.0", "downloads": map[string]any{"artifact": map[string]any{"path": "cpw/mods/modlauncher/10.0/modlauncher-10.0.jar", "url": srvURL + "/libs/modlauncher.jar", "size": len(verLibBytes), "sha1": sha1Hex(verLibBytes)}}}},
		})
		return buildTestZIP(t, map[string][]byte{"install_profile.json": profJSON, "version.json": verJSON, "data/client.lzma": []byte("LZMA")})
	}

	clientData := []byte("VANILLA-CLIENT-JAR")
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mc/manifest.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"latest": map[string]any{"release": "1.21.1"}, "versions": []map[string]any{{"id": "1.20.1", "type": "release", "url": srv.URL + "/mc/1.20.1.json"}, {"id": "1.21.1", "type": "release", "url": srv.URL + "/mc/1.21.1.json"}}})
		case "/mc/1.20.1.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "1.20.1", "mainClass": "net.minecraft.client.main.Main", "javaVersion": map[string]any{"majorVersion": 17}, "downloads": map[string]any{"client": map[string]any{"url": srv.URL + "/mc/client.jar", "size": len(clientData), "sha1": sha1Hex(clientData)}}})
		case "/mc/1.21.1.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "1.21.1", "mainClass": "net.minecraft.client.main.Main", "javaVersion": map[string]any{"majorVersion": 21}, "downloads": map[string]any{"client": map[string]any{"url": srv.URL + "/mc/client.jar", "size": len(clientData), "sha1": sha1Hex(clientData)}}})
		case "/mc/client.jar":
			_, _ = w.Write(clientData)
		case "/forge/promos.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"promos": map[string]string{"1.20.1-recommended": "47.3.0"}})
		case "/forge/maven-metadata.xml":
			_, _ = w.Write([]byte(`<metadata><versioning><versions><version>1.20.1-47.3.0</version></versions></versioning></metadata>`))
		case "/forge/net/minecraftforge/forge/1.20.1-47.3.0/forge-1.20.1-47.3.0-installer.jar":
			_, _ = w.Write(makeInstaller(srv.URL, "1.20.1-forge-47.3.0", "1.20.1"))
		case "/neoforge/maven-metadata.xml":
			_, _ = w.Write([]byte(`<metadata><versioning><versions><version>21.1.77</version><version>20.4.200</version></versions></versioning></metadata>`))
		case "/neoforge/net/neoforged/neoforge/21.1.77/neoforge-21.1.77-installer.jar":
			_, _ = w.Write(makeInstaller(srv.URL, "neoforge-21.1.77", "1.21.1"))
		case "/libs/proc.jar":
			_, _ = w.Write(procJarBytes)
		case "/libs/modlauncher.jar":
			_, _ = w.Write(verLibBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	db, _ := storage.OpenDatabase(filepath.Join(dir, "fnf.db"))
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

	javaExeDir := filepath.Join(dir, "runtimes", "mockjava", "bin")
	_ = os.MkdirAll(javaExeDir, 0o755)
	mockJavaExe := filepath.Join(javaExeDir, "java")
	_ = os.WriteFile(mockJavaExe, []byte("MOCK-JAVA"), 0o755)
	javaMgr := javart.NewManager(filepath.Join(dir, "runtimes"), hc, dlMgr, db, bus)
	javaMgr.SetProbeFunc(func(_ context.Context, _ string) (javart.ProbeInfo, error) {
		return javart.ProbeInfo{Version: "21.0.3", MajorVersion: 21, Vendor: "Mock", Arch: "x64", Bitness: 64, Runnable: true}, nil
	})
	_, _ = javaMgr.AddCustomPath(context.Background(), mockJavaExe)

	sharedDir := filepath.Join(dir, "shared")
	mcMgr := mcversion.NewManager(sharedDir, hc, dlMgr, javaMgr, bus)
	mcMgr.SetManifestURL(srv.URL + "/mc/manifest.json")
	instMgr := instances.NewManager(dir, filepath.Join(dir, "instances"), sharedDir, filepath.Join(dir, "runtimes"), filepath.Join(dir, "media"), db, javaMgr, bus, nil)
	prof, err := instMgr.Create(instances.CreateInstanceRequest{Name: "Modded Profile"})
	if err != nil {
		t.Fatalf("Create profile: %v", err)
	}

	runner := &fakeProcRunner{writeOut: map[string][]byte{"patched-1.0-client.jar": patchedOut}}
	mlMgr := NewManager(hc, mcMgr, dlMgr, javaMgr, instMgr, db, bus)
	defer mlMgr.Close()

	forgeP := mlMgr.providers[LoaderForge].(*ForgeProvider)
	forgeP.SetURLs(srv.URL+"/forge/promos.json", srv.URL+"/forge/maven-metadata.xml", srv.URL+"/forge")
	forgeP.SetProcessRunner(runner)
	nfP := mlMgr.providers[LoaderNeoForge].(*NeoForgeProvider)
	nfP.SetURLs(srv.URL+"/neoforge/maven-metadata.xml", srv.URL+"/neoforge/legacy.xml", srv.URL+"/neoforge")
	nfP.SetProcessRunner(runner)

	runner.shouldFail.Store(true)
	if _, err := mlMgr.InstallForInstanceSync(context.Background(), InstallProfileVersionRequest{InstanceID: prof.ID, MCVersion: "1.20.1", Loader: LoaderForge, LoaderVersion: "47.3.0"}); err == nil {
		t.Fatal("expected Forge install to fail on processor error")
	}
	runner.shouldFail.Store(false)

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mlMgr.InstallForInstanceSync(cancelCtx, InstallProfileVersionRequest{InstanceID: prof.ID, MCVersion: "1.20.1", Loader: LoaderForge, LoaderVersion: "47.3.0"}); err == nil {
		t.Fatal("expected cancelled install to fail")
	}

	ivForge, err := mlMgr.InstallForInstanceSync(context.Background(), InstallProfileVersionRequest{InstanceID: prof.ID, MCVersion: "1.20.1", Loader: LoaderForge, LoaderVersion: "47.3.0"})
	if err != nil || ivForge.VersionID != "1.20.1-forge-47.3.0" || !ivForge.Verified || runner.calls.Load() < 1 {
		t.Fatalf("Forge install failed: iv=%+v err=%v calls=%d", ivForge, err, runner.calls.Load())
	}
	ivNF, err := mlMgr.InstallForInstanceSync(context.Background(), InstallProfileVersionRequest{InstanceID: prof.ID, MCVersion: "1.21.1", Loader: LoaderNeoForge, LoaderVersion: "21.1.77", CreateBackup: true})
	if err != nil || ivNF.VersionID != "neoforge-21.1.77" || !ivNF.Verified {
		t.Fatalf("NeoForge install failed: iv=%+v err=%v", ivNF, err)
	}
	allIVs, _ := mlMgr.ListInstalledVersions(prof.ID)
	if len(allIVs) != 2 {
		t.Fatalf("expected 2 installed versions preserved in profile, got %d", len(allIVs))
	}
	if rep, err := mlMgr.VerifyInstalledVersion(context.Background(), prof.ID, ivNF.ID); err != nil || !rep.Valid {
		t.Fatalf("VerifyInstalledVersion failed: rep=%+v err=%v", rep, err)
	}
	readyProf, _ := instMgr.Get(prof.ID)
	if readyProf.Status != instances.StatusReady {
		t.Fatalf("expected profile status Ready, got %s (issues=%+v)", readyProf.Status, readyProf.Issues)
	}
}
