package runtime

import (
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
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"modhub/internal/downloads"
	"modhub/internal/events"
	"modhub/internal/netx"
	"modhub/internal/storage"
)

func sha1Str(b []byte) string {
	s := sha1.Sum(b)
	return hex.EncodeToString(s[:])
}

func TestParseJavaVersionOutputAndCompatibilityRules(t *testing.T) {
	// Java 8 output
	out8 := `
Property settings:
    java.vendor = Oracle Corporation
    java.version = 1.8.0_51
    os.arch = amd64
    sun.arch.data.model = 64
java version "1.8.0_51"
`
	p8 := ParseJavaVersionOutput(out8)
	if p8.MajorVersion != 8 || p8.Arch != "x64" || p8.Bitness != 64 || !p8.Runnable {
		t.Fatalf("Java 8 parse failed: %+v", p8)
	}

	// Java 17 output
	out17 := `openjdk version "17.0.10" 2024-01-16
OpenJDK Runtime Environment Temurin-17.0.10+7 (build 17.0.10+7)
OpenJDK 64-Bit Server VM Temurin-17.0.10+7 (build 17.0.10+7, mixed mode)`
	p17 := ParseJavaVersionOutput(out17)
	if p17.MajorVersion != 17 || p17.Bitness != 64 || p17.Vendor != "Eclipse Adoptium" {
		t.Fatalf("Java 17 parse failed: %+v", p17)
	}

	// Java 21 ARM64 output
	out21 := `
    java.vendor = Microsoft
    java.version = 21.0.3
    os.arch = aarch64
`
	p21 := ParseJavaVersionOutput(out21)
	if p21.MajorVersion != 21 || p21.Arch != "arm64" || p21.Bitness != 64 {
		t.Fatalf("Java 21 parse failed: %+v", p21)
	}

	// Compatibility engine rules
	if r := ResolveRequirement(RequirementInput{MCVersion: "1.16.5"}); r.RecommendedMajor != 8 || r.PreferredComponent != "jre-legacy" {
		t.Fatalf("1.16.5 rule mismatch: %+v", r)
	}
	if r := ResolveRequirement(RequirementInput{MCVersion: "1.17.1"}); r.RecommendedMajor != 16 || r.PreferredComponent != "java-runtime-alpha" {
		t.Fatalf("1.17.1 rule mismatch: %+v", r)
	}
	if r := ResolveRequirement(RequirementInput{MCVersion: "1.20.1"}); r.RecommendedMajor != 17 || r.PreferredComponent != "java-runtime-gamma" {
		t.Fatalf("1.20.1 rule mismatch: %+v", r)
	}
	if r := ResolveRequirement(RequirementInput{MCVersion: "1.21.1"}); r.RecommendedMajor != 21 || r.PreferredComponent != "java-runtime-delta" {
		t.Fatalf("1.21.1 rule mismatch: %+v", r)
	}
	// Explicit Minecraft manifest metadata overrides heuristic rules
	if r := ResolveRequirement(RequirementInput{MCVersion: "1.20.1", ManifestJavaMajor: 21}); r.RecommendedMajor != 21 || r.Source != "manifest" {
		t.Fatalf("manifest override mismatch: %+v", r)
	}
}

func TestInstallJava8_17_21IntegrityVerifyRemoveAndInterruptSafety(t *testing.T) {
	exeName := "java"
	if goruntime.GOOS == "windows" {
		exeName = "java.exe"
	}
	binRel := "bin/" + exeName

	java8Bytes := []byte("MOCK-JAVA-8-BIN")
	java17Bytes := []byte("MOCK-JAVA-17-BIN")
	java21Bytes := []byte("MOCK-JAVA-21-BIN")
	corruptBytes := []byte("CORRUPTED-PAYLOAD")

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/all.json":
			platKey, _ := currentMojangPlatform()
			payload := map[string]any{
				platKey: map[string]any{
					"jre-legacy": []map[string]any{{
						"manifest": map[string]any{"url": srv.URL + "/manifest-8.json", "size": 100, "sha1": "abc"},
						"version":  map[string]any{"name": "8u51", "released": "2015-07-14T00:00:00Z"},
					}},
					"java-runtime-gamma": []map[string]any{{
						"manifest": map[string]any{"url": srv.URL + "/manifest-17.json", "size": 100, "sha1": "def"},
						"version":  map[string]any{"name": "17.0.8", "released": "2023-07-18T00:00:00Z"},
					}},
					"java-runtime-delta": []map[string]any{{
						"manifest": map[string]any{"url": srv.URL + "/manifest-21.json", "size": 100, "sha1": "ghi"},
						"version":  map[string]any{"name": "21.0.3", "released": "2024-04-16T00:00:00Z"},
					}},
					"java-runtime-beta": []map[string]any{{
						"manifest": map[string]any{"url": srv.URL + "/manifest-corrupt.json", "size": 100, "sha1": "bad"},
						"version":  map[string]any{"name": "17.0.1", "released": "2021-10-19T00:00:00Z"},
					}},
				},
			}
			_ = json.NewEncoder(w).Encode(payload)
		case "/manifest-8.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": map[string]any{
					"bin":  map[string]any{"type": "directory"},
					binRel: map[string]any{"type": "file", "executable": true, "downloads": map[string]any{"raw": map[string]any{"url": srv.URL + "/files/j8", "size": len(java8Bytes), "sha1": sha1Str(java8Bytes)}}},
				},
			})
		case "/manifest-17.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": map[string]any{
					"bin":  map[string]any{"type": "directory"},
					binRel: map[string]any{"type": "file", "executable": true, "downloads": map[string]any{"raw": map[string]any{"url": srv.URL + "/files/j17", "size": len(java17Bytes), "sha1": sha1Str(java17Bytes)}}},
				},
			})
		case "/manifest-21.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": map[string]any{
					"bin":  map[string]any{"type": "directory"},
					binRel: map[string]any{"type": "file", "executable": true, "downloads": map[string]any{"raw": map[string]any{"url": srv.URL + "/files/j21", "size": len(java21Bytes), "sha1": sha1Str(java21Bytes)}}},
				},
			})
		case "/manifest-corrupt.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"files": map[string]any{
					"bin":  map[string]any{"type": "directory"},
					binRel: map[string]any{"type": "file", "executable": true, "downloads": map[string]any{"raw": map[string]any{"url": srv.URL + "/files/corrupt", "size": len(corruptBytes), "sha1": strings.Repeat("a", 40)}}},
				},
			})
		case "/files/j8":
			_, _ = w.Write(java8Bytes)
		case "/files/j17":
			_, _ = w.Write(java17Bytes)
		case "/files/j21":
			_, _ = w.Write(java21Bytes)
		case "/files/corrupt":
			_, _ = w.Write(corruptBytes)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	db, err := storage.OpenDatabase(filepath.Join(dir, "java.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
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

	runtimesDir := filepath.Join(dir, "runtimes")
	jMgr := NewManager(runtimesDir, hc, dlMgr, db, bus)
	jMgr.SetManifestURL(srv.URL + "/all.json")
	jMgr.SetProbeFunc(func(_ context.Context, exePath string) (ProbeInfo, error) {
		raw, err := os.ReadFile(exePath)
		if err != nil {
			return ProbeInfo{}, err
		}
		switch string(raw) {
		case string(java8Bytes):
			return ProbeInfo{Version: "1.8.0_51", MajorVersion: 8, Vendor: "Mojang", Arch: "x64", Bitness: 64, Runnable: true}, nil
		case string(java17Bytes):
			return ProbeInfo{Version: "17.0.8", MajorVersion: 17, Vendor: "Mojang", Arch: "x64", Bitness: 64, Runnable: true}, nil
		case string(java21Bytes):
			return ProbeInfo{Version: "21.0.3", MajorVersion: 21, Vendor: "Mojang", Arch: "x64", Bitness: 64, Runnable: true}, nil
		default:
			return ProbeInfo{Version: "21.0.0", MajorVersion: 21, Vendor: "External", Arch: "x64", Bitness: 64, Runnable: true}, nil
		}
	})
	defer jMgr.Close()

	// 4: Fetch official metadata
	av, err := jMgr.RefreshAvailable(context.Background())
	if err != nil || len(av) < 3 {
		t.Fatalf("RefreshAvailable failed: len=%d err=%v", len(av), err)
	}

	// 5, 6, 7, 8, 9: Install Java 8, Java 17, Java 21 via Download Manager + SHA-1 + probe check
	rt8, err := jMgr.InstallRuntimeSync(context.Background(), "jre-legacy")
	if err != nil || rt8.MajorVersion != 8 || !rt8.Managed || !rt8.Verified || !rt8.Runnable {
		t.Fatalf("Install Java 8 failed: rt=%+v err=%v", rt8, err)
	}
	rt17, err := jMgr.InstallRuntimeSync(context.Background(), "java-runtime-gamma")
	if err != nil || rt17.MajorVersion != 17 || !rt17.Managed || !rt17.Verified {
		t.Fatalf("Install Java 17 failed: rt=%+v err=%v", rt17, err)
	}
	rt21, err := jMgr.InstallRuntimeSync(context.Background(), "java-runtime-delta")
	if err != nil || rt21.MajorVersion != 21 || !rt21.Managed || !rt21.Verified {
		t.Fatalf("Install Java 21 failed: rt=%+v err=%v", rt21, err)
	}

	// 12: Corrupted SHA-1 install fails cleanly and leaves NO staging dir or broken registered runtime
	if _, err := jMgr.InstallRuntimeSync(context.Background(), "java-runtime-beta"); err == nil {
		t.Fatal("expected corrupted runtime install to fail")
	}
	entries, _ := os.ReadDir(runtimesDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".staging-") {
			t.Fatalf("leftover staging dir not cleaned up: %s", e.Name())
		}
	}

	// 10: In-use runtime cannot be deleted; once released, managed runtime deletes cleanly
	jMgr.AcquireInUse(rt17.ID)
	if err := jMgr.RemoveRuntime(rt17.ID); !errors.Is(err, ErrRuntimeInUse) {
		t.Fatalf("expected ErrRuntimeInUse, got %v", err)
	}
	jMgr.ReleaseInUse(rt17.ID)
	if err := jMgr.RemoveRuntime(rt17.ID); err != nil {
		t.Fatalf("RemoveRuntime managed Java 17 failed: %v", err)
	}
	if _, err := os.Stat(rt17.RootDir); !os.IsNotExist(err) {
		t.Fatal("expected managed runtime directory to be removed")
	}

	// 11: External / custom Java MUST NEVER be deleted!
	extDir := filepath.Join(dir, "external_jdk", "bin")
	_ = os.MkdirAll(extDir, 0o755)
	extExe := filepath.Join(extDir, exeName)
	_ = os.WriteFile(extExe, []byte("EXTERNAL-JDK"), 0o755)

	customRT, err := jMgr.AddCustomPath(context.Background(), extExe)
	if err != nil || customRT.Managed {
		t.Fatalf("AddCustomPath failed: rt=%+v err=%v", customRT, err)
	}
	if err := jMgr.RemoveRuntime(customRT.ID); !errors.Is(err, ErrNotManagedRuntime) {
		t.Fatalf("expected ErrNotManagedRuntime for external Java, got %v", err)
	}
	if _, err := os.Stat(extExe); err != nil {
		t.Fatal("external Java executable was unexpectedly touched!")
	}
}
