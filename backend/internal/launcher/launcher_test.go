package launcher

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"modhub/internal/accounts"
	"modhub/internal/events"
	"modhub/internal/instances"
	"modhub/internal/mcversion"
	javart "modhub/internal/runtime"
	"modhub/internal/storage"
)

func writeMockJAR(t *testing.T, p string, entries map[string]string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for k, v := range entries {
		w, err := zw.Create(k)
		if err != nil {
			t.Fatalf("zip create %s: %v", k, err)
		}
		_, _ = w.Write([]byte(v))
	}
	_ = zw.Close()
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write jar %s: %v", p, err)
	}
}

func seedMockFile(t *testing.T, p string, data string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatalf("seed %s: %v", p, err)
	}
}

// Covers Launch Arguments: legacy minecraftArguments, modern arguments.game/jvm, OS/arch/feature rules,
// variable substitution, Fabric/Forge/NeoForge inheritsFrom merging & classpath deduplication, logging config, safe JVM arg validation.
func TestBuildLaunchSpecLegacyModernLoadersAndSecurity(t *testing.T) {
	dir := t.TempDir()
	sharedDir := filepath.Join(dir, "shared")
	mcMgr := mcversion.NewManager(sharedDir, nil, nil, nil, nil)

	// Seed libraries & client JARs
	seedMockFile(t, filepath.Join(sharedDir, "versions", "1.12.2", "1.12.2.jar"), "MC-1.12.2-JAR")
	seedMockFile(t, filepath.Join(sharedDir, "versions", "1.21.1", "1.21.1.jar"), "MC-1.21.1-JAR")
	seedMockFile(t, filepath.Join(sharedDir, "libraries", "com", "mojang", "base", "1.0", "base-1.0.jar"), "LIB-BASE-1.0")
	seedMockFile(t, filepath.Join(sharedDir, "libraries", "com", "mojang", "base", "2.0", "base-2.0.jar"), "LIB-BASE-2.0")
	seedMockFile(t, filepath.Join(sharedDir, "libraries", "net", "fabricmc", "fabric-loader", "0.16.9", "fabric-loader-0.16.9.jar"), "FAB-LOADER")
	seedMockFile(t, filepath.Join(sharedDir, "libraries", "cpw", "mods", "bootstraplauncher", "2.0", "bootstraplauncher-2.0.jar"), "BOOT")
	seedMockFile(t, filepath.Join(sharedDir, "assets", "log_configs", "client-1.12.xml"), "<Configuration/>")

	creds := accounts.LaunchCredentials{
		AccountID: "acct_1", MinecraftUUID: "00000000000000000000000000000001",
		MinecraftUsername: "Alex", XboxUserID: "xuid_1",
		LicenseStatus: accounts.LicenseOwned, MinecraftAccessToken: "TOP_SECRET_MC_JWT_TOKEN_99999",
	}
	platWin := mcversion.PlatformFor("windows", "amd64")
	sysMem := SystemMemoryInfo{TotalRAMMB: 16384, AvailableRAMMB: 12000, RecommendedMaxMB: 12288}
	inst := instances.Instance{
		ID: "inst_1", Name: "Test", GameDir: filepath.Join(dir, "game"),
		MemoryMinMB: 1024, MemoryMaxMB: 4096,
		LaunchSettings: instances.LaunchSettings{JVMArgs: "-XX:+UseG1GC", WindowWidth: 1600, WindowHeight: 900, Fullscreen: true},
	}

	// 1. Legacy 1.12.2 Vanilla (minecraftArguments)
	legacyMeta := &mcversion.VersionMetadata{
		ID: "1.12.2", Type: "release", MainClass: "net.minecraft.client.main.Main", Assets: "1.12",
		MinecraftArguments: "--username ${auth_player_name} --version ${version_name} --gameDir ${game_directory} --assetsDir ${assets_root} --assetIndex ${assets_index_name} --uuid ${auth_uuid} --accessToken ${auth_access_token} --userType ${user_type}",
		Libraries: []mcversion.Library{
			{Name: "com.mojang:base:1.0", Downloads: &mcversion.LibraryDownloads{Artifact: &mcversion.DownloadArtifact{Path: "com/mojang/base/1.0/base-1.0.jar", URL: "https://libraries.minecraft.net/x"}}},
		},
	}
	specLegacy, err := BuildLaunchSpec(sharedDir, inst, legacyMeta, "1.12.2", filepath.Join(dir, "natives"), "/usr/bin/java", creds, platWin, sysMem)
	if err != nil {
		t.Fatalf("BuildLaunchSpec legacy failed: %v", err)
	}
	if !slices.Contains(specLegacy.RawArgs, "Alex") || !slices.Contains(specLegacy.RawArgs, "--fullscreen") || !slices.Contains(specLegacy.RawArgs, "1600") {
		t.Fatalf("legacy args missing expected values: %+v", specLegacy.SanitizedArgs)
	}
	// Ensure sanitized preview NEVER contains access token!
	for _, a := range specLegacy.SanitizedArgs {
		if strings.Contains(a, "TOP_SECRET_MC_JWT_TOKEN_99999") {
			t.Fatalf("secret access token leaked into SanitizedArgs: %q", a)
		}
	}

	// 2. Modern 1.21.1 + Fabric inheritsFrom (verifies child library override 2.0 > 1.0, feature rules, logging config)
	base121 := mcversion.VersionMetadata{
		ID: "1.21.1", Type: "release", MainClass: "net.minecraft.client.main.Main",
		AssetIndex: &mcversion.AssetIndexRef{ID: "17"},
		Logging: &mcversion.LoggingConfig{Client: &struct {
			Argument string                     `json:"argument"`
			File     mcversion.DownloadArtifact `json:"file"`
			Type     string                     `json:"type"`
		}{Argument: "-Dlog4j.configurationFile=${path}", File: mcversion.DownloadArtifact{ID: "client-1.12.xml"}}},
		Arguments: &mcversion.VersionArguments{
			JVM: []mcversion.ArgumentEntry{
				{Rules: []mcversion.Rule{{Action: "allow", OS: &mcversion.RuleOS{Name: "windows"}}}, Values: []string{"-Dwin.only=true"}},
				{Rules: []mcversion.Rule{{Action: "allow", OS: &mcversion.RuleOS{Name: "osx"}}}, Values: []string{"-XstartOnFirstThread"}},
				{Values: []string{"-Djava.library.path=${natives_directory}", "-cp", "${classpath}"}},
			},
			Game: []mcversion.ArgumentEntry{
				{Values: []string{"--username", "${auth_player_name}", "--accessToken", "${auth_access_token}"}},
				{Rules: []mcversion.Rule{{Action: "allow", Features: map[string]bool{"has_custom_resolution": true}}}, Values: []string{"--width", "${resolution_width}", "--height", "${resolution_height}"}},
				{Rules: []mcversion.Rule{{Action: "allow", Features: map[string]bool{"is_demo_user": true}}}, Values: []string{"--demo"}},
			},
		},
		Libraries: []mcversion.Library{
			{Name: "com.mojang:base:1.0", Downloads: &mcversion.LibraryDownloads{Artifact: &mcversion.DownloadArtifact{Path: "com/mojang/base/1.0/base-1.0.jar", URL: "https://libraries.minecraft.net/1"}}},
		},
	}
	_ = storage.WriteJSONFileAtomic(mcMgr.VersionDir("1.21.1"), "1.21.1.json", base121, 0o644)

	fabricChild := mcversion.VersionMetadata{
		ID: "fabric-loader-0.16.9-1.21.1", InheritsFrom: "1.21.1", MainClass: "net.fabricmc.loader.impl.launch.knot.KnotClient",
		Libraries: []mcversion.Library{
			{Name: "com.mojang:base:2.0", Downloads: &mcversion.LibraryDownloads{Artifact: &mcversion.DownloadArtifact{Path: "com/mojang/base/2.0/base-2.0.jar", URL: "https://libraries.minecraft.net/2"}}},
			{Name: "net.fabricmc:fabric-loader:0.16.9", Downloads: &mcversion.LibraryDownloads{Artifact: &mcversion.DownloadArtifact{Path: "net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar", URL: "https://maven.fabricmc.net/x"}}},
		},
	}
	_ = storage.WriteJSONFileAtomic(mcMgr.VersionDir("fabric-loader-0.16.9-1.21.1"), "fabric-loader-0.16.9-1.21.1.json", fabricChild, 0o644)

	mergedFab, baseID, err := ResolveEffectiveMetadata(mcMgr, "fabric-loader-0.16.9-1.21.1")
	if err != nil || baseID != "1.21.1" || mergedFab.MainClass != "net.fabricmc.loader.impl.launch.knot.KnotClient" {
		t.Fatalf("ResolveEffectiveMetadata fabric failed: base=%s meta=%+v err=%v", baseID, mergedFab, err)
	}
	specFab, err := BuildLaunchSpec(sharedDir, inst, mergedFab, baseID, filepath.Join(dir, "natives"), "/usr/bin/java", creds, platWin, sysMem)
	if err != nil {
		t.Fatalf("BuildLaunchSpec fabric failed: %v", err)
	}
	joined := strings.Join(specFab.SanitizedArgs, " ")
	if !strings.Contains(joined, "-Dwin.only=true") || strings.Contains(joined, "-XstartOnFirstThread") || strings.Contains(joined, "--demo") {
		t.Fatalf("OS/feature rules mismatch: %s", joined)
	}
	if !strings.Contains(joined, "base-2.0.jar") || strings.Contains(joined, "base-1.0.jar") {
		t.Fatalf("expected child library 2.0 to override parent 1.0 in classpath: %s", joined)
	}
	if !strings.Contains(joined, "-Dlog4j.configurationFile=") {
		t.Fatalf("expected logging config JVM arg: %s", joined)
	}

	// 3. Modern NeoForge BootstrapLauncher (does NOT pollute -cp with raw vanilla client.jar, resolves ${library_directory})
	neoChild := mcversion.VersionMetadata{
		ID: "neoforge-21.1.77", InheritsFrom: "1.21.1", MainClass: "cpw.mods.bootstraplauncher.BootstrapLauncher",
		Arguments: &mcversion.VersionArguments{
			JVM:  []mcversion.ArgumentEntry{{Values: []string{"-DlibraryDirectory=${library_directory}", "-DignoreList=bootstraplauncher"}}},
			Game: []mcversion.ArgumentEntry{{Values: []string{"--fml.neoForgeVersion", "21.1.77"}}},
		},
		Libraries: []mcversion.Library{
			{Name: "cpw.mods:bootstraplauncher:2.0", Downloads: &mcversion.LibraryDownloads{Artifact: &mcversion.DownloadArtifact{Path: "cpw/mods/bootstraplauncher/2.0/bootstraplauncher-2.0.jar", URL: "https://maven.neoforged.net/b"}}},
		},
	}
	_ = storage.WriteJSONFileAtomic(mcMgr.VersionDir("neoforge-21.1.77"), "neoforge-21.1.77.json", neoChild, 0o644)
	mergedNeo, _, err := ResolveEffectiveMetadata(mcMgr, "neoforge-21.1.77")
	if err != nil {
		t.Fatalf("ResolveEffectiveMetadata neoforge: %v", err)
	}
	specNeo, err := BuildLaunchSpec(sharedDir, inst, mergedNeo, "1.21.1", filepath.Join(dir, "natives"), "/usr/bin/java", creds, platWin, sysMem)
	if err != nil {
		t.Fatalf("BuildLaunchSpec neoforge: %v", err)
	}
	neoJoined := strings.Join(specNeo.SanitizedArgs, " ")
	if strings.Contains(neoJoined, "1.21.1.jar") || !strings.Contains(neoJoined, "--fml.neoForgeVersion") || !strings.Contains(neoJoined, "-DlibraryDirectory=") {
		t.Fatalf("unexpected NeoForge launch spec: %s", neoJoined)
	}

	// 4. Safe JVM arg validator rejects shell injection & reserved flags
	for _, bad := range []string{"-Xmx2G; calc.exe", "-XX:+UseG1GC && whoami", "$(evil)", "-cp /tmp/evil.jar", "positional"} {
		if _, err := ParseSafeJVMArgs(bad); err == nil {
			t.Errorf("expected dangerous JVM arg %q to be rejected", bad)
		}
	}
}

// Covers Native Libraries: extraction, exclude rules, Zip Slip protection, and safe reuse
func TestPrepareNativesExtractExcludeZipSlipAndReuse(t *testing.T) {
	dir := t.TempDir()
	sharedDir := filepath.Join(dir, "shared")
	plat := mcversion.PlatformFor("windows", "amd64")

	goodRel := "org/lwjgl/lwjgl-nat/3.3/lwjgl-nat-3.3.jar"
	writeMockJAR(t, filepath.Join(sharedDir, "libraries", filepath.FromSlash(goodRel)), map[string]string{
		"META-INF/MANIFEST.MF": "Manifest",
		"lwjgl.dll":            "DLL-BINARY",
		"skip/ignored.txt":     "SKIP",
	})
	libs := []mcversion.ResolvedLibraryFile{{
		Name: "org.lwjgl:lwjgl-nat:3.3", RelPath: goodRel, IsNative: true, SHA1: "abc123",
		Extract: &mcversion.ExtractRule{Exclude: []string{"META-INF/", "skip/"}},
	}}

	natDir, cnt, reused, err := PrepareNatives(sharedDir, "1.21.1", libs, plat)
	if err != nil || cnt != 1 || reused {
		t.Fatalf("first PrepareNatives = (%q, %d, %v, %v)", natDir, cnt, reused, err)
	}
	if _, err := os.Stat(filepath.Join(natDir, "lwjgl.dll")); err != nil {
		t.Fatal("expected lwjgl.dll extracted")
	}
	if _, err := os.Stat(filepath.Join(natDir, "skip", "ignored.txt")); !os.IsNotExist(err) {
		t.Fatal("excluded file should not be extracted")
	}

	// Reuse on second call
	_, _, reused2, err := PrepareNatives(sharedDir, "1.21.1", libs, plat)
	if err != nil || !reused2 {
		t.Fatalf("expected natives reuse=true on second call, got reused=%v err=%v", reused2, err)
	}

	// Zip Slip archive MUST be rejected
	evilRel := "org/evil/zipslip/1.0/zipslip-1.0.jar"
	writeMockJAR(t, filepath.Join(sharedDir, "libraries", filepath.FromSlash(evilRel)), map[string]string{
		"../../escaped.dll": "EVIL",
	})
	evilLibs := []mcversion.ResolvedLibraryFile{{Name: "org.evil:zipslip:1.0", RelPath: evilRel, IsNative: true}}
	if _, _, _, err := PrepareNatives(sharedDir, "1.21.1-evil", evilLibs, plat); err == nil {
		t.Fatal("expected Zip Slip native JAR to be rejected")
	}
}

// Covers Java compatibility checks: incompatible major, 32-bit arch with >1536MB RAM, missing Java, custom Java
func TestJavaCompatibilityAndArchitectureChecks(t *testing.T) {
	dir := t.TempDir()
	mcMgr := mcversion.NewManager(filepath.Join(dir, "shared"), nil, nil, nil, nil)
	mcMgr.SetPlatform(mcversion.PlatformFor("windows", "amd64"))
	mgr := NewManager(filepath.Join(dir, "shared"), nil, mcMgr, nil, nil, nil, nil, nil)

	meta21 := &mcversion.VersionMetadata{ID: "1.21.1", JavaVersion: &mcversion.JavaVersionSpec{MajorVersion: 21}}
	inst := instances.Instance{ID: "i1", MCVersion: "1.21.1", MemoryMaxMB: 4096, JavaMode: instances.JavaModeCustom, JavaPath: "/fake/java"}

	// Wrong major version (Java 8 for MC 1.21.1)
	mgr.SetJavaProbeFunc(func(context.Context, string) (javart.ProbeInfo, error) {
		return javart.ProbeInfo{Version: "1.8.0_392", MajorVersion: 8, Arch: "x64", Bitness: 64, Runnable: true}, nil
	})
	if _, _, _, err := mgr.resolveAndVerifyJava(context.Background(), inst, meta21, "vanilla"); !errors.Is(err, ErrJavaIncompatible) {
		t.Fatalf("expected ErrJavaIncompatible for Java 8 on 1.21.1, got %v", err)
	}

	// Wrong architecture (32-bit Java with 4096 MB RAM)
	mgr.SetJavaProbeFunc(func(context.Context, string) (javart.ProbeInfo, error) {
		return javart.ProbeInfo{Version: "21.0.3", MajorVersion: 21, Arch: "x86", Bitness: 32, Runnable: true}, nil
	})
	if _, _, _, err := mgr.resolveAndVerifyJava(context.Background(), inst, meta21, "vanilla"); !errors.Is(err, ErrJavaIncompatible) {
		t.Fatalf("expected ErrJavaIncompatible for 32-bit Java with 4GB RAM, got %v", err)
	}

	// Compatible 64-bit custom Java succeeds
	mgr.SetJavaProbeFunc(func(context.Context, string) (javart.ProbeInfo, error) {
		return javart.ProbeInfo{Version: "21.0.3", MajorVersion: 21, Arch: "x64", Bitness: 64, Runnable: true}, nil
	})
	if exe, _, _, err := mgr.resolveAndVerifyJava(context.Background(), inst, meta21, "vanilla"); err != nil || exe != "/fake/java" {
		t.Fatalf("expected valid custom Java 21, got (%q, %v)", exe, err)
	}
}

// Covers Real Game Process lifecycle: spawn, PID, stdout/stderr log capture, token redaction in logs,
// duplicate launch prevention, concurrent launch of 2 profiles, stop & force kill, exit code, SQLite zero-secret persistence!
func TestRealSubprocessLifecycleConcurrencyStopAndRedaction(t *testing.T) {
	// Use real `java -version` if available, else fall back to `go` / `sh` / `cmd`
	shortExe, shortArgs := "sh", []string{"-c", "echo 'Sound engine started'; echo 'Bearer SECRET_GAME_TOKEN_777777'; exit 0"}
	sleepExe, sleepArgs := "sh", []string{"-c", "echo 'Booting test instance'; sleep 30"}
	if _, err := exec.LookPath("sh"); err != nil {
		shortExe, shortArgs = "cmd", []string{"/C", "echo Sound engine started & echo Bearer SECRET_GAME_TOKEN_777777"}
		sleepExe, sleepArgs = "cmd", []string{"/C", "ping -n 30 127.0.0.1 >nul"}
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "launch_test.db")
	db, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer db.Close()
	bus := events.NewBus()
	defer bus.Close()

	mgr := NewManager(filepath.Join(dir, "shared"), nil, nil, nil, nil, nil, db, bus)
	mgr.SetGracefulWait(300 * time.Millisecond)

	// 1. Real subprocess exit 0 + log redaction + game_loaded detection
	workA := filepath.Join(dir, "instA")
	sessA, err := mgr.LaunchTestProcessSync("inst_A", shortExe, shortArgs, workA, []string{"SECRET_GAME_TOKEN_777777"})
	if err != nil || sessA.PID <= 0 {
		t.Fatalf("LaunchTestProcessSync A failed: sess=%+v err=%v", sessA, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cur := mgr.GetInstanceSession("inst_A"); cur != nil && !cur.State.IsActive() {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	finA := mgr.GetInstanceSession("inst_A")
	if finA == nil || finA.State != StateExited || finA.ExitCode != 0 {
		t.Fatalf("expected clean exit 0 for inst_A, got %+v", finA)
	}
	// Verify token redacted in both ring buffer and log file!
	for _, ln := range mgr.GetInstanceLogs("inst_A", 100) {
		if strings.Contains(ln.Text, "SECRET_GAME_TOKEN_777777") {
			t.Fatalf("token leaked into ring buffer log: %q", ln.Text)
		}
	}
	if rawLog, err := os.ReadFile(finA.LogFilePath); err == nil && strings.Contains(string(rawLog), "SECRET_GAME_TOKEN_777777") {
		t.Fatal("token leaked into disk log file!")
	}

	// 2. Concurrent launch of 2 different profiles + duplicate launch block + Stop/ForceKill
	workB := filepath.Join(dir, "instB")
	workC := filepath.Join(dir, "instC")
	sessB, err := mgr.LaunchTestProcessSync("inst_B", sleepExe, sleepArgs, workB, nil)
	if err != nil || sessB.PID <= 0 {
		t.Fatalf("Launch B failed: %v", err)
	}
	// Duplicate launch of inst_B MUST fail with ErrAlreadyRunning
	if _, err := mgr.LaunchTestProcessSync("inst_B", sleepExe, sleepArgs, workB, nil); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning on duplicate inst_B launch, got %v", err)
	}
	// Different profile inst_C MUST launch concurrently
	sessC, err := mgr.LaunchTestProcessSync("inst_C", sleepExe, sleepArgs, workC, nil)
	if err != nil || sessC.PID <= 0 {
		t.Fatalf("concurrent Launch C failed: %v", err)
	}
	if mgr.RunningCount() != 2 {
		t.Fatalf("expected 2 concurrent running processes, got %d", mgr.RunningCount())
	}

	// Graceful stop on B, force kill on C
	if err := mgr.Stop("inst_B", false); err != nil {
		t.Fatalf("Stop B failed: %v", err)
	}
	if err := mgr.Stop("inst_C", true); err != nil {
		t.Fatalf("Force Stop C failed: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && mgr.HasRunningGames() {
		time.Sleep(30 * time.Millisecond)
	}
	if mgr.HasRunningGames() {
		t.Fatal("expected both stopped processes to exit")
	}

	// 3. Invalid executable fails cleanly with StateError
	if _, err := mgr.LaunchTestProcessSync("inst_bad", filepath.Join(dir, "nonexistent_java_bin"), nil, filepath.Join(dir, "instBad"), nil); err == nil {
		t.Fatal("expected launch failure for nonexistent executable")
	}
	badSess := mgr.GetInstanceSession("inst_bad")
	if badSess == nil || badSess.State != StateError {
		t.Fatalf("expected StateError for failed launch, got %+v", badSess)
	}

	// 4. Verify SQLite file contains zero tokens
	dbBytes, _ := os.ReadFile(dbPath)
	if strings.Contains(string(dbBytes), "SECRET_GAME_TOKEN_777777") {
		t.Fatal("token leaked into SQLite database!")
	}
}

func TestAnalyzeCrashCases(t *testing.T) {
	// 1. Java version incompatibility
	dJava := AnalyzeCrash(1, "", []LogLine{{Text: "java.lang.UnsupportedClassVersionError: net/minecraft/client/main/Main has been compiled by a more recent version of the Java Runtime (class file version 65.0)"}}, nil)
	if dJava == nil || dJava.Code != "java_version" || !strings.Contains(dJava.Cause, "Java 21") {
		t.Fatalf("expected java_version Java 21 diagnosis, got %+v", dJava)
	}

	// 2. Fabric missing dependency
	dDep := AnalyzeCrash(1, "", []LogLine{{Text: "Mod 'sodium' requires any version of fabric-api, which is missing!"}}, nil)
	if dDep == nil || dDep.Code != "missing_dependency" || dDep.RelatedMod != "fabric-api" {
		t.Fatalf("expected missing_dependency fabric-api diagnosis, got %+v", dDep)
	}

	// 3. Out of memory
	dOOM := AnalyzeCrash(1, "", []LogLine{{Text: "java.lang.OutOfMemoryError: Java heap space"}}, nil)
	if dOOM == nil || dOOM.Code != "out_of_memory" {
		t.Fatalf("expected out_of_memory diagnosis, got %+v", dOOM)
	}

	// 4. Mixin failure
	dMix := AnalyzeCrash(1, "", []LogLine{{Text: "org.spongepowered.asm.mixin.transformer.throwables.MixinTransformerError: Critical injection failure from mod iris"}}, nil)
	if dMix == nil || dMix.Code != "mixin_conflict" || dMix.RelatedMod != "iris" {
		t.Fatalf("expected mixin_conflict iris diagnosis, got %+v", dMix)
	}

	// 5. OpenGL driver error
	dGPU := AnalyzeCrash(1, "", []LogLine{{Text: "GLFW error 65542: WGL: The driver does not appear to support OpenGL"}}, nil)
	if dGPU == nil || dGPU.Code != "graphics_driver" {
		t.Fatalf("expected graphics_driver diagnosis, got %+v", dGPU)
	}
}
