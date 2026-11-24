package runtime

import (
	"context"
	"fmt"
	"path"
	goruntime "runtime"
	"sort"
	"strings"

	"modhub/internal/netx"
)

// OfficialMojangJavaManifestURL is the official Mojang Minecraft Java Runtime metadata catalog.
const OfficialMojangJavaManifestURL = "https://launchermeta.mojang.com/v1/products/java-runtime/2ec0cc96c44e5a76b9c8b7c39df7210883d12871/all.json"

// MojangAllRuntimes mirrors the top-level structure of Mojang's java-runtime/all.json.
// platform -> component -> []MojangRuntimeEntry
type MojangAllRuntimes map[string]map[string][]MojangRuntimeEntry

type MojangRuntimeEntry struct {
	Availability struct {
		Group    int `json:"group"`
		Progress int `json:"progress"`
	} `json:"availability"`
	Manifest struct {
		SHA1 string `json:"sha1"`
		Size int64  `json:"size"`
		URL  string `json:"url"`
	} `json:"manifest"`
	Version struct {
		Name     string `json:"name"`
		Released string `json:"released"`
	} `json:"version"`
}

type MojangRuntimeManifest struct {
	Files map[string]MojangRuntimeFile `json:"files"`
}

type MojangRuntimeFile struct {
	Type       string `json:"type"` // file | directory | link
	Executable bool   `json:"executable"`
	Target     string `json:"target,omitempty"`
	Downloads  struct {
		Raw struct {
			SHA1 string `json:"sha1"`
			Size int64  `json:"size"`
			URL  string `json:"url"`
		} `json:"raw"`
	} `json:"downloads"`
}

func MojangPlatformKey(goos, goarch string) (string, string) {
	switch goos {
	case "windows":
		switch goarch {
		case "arm64":
			return "windows-arm64", "arm64"
		case "386":
			return "windows-x86", "x86"
		default:
			return "windows-x64", "x64"
		}
	case "darwin":
		if goarch == "arm64" {
			return "mac-os-arm64", "arm64"
		}
		return "mac-os", "x64"
	default:
		if goarch == "386" {
			return "linux-i386", "x86"
		}
		return "linux", "x64"
	}
}

func currentMojangPlatform() (string, string) {
	return MojangPlatformKey(goruntime.GOOS, goruntime.GOARCH)
}

func sanitizeRuntimeRelPath(rel string) (string, error) {
	s := strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/")
	if s == "" || strings.HasPrefix(s, "/") || strings.Contains(s, ":") {
		return "", fmt.Errorf("небезопасный путь в манифесте runtime: %q", rel)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("управляющие символы в пути runtime: %q", rel)
		}
	}
	cleaned := path.Clean(s)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("выход за пределы директории runtime: %q", rel)
	}
	return cleaned, nil
}

// FetchMojangAvailableRuntimes fetches official Mojang Java Runtime metadata for the current platform.
func FetchMojangAvailableRuntimes(ctx context.Context, httpClient *netx.HTTPClient, manifestURL, platKey, arch string) ([]AvailableRuntime, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("HTTP-клиент не инициализирован")
	}
	if strings.TrimSpace(manifestURL) == "" {
		manifestURL = OfficialMojangJavaManifestURL
	}
	if _, err := httpClient.ValidateLauncherURL(manifestURL); err != nil {
		return nil, fmt.Errorf("недопустимый URL манифеста Java: %w", err)
	}

	var all MojangAllRuntimes
	if err := httpClient.WithContext(ctx).GetJSON(manifestURL, nil, 1800, &all); err != nil {
		return nil, err
	}

	platMap := all[platKey]
	if len(platMap) == 0 && platKey == "windows-arm64" {
		// Windows on ARM64 can emulate x64 Java if arm64 runtime is absent
		platMap = all["windows-x64"]
		platKey = "windows-x64"
		arch = "x64"
	}
	if len(platMap) == 0 {
		return nil, fmt.Errorf("официальные метаданные Mojang не содержат runtime для платформы %s", platKey)
	}

	preferredOrder := []string{"java-runtime-delta", "java-runtime-gamma", "java-runtime-alpha", "jre-legacy", "java-runtime-beta"}
	orderIdx := map[string]int{}
	for i, c := range preferredOrder {
		orderIdx[c] = i
	}

	out := make([]AvailableRuntime, 0, len(platMap))
	for comp, entries := range platMap {
		if len(entries) == 0 {
			continue
		}
		// Skip Mojang internal diagnostic/snapshot-only entries unless explicitly known
		if strings.Contains(comp, "minecraft-java-exe") || strings.Contains(comp, "snapshot") {
			continue
		}
		ent := entries[0]
		if ent.Manifest.URL == "" {
			continue
		}
		major := ParseJavaMajor(ent.Version.Name)
		if major == 0 {
			major = MajorForComponent(comp)
		}
		out = append(out, AvailableRuntime{
			Component:    comp,
			DisplayName:  DisplayNameForComponent(comp),
			MajorVersion: major,
			Version:      ent.Version.Name,
			Released:     ent.Version.Released,
			PlatformKey:  platKey,
			Arch:         arch,
			CompatibleMC: CompatibleMCRangesForMajor(major),
			ManifestURL:  ent.Manifest.URL,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		oi, okI := orderIdx[out[i].Component]
		oj, okJ := orderIdx[out[j].Component]
		if !okI {
			oi = 99
		}
		if !okJ {
			oj = 99
		}
		if oi != oj {
			return oi < oj
		}
		return out[i].MajorVersion > out[j].MajorVersion
	})
	return out, nil
}
