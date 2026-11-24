package mcversion

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
)

type PlatformInfo struct {
	OS      string
	Arch    string
	Bitness string
}

func CurrentPlatform() PlatformInfo { return PlatformFor(goruntime.GOOS, goruntime.GOARCH) }

func PlatformFor(goos, goarch string) PlatformInfo {
	osName := "linux"
	switch goos {
	case "windows":
		osName = "windows"
	case "darwin":
		osName = "osx"
	}
	arch, bits := "x64", "64"
	switch goarch {
	case "386":
		arch, bits = "x86", "32"
	case "arm64":
		arch, bits = "arm64", "64"
	}
	return PlatformInfo{OS: osName, Arch: arch, Bitness: bits}
}

func archMatches(ruleArch, targetArch string) bool {
	ra := strings.ToLower(strings.TrimSpace(ruleArch))
	ta := strings.ToLower(strings.TrimSpace(targetArch))
	if ra == "" {
		return true
	}
	switch ra {
	case "x86", "i386", "i686", "32":
		return ta == "x86"
	case "x64", "x86_64", "amd64", "64":
		return ta == "x64"
	case "arm64", "aarch64":
		return ta == "arm64"
	default:
		return ra == ta
	}
}

func EvaluateRules(rules []Rule, plat PlatformInfo, features map[string]bool) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, r := range rules {
		if !ruleMatches(r, plat, features) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(r.Action)) {
		case "allow":
			allowed = true
		case "disallow":
			allowed = false
		}
	}
	return allowed
}

func ruleMatches(r Rule, plat PlatformInfo, features map[string]bool) bool {
	if r.OS != nil {
		if r.OS.Name != "" && !strings.EqualFold(r.OS.Name, plat.OS) {
			return false
		}
		if r.OS.Arch != "" && !archMatches(r.OS.Arch, plat.Arch) {
			return false
		}
		if r.OS.Version != "" {
			if _, err := regexp.Compile(r.OS.Version); err != nil {
				return false
			}
		}
	}
	if len(r.Features) > 0 {
		for feat, want := range r.Features {
			actual := false
			if features != nil {
				actual = features[feat]
			}
			if actual != want {
				return false
			}
		}
	}
	return true
}

func MavenCoordToRelPath(coord string) (string, error) {
	c := strings.TrimSpace(coord)
	if c == "" {
		return "", fmt.Errorf("пустая Maven-координата")
	}
	ext := "jar"
	if idx := strings.LastIndex(c, "@"); idx > 0 {
		ext = strings.TrimSpace(c[idx+1:])
		c = c[:idx]
	}
	parts := strings.Split(c, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return "", fmt.Errorf("некорректная Maven-координата %q", coord)
	}
	group, artifact, version := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2])
	classifier := ""
	if len(parts) == 4 {
		classifier = strings.TrimSpace(parts[3])
	}
	if group == "" || artifact == "" || version == "" || ext == "" {
		return "", fmt.Errorf("неполная Maven-координата %q", coord)
	}
	groupPath := strings.ReplaceAll(group, ".", "/")
	filename := artifact + "-" + version
	if classifier != "" {
		filename += "-" + classifier
	}
	filename += "." + ext
	rel := path.Clean(groupPath + "/" + artifact + "/" + version + "/" + filename)
	if strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "..") || strings.Contains(rel, "/../") {
		return "", fmt.Errorf("небезопасный Maven-путь %q", rel)
	}
	return rel, nil
}

func ResolveLibraryFiles(lib Library, plat PlatformInfo) []ResolvedLibraryFile {
	if !EvaluateRules(lib.Rules, plat, nil) {
		return nil
	}
	lowName := strings.ToLower(lib.Name)
	if strings.Contains(lowName, ":natives-") && !nativeNameMatchesPlatform(lowName, plat) {
		return nil
	}

	var out []ResolvedLibraryFile
	if lib.Downloads != nil && lib.Downloads.Artifact != nil && lib.Downloads.Artifact.URL != "" {
		art := lib.Downloads.Artifact
		rel := art.Path
		if rel == "" {
			if p, err := MavenCoordToRelPath(lib.Name); err == nil {
				rel = p
			}
		}
		if rel != "" {
			out = append(out, ResolvedLibraryFile{
				Name: lib.Name, RelPath: rel, URL: art.URL, SHA1: art.SHA1, Size: art.Size,
				IsNative: strings.Contains(lowName, ":natives-"), Extract: lib.Extract,
			})
		}
	} else if lib.Downloads == nil && lib.Name != "" {
		if rel, err := MavenCoordToRelPath(lib.Name); err == nil {
			baseURL := strings.TrimRight(strings.TrimSpace(lib.URL), "/")
			if baseURL == "" {
				baseURL = "https://libraries.minecraft.net"
			}
			out = append(out, ResolvedLibraryFile{
				Name: lib.Name, RelPath: rel, URL: baseURL + "/" + rel, SHA1: lib.SHA1, Size: lib.Size,
			})
		}
	}

	if len(lib.Natives) > 0 && lib.Downloads != nil && len(lib.Downloads.Classifiers) > 0 {
		if rawClassifier, ok := lib.Natives[plat.OS]; ok && rawClassifier != "" {
			clsKey := strings.ReplaceAll(rawClassifier, "${arch}", plat.Bitness)
			if clsArt, found := lib.Downloads.Classifiers[clsKey]; found && clsArt.URL != "" {
				rel := clsArt.Path
				if rel == "" {
					if p, err := MavenCoordToRelPath(lib.Name + ":" + clsKey); err == nil {
						rel = p
					}
				}
				if rel != "" {
					out = append(out, ResolvedLibraryFile{
						Name: lib.Name + ":" + clsKey, RelPath: rel, URL: clsArt.URL, SHA1: clsArt.SHA1, Size: clsArt.Size,
						IsNative: true, Extract: lib.Extract,
					})
				}
			}
		}
	}
	return out
}

func nativeNameMatchesPlatform(lowName string, plat PlatformInfo) bool {
	idx := strings.Index(lowName, ":natives-")
	if idx < 0 {
		return true
	}
	suf := lowName[idx+len(":natives-"):]
	switch plat.OS {
	case "windows":
		if !strings.HasPrefix(suf, "windows") {
			return false
		}
	case "osx":
		if !strings.HasPrefix(suf, "macos") && !strings.HasPrefix(suf, "osx") {
			return false
		}
	case "linux":
		if !strings.HasPrefix(suf, "linux") {
			return false
		}
	}
	if strings.Contains(suf, "arm64") {
		return plat.Arch == "arm64"
	}
	if strings.Contains(suf, "x86") && !strings.Contains(suf, "x86_64") {
		return plat.Arch == "x86"
	}
	return true
}

func FilterLibrariesForPlatform(meta *VersionMetadata, plat PlatformInfo) []ResolvedLibraryFile {
	if meta == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]ResolvedLibraryFile, 0, len(meta.Libraries))
	for _, lib := range meta.Libraries {
		for _, rf := range ResolveLibraryFiles(lib, plat) {
			key := strings.ToLower(rf.RelPath)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, rf)
		}
	}
	return out
}

func ExtractNativeJAR(jarPath, destDir string, rule *ExtractRule) error {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absDest, 0o755); err != nil {
		return err
	}
	excludes := []string{"META-INF/"}
	if rule != nil && len(rule.Exclude) > 0 {
		excludes = append(excludes, rule.Exclude...)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		norm := strings.ReplaceAll(f.Name, "\\", "/")
		skip := false
		for _, ex := range excludes {
			if strings.HasPrefix(norm, strings.ReplaceAll(ex, "\\", "/")) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		cleanRel := path.Clean(norm)
		if cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, "/") || strings.HasPrefix(cleanRel, "../") || strings.Contains(cleanRel, "/../") {
			return fmt.Errorf("Zip Slip обнаружен в нативном архиве %s: %q", filepath.Base(jarPath), f.Name)
		}
		target := filepath.Join(absDest, filepath.FromSlash(cleanRel))
		relCheck, err := filepath.Rel(absDest, target)
		if err != nil || strings.HasPrefix(relCheck, "..") {
			return fmt.Errorf("выход за пределы папки natives: %q", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		outF, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, copyErr := io.Copy(outF, io.LimitReader(rc, 64*1024*1024))
		_ = rc.Close()
		closeErr := outF.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
