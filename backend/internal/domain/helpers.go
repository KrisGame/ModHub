package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	reBracketSuffix   = regexp.MustCompile(`(?i)[\[\(\{]\s*(?:neoforge|fabric|forge|quilt|liteloader|reforged|neoforged|rift|legacy\s*fabric)(?:\s*[/,&+|•\-]\s*(?:neoforge|fabric|forge|quilt|liteloader|reforged|neoforged|rift|legacy\s*fabric))*\s*[\]\)\}]`)
	reTrailingLoader  = regexp.MustCompile(`(?i)(?:\s*[-–—|:]\s*|\s+for\s+)(?:neoforge|fabric|forge|quilt)(?:\s*[/,&+]\s*(?:neoforge|fabric|forge|quilt))*\s*$`)
	ReParensShortCode = regexp.MustCompile(`\s*\([A-Za-z0-9]{2,6}\)\s*$`)
	reGitRepoURL      = regexp.MustCompile(`(?i)https?://(?:www\.)?(?:github\.com|gitlab\.com|codeberg\.org)/([a-z0-9_.-]+/[a-z0-9_.-]+)`)
)

func CleanDisplayTitle(raw string) string {
	cleaned := reBracketSuffix.ReplaceAllString(raw, " ")
	cleaned = reTrailingLoader.ReplaceAllString(cleaned, " ")
	fields := strings.Fields(cleaned)
	if len(fields) == 0 {
		return strings.TrimSpace(raw)
	}
	return strings.Join(fields, " ")
}

func NormalizeCompactTitle(raw string) string {
	cleaned := strings.ToLower(CleanDisplayTitle(raw))
	var b strings.Builder
	b.Grow(len(cleaned))
	for _, r := range cleaned {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func NormalizeTitleNoAcronym(raw string) string {
	stripped := strings.ToLower(ReParensShortCode.ReplaceAllString(CleanDisplayTitle(raw), " "))
	var b strings.Builder
	b.Grow(len(stripped))
	for _, r := range stripped {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func NormalizeHaystack(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		if r == '\'' || r == '’' || r == '`' {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func ExtractNormalizedRepo(links ProjectLinks) string {
	for _, u := range []string{links.SourceURL, links.IssuesURL, links.WikiURL} {
		if u == "" {
			continue
		}
		if m := reGitRepoURL.FindStringSubmatch(u); len(m) == 2 {
			repo := strings.ToLower(strings.TrimSuffix(m[1], ".git"))
			repo = strings.TrimRight(repo, "/")
			if repo != "" {
				return repo
			}
		}
	}
	return ""
}

func AppendUniqueCI(slice []string, val string) []string {
	v := strings.TrimSpace(val)
	if v == "" {
		return slice
	}
	low := strings.ToLower(v)
	for _, existing := range slice {
		if strings.ToLower(existing) == low {
			return slice
		}
	}
	return append(slice, v)
}

func LoadersDisjoint(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	set := map[string]struct{}{}
	for _, x := range a {
		if IsKnownLoader(x) {
			set[strings.ToLower(x)] = struct{}{}
		}
	}
	if len(set) == 0 {
		return false
	}
	hasB := false
	for _, y := range b {
		low := strings.ToLower(y)
		if IsKnownLoader(low) {
			hasB = true
			if _, dup := set[low]; dup {
				return false
			}
		}
	}
	return hasB
}

func ParseMCTuple(v string) [3]int {
	parts := strings.Split(v, ".")
	var res [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		if n, err := strconv.Atoi(parts[i]); err == nil {
			res[i] = n
		}
	}
	return res
}

func MCTupleGreater(a, b [3]int) bool {
	if a[0] != b[0] {
		return a[0] > b[0]
	}
	if a[1] != b[1] {
		return a[1] > b[1]
	}
	return a[2] > b[2]
}

func TopMCTuple(gvs []string) [3]int {
	var best [3]int
	for _, g := range gvs {
		t := ParseMCTuple(g)
		if MCTupleGreater(t, best) {
			best = t
		}
	}
	return best
}

func ReleaseRank(rt string) int {
	switch strings.ToLower(rt) {
	case "release":
		return 3
	case "beta":
		return 2
	default:
		return 1
	}
}

func CompareVersionRelease(a, b VersionFile) bool {
	if a.IsAvailable != b.IsAvailable {
		return a.IsAvailable
	}
	aRel := strings.ToLower(a.ReleaseType) == "release"
	bRel := strings.ToLower(b.ReleaseType) == "release"
	if aRel != bRel {
		return aRel
	}
	if ReleaseRank(a.ReleaseType) != ReleaseRank(b.ReleaseType) {
		return ReleaseRank(a.ReleaseType) > ReleaseRank(b.ReleaseType)
	}
	if a.DatePublished != b.DatePublished {
		return a.DatePublished > b.DatePublished
	}
	ta, tb := TopMCTuple(a.GameVersions), TopMCTuple(b.GameVersions)
	if ta != tb {
		return MCTupleGreater(ta, tb)
	}
	return a.Platform == "modrinth" && b.Platform != "modrinth"
}

func NormalizeDepType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "required":
		return "required"
	case "incompatible":
		return "incompatible"
	case "optional":
		return "optional"
	case "embedded":
		return "embedded"
	case "include":
		return "include"
	case "tool":
		return "tool"
	default:
		return ""
	}
}

func DepTypePriority(t string) int {
	switch NormalizeDepType(t) {
	case "required":
		return 6
	case "incompatible":
		return 5
	case "optional":
		return 4
	case "embedded":
		return 3
	case "include":
		return 2
	case "tool":
		return 1
	default:
		return 0
	}
}

func DedupeDependencyRefs(deps []DependencyRef, selfID string) []DependencyRef {
	if len(deps) == 0 {
		return []DependencyRef{}
	}
	byKey := map[string]DependencyRef{}
	order := make([]string, 0, len(deps))
	for _, d := range deps {
		plat := strings.ToLower(strings.TrimSpace(d.Platform))
		pid := strings.TrimSpace(d.ProjectID)
		vid := strings.TrimSpace(d.VersionID)
		dt := NormalizeDepType(d.Type)
		if dt == "" || (pid == "" && vid == "") {
			continue
		}
		if selfID != "" && pid == selfID {
			continue
		}
		key := plat + ":" + pid
		if pid == "" {
			key = plat + ":ver:" + vid
		}
		norm := DependencyRef{Platform: plat, ProjectID: pid, VersionID: vid, Type: dt}
		if existing, ok := byKey[key]; ok {
			if DepTypePriority(dt) > DepTypePriority(existing.Type) {
				if norm.VersionID == "" {
					norm.VersionID = existing.VersionID
				}
				byKey[key] = norm
			} else if existing.VersionID == "" && norm.VersionID != "" {
				existing.VersionID = norm.VersionID
				byKey[key] = existing
			}
			continue
		}
		byKey[key] = norm
		order = append(order, key)
	}
	out := make([]DependencyRef, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		return DepTypePriority(out[i].Type) > DepTypePriority(out[j].Type)
	})
	return out
}

func PickMatchedMCVersion(gameVersions []string, preferred string) string {
	if preferred != "" {
		for _, g := range gameVersions {
			if g == preferred {
				return preferred
			}
		}
	}
	var best string
	var bestT [3]int
	for _, g := range gameVersions {
		t := ParseMCTuple(g)
		if best == "" || MCTupleGreater(t, bestT) {
			best = g
			bestT = t
		}
	}
	if best != "" {
		return best
	}
	return preferred
}

func PickMatchedLoader(loaders []string, preferred string) string {
	if preferred != "" {
		for _, l := range loaders {
			if strings.EqualFold(l, preferred) {
				return strings.ToLower(preferred)
			}
		}
	}
	if len(loaders) > 0 {
		return strings.ToLower(loaders[0])
	}
	return strings.ToLower(preferred)
}

func PickBestDependencyVersion(all []VersionFile, mcVer, loader string, usesLoader bool) (*VersionFile, string) {
	if len(all) == 0 {
		return nil, "нет опубликованных версий"
	}
	reqLD := ""
	if usesLoader {
		reqLD = strings.ToLower(strings.TrimSpace(loader))
	}
	reqMC := strings.TrimSpace(mcVer)
	reqTuple := ParseMCTuple(reqMC)

	hasLoader := func(v VersionFile) bool {
		if reqLD == "" {
			return true
		}
		if len(v.Loaders) == 0 {
			return false
		}
		for _, l := range v.Loaders {
			if strings.EqualFold(l, reqLD) {
				return true
			}
		}
		return false
	}
	hasExactMC := func(v VersionFile) bool {
		if reqMC == "" {
			return true
		}
		for _, g := range v.GameVersions {
			if g == reqMC {
				return true
			}
		}
		return false
	}
	hasSameMinorMC := func(v VersionFile) bool {
		if reqMC == "" || reqTuple[0] == 0 {
			return false
		}
		for _, g := range v.GameVersions {
			gt := ParseMCTuple(g)
			if gt[0] == reqTuple[0] && gt[1] == reqTuple[1] && gt[2] <= reqTuple[2] {
				return true
			}
		}
		return false
	}
	hasAnySameMinorMC := func(v VersionFile) bool {
		if reqMC == "" || reqTuple[0] == 0 {
			return false
		}
		for _, g := range v.GameVersions {
			gt := ParseMCTuple(g)
			if gt[0] == reqTuple[0] && gt[1] == reqTuple[1] {
				return true
			}
		}
		return false
	}
	pickFrom := func(pool []VersionFile) *VersionFile {
		if len(pool) == 0 {
			return nil
		}
		cp := append([]VersionFile(nil), pool...)
		sort.SliceStable(cp, func(i, j int) bool { return CompareVersionRelease(cp[i], cp[j]) })
		res := cp[0]
		return &res
	}

	var exactExact, minorExact, anyMinorExact, untaggedLoaderExactMC, loaderOnly []VersionFile
	for _, v := range all {
		ldOK := hasLoader(v)
		mcExact := hasExactMC(v)
		if ldOK && mcExact {
			exactExact = append(exactExact, v)
		} else if ldOK && hasSameMinorMC(v) {
			minorExact = append(minorExact, v)
		} else if ldOK && hasAnySameMinorMC(v) {
			anyMinorExact = append(anyMinorExact, v)
		} else if reqLD != "" && len(v.Loaders) == 0 && mcExact {
			untaggedLoaderExactMC = append(untaggedLoaderExactMC, v)
		} else if ldOK {
			loaderOnly = append(loaderOnly, v)
		}
	}

	if v := pickFrom(exactExact); v != nil {
		return v, "exact_mc_loader"
	}
	if v := pickFrom(minorExact); v != nil {
		return v, "minor_mc_loader"
	}
	if v := pickFrom(anyMinorExact); v != nil {
		return v, "same_branch_mc_loader"
	}
	if v := pickFrom(untaggedLoaderExactMC); v != nil {
		return v, "exact_mc_untagged_loader"
	}
	if reqMC == "" {
		if v := pickFrom(loaderOnly); v != nil {
			return v, "loader_only"
		}
	}
	if reqLD == "" && reqMC == "" {
		if v := pickFrom(all); v != nil {
			return v, "latest_any"
		}
	}
	return nil, fmt.Sprintf("нет совместимого файла для MC %s / %s", reqMC, reqLD)
}

func ParseISO8601Millis(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UnixMilli()
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UnixMilli()
	}
	if len(raw) >= 19 {
		if t, err := time.Parse("2006-01-02T15:04:05", raw[:19]); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}
