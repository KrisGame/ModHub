package runtime

import (
	"fmt"
	"sort"
	"strings"

	"modhub/internal/domain"
)

// RequirementInput encapsulates all signals used to pick a compatible Java Runtime.
// Future Minecraft Version Manager and Modloader Manager stages populate ManifestJavaMajor & Loader.
type RequirementInput struct {
	MCVersion         string `json:"mc_version"`
	ManifestJavaMajor int    `json:"manifest_java_major,omitempty"` // Authoritative javaVersion.majorVersion from Mojang version JSON
	Loader            string `json:"loader,omitempty"`              // vanilla | forge | neoforge | fabric | quilt
	LoaderVersion     string `json:"loader_version,omitempty"`
}

// RequirementResult describes the compatible Java major version range and preferred Mojang component.
type RequirementResult struct {
	RecommendedMajor   int    `json:"recommended_major"`
	MinMajor           int    `json:"min_major"`
	MaxMajor           int    `json:"max_major"` // 0 means no upper cap
	PreferredComponent string `json:"preferred_component"`
	Reason             string `json:"reason"`
	Source             string `json:"source"` // manifest | loader_rule | mc_rule | fallback
}

// CompatibilityRule allows extending Java compatibility rules without rewriting core selection logic.
type CompatibilityRule struct {
	Name  string
	Match func(in RequirementInput, mc [3]int) (RequirementResult, bool)
}

var defaultCompatibilityRules = []CompatibilityRule{
	{
		Name: "forge_legacy_strict_java8",
		Match: func(in RequirementInput, mc [3]int) (RequirementResult, bool) {
			if strings.EqualFold(in.Loader, "forge") && mc[0] == 1 && mc[1] > 0 && mc[1] <= 12 {
				return RequirementResult{
					RecommendedMajor:   8,
					MinMajor:           8,
					MaxMajor:           8,
					PreferredComponent: "jre-legacy",
					Reason:             "Forge для Minecraft " + in.MCVersion + " требует строго Java 8",
					Source:             "loader_rule",
				}, true
			}
			return RequirementResult{}, false
		},
	},
	{
		Name: "mc_26_and_newer",
		Match: func(in RequirementInput, mc [3]int) (RequirementResult, bool) {
			if mc[0] >= 26 {
				return RequirementResult{
					RecommendedMajor:   21,
					MinMajor:           21,
					MaxMajor:           0,
					PreferredComponent: "java-runtime-delta",
					Reason:             "Minecraft " + in.MCVersion + " требует Java 21+",
					Source:             "mc_rule",
				}, true
			}
			return RequirementResult{}, false
		},
	},
	{
		Name: "mc_1_20_5_plus",
		Match: func(in RequirementInput, mc [3]int) (RequirementResult, bool) {
			if mc[0] == 1 && (mc[1] > 20 || (mc[1] == 20 && mc[2] >= 5)) {
				return RequirementResult{
					RecommendedMajor:   21,
					MinMajor:           21,
					MaxMajor:           0,
					PreferredComponent: "java-runtime-delta",
					Reason:             "Minecraft 1.20.5 и новее требуют Java 21",
					Source:             "mc_rule",
				}, true
			}
			return RequirementResult{}, false
		},
	},
	{
		Name: "mc_1_18_to_1_20_4",
		Match: func(in RequirementInput, mc [3]int) (RequirementResult, bool) {
			if mc[0] == 1 && mc[1] >= 18 && mc[1] <= 20 {
				return RequirementResult{
					RecommendedMajor:   17,
					MinMajor:           17,
					MaxMajor:           21,
					PreferredComponent: "java-runtime-gamma",
					Reason:             "Minecraft 1.18–1.20.4 используют Java 17",
					Source:             "mc_rule",
				}, true
			}
			return RequirementResult{}, false
		},
	},
	{
		Name: "mc_1_17",
		Match: func(in RequirementInput, mc [3]int) (RequirementResult, bool) {
			if mc[0] == 1 && mc[1] == 17 {
				return RequirementResult{
					RecommendedMajor:   16,
					MinMajor:           16,
					MaxMajor:           17,
					PreferredComponent: "java-runtime-alpha",
					Reason:             "Minecraft 1.17 использует Java 16 (совместима с Java 17)",
					Source:             "mc_rule",
				}, true
			}
			return RequirementResult{}, false
		},
	},
	{
		Name: "mc_1_16_5_and_older",
		Match: func(in RequirementInput, mc [3]int) (RequirementResult, bool) {
			if mc[0] == 1 && mc[1] > 0 && mc[1] <= 16 {
				return RequirementResult{
					RecommendedMajor:   8,
					MinMajor:           8,
					MaxMajor:           8,
					PreferredComponent: "jre-legacy",
					Reason:             "Minecraft 1.16.5 и более старые версии используют Java 8",
					Source:             "mc_rule",
				}, true
			}
			return RequirementResult{}, false
		},
	},
}

func ComponentForMajor(major int) string {
	switch {
	case major >= 21:
		return "java-runtime-delta"
	case major >= 17:
		return "java-runtime-gamma"
	case major == 16:
		return "java-runtime-alpha"
	default:
		return "jre-legacy"
	}
}

func MajorForComponent(component string) int {
	switch strings.ToLower(strings.TrimSpace(component)) {
	case "java-runtime-delta":
		return 21
	case "java-runtime-gamma", "java-runtime-gamma-snapshot", "java-runtime-beta":
		return 17
	case "java-runtime-alpha":
		return 16
	case "jre-legacy":
		return 8
	default:
		return 0
	}
}

func DisplayNameForComponent(component string) string {
	switch strings.ToLower(strings.TrimSpace(component)) {
	case "java-runtime-delta":
		return "Java 21 (Mojang Runtime Delta)"
	case "java-runtime-gamma":
		return "Java 17 (Mojang Runtime Gamma)"
	case "java-runtime-beta":
		return "Java 17 (Mojang Runtime Beta)"
	case "java-runtime-alpha":
		return "Java 16 (Mojang Runtime Alpha)"
	case "jre-legacy":
		return "Java 8 (Mojang JRE Legacy)"
	default:
		return component
	}
}

func CompatibleMCRangesForMajor(major int) string {
	switch {
	case major >= 21:
		return "MC 1.20.5+ / 1.21+"
	case major == 17:
		return "MC 1.18 – 1.20.4"
	case major == 16:
		return "MC 1.17"
	case major == 8:
		return "MC 1.16.5 и старше"
	case major > 8 && major < 16:
		return "Промежуточная версия (ограниченная совместимость)"
	default:
		return "Не определено"
	}
}

// ResolveRequirement determines the optimal Java major version using authoritative manifest metadata first,
// then extensible loader/MC version rules.
func ResolveRequirement(in RequirementInput, extraRules ...CompatibilityRule) RequirementResult {
	if in.ManifestJavaMajor > 0 {
		maxM := 0
		if in.ManifestJavaMajor <= 8 {
			maxM = 8
		}
		return RequirementResult{
			RecommendedMajor:   in.ManifestJavaMajor,
			MinMajor:           in.ManifestJavaMajor,
			MaxMajor:           maxM,
			PreferredComponent: ComponentForMajor(in.ManifestJavaMajor),
			Reason:             fmt.Sprintf("Указано в метаданных версии Minecraft (Java %d)", in.ManifestJavaMajor),
			Source:             "manifest",
		}
	}

	mcTuple := domain.ParseMCTuple(strings.TrimSpace(in.MCVersion))
	for _, r := range extraRules {
		if res, ok := r.Match(in, mcTuple); ok {
			return res
		}
	}
	for _, r := range defaultCompatibilityRules {
		if res, ok := r.Match(in, mcTuple); ok {
			return res
		}
	}
	return RequirementResult{
		RecommendedMajor:   21,
		MinMajor:           17,
		MaxMajor:           0,
		PreferredComponent: "java-runtime-delta",
		Reason:             "Рекомендуемая современная версия Java 21",
		Source:             "fallback",
	}
}

// RecommendRuntime selects the best verified, runnable JavaRuntime matching req.
func RecommendRuntime(installed []JavaRuntime, req RequirementResult) *JavaRuntime {
	var candidates []JavaRuntime
	for _, rt := range installed {
		if !rt.Verified || !rt.Runnable || rt.MajorVersion <= 0 {
			continue
		}
		if rt.MajorVersion < req.MinMajor {
			continue
		}
		if req.MaxMajor > 0 && rt.MajorVersion > req.MaxMajor {
			continue
		}
		candidates = append(candidates, rt)
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		aExact := a.MajorVersion == req.RecommendedMajor
		bExact := b.MajorVersion == req.RecommendedMajor
		if aExact != bExact {
			return aExact
		}
		if a.Bitness != b.Bitness {
			return a.Bitness > b.Bitness
		}
		if a.Managed != b.Managed {
			return a.Managed
		}
		return a.Version > b.Version
	})
	best := candidates[0]
	return &best
}
