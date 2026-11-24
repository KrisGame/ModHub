package domain

import (
	"sort"
	"strings"
)

const (
	UserAgent        = "ModHub-Go/1.0 (local-web-app)"
	BrowserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	ModrinthAPI      = "https://api.modrinth.com/v2"
	CurseForgeAPI    = "https://api.curseforge.com/v1"
	CurseForgeGameID = 432
)

var KnownLoaders = []string{"neoforge", "fabric", "forge", "quilt"}

type ContentTypeMeta struct {
	ID         string
	Folder     string
	MRType     string
	CFClassID  int
	CFURLSlug  string
	UsesLoader bool
}


// ClassifyCFClassID maps official CurseForge classId values to ModHub content types.
// Unknown/unsupported classes (Worlds=17, Plugins=5, Customization=4546, etc.) return ""
// and are NEVER silently misclassified as "mod".
func ClassifyCFClassID(classID int) string {
	switch classID {
	case 6:
		return "mod"
	case 12:
		return "resourcepack"
	case 6552:
		return "shader"
	case 6945:
		return "datapack"
	case 4471:
		return "modpack"
	default:
		return ""
	}
}

// ClassifyMRProjectType maps official Modrinth project_type values to ModHub content types.
// Unsupported types (e.g. "plugin") return "" and are NEVER silently misclassified as "mod".
func ClassifyMRProjectType(projectType string) string {
	switch strings.ToLower(strings.TrimSpace(projectType)) {
	case "mod":
		return "mod"
	case "resourcepack":
		return "resourcepack"
	case "shader":
		return "shader"
	case "datapack":
		return "datapack"
	case "modpack":
		return "modpack"
	default:
		return ""
	}
}

func IsSupportedCatalogType(ct string) bool {
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "mod", "resourcepack", "shader", "datapack":
		return true
	default:
		return false
	}
}

func GetContentTypeMeta(ct string) ContentTypeMeta {
	switch strings.ToLower(ct) {
	case "resourcepack":
		return ContentTypeMeta{ID: "resourcepack", Folder: "resourcepacks", MRType: "resourcepack", CFClassID: 12, CFURLSlug: "texture-packs", UsesLoader: false}
	case "shader":
		return ContentTypeMeta{ID: "shader", Folder: "shaderpacks", MRType: "shader", CFClassID: 6552, CFURLSlug: "shaders", UsesLoader: false}
	case "datapack":
		return ContentTypeMeta{ID: "datapack", Folder: "datapacks", MRType: "datapack", CFClassID: 6945, CFURLSlug: "data-packs", UsesLoader: false}
	default:
		return ContentTypeMeta{ID: "mod", Folder: "mods", MRType: "mod", CFClassID: 6, CFURLSlug: "mc-mods", UsesLoader: true}
	}
}

func IsKnownLoader(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	for _, l := range KnownLoaders {
		if l == low {
			return true
		}
	}
	return false
}

func CFLoaderIDToName(id int) string {
	switch id {
	case 1:
		return "forge"
	case 4:
		return "fabric"
	case 5:
		return "quilt"
	case 6:
		return "neoforge"
	default:
		return ""
	}
}

func CFLoaderNameToID(name string) int {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "forge":
		return 1
	case "fabric":
		return 4
	case "quilt":
		return 5
	case "neoforge":
		return 6
	default:
		return 0
	}
}

func CategoriesForType(ct string) []CategoryDef {
	switch strings.ToLower(ct) {
	case "resourcepack":
		return []CategoryDef{
			{ID: "vanilla-like", Label: "Ванильный стиль", MRTags: []string{"vanilla-like"}, CFSlugs: []string{"traditional"}},
			{ID: "realistic", Label: "Реалистичный", MRTags: []string{"realistic"}, CFSlugs: []string{"photo-realistic"}},
			{ID: "simplistic", Label: "Упрощённый", MRTags: []string{"simplistic"}, CFSlugs: []string{"simplistic"}},
			{ID: "themed", Label: "Тематический", MRTags: []string{"themed"}, CFSlugs: []string{"steampunk", "medieval"}},
			{ID: "tweaks", Label: "Твики", MRTags: []string{"tweaks"}, CFSlugs: []string{"miscellaneous"}},
			{ID: "utility", Label: "Утилиты", MRTags: []string{"utility"}, CFSlugs: []string{"mod-support"}},
			{ID: "combat", Label: "PvP / Бой", MRTags: []string{"combat"}, CFSlugs: []string{"pvp"}},
			{ID: "cursed", Label: "Забавные", MRTags: []string{"cursed"}, CFSlugs: []string{"miscellaneous"}},
			{ID: "gui", Label: "Интерфейс (GUI)", MRTags: []string{"gui"}, CFSlugs: []string{"font-packs"}},
			{ID: "audio", Label: "Звуки", MRTags: []string{"audio"}, CFSlugs: []string{"miscellaneous"}},
			{ID: "models", Label: "3D-модели", MRTags: []string{"models"}, CFSlugs: []string{"animated"}},
		}
	case "shader":
		return []CategoryDef{
			{ID: "realistic", Label: "Реалистичный", MRTags: []string{"realistic"}, CFSlugs: []string{"realistic"}},
			{ID: "vanilla-like", Label: "Ванильный", MRTags: []string{"vanilla-like"}, CFSlugs: []string{"vanilla"}},
			{ID: "fantasy", Label: "Фэнтези", MRTags: []string{"fantasy"}, CFSlugs: []string{"fantasy"}},
			{ID: "cartoon", Label: "Мультяшный", MRTags: []string{"cartoon"}, CFSlugs: []string{"cartoon"}},
			{ID: "semi-realistic", Label: "Полуреалистичный", MRTags: []string{"semi-realistic"}, CFSlugs: []string{"realistic"}},
			{ID: "potato", Label: "Для слабых ПК", MRTags: []string{"potato", "low"}, CFSlugs: []string{"low"}},
			{ID: "atmosphere", Label: "Атмосферный", MRTags: []string{"atmosphere"}, CFSlugs: []string{"realistic"}},
			{ID: "bloom", Label: "Свечение (Bloom)", MRTags: []string{"bloom"}, CFSlugs: []string{"realistic"}},
			{ID: "path-tracing", Label: "Трассировка лучей", MRTags: []string{"path-tracing"}, CFSlugs: []string{"realistic"}},
		}
	case "datapack":
		return []CategoryDef{
			{ID: "worldgen", Label: "Генерация мира", MRTags: []string{"worldgen"}, CFSlugs: []string{"world-gen", "biomes", "structures", "world-generation"}},
			{ID: "adventure", Label: "Приключения", MRTags: []string{"adventure"}, CFSlugs: []string{"adventure-rpg", "adventure-and-rpg"}},
			{ID: "game-mechanics", Label: "Механики", MRTags: []string{"game-mechanics"}, CFSlugs: []string{"miscellaneous"}},
			{ID: "magic", Label: "Магия", MRTags: []string{"magic"}, CFSlugs: []string{"magic"}},
			{ID: "technology", Label: "Технологии", MRTags: []string{"technology"}, CFSlugs: []string{"technology"}},
			{ID: "mobs", Label: "Мобы", MRTags: []string{"mobs"}, CFSlugs: []string{"mobs"}},
			{ID: "equipment", Label: "Снаряжение", MRTags: []string{"equipment"}, CFSlugs: []string{"armor-weapons-tools"}},
			{ID: "utility", Label: "Утилиты", MRTags: []string{"utility"}, CFSlugs: []string{"utility-qol"}},
			{ID: "decoration", Label: "Декор", MRTags: []string{"decoration"}, CFSlugs: []string{"cosmetic"}},
			{ID: "food", Label: "Еда", MRTags: []string{"food"}, CFSlugs: []string{"food"}},
		}
	default:
		return []CategoryDef{
			{ID: "optimization", Label: "Оптимизация", MRTags: []string{"optimization"}, CFSlugs: []string{"performance", "bugfixes"}},
			{ID: "technology", Label: "Технологии", MRTags: []string{"technology"}, CFSlugs: []string{"technology", "tech"}},
			{ID: "magic", Label: "Магия", MRTags: []string{"magic"}, CFSlugs: []string{"magic"}},
			{ID: "adventure", Label: "Приключения", MRTags: []string{"adventure"}, CFSlugs: []string{"adventure-rpg", "adventure-and-rpg"}},
			{ID: "worldgen", Label: "Генерация мира", MRTags: []string{"worldgen"}, CFSlugs: []string{"world-gen", "biomes", "structures", "world-generation"}},
			{ID: "mobs", Label: "Мобы", MRTags: []string{"mobs"}, CFSlugs: []string{"mobs"}},
			{ID: "equipment", Label: "Снаряжение", MRTags: []string{"equipment"}, CFSlugs: []string{"armor-weapons-tools"}},
			{ID: "storage", Label: "Хранение", MRTags: []string{"storage"}, CFSlugs: []string{"storage"}},
			{ID: "utility", Label: "Утилиты", MRTags: []string{"utility"}, CFSlugs: []string{"utility-qol"}},
			{ID: "decoration", Label: "Декор", MRTags: []string{"decoration"}, CFSlugs: []string{"cosmetic", "building-gadgets"}},
			{ID: "food", Label: "Еда", MRTags: []string{"food"}, CFSlugs: []string{"food", "farming"}},
			{ID: "library", Label: "Библиотеки", MRTags: []string{"library"}, CFSlugs: []string{"library-api", "api-and-library"}},
			{ID: "game-mechanics", Label: "Механики", MRTags: []string{"game-mechanics"}, CFSlugs: []string{"miscellaneous"}},
			{ID: "transportation", Label: "Транспорт", MRTags: []string{"transportation"}, CFSlugs: []string{"technology-player-transport"}},
			{ID: "social", Label: "Мультиплеер", MRTags: []string{"social"}, CFSlugs: []string{"server-utility"}},
		}
	}
}

var cfCategoryAliases = map[string][]string{
	"worldgen":       {"world gen", "world generation", "biomes", "world biomes", "structures", "world structures", "world dimensions"},
	"adventure":      {"adventure and rpg", "adventure & rpg", "rpg"},
	"equipment":      {"armor, tools, and weapons", "armor tools and weapons", "weapons", "armor"},
	"utility":        {"utility & qol", "utility and qol", "qol"},
	"decoration":     {"cosmetic", "building gadgets", "building", "decor"},
	"library":        {"api and library", "library-api", "library"},
	"optimization":   {"performance", "bugfixes", "optimization"},
	"game-mechanics": {"game mechanics", "mechanics", "miscellaneous"},
}

func ExpandCategoryTags(ct, catID string) map[string]struct{} {
	tags := map[string]struct{}{}
	add := func(s string) {
		low := strings.ToLower(strings.TrimSpace(s))
		if low == "" {
			return
		}
		tags[low] = struct{}{}
		tags[strings.ReplaceAll(low, "-", " ")] = struct{}{}
		tags[strings.ReplaceAll(low, " ", "-")] = struct{}{}
	}
	lowID := strings.ToLower(strings.TrimSpace(catID))
	add(lowID)
	for _, c := range CategoriesForType(ct) {
		if c.ID == lowID {
			for _, t := range c.MRTags {
				add(t)
			}
			for _, s := range c.CFSlugs {
				add(s)
			}
			break
		}
	}
	for _, al := range cfCategoryAliases[lowID] {
		add(al)
	}
	return tags
}

func ItemPassesCategories(itemCats []string, ct string, catsIn, catsOut []string, catMode string) bool {
	if len(catsIn) == 0 && len(catsOut) == 0 {
		return true
	}
	norm := map[string]struct{}{}
	for _, c := range itemCats {
		l := strings.ToLower(strings.TrimSpace(c))
		if l == "" {
			continue
		}
		norm[l] = struct{}{}
		norm[strings.ReplaceAll(l, "-", " ")] = struct{}{}
		norm[strings.ReplaceAll(l, " ", "-")] = struct{}{}
	}

	hasOverlap := func(tagSet map[string]struct{}) bool {
		for k := range norm {
			if _, ok := tagSet[k]; ok {
				return true
			}
		}
		return false
	}

	for _, outID := range catsOut {
		if hasOverlap(ExpandCategoryTags(ct, outID)) {
			return false
		}
	}
	if len(catsIn) > 0 {
		if len(norm) == 0 {
			return false
		}
		if strings.ToLower(catMode) == "or" {
			for _, inID := range catsIn {
				if hasOverlap(ExpandCategoryTags(ct, inID)) {
					return true
				}
			}
			return false
		}
		for _, inID := range catsIn {
			if !hasOverlap(ExpandCategoryTags(ct, inID)) {
				return false
			}
		}
	}
	return true
}

func ItemPassesLoaders(itemLoaders []string, ct string, loadersIn, loadersOut []string) bool {
	if len(loadersIn) == 0 && len(loadersOut) == 0 {
		return true
	}
	if ct != "mod" && ct != "datapack" {
		return true
	}
	lset := map[string]struct{}{}
	for _, l := range itemLoaders {
		low := strings.ToLower(strings.TrimSpace(l))
		if IsKnownLoader(low) {
			lset[low] = struct{}{}
		}
	}
	for _, lo := range loadersOut {
		low := strings.ToLower(strings.TrimSpace(lo))
		if low == "datapack" && ct == "datapack" {
			return false
		}
		if _, ok := lset[low]; ok {
			return false
		}
	}
	var realIn []string
	for _, li := range loadersIn {
		low := strings.ToLower(strings.TrimSpace(li))
		if IsKnownLoader(low) {
			realIn = append(realIn, low)
		}
	}
	if len(realIn) > 0 {
		matched := false
		for _, r := range realIn {
			if _, ok := lset[r]; ok {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func ItemPassesEnv(clientSide, serverSide, env string) bool {
	if env == "" {
		return true
	}
	cs := strings.ToLower(strings.TrimSpace(clientSide))
	ss := strings.ToLower(strings.TrimSpace(serverSide))
	if cs == "" && ss == "" {
		return true
	}
	switch env {
	case "client_only":
		return (cs == "required" || cs == "optional") && (ss == "unsupported" || ss == "")
	case "server_only":
		return (ss == "required" || ss == "optional") && (cs == "unsupported" || cs == "")
	case "both":
		return (cs == "required" || cs == "optional") && (ss == "required" || ss == "optional")
	default:
		return true
	}
}


func LoaderSupportsMCVersion(loader, ver string) bool {
	t := ParseMCTuple(ver)
	major, minor, patch := t[0], t[1], t[2]
	if major >= 26 {
		return loader == "neoforge" || loader == "fabric"
	}
	if major != 1 {
		return true
	}
	switch strings.ToLower(loader) {
	case "neoforge":
		return minor > 20 || (minor == 20 && patch >= 1)
	case "fabric":
		return minor >= 14
	case "quilt":
		return minor >= 14
	case "forge":
		return minor >= 6
	default:
		return true
	}
}

func BuildLoaderVersionMatrix(versions []string) map[string][]string {
	out := map[string][]string{}
	for _, l := range KnownLoaders {
		var supported []string
		for _, v := range versions {
			if LoaderSupportsMCVersion(l, v) {
				supported = append(supported, v)
			}
		}
		if supported == nil {
			supported = []string{}
		}
		out[l] = supported
	}
	return out
}

func BuildFiltersCategories(ct string) []FilterCategoryItem {
	defs := CategoriesForType(ct)
	out := make([]FilterCategoryItem, 0, len(defs))
	for _, d := range defs {
		tagMap := ExpandCategoryTags(ct, d.ID)
		tags := make([]string, 0, len(tagMap))
		for k := range tagMap {
			tags = append(tags, k)
		}
		sort.Strings(tags)
		plats := []string{}
		if len(d.MRTags) > 0 {
			plats = append(plats, "modrinth")
		}
		if len(d.CFSlugs) > 0 {
			plats = append(plats, "curseforge")
		}
		out = append(out, FilterCategoryItem{
			ID:        d.ID,
			Label:     d.Label,
			Icon:      "tag",
			Tags:      tags,
			Platforms: plats,
		})
	}
	return out
}
