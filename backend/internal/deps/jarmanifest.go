package deps

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"modhub/internal/catalog"
	"modhub/internal/domain"
	"modhub/internal/netx"
	"modhub/internal/security"
	"modhub/internal/storage"
)

const (
	maxManifestJarBytes   int64 = 24 * 1024 * 1024 // 24 MB cap for quick manifest inspection
	maxManifestEntryBytes int64 = 512 * 1024       // 512 KB cap for a single toml/json manifest
)

type ManifestDep struct {
	ModID        string `json:"mod_id"`
	Type         string `json:"type"` // required | optional | incompatible
	ManifestFile string `json:"manifest_file"`
}

var (
	reTomlDepSection = regexp.MustCompile(`^\[\[dependencies\.([^\]]+)\]\]\s*$`)
	reTomlModSection = regexp.MustCompile(`^\[\[mods\]\]\s*$`)
	reTomlKV         = regexp.MustCompile(`^([A-Za-z0-9_.-]+)\s*=\s*(.+)$`)
)

// Built-in runtime/loader IDs that are NEVER standalone user mod dependencies.
var builtinRuntimeModIDs = map[string]struct{}{
	"minecraft":      {},
	"java":           {},
	"forge":          {},
	"neoforge":       {},
	"fml":            {},
	"fabricloader":   {},
	"fabric-loader":  {},
	"quilt_loader":   {},
	"quilt-loader":   {},
	"mixinextras":    {},
	"liteloader":     {},
	"rift":           {},
	"modloader":      {},
	"mcp":            {},
	"optifine":       {},
}

// Well-known modId -> preferred search/slug lookup hints where a mod's internal Forge/Fabric modId
// differs significantly from its CurseForge/Modrinth display title (e.g. "cataclysm" -> "L_Ender's Cataclysm").
// NOTE: These are ONLY search/slug hints used to locate the catalog page for a modId that was ALREADY
// explicitly declared inside the mod's own official manifest file.
var modIDLookupHints = map[string][]string{
	"cataclysm":        {"lendercataclysm", "l_enders_cataclysm", "L_Ender's Cataclysm"},
	"bettercombat":     {"better-combat", "better-combat-by-daedelus", "Better Combat"},
	"playeranimator":   {"playeranimator", "player-animator", "playerAnimator"},
	"cloth_config":     {"cloth-config", "Cloth Config API"},
	"cloth-config":     {"cloth-config", "Cloth Config API"},
	"lionfishapi":      {"lionfish-api", "Lionfish API"},
	"citadel":          {"citadel", "Citadel"},
	"curios":           {"curios", "Curios API"},
	"geckolib":         {"geckolib", "GeckoLib"},
	"geckolib3":        {"geckolib", "GeckoLib"},
	"architectury":     {"architectury-api", "Architectury API"},
	"fabric-api":       {"fabric-api", "Fabric API"},
	"fabric":           {"fabric-api", "Fabric API"},
	"quilted_fabric_api": {"qsl", "Quilted Fabric API"},
	"jei":              {"jei", "Just Enough Items"},
	"rei":              {"rei", "Roughly Enough Items"},
	"emi":              {"emi", "EMI"},
	"curios_api":       {"curios", "Curios API"},
	"trinkets":         {"trinkets", "Trinkets"},
	"accessories":      {"accessories", "Accessories"},
	"azurelib":         {"azurelib", "AzureLib"},
	"patchouli":        {"patchouli", "Patchouli"},
	"kotlinforforge":   {"kotlin-for-forge", "Kotlin for Forge"},
	"fabric-language-kotlin": {"fabric-language-kotlin", "Fabric Language Kotlin"},
	"yungsapi":         {"yungs-api", "YUNG's API"},
	"balm":             {"balm", "Balm"},
	"moonlight":        {"moonlight", "Moonlight Lib"},
	"puzzleslib":       {"puzzles-lib", "Puzzles Lib"},
	"bookshelf":        {"bookshelf", "Bookshelf"},
	"collective":       {"collective", "Collective"},
	"caelus":           {"caelus", "Caelus API"},
	"smartbrainlib":    {"smartbrainlib", "SmartBrainLib"},
	"resourcefullib":         {"resourceful-lib", "Resourceful Lib"},
	"terrablender":           {"terrablender", "TerraBlender"},
	"flywheel":               {"flywheel", "Flywheel"},
	"create":                 {"create", "Create"},
	"cardinal-components-api": {"cardinal-components-api", "Cardinal Components API"},
	"fabric-permissions-api": {"fabric-permissions-api", "fabric-permissions-api"},
	"yet_another_config_lib_v3": {"yacl", "YetAnotherConfigLib"},
	"yet-another-config-lib": {"yacl", "YetAnotherConfigLib"},
	"yacl":                   {"yacl", "YetAnotherConfigLib"},
	"fzzy_config":            {"fzzy-config", "Fzzy Config"},
	"midnightlib":            {"midnightlib", "MidnightLib"},
	"owo":                    {"owo-lib", "oωo (owo-lib)"},
	"owo-lib":                {"owo-lib", "oωo (owo-lib)"},
	"forgeconfigapiport":     {"forge-config-api-port", "Forge Config API Port"},
	"supermartijn642configlib": {"supermartijn642s-config-lib", "SuperMartijn642's Config Lib"},
	"supermartijn642corelib": {"supermartijn642s-core-lib", "SuperMartijn642's Core Lib"},
	"glitchcore":             {"glitchcore", "GlitchCore"},
	"cupboard":               {"cupboard", "Cupboard"},
	"cucumber":               {"cucumber", "Cucumber Library"},
	"placebo":                {"placebo", "Placebo"},
	"octolib":                {"octolib", "OctoLib"},
	"libraryferret":          {"library-ferret", "Library Ferret"},
	"entityculling":          {"entityculling", "Entity Culling"},
	"ferritecore":            {"ferritecore", "FerriteCore"},
	"coroutil":               {"coroutil", "CoroUtil"},
	"iceberg":                {"iceberg", "Iceberg"},
	"prism":                  {"prism-lib", "Prism"},
	"fusion":                 {"fusion-connected-textures", "Fusion (Connected Textures)"},
	"cloth-config2":          {"cloth-config", "Cloth Config API"},
}

func isBuiltinRuntimeID(modID string) bool {
	low := strings.ToLower(strings.TrimSpace(modID))
	if low == "" {
		return true
	}
	if _, ok := builtinRuntimeModIDs[low]; ok {
		return true
	}
	return strings.HasPrefix(low, "org_jetbrains") ||
		strings.HasPrefix(low, "com_google_") ||
		strings.HasPrefix(low, "org_spongepowered") ||
		strings.HasPrefix(low, "io_github_")
}

func canonicalizeManifestModID(raw string) string {
	low := strings.ToLower(strings.TrimSpace(raw))
	if low == "cloth-config2" {
		return "cloth-config"
	}
	if strings.HasPrefix(low, "cardinal-components-") {
		return "cardinal-components-api"
	}
	if strings.HasPrefix(low, "fabric-permissions-api") {
		return "fabric-permissions-api"
	}
	if strings.HasPrefix(low, "fabric-") && low != "fabric-api" && low != "fabric-language-kotlin" && low != "fabric-loader" {
		// Individual Fabric API modules (e.g. fabric-lifecycle-events-v1) are part of Fabric API
		if strings.HasSuffix(low, "-v0") || strings.HasSuffix(low, "-v1") || strings.HasSuffix(low, "-v2") || strings.HasSuffix(low, "-v3") || low == "fabric-api-base" {
			return "fabric-api"
		}
	}
	return low
}

func stripTomlComment(line string) string {
	inSingle := false
	inDouble := false
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && inDouble {
			escaped = true
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
		} else if r == '\'' && !inDouble {
			inSingle = !inSingle
		} else if r == '#' && !inSingle && !inDouble {
			return strings.TrimSpace(line[:i])
		}
	}
	return strings.TrimSpace(line)
}

func unquoteTomlValue(val string) string {
	v := strings.TrimSpace(val)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// ParseModsTomlDeps parses Forge META-INF/mods.toml and NeoForge META-INF/neoforge.mods.toml.
func ParseModsTomlDeps(content, manifestFile string) []ManifestDep {
	selfIDs := map[string]struct{}{}
	type rawEntry struct {
		modID     string
		mandatory *bool
		depType   string
	}
	var entries []rawEntry
	section := ""
	var cur *rawEntry

	flush := func() {
		if cur != nil && cur.modID != "" {
			entries = append(entries, *cur)
		}
		cur = nil
	}

	for _, rawLine := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line := stripTomlComment(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			flush()
			if reTomlModSection.MatchString(line) {
				section = "mod"
			} else if m := reTomlDepSection.FindStringSubmatch(line); len(m) == 2 {
				section = "dep"
				cur = &rawEntry{}
				owner := strings.ToLower(strings.Trim(strings.TrimSpace(m[1]), `"'`))
				if owner != "" {
					selfIDs[owner] = struct{}{}
				}
			} else {
				section = ""
			}
			continue
		}
		m := reTomlKV.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(m[1]))
		v := unquoteTomlValue(m[2])
		if section == "mod" && k == "modid" && v != "" {
			selfIDs[strings.ToLower(v)] = struct{}{}
		} else if section == "dep" && cur != nil {
			switch k {
			case "modid":
				cur.modID = strings.ToLower(strings.TrimSpace(v))
			case "mandatory":
				b := strings.EqualFold(v, "true")
				cur.mandatory = &b
			case "type":
				cur.depType = strings.ToLower(strings.TrimSpace(v))
			}
		}
	}
	flush()

	out := make([]ManifestDep, 0, len(entries))
	seen := map[string]int{}
	for _, e := range entries {
		cid := canonicalizeManifestModID(e.modID)
		if isBuiltinRuntimeID(cid) {
			continue
		}
		if _, isSelf := selfIDs[cid]; isSelf {
			continue
		}
		dt := "optional"
		switch e.depType {
		case "required":
			dt = "required"
		case "incompatible", "discouraged":
			dt = "incompatible"
		case "optional":
			dt = "optional"
		default:
			if e.mandatory != nil && *e.mandatory {
				dt = "required"
			}
		}
		if idx, dup := seen[cid]; dup {
			if domain.DepTypePriority(dt) > domain.DepTypePriority(out[idx].Type) {
				out[idx].Type = dt
			}
			continue
		}
		seen[cid] = len(out)
		out = append(out, ManifestDep{ModID: cid, Type: dt, ManifestFile: manifestFile})
	}
	return out
}

// ParseFabricModJSONDeps parses Fabric fabric.mod.json depends/recommends/suggests/breaks/conflicts.
func ParseFabricModJSONDeps(content []byte, manifestFile string) []ManifestDep {
	var parsed struct {
		ID         string         `json:"id"`
		Depends    map[string]any `json:"depends"`
		Recommends map[string]any `json:"recommends"`
		Suggests   map[string]any `json:"suggests"`
		Breaks     map[string]any `json:"breaks"`
		Conflicts  map[string]any `json:"conflicts"`
	}
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil
	}
	selfID := strings.ToLower(strings.TrimSpace(parsed.ID))
	var out []ManifestDep
	seen := map[string]int{}
	addMap := func(m map[string]any, dt string) {
		for k := range m {
			cid := canonicalizeManifestModID(k)
			if isBuiltinRuntimeID(cid) || (selfID != "" && cid == selfID) {
				continue
			}
			if idx, dup := seen[cid]; dup {
				if domain.DepTypePriority(dt) > domain.DepTypePriority(out[idx].Type) {
					out[idx].Type = dt
				}
				continue
			}
			seen[cid] = len(out)
			out = append(out, ManifestDep{ModID: cid, Type: dt, ManifestFile: manifestFile})
		}
	}
	addMap(parsed.Depends, "required")
	addMap(parsed.Breaks, "incompatible")
	addMap(parsed.Conflicts, "incompatible")
	addMap(parsed.Recommends, "optional")
	addMap(parsed.Suggests, "optional")
	return out
}

// ParseQuiltModJSONDeps parses Quilt quilt.mod.json quilt_loader.depends & breaks.
func ParseQuiltModJSONDeps(content []byte, manifestFile string) []ManifestDep {
	var parsed struct {
		QuiltLoader struct {
			ID      string `json:"id"`
			Depends []any  `json:"depends"`
			Breaks  []any  `json:"breaks"`
		} `json:"quilt_loader"`
	}
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil
	}
	selfID := strings.ToLower(strings.TrimSpace(parsed.QuiltLoader.ID))
	var out []ManifestDep
	seen := map[string]int{}
	addOne := func(rawID, dt string) {
		cid := canonicalizeManifestModID(rawID)
		if isBuiltinRuntimeID(cid) || (selfID != "" && cid == selfID) {
			return
		}
		if idx, dup := seen[cid]; dup {
			if domain.DepTypePriority(dt) > domain.DepTypePriority(out[idx].Type) {
				out[idx].Type = dt
			}
			return
		}
		seen[cid] = len(out)
		out = append(out, ManifestDep{ModID: cid, Type: dt, ManifestFile: manifestFile})
	}
	for _, entry := range parsed.QuiltLoader.Depends {
		switch v := entry.(type) {
		case string:
			addOne(v, "required")
		case map[string]any:
			if id, ok := v["id"].(string); ok {
				dt := "required"
				if opt, ok := v["optional"].(bool); ok && opt {
					dt = "optional"
				}
				addOne(id, dt)
			}
		}
	}
	for _, entry := range parsed.QuiltLoader.Breaks {
		switch v := entry.(type) {
		case string:
			addOne(v, "incompatible")
		case map[string]any:
			if id, ok := v["id"].(string); ok {
				addOne(id, "incompatible")
			}
		}
	}
	return out
}

// ExtractManifestDepsFromJARFile opens a local .jar (ZIP) and parses official modloader manifests.
func ExtractManifestDepsFromJARFile(jarPath string) []ManifestDep {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil
	}
	defer zr.Close()

	readEntry := func(f *zip.File) ([]byte, bool) {
		rc, err := f.Open()
		if err != nil {
			return nil, false
		}
		defer rc.Close()
		b, err := io.ReadAll(io.LimitReader(rc, maxManifestEntryBytes))
		if err != nil || len(b) == 0 {
			return nil, false
		}
		return b, true
	}

	var out []ManifestDep
	bundledIDs := map[string]struct{}{}
	seen := map[string]int{}
	merge := func(list []ManifestDep) {
		for _, d := range list {
			if idx, dup := seen[d.ModID]; dup {
				if domain.DepTypePriority(d.Type) > domain.DepTypePriority(out[idx].Type) {
					out[idx].Type = d.Type
				}
				continue
			}
			seen[d.ModID] = len(out)
			out = append(out, d)
		}
	}

	readSelfIDsFromEmbeddedJar := func(rawJar []byte) {
		if len(rawJar) == 0 {
			return
		}
		iz, err := zip.NewReader(bytes.NewReader(rawJar), int64(len(rawJar)))
		if err != nil {
			return
		}
		for _, ef := range iz.File {
			el := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(ef.Name, "/"), "\\", "/"))
			switch el {
			case "fabric.mod.json":
				if b, ok := readEntry(ef); ok {
					var p struct {
						ID string `json:"id"`
					}
					if json.Unmarshal(b, &p) == nil && p.ID != "" {
						bundledIDs[canonicalizeManifestModID(p.ID)] = struct{}{}
						bundledIDs[strings.ToLower(strings.TrimSpace(p.ID))] = struct{}{}
					}
				}
			case "quilt.mod.json":
				if b, ok := readEntry(ef); ok {
					var p struct {
						QuiltLoader struct {
							ID string `json:"id"`
						} `json:"quilt_loader"`
					}
					if json.Unmarshal(b, &p) == nil && p.QuiltLoader.ID != "" {
						bundledIDs[canonicalizeManifestModID(p.QuiltLoader.ID)] = struct{}{}
						bundledIDs[strings.ToLower(strings.TrimSpace(p.QuiltLoader.ID))] = struct{}{}
					}
				}
			case "meta-inf/neoforge.mods.toml", "meta-inf/mods.toml":
				if b, ok := readEntry(ef); ok {
					inMod := false
					for _, rawLine := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
						line := stripTomlComment(rawLine)
						if strings.HasPrefix(line, "[") {
							inMod = reTomlModSection.MatchString(line)
							continue
						}
						if inMod {
							if m := reTomlKV.FindStringSubmatch(line); len(m) == 3 && strings.EqualFold(strings.TrimSpace(m[1]), "modid") {
								id := strings.ToLower(strings.TrimSpace(unquoteTomlValue(m[2])))
								if id != "" {
									bundledIDs[canonicalizeManifestModID(id)] = struct{}{}
									bundledIDs[id] = struct{}{}
								}
							}
						}
					}
				}
			}
		}
	}

	embeddedCount := 0
	for _, f := range zr.File {
		name := strings.ReplaceAll(strings.TrimPrefix(f.Name, "/"), "\\", "/")
		low := strings.ToLower(name)
		switch low {
		case "meta-inf/neoforge.mods.toml", "meta-inf/mods.toml":
			if b, ok := readEntry(f); ok {
				merge(ParseModsTomlDeps(string(b), name))
			}
		case "fabric.mod.json":
			if b, ok := readEntry(f); ok {
				merge(ParseFabricModJSONDeps(b, name))
			}
		case "quilt.mod.json":
			if b, ok := readEntry(f); ok {
				merge(ParseQuiltModJSONDeps(b, name))
			}
		default:
			if embeddedCount < 32 && strings.HasSuffix(low, ".jar") &&
				(strings.HasPrefix(low, "meta-inf/jars/") || strings.HasPrefix(low, "meta-inf/jarjar/")) &&
				f.UncompressedSize64 <= 2*1024*1024 {
				embeddedCount++
				if rc, err := f.Open(); err == nil {
					if rawJar, rErr := io.ReadAll(io.LimitReader(rc, 2*1024*1024)); rErr == nil {
						readSelfIDsFromEmbeddedJar(rawJar)
					}
					_ = rc.Close()
				}
			}
		}
	}
	if len(bundledIDs) > 0 && len(out) > 0 {
		filtered := out[:0]
		for _, d := range out {
			if _, bundled := bundledIDs[strings.ToLower(d.ModID)]; bundled {
				continue
			}
			filtered = append(filtered, d)
		}
		out = filtered
	}
	return out
}

// InspectJarManifestDepsWithStatus downloads an official .jar (strictly validated via ValidateDownloadURL),
// parses its modloader manifest dependencies, caches successful reads in SQLite for 24h, and reports exact status:
// "checked" | "empty" | "unavailable" | "failed".
func InspectJarManifestDepsWithStatus(ctx context.Context, httpClient *netx.HTTPClient, db *storage.Database, fileURL string) ([]ManifestDep, string, error) {
	rawURL := strings.TrimSpace(fileURL)
	if rawURL == "" || !strings.HasSuffix(strings.ToLower(strings.Split(rawURL, "?")[0]), ".jar") {
		return nil, "unavailable", nil
	}
	if httpClient == nil {
		return nil, "unavailable", nil
	}
	cacheKey := "jar_manifest_v2|" + rawURL
	if db != nil {
		if cached, ok := db.CacheGet(cacheKey); ok {
			var entry struct {
				Deps   []ManifestDep `json:"deps"`
				Status string        `json:"status"`
			}
			if json.Unmarshal([]byte(cached), &entry) == nil && (entry.Status == "checked" || entry.Status == "empty") {
				return entry.Deps, entry.Status, nil
			}
		}
	}
	tmpPath, _, err := httpClient.DownloadToTempFile(ctx, rawURL, maxManifestJarBytes)
	if err != nil {
		return nil, "failed", err
	}
	defer os.Remove(tmpPath)

	deps, okOpen := extractManifestDepsFromJARFileChecked(tmpPath)
	if !okOpen {
		return nil, "failed", os.ErrInvalid
	}
	status := "empty"
	if len(deps) > 0 {
		status = "checked"
	}
	if db != nil {
		if b, err := json.Marshal(map[string]any{"deps": deps, "status": status}); err == nil {
			db.CacheSet(cacheKey, string(b), 24*3600)
		}
	}
	return deps, status, nil
}

func InspectJarManifestDeps(ctx context.Context, httpClient *netx.HTTPClient, db *storage.Database, fileURL string) []ManifestDep {
	deps, _, _ := InspectJarManifestDepsWithStatus(ctx, httpClient, db, fileURL)
	return deps
}

func extractManifestDepsFromJARFileChecked(jarPath string) ([]ManifestDep, bool) {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil, false
	}
	_ = zr.Close()
	return ExtractManifestDepsFromJARFile(jarPath), true
}

func modIDMatchesCandidate(modID string, title, slug string) bool {
	want := domain.NormalizeCompactTitle(modID)
	if len(want) < 2 {
		return false
	}
	sNorm := domain.NormalizeCompactTitle(slug)
	tNorm := domain.NormalizeCompactTitle(title)
	if sNorm == want || tNorm == want {
		return true
	}
	if len(want) >= 4 && (strings.Contains(sNorm, want) || strings.Contains(tNorm, want)) {
		return true
	}
	// Check known hint aliases for this modID (e.g. cataclysm -> L_Ender's Cataclysm)
	for _, hint := range modIDLookupHints[strings.ToLower(modID)] {
		hn := domain.NormalizeCompactTitle(hint)
		if hn != "" && (sNorm == hn || tNorm == hn || (len(hn) >= 5 && (strings.Contains(sNorm, hn) || strings.Contains(tNorm, hn)))) {
			return true
		}
	}
	return false
}

// ResolveManifestDepsToNodes converts manifest-extracted modIds (e.g. "bettercombat", "cataclysm")
// into real DepTreeNodes resolved against Modrinth / CurseForge / local SQLite.
func ResolveManifestDepsToNodes(
	httpClient *netx.HTTPClient,
	db *storage.Database,
	cfKeyRaw, preferredPlatform string,
	mdeps []ManifestDep,
) []domain.DepTreeNode {
	if len(mdeps) == 0 {
		return nil
	}
	cfKey := security.SanitizeCFKey(cfKeyRaw)
	nodes := make([]domain.DepTreeNode, len(mdeps))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for i, md := range mdeps {
		wg.Add(1)
		go func(idx int, d ManifestDep) {
			defer wg.Done()
			if !netx.AcquireSemCtx(httpClient.ReqCtx(), sem) {
				return
			}
			defer func() { <-sem }()

			slugCands := []string{d.ModID}
			if hyph := strings.ReplaceAll(d.ModID, "_", "-"); hyph != d.ModID {
				slugCands = append(slugCands, hyph)
			}
			for _, h := range modIDLookupHints[d.ModID] {
				if !strings.Contains(h, " ") {
					slugCands = domain.AppendUniqueCI(slugCands, strings.ToLower(h))
				}
			}

			searchTerms := []string{}
			if hints := modIDLookupHints[d.ModID]; len(hints) > 0 {
				searchTerms = append(searchTerms, hints[len(hints)-1])
			}
			searchTerms = domain.AppendUniqueCI(searchTerms, strings.ReplaceAll(strings.ReplaceAll(d.ModID, "_", " "), "-", " "))

			resolveFromCard := func(c domain.RawProjectCard) domain.DepTreeNode {
				ct := c.ContentType
				if !domain.IsSupportedCatalogType(ct) {
					ct = "mod"
				}
				return domain.DepTreeNode{
					Platform:     c.Platform,
					ProjectID:    c.ID,
					SourceKey:    c.SourceKey,
					Type:         d.Type,
					Title:        c.Title,
					Slug:         c.Slug,
					IconURL:      c.IconURL,
					URL:          c.URL,
					Folder:       domain.GetContentTypeMeta(ct).Folder,
					ContentType:  ct,
					Source:       "manifest",
					ManifestFile: d.ManifestFile,
					Resolved:     true,
					Children:     []domain.DepTreeNode{},
				}
			}

			tryModrinth := func() *domain.DepTreeNode {
				for _, sv := range slugCands {
					if card, err := catalog.FetchModrinthProjectBySlug(httpClient, sv); err == nil && card != nil && card.ID != "" {
						if modIDMatchesCandidate(d.ModID, card.Title, card.Slug) {
							n := resolveFromCard(*card)
							return &n
						}
					}
				}
				for _, q := range searchTerms {
					hits, _, err := catalog.SearchModrinth(httpClient, q, "mod", "", nil, nil, nil, nil, "and", "", "downloads", 6, 0)
					if err != nil {
						continue
					}
					for _, h := range hits {
						if modIDMatchesCandidate(d.ModID, h.Title, h.Slug) {
							n := resolveFromCard(h)
							return &n
						}
					}
				}
				return nil
			}

			tryCurseForge := func() *domain.DepTreeNode {
				if cfKey == "" {
					return nil
				}
				for _, sv := range slugCands {
					if card := catalog.FetchCFExactSlug(httpClient, cfKey, sv, "mod"); card != nil && card.ID != "" {
						if modIDMatchesCandidate(d.ModID, card.Title, card.Slug) {
							n := resolveFromCard(*card)
							return &n
						}
					}
				}
				for _, q := range searchTerms {
					hits, _, err := catalog.SearchCurseForge(httpClient, cfKey, q, "mod", "", "", "downloads", 6, 0)
					if err != nil {
						continue
					}
					for _, h := range hits {
						if modIDMatchesCandidate(d.ModID, h.Title, h.Slug) {
							n := resolveFromCard(h)
							return &n
						}
					}
				}
				return nil
			}

			var found *domain.DepTreeNode
			if strings.EqualFold(preferredPlatform, "curseforge") && cfKey != "" {
				found = tryCurseForge()
				if found == nil {
					found = tryModrinth()
				}
			} else {
				found = tryModrinth()
				if found == nil {
					found = tryCurseForge()
				}
			}

			// Local SQLite fallback if live lookup failed
			if found == nil && db != nil {
				for _, q := range searchTerms {
					for _, sk := range db.FindSourceKeysByQuery(q, 5) {
						if c, ok := db.GetCatalogItem(sk); ok && modIDMatchesCandidate(d.ModID, c.Title, c.Slug) {
							n := resolveFromCard(*c)
							found = &n
							break
						}
					}
					if found != nil {
						break
					}
				}
			}

			if found != nil {
				nodes[idx] = *found
				return
			}

			// Even if unresolved in external catalogs, expose the exact manifest modId cleanly
			nodes[idx] = domain.DepTreeNode{
				Platform:     strings.ToLower(preferredPlatform),
				ProjectID:    d.ModID,
				SourceKey:    "manifest:" + d.ModID,
				Type:         d.Type,
				Title:        d.ModID,
				Slug:         d.ModID,
				Folder:       "mods",
				ContentType:  "mod",
				Source:       "manifest",
				ManifestFile: d.ManifestFile,
				Resolved:     false,
				Children:     []domain.DepTreeNode{},
			}
		}(i, md)
	}
	wg.Wait()

	out := make([]domain.DepTreeNode, 0, len(nodes))
	seen := map[string]struct{}{}
	for _, n := range nodes {
		if n.SourceKey == "" {
			continue
		}
		if _, dup := seen[n.SourceKey]; dup {
			continue
		}
		seen[n.SourceKey] = struct{}{}
		out = append(out, n)
	}
	return out
}

// Helper for manifest nodes converted into domain.DependencyRef when resolving pack downloads
func ManifestNodesToDepRefs(nodes []domain.DepTreeNode) []domain.DependencyRef {
	out := make([]domain.DependencyRef, 0, len(nodes))
	for _, n := range nodes {
		if !n.Resolved {
			continue
		}
		if (n.Platform == "modrinth" || n.Platform == "curseforge") && n.ProjectID != "" {
			if n.Platform == "curseforge" {
				if _, err := strconv.Atoi(n.ProjectID); err != nil {
					continue
				}
			}
			out = append(out, domain.DependencyRef{
				Platform:  n.Platform,
				ProjectID: n.ProjectID,
				Type:      n.Type,
			})
		}
	}
	return out
}
