package runtime

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

var (
	reJavaVersionQuote = regexp.MustCompile(`(?i)version\s+"([^"]+)"`)
	reJavaPropVersion  = regexp.MustCompile(`(?m)^\s*java\.version\s*=\s*(.+)$`)
	reJavaPropVendor   = regexp.MustCompile(`(?m)^\s*java\.vendor\s*=\s*(.+)$`)
	reJavaPropArch     = regexp.MustCompile(`(?m)^\s*os\.arch\s*=\s*(.+)$`)
	reJavaPropBits     = regexp.MustCompile(`(?m)^\s*sun\.arch\.data\.model\s*=\s*(\d+)`)
)

// ProbeInfo holds verified metadata obtained by running a real java executable.
type ProbeInfo struct {
	Version      string
	MajorVersion int
	Vendor       string
	Arch         string
	Bitness      int
	Runnable     bool
}

func isAllowedJavaBinaryName(path string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	return base == "java" || base == "java.exe" || base == "javaw.exe"
}

// ResolveJavaExecutable normalizes a user-supplied path (either a JDK root dir, bin dir, or java executable).
func ResolveJavaExecutable(inputPath string) (string, error) {
	p := strings.TrimSpace(inputPath)
	if p == "" {
		return "", fmt.Errorf("пустой путь к Java")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("путь не существует: %w", err)
	}
	exeName := "java"
	if goruntime.GOOS == "windows" {
		exeName = "java.exe"
	}
	if info.IsDir() {
		cands := []string{
			filepath.Join(abs, "bin", exeName),
			filepath.Join(abs, exeName),
			filepath.Join(abs, "Contents", "Home", "bin", exeName),
		}
		for _, c := range cands {
			if ci, err := os.Stat(c); err == nil && !ci.IsDir() {
				return filepath.Clean(c), nil
			}
		}
		return "", fmt.Errorf("исполняемый файл %s не найден в указанной папке", exeName)
	}
	if !isAllowedJavaBinaryName(abs) {
		return "", fmt.Errorf("допускаются только исполняемые файлы java / java.exe / javaw.exe")
	}
	// Prefer console java.exe next to javaw.exe so `-version` writes reliably to stderr
	if strings.EqualFold(filepath.Base(abs), "javaw.exe") {
		alt := filepath.Join(filepath.Dir(abs), "java.exe")
		if ai, err := os.Stat(alt); err == nil && !ai.IsDir() {
			return filepath.Clean(alt), nil
		}
	}
	return filepath.Clean(abs), nil
}

// ParseJavaMajor extracts the numeric major Java version from strings like "1.8.0_392", "17.0.10", "21.0.3", "21-ea".
func ParseJavaMajor(ver string) int {
	v := strings.TrimSpace(ver)
	if v == "" {
		return 0
	}
	// Strip suffixes like -ea, +7, _392
	for _, sep := range []string{"-", "+", "_"} {
		if idx := strings.Index(v, sep); idx > 0 {
			v = v[:idx]
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 {
		return 0
	}
	first, err := strconv.Atoi(parts[0])
	if err != nil || first <= 0 {
		return 0
	}
	if first == 1 && len(parts) >= 2 {
		if second, err := strconv.Atoi(parts[1]); err == nil && second > 0 {
			return second
		}
	}
	return first
}

func normalizeArch(raw string) (string, int) {
	low := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(low, "aarch64") || strings.Contains(low, "arm64"):
		return "arm64", 64
	case strings.Contains(low, "amd64") || strings.Contains(low, "x86_64") || strings.Contains(low, "x64") || strings.Contains(low, "64-bit"):
		return "x64", 64
	case strings.Contains(low, "x86") || strings.Contains(low, "i386") || strings.Contains(low, "i686") || strings.Contains(low, "32-bit"):
		return "x86", 32
	default:
		return "", 0
	}
}

// ParseJavaVersionOutput parses stdout/stderr of `java -XshowSettings:properties -version` or `java -version`.
func ParseJavaVersionOutput(output string) ProbeInfo {
	info := ProbeInfo{}
	if m := reJavaPropVersion.FindStringSubmatch(output); len(m) == 2 {
		info.Version = strings.TrimSpace(m[1])
	}
	if info.Version == "" {
		if m := reJavaVersionQuote.FindStringSubmatch(output); len(m) == 2 {
			info.Version = strings.TrimSpace(m[1])
		}
	}
	info.MajorVersion = ParseJavaMajor(info.Version)

	if m := reJavaPropVendor.FindStringSubmatch(output); len(m) == 2 {
		info.Vendor = strings.TrimSpace(m[1])
	} else {
		low := strings.ToLower(output)
		switch {
		case strings.Contains(low, "temurin") || strings.Contains(low, "adoptium"):
			info.Vendor = "Eclipse Adoptium"
		case strings.Contains(low, "microsoft"):
			info.Vendor = "Microsoft"
		case strings.Contains(low, "zulu") || strings.Contains(low, "azul"):
			info.Vendor = "Azul Zulu"
		case strings.Contains(low, "corretto"):
			info.Vendor = "Amazon Corretto"
		case strings.Contains(low, "liberica") || strings.Contains(low, "bellsoft"):
			info.Vendor = "BellSoft Liberica"
		case strings.Contains(low, "openjdk"):
			info.Vendor = "OpenJDK"
		case strings.Contains(low, "hotspot") || strings.Contains(low, "oracle"):
			info.Vendor = "Oracle"
		}
	}

	if m := reJavaPropArch.FindStringSubmatch(output); len(m) == 2 {
		info.Arch, info.Bitness = normalizeArch(m[1])
	}
	if info.Arch == "" {
		info.Arch, info.Bitness = normalizeArch(output)
	}
	if m := reJavaPropBits.FindStringSubmatch(output); len(m) == 2 {
		if b, err := strconv.Atoi(m[1]); err == nil && (b == 32 || b == 64) {
			info.Bitness = b
		}
	}
	if info.Arch == "" {
		switch goruntime.GOARCH {
		case "amd64":
			info.Arch = "x64"
		case "arm64":
			info.Arch = "arm64"
		case "386":
			info.Arch = "x86"
		default:
			info.Arch = goruntime.GOARCH
		}
	}
	if info.Bitness == 0 {
		if info.Arch == "x86" {
			info.Bitness = 32
		} else {
			info.Bitness = 64
		}
	}
	info.Runnable = info.MajorVersion > 0
	return info
}

// ProbeJavaExecutable safely executes a verified java binary with a strict 5-second timeout.
func ProbeJavaExecutable(ctx context.Context, rawPath string) (ProbeInfo, error) {
	exePath, err := ResolveJavaExecutable(rawPath)
	if err != nil {
		return ProbeInfo{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	pCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(pCtx, exePath, "-XshowSettings:properties", "-version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	parsed := ParseJavaVersionOutput(out.String())
	if err != nil || !parsed.Runnable {
		// Fallback to plain `-version` (some minimal/legacy JREs reject -XshowSettings)
		pCtx2, cancel2 := context.WithTimeout(ctx, 5*time.Second)
		defer cancel2()
		cmd2 := exec.CommandContext(pCtx2, exePath, "-version")
		var out2 bytes.Buffer
		cmd2.Stdout = &out2
		cmd2.Stderr = &out2
		if err2 := cmd2.Run(); err2 != nil {
			return ProbeInfo{}, fmt.Errorf("не удалось запустить Java (%s): %w", exePath, err2)
		}
		parsed = ParseJavaVersionOutput(out2.String())
		if !parsed.Runnable {
			return ProbeInfo{}, fmt.Errorf("не удалось распознать версию Java из вывода %s", exePath)
		}
	}
	return parsed, nil
}

func makeRuntimeID(prefix, exePath string) string {
	norm := strings.ToLower(filepath.Clean(exePath))
	h := sha1.Sum([]byte(norm))
	return prefix + "_" + hex.EncodeToString(h[:])[:12]
}

type discoveredCandidate struct {
	exePath string
	source  string
}

func discoverSystemCandidates(runtimesDir string) []discoveredCandidate {
	seen := map[string]struct{}{}
	var out []discoveredCandidate
	add := func(raw, source string) {
		exe, err := ResolveJavaExecutable(raw)
		if err != nil {
			return
		}
		key := strings.ToLower(filepath.Clean(exe))
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		out = append(out, discoveredCandidate{exePath: exe, source: source})
	}

	// 1. ModHub-managed runtimes under data/runtimes/
	if runtimesDir != "" {
		if entries, err := os.ReadDir(runtimesDir); err == nil {
			for _, ent := range entries {
				if !ent.IsDir() || strings.HasPrefix(ent.Name(), ".staging-") {
					continue
				}
				add(filepath.Join(runtimesDir, ent.Name()), "managed")
			}
		}
	}

	// 2. JAVA_HOME & PATH
	if jh := strings.TrimSpace(os.Getenv("JAVA_HOME")); jh != "" {
		add(jh, "path")
	}
	if lp, err := exec.LookPath("java"); err == nil && lp != "" {
		add(lp, "path")
	}

	// 3. System Registry (Windows JDK/JRE installers)
	discoverRegistryCandidates(add)

	// 4. Standard OS installation roots
	scanParent := func(parentDir string, depth int) {
		if strings.TrimSpace(parentDir) == "" {
			return
		}
		entries, err := os.ReadDir(parentDir)
		if err != nil {
			return
		}
		for _, ent := range entries {
			if !ent.IsDir() {
				continue
			}
			sub := filepath.Join(parentDir, ent.Name())
			add(sub, "system")
			if depth > 1 {
				if subEntries, err := os.ReadDir(sub); err == nil {
					for _, se := range subEntries {
						if se.IsDir() {
							add(filepath.Join(sub, se.Name()), "system")
						}
					}
				}
			}
		}
	}

	home, _ := os.UserHomeDir()
	if goruntime.GOOS == "windows" {
		pf := os.Getenv("ProgramFiles")
		if pf == "" {
			pf = `C:\Program Files`
		}
		pf86 := os.Getenv("ProgramFiles(x86)")
		if pf86 == "" {
			pf86 = `C:\Program Files (x86)`
		}
		localApp := os.Getenv("LOCALAPPDATA")
		for _, dir := range []string{
			filepath.Join(pf, "Java"),
			filepath.Join(pf, "Eclipse Adoptium"),
			filepath.Join(pf, "AdoptOpenJDK"),
			filepath.Join(pf, "Microsoft"),
			filepath.Join(pf, "Zulu"),
			filepath.Join(pf, "BellSoft"),
			filepath.Join(pf, "Amazon Corretto"),
			filepath.Join(pf86, "Java"),
			filepath.Join(pf86, "Minecraft Launcher", "runtime"),
		} {
			scanParent(dir, 2)
		}
		if localApp != "" {
			scanParent(filepath.Join(localApp, "Packages", "Microsoft.4297127D64EC6_8wekyb3d8bbwe", "LocalCache", "Local", "runtime"), 2)
		}
		if home != "" {
			scanParent(filepath.Join(home, ".jdks"), 1)
		}
	} else if goruntime.GOOS == "darwin" {
		scanParent("/Library/Java/JavaVirtualMachines", 1)
		if home != "" {
			scanParent(filepath.Join(home, "Library", "Java", "JavaVirtualMachines"), 1)
			scanParent(filepath.Join(home, ".jdks"), 1)
		}
	} else {
		scanParent("/usr/lib/jvm", 1)
		scanParent("/usr/java", 1)
		if home != "" {
			scanParent(filepath.Join(home, ".jdks"), 1)
		}
	}
	return out
}
