package catalog

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"modhub/internal/domain"
)

var (
	reMRURL       = regexp.MustCompile(`(?i)https?://(?:www\.)?modrinth\.com/(mod|resourcepack|shader|datapack|plugin)/([A-Za-z0-9._-]+)`)
	reCFURL       = regexp.MustCompile(`(?i)https?://(?:www\.)?curseforge\.com/minecraft/(mc-mods|texture-packs|shaders|data-packs|customization)/([A-Za-z0-9._-]+)`)
	reToken       = regexp.MustCompile(`-?(?:[a-zA-Z]+:(?:"[^"]*"|[^\s|]*)|"[^"]*")|\||[^\s|]+`)
	rePrefix      = regexp.MustCompile(`(?i)^(author|by|dep|depends|mc|v|version|dl|downloads|loader|modloader|source|platform|env|side|cat|category):(.*)$`)
	reCyr         = regexp.MustCompile(`[а-яА-ЯёЁ]`)
)

var ruGamingDict = map[string]string{
	"миникарта": "minimap", "миникарты": "minimap", "миникарту": "minimap",
	"карта": "world map", "карты": "world map",
	"рюкзак": "backpack", "рюкзаки": "backpack", "рюкзаков": "backpack", "сумка": "backpack",
	"данж": "dungeon", "данжи": "dungeon", "данжей": "dungeon", "подземелье": "dungeon", "подземелья": "dungeon",
	"босс": "boss", "боссы": "boss", "боссов": "boss",
	"оптимизация": "optimization", "оптимизации": "optimization", "фпс": "fps", "лаги": "performance",
	"шейдер": "shader", "шейдеры": "shaders", "ресурспак": "resourcepack", "текстуры": "textures",
	"магия": "magic", "заклинания": "spells", "заклинание": "spells",
	"оружие": "weapons", "меч": "sword", "мечи": "sword", "броня": "armor", "доспехи": "armor",
	"еда": "food", "кулинария": "cooking", "ферма": "farming",
	"мобы": "mobs", "животные": "animals", "монстры": "monsters", "биом": "biome", "биомы": "biomes",
	"мебель": "furniture", "декор": "decoration", "сундук": "chest", "сундуки": "storage", "хранилище": "storage",
	"телепорт": "waystones", "телепорты": "waystones", "дракон": "dragon", "драконы": "dragons",
	"анимация": "animations", "анимации": "animations", "интерфейс": "hud", "рецепты": "recipes", "крафт": "crafting",
	"рыбалка": "fishing", "поезда": "trains", "транспорт": "vehicles", "машины": "vehicles",
	"голос": "voice chat", "войс": "voice chat", "инвентарь": "inventory", "освещение": "dynamic lights",
	"звук": "sound", "звуки": "sound", "квест": "quests", "квесты": "quests", "навыки": "skills", "рпг": "rpg",
	"аксессуары": "accessories", "артефакты": "artifacts", "реликвии": "relics",
	"генерация": "worldgen", "структуры": "structures", "деревни": "villages",
	"портал": "portal", "измерение": "dimension", "измерения": "dimensions", "космос": "space",
	"технологии": "technology", "механизмы": "create", "энергия": "energy", "жители": "villagers",
	"чары": "enchantments", "зелья": "potions", "алхимия": "alchemy", "руда": "ores", "руды": "ores",
	"погода": "weather", "сезоны": "seasons", "питомцы": "pets", "боевка": "combat", "бой": "combat",
}

var abbrevMap = map[string]string{
	"jei": "Just Enough Items", "rei": "Roughly Enough Items", "emi": "EMI",
	"ae2": "Applied Energistics 2", "rs": "Refined Storage", "dh": "Distant Horizons",
	"iss": "Iron's Spells 'n Spellbooks", "bop": "Biomes O' Plenty",
	"byg": "Oh The Biomes You'll Go", "bwg": "Oh The Biomes We've Gone",
	"fd": "Farmer's Delight", "vs2": "Valkyrien Skies", "tcon": "Tinkers' Construct",
	"ie": "Immersive Engineering", "cc": "CC: Tweaked", "tf": "Twilight Forest",
	"emf": "Entity Model Features", "etf": "Entity Texture Features", "cit": "CIT Resewn",
	"vmp": "Very Many Players", "ipn": "Inventory Profiles Next", "svc": "Simple Voice Chat",
	"waila": "Jade", "hwyla": "Jade", "sinytra": "Sinytra Connector",
}

var popularLexicon = []string{
	"Sodium", "Embeddium", "Rubidium", "Lithium", "Radium", "Iris Shaders", "Oculus",
	"Create", "Mekanism", "Applied Energistics 2", "Refined Storage", "Farmer's Delight",
	"Iron's Spells 'n Spellbooks", "Just Enough Items", "Roughly Enough Items", "Jade",
	"Sinytra Connector", "Forgified Fabric API", "Fabric API", "Curios API", "Trinkets",
	"ModernFix", "FerriteCore", "EntityCulling", "ImmediatelyFast", "Distant Horizons",
	"JourneyMap", "Xaero's Minimap", "Xaero's World Map", "Waystones", "Sophisticated Backpacks",
	"Biomes O' Plenty", "Terralith", "Tectonic", "GeckoLib", "Architectury API",
	"Continuity", "Polytone", "Fresh Animations", "Complementary Shaders", "BSL Shaders",
}

var forkGroups = [][]string{
	{"Sodium", "Embeddium", "Rubidium"},
	{"Iris Shaders", "Oculus"},
	{"Lithium", "Radium", "Radium Reforged"},
	{"Curios API", "Trinkets", "Accessories"},
	{"Fabric API", "Forgified Fabric API"},
	{"ModernFix", "ModernFix-mVUS"},
	{"Continuity", "Continuity: NeoForged"},
	{"Animatica", "Animatica Refabricated"},
	{"CIT Resewn", "CIT Resewn Refabricated"},
	{"LambDynamicLights", "RyoamicLights"},
	{"Just Enough Items", "Roughly Enough Items", "EMI"},
}

type ParsedProjectURL struct {
	Platform    string
	ContentType string
	Slug        string
}

type ParsedPrefixes struct {
	Authors         []string
	ExcludedAuthors []string
	Deps            []string
	MCVersion       string
	MinDownloads    *int64
	MaxDownloads    *int64
	LoadersIn       []string
	LoadersOut      []string
	Platforms       []string
	Env             string
	CategoriesIn    []string
	CategoriesOut   []string
}

type ParsedQuery struct {
	Raw          string
	CleanText    string
	HasOperators bool
	APIQueries   []string
	ExactPhrases []string
	Negatives    []string
	Prefixes     ParsedPrefixes
}

type SmartExpansion struct {
	CanonicalQuery string
	ExtraQueries   []string
	Notice         string
	Changed        bool
}

func ParseProjectURL(raw string) *ParsedProjectURL {
	s := strings.TrimSpace(raw)
	if !strings.Contains(strings.ToLower(s), "http") {
		return nil
	}
	if m := reMRURL.FindStringSubmatch(s); len(m) >= 3 {
		ct := strings.ToLower(m[1])
		if ct == "plugin" {
			ct = "mod"
		}
		return &ParsedProjectURL{Platform: "modrinth", ContentType: ct, Slug: strings.Trim(m[2], "/")}
	}
	if m := reCFURL.FindStringSubmatch(s); len(m) >= 3 {
		ct := "mod"
		switch strings.ToLower(m[1]) {
		case "texture-packs":
			ct = "resourcepack"
		case "shaders":
			ct = "shader"
		case "data-packs", "customization":
			ct = "datapack"
		}
		return &ParsedProjectURL{Platform: "curseforge", ContentType: ct, Slug: strings.Trim(m[2], "/")}
	}
	return nil
}


func FixRuLayout(text string) string {
	mapping := map[rune]rune{
		'й': 'q', 'ц': 'w', 'у': 'e', 'к': 'r', 'е': 't', 'н': 'y', 'г': 'u', 'ш': 'i', 'щ': 'o', 'з': 'p',
		'ф': 'a', 'ы': 's', 'в': 'd', 'а': 'f', 'п': 'g', 'р': 'h', 'о': 'j', 'л': 'k', 'д': 'l',
		'я': 'z', 'ч': 'x', 'с': 'c', 'м': 'v', 'и': 'b', 'т': 'n', 'ь': 'm',
	}
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		if lat, ok := mapping[r]; ok {
			b.WriteRune(lat)
		} else if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == ' ' || r == '-' || r == '\'' {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func levenshteinRunes(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ca := range a {
		curr[0] = i + 1
		for j, cb := range b {
			cost := 0
			if ca != cb {
				cost = 1
			}
			v := prev[j+1] + 1
			if curr[j]+1 < v {
				v = curr[j] + 1
			}
			if prev[j]+cost < v {
				v = prev[j] + cost
			}
			curr[j+1] = v
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func StrSimilarity(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	maxLen := len(ra)
	if len(rb) > maxLen {
		maxLen = len(rb)
	}
	if maxLen == 0 {
		return 1.0
	}
	return 1.0 - float64(levenshteinRunes(ra, rb))/float64(maxLen)
}

func bestLexiconMatch(word string, extraTitles []string) string {
	wComp := strings.ReplaceAll(domain.NormalizeHaystack(word), " ", "")
	if len(wComp) < 3 {
		return ""
	}
	bestSim := 0.0
	bestName := ""
	check := func(cand string) {
		cComp := strings.ReplaceAll(domain.NormalizeHaystack(cand), " ", "")
		if cComp == "" {
			return
		}
		sim := StrSimilarity(wComp, cComp)
		if sim > bestSim {
			bestSim = sim
			bestName = cand
		}
	}
	for _, p := range popularLexicon {
		check(p)
	}
	for _, t := range extraTitles {
		check(t)
	}
	if bestSim >= 0.72 {
		return bestName
	}
	return ""
}

func GetForkAnalogs(query string) []string {
	qNorm := domain.NormalizeHaystack(query)
	if qNorm == "" {
		return nil
	}
	qComp := strings.ReplaceAll(qNorm, " ", "")
	var grp []string
	switch qNorm {
	case "iris":
		grp = forkGroups[1]
	case "curios":
		grp = forkGroups[3]
	default:
		for _, g := range forkGroups {
			for _, name := range g {
				nn := domain.NormalizeHaystack(name)
				if nn == qNorm || strings.ReplaceAll(nn, " ", "") == qComp {
					grp = g
					break
				}
			}
			if grp != nil {
				break
			}
		}
	}
	if grp == nil {
		return nil
	}
	var out []string
	for _, name := range grp {
		nn := domain.NormalizeHaystack(name)
		if nn != qNorm && strings.ReplaceAll(nn, " ", "") != qComp {
			out = append(out, name)
			if len(out) >= 3 {
				break
			}
		}
	}
	return out
}

func ExpandSmartQuery(raw string, catalogTitles []string) SmartExpansion {
	q := strings.TrimSpace(raw)
	if q == "" {
		return SmartExpansion{}
	}
	canonical := q
	var notices []string
	changed := false

	if reCyr.MatchString(q) {
		words := strings.Fields(strings.ToLower(q))
		allDict := true
		hasCyr := false
		for _, w := range words {
			cw := strings.Trim(w, ".,!?\"'()[]")
			if reCyr.MatchString(cw) {
				hasCyr = true
				if _, ok := ruGamingDict[cw]; !ok {
					allDict = false
				}
			}
		}
		if hasCyr && allDict {
			var trans []string
			for _, w := range words {
				cw := strings.Trim(w, ".,!?\"'()[]")
				if tr, ok := ruGamingDict[cw]; ok {
					trans = append(trans, tr)
				} else if cw != "" {
					trans = append(trans, cw)
				}
			}
			canonical = strings.Join(trans, " ")
			changed = true
			notices = append(notices, q+" → "+canonical)
		} else {
			laid := FixRuLayout(q)
			if len(laid) >= 2 {
				if exp, ok := abbrevMap[strings.ToLower(laid)]; ok {
					canonical = exp
				} else if rec := bestLexiconMatch(laid, catalogTitles); rec != "" {
					canonical = rec
				} else {
					canonical = laid
				}
				changed = true
				notices = append(notices, q+" → "+canonical)
			}
		}
	}

	cLow := strings.ToLower(strings.TrimSpace(canonical))
	if exp, ok := abbrevMap[cLow]; ok && strings.ToLower(exp) != cLow {
		notices = append(notices, strings.ToUpper(canonical)+" → "+exp)
		canonical = exp
		changed = true
	}

	extras := GetForkAnalogs(canonical)
	if len(extras) == 0 {
		extras = GetForkAnalogs(q)
	}

	return SmartExpansion{
		CanonicalQuery: canonical,
		ExtraQueries:   extras,
		Notice:         strings.Join(notices, " • "),
		Changed:        changed,
	}
}

func parseDLNum(raw string) *int64 {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(strings.ReplaceAll(s, ",", "."), "_", "")
	if s == "" {
		return nil
	}
	runes := []rune(s)
	last := runes[len(runes)-1]
	mult := 1.0
	numPart := s
	switch last {
	case 'k', 'к':
		mult = 1_000
		numPart = string(runes[:len(runes)-1])
	case 'm', 'м':
		mult = 1_000_000
		numPart = string(runes[:len(runes)-1])
	case 'b':
		mult = 1_000_000_000
		numPart = string(runes[:len(runes)-1])
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(numPart), 64)
	if err != nil {
		return nil
	}
	res := int64(math.Round(v * mult))
	if res < 0 {
		res = 0
	}
	return &res
}

func normalizeEnvOperator(val string) string {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "client", "client_only", "клиент":
		return "client_only"
	case "server", "server_only", "сервер":
		return "server_only"
	case "both", "all", "оба":
		return "both"
	default:
		return ""
	}
}

func normalizePlatformOperator(val string) string {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "modrinth", "mr":
		return "modrinth"
	case "curseforge", "cf":
		return "curseforge"
	default:
		return ""
	}
}

func ParseSearchQuery(raw string) ParsedQuery {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ParsedQuery{APIQueries: []string{""}}
	}
	tokens := reToken.FindAllString(trimmed, -1)
	pq := ParsedQuery{Raw: trimmed}
	branches := [][]string{{}}

	for _, tok := range tokens {
		if tok == "|" || tok == "OR" {
			pq.HasOperators = true
			if len(branches[len(branches)-1]) > 0 {
				branches = append(branches, []string{})
			}
			continue
		}
		isNeg := false
		body := tok
		if strings.HasPrefix(tok, "-") && len(tok) > 1 {
			isNeg = true
			pq.HasOperators = true
			body = tok[1:]
		}

		if m := rePrefix.FindStringSubmatch(body); len(m) == 3 {
			op := strings.ToLower(m[1])
			val := strings.Trim(strings.TrimSpace(m[2]), "\"")
			pq.HasOperators = true
			// Gracefully skip dangling incomplete commands (e.g. "author:" or "dl:>")
			if val == "" || val == ">" || val == ">=" || val == "<" || val == "<=" || val == ".." {
				continue
			}
			switch op {
			case "author", "by":
				if isNeg {
					pq.Prefixes.ExcludedAuthors = domain.AppendUniqueCI(pq.Prefixes.ExcludedAuthors, val)
				} else {
					pq.Prefixes.Authors = domain.AppendUniqueCI(pq.Prefixes.Authors, val)
				}
			case "dep", "depends":
				if !isNeg {
					pq.Prefixes.Deps = domain.AppendUniqueCI(pq.Prefixes.Deps, val)
				}
			case "mc", "v", "version":
				if !isNeg {
					pq.Prefixes.MCVersion = val
				}
			case "loader", "modloader":
				lv := strings.ToLower(val)
				if lv == "datapack" || domain.IsKnownLoader(lv) {
					if isNeg {
						pq.Prefixes.LoadersOut = domain.AppendUniqueCI(pq.Prefixes.LoadersOut, lv)
					} else {
						pq.Prefixes.LoadersIn = domain.AppendUniqueCI(pq.Prefixes.LoadersIn, lv)
					}
				}
			case "source", "platform":
				if !isNeg {
					if p := normalizePlatformOperator(val); p != "" {
						pq.Prefixes.Platforms = domain.AppendUniqueCI(pq.Prefixes.Platforms, p)
					}
				}
			case "env", "side":
				if !isNeg {
					if e := normalizeEnvOperator(val); e != "" {
						pq.Prefixes.Env = e
					}
				}
			case "cat", "category":
				cv := strings.ToLower(val)
				if isNeg {
					pq.Prefixes.CategoriesOut = domain.AppendUniqueCI(pq.Prefixes.CategoriesOut, cv)
				} else {
					pq.Prefixes.CategoriesIn = domain.AppendUniqueCI(pq.Prefixes.CategoriesIn, cv)
				}
			case "dl", "downloads":
				if !isNeg {
					if loS, hiS, ok := strings.Cut(val, ".."); ok {
						pq.Prefixes.MinDownloads = parseDLNum(loS)
						pq.Prefixes.MaxDownloads = parseDLNum(hiS)
					} else if strings.HasPrefix(val, ">=") {
						pq.Prefixes.MinDownloads = parseDLNum(val[2:])
					} else if strings.HasPrefix(val, ">") {
						if n := parseDLNum(val[1:]); n != nil {
							v := *n + 1
							pq.Prefixes.MinDownloads = &v
						}
					} else if strings.HasPrefix(val, "<=") {
						pq.Prefixes.MaxDownloads = parseDLNum(val[2:])
					} else if strings.HasPrefix(val, "<") {
						if n := parseDLNum(val[1:]); n != nil {
							v := *n - 1
							if v < 0 {
								v = 0
							}
							pq.Prefixes.MaxDownloads = &v
						}
					} else {
						pq.Prefixes.MinDownloads = parseDLNum(val)
					}
				}
			}
			continue
		}

		isQuoted := len(body) >= 2 && strings.HasPrefix(body, "\"") && strings.HasSuffix(body, "\"")
		if isQuoted || strings.Contains(body, "*") {
			pq.HasOperators = true
		}
		clean := strings.TrimSpace(strings.ReplaceAll(strings.Trim(body, "\""), "*", " "))
		if clean == "" {
			continue
		}
		if isNeg {
			pq.Negatives = append(pq.Negatives, strings.ToLower(clean))
		} else {
			if isQuoted {
				pq.ExactPhrases = append(pq.ExactPhrases, clean)
			}
			branches[len(branches)-1] = append(branches[len(branches)-1], clean)
		}
	}

	var positives []string
	for _, b := range branches {
		if len(b) > 0 {
			joined := strings.Join(b, " ")
			pq.APIQueries = append(pq.APIQueries, joined)
			positives = append(positives, joined)
		}
	}
	if len(pq.APIQueries) == 0 {
		pq.APIQueries = []string{""}
	}
	if len(pq.APIQueries) > 4 {
		pq.APIQueries = pq.APIQueries[:4]
	}
	pq.CleanText = strings.Join(positives, " ")
	return pq
}

func cardHasAuthor(item domain.ProjectCard, needleLower string) bool {
	for _, a := range item.Authors {
		if strings.Contains(strings.ToLower(a), needleLower) {
			return true
		}
	}
	for _, s := range item.Sources {
		for _, a := range s.Authors {
			if strings.Contains(strings.ToLower(a), needleLower) {
				return true
			}
		}
	}
	return false
}

func ItemMatchesParsed(item domain.ProjectCard, pq ParsedQuery) bool {
	for _, reqA := range pq.Prefixes.Authors {
		if !cardHasAuthor(item, strings.ToLower(reqA)) {
			return false
		}
	}
	for _, exclA := range pq.Prefixes.ExcludedAuthors {
		if cardHasAuthor(item, strings.ToLower(exclA)) {
			return false
		}
	}
	if pq.Prefixes.MinDownloads != nil && item.Downloads < *pq.Prefixes.MinDownloads {
		return false
	}
	if pq.Prefixes.MaxDownloads != nil && item.Downloads > *pq.Prefixes.MaxDownloads {
		return false
	}
	if pq.Prefixes.MCVersion != "" && len(item.GameVersions) > 0 {
		want := strings.TrimSpace(pq.Prefixes.MCVersion)
		found := false
		for _, gv := range item.GameVersions {
			if gv == want || (strings.Count(want, ".") == 1 && strings.HasPrefix(gv, want+".")) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(pq.ExactPhrases) > 0 {
		normHay := domain.NormalizeHaystack(item.Title + " " + item.Slug + " " + item.Summary)
		for _, ph := range pq.ExactPhrases {
			np := domain.NormalizeHaystack(ph)
			if np != "" && !strings.Contains(normHay, np) {
				return false
			}
		}
	}
	if len(pq.Negatives) > 0 {
		hay := strings.ToLower(item.Title + " " + item.Slug + " " + item.Summary + " " + strings.Join(item.Authors, " "))
		realLoaders := make([]string, 0, len(item.Loaders))
		for _, l := range item.Loaders {
			low := strings.ToLower(l)
			if domain.IsKnownLoader(low) {
				realLoaders = append(realLoaders, low)
			}
		}
		for _, neg := range pq.Negatives {
			if strings.Contains(hay, neg) {
				return false
			}
			// If user writes "-fabric", also exclude Fabric-only mods even if "fabric" isn't in the title
			if domain.IsKnownLoader(neg) && len(realLoaders) == 1 && realLoaders[0] == neg {
				return false
			}
		}
	}
	return true
}

func scoreSingleMatch(item domain.ProjectCard, rq string) (int, float64) {
	qNorm := domain.NormalizeHaystack(rq)
	if qNorm == "" {
		return 0, 0
	}
	qComp := strings.ReplaceAll(qNorm, " ", "")
	qTokens := strings.Fields(qNorm)

	cleanTitle := domain.CleanDisplayTitle(item.Title)
	titleNorm := domain.NormalizeHaystack(cleanTitle)
	titleComp := strings.ReplaceAll(titleNorm, " ", "")
	titleNoAcrNorm := domain.NormalizeHaystack(domain.ReParensShortCode.ReplaceAllString(cleanTitle, " "))
	titleNoAcrComp := strings.ReplaceAll(titleNoAcrNorm, " ", "")

	titleWords := map[string]struct{}{}
	for _, w := range strings.Fields(titleNorm) {
		titleWords[w] = struct{}{}
	}

	slugNorm := domain.NormalizeHaystack(item.Slug)
	slugComp := strings.ReplaceAll(slugNorm, " ", "")
	summaryNorm := domain.NormalizeHaystack(item.Summary)

	// Check exact ID / slug matches across all merged platform sources
	exactSourceIDOrSlug := false
	for _, s := range item.Sources {
		if strings.EqualFold(strings.TrimSpace(s.ID), strings.TrimSpace(rq)) {
			exactSourceIDOrSlug = true
			break
		}
		sn := domain.NormalizeHaystack(s.Slug)
		if sn == qNorm || (qComp != "" && strings.ReplaceAll(sn, " ", "") == qComp) {
			exactSourceIDOrSlug = true
			break
		}
	}

	// 1. Exact title or exact slug/ID match (highest priority)
	if titleNorm == qNorm ||
		(qComp != "" && titleComp == qComp) ||
		(titleNoAcrNorm != "" && titleNoAcrNorm == qNorm) ||
		(qComp != "" && titleNoAcrComp != "" && titleNoAcrComp == qComp) ||
		slugNorm == qNorm ||
		(qComp != "" && slugComp == qComp) ||
		exactSourceIDOrSlug {
		return 100, 1.0
	}

	// 2. Title starts with query as whole word(s) (e.g. "EMI Loot" for "EMI", "Create: Steam 'n' Rails" for "Create")
	if strings.HasPrefix(titleNorm, qNorm+" ") || (titleNoAcrNorm != "" && strings.HasPrefix(titleNoAcrNorm, qNorm+" ")) {
		sub := float64(len(qComp)) / math.Max(float64(len(titleComp)), 1)
		return 92, sub
	}

	// 2b. Compact prefix match (e.g. "EMIffect" for "EMI")
	if len(qComp) >= 3 && strings.HasPrefix(titleComp, qComp) {
		sub := float64(len(qComp)) / math.Max(float64(len(titleComp)), 1)
		return 90, sub
	}

	// 3. All query words appear as standalone words in title (e.g. "Extra EMI" for "EMI")
	allInTitle := len(qTokens) > 0
	for _, qt := range qTokens {
		if _, ok := titleWords[qt]; !ok {
			allInTitle = false
			break
		}
	}
	if allInTitle {
		sub := float64(len(qTokens)) / math.Max(float64(len(titleWords)), 1)
		return 84, sub
	}

	// 4. Substring in title (for queries >= 3 chars so short acronyms don't falsely match mid-word like "ch[emi]stry")
	if len(qComp) >= 3 && (strings.Contains(titleNorm, qNorm) || strings.Contains(titleComp, qComp)) {
		// Penalize mid-word match for short 3-char queries (e.g. "emi" inside "chemistry")
		if len(qComp) <= 3 {
			sub := float64(len(qComp)) / math.Max(float64(len(titleComp)), 1)
			return 64, sub
		}
		sub := float64(len(qComp)) / math.Max(float64(len(titleComp)), 1)
		return 74, sub
	}

	// 5. Author match
	for _, a := range item.Authors {
		an := domain.NormalizeHaystack(a)
		if an == qNorm {
			return 72, 1.0
		}
		if len(qNorm) >= 3 && strings.Contains(an, qNorm) {
			return 66, 0.7
		}
	}

	// 6. Summary / description & categories match
	if strings.Contains(summaryNorm, qNorm) {
		// Require word boundary for short queries in summary to avoid "chemistry" matching "emi"
		if len(qComp) > 3 || strings.Contains(" "+summaryNorm+" ", " "+qNorm+" ") {
			return 44, 0.85
		}
	}
	allInSum := len(qTokens) > 0
	for _, qt := range qTokens {
		if len(qt) <= 3 {
			if !strings.Contains(" "+summaryNorm+" ", " "+qt+" ") {
				allInSum = false
				break
			}
		} else if !strings.Contains(summaryNorm, qt) {
			allInSum = false
			break
		}
	}
	if allInSum {
		return 36, 0.7
	}
	for _, cat := range item.Categories {
		cn := domain.NormalizeHaystack(cat)
		if cn == qNorm || (len(qNorm) >= 4 && strings.Contains(cn, qNorm)) {
			return 32, 0.6
		}
	}

	// 7. Partial token overlap in title or summary
	if len(qTokens) > 1 {
		matchedTitle := 0
		matchedAny := 0
		for _, qt := range qTokens {
			if len(qt) < 2 {
				continue
			}
			if _, ok := titleWords[qt]; ok || (len(qt) >= 4 && strings.Contains(titleNorm, qt)) {
				matchedTitle++
				matchedAny++
			} else if strings.Contains(" "+summaryNorm+" ", " "+qt+" ") {
				matchedAny++
			}
		}
		if matchedTitle > 0 {
			return 24, float64(matchedTitle) / float64(len(qTokens))
		}
		if matchedAny > 0 {
			return 16, float64(matchedAny) / float64(len(qTokens))
		}
	}

	return 0, 0
}

func matchTier(item domain.ProjectCard, queries []string) (int, float64) {
	bestTier := 0
	bestSub := 0.0
	for _, rq := range queries {
		tier, sub := scoreSingleMatch(item, rq)
		if tier > bestTier || (tier == bestTier && sub > bestSub) {
			bestTier, bestSub = tier, sub
		}
	}
	return bestTier, bestSub
}

// ComputeSearchScoreMulti ranks direct query matches strictly ahead of fork analogs
// (e.g. searching "EMI" ALWAYS ranks EMI above JEI/REI) and unrelated high-download noise,
// while preserving download popularity ordering within the same match tier.
func ComputeSearchScoreMulti(item domain.ProjectCard, primaryQueries []string, analogQueries []string, sortMode string) float64 {
	pTier, pSub := matchTier(item, primaryQueries)
	aTier, aSub := 0, 0.0
	if pTier < 64 && len(analogQueries) > 0 {
		aTier, aSub = matchTier(item, analogQueries)
	}

	dl := float64(item.Downloads)
	if dl < 0 {
		dl = 0
	}
	logDL := math.Log10(dl + 1.0)

	// Base relevance score strictly separated by tier (gaps >= 55 pts exceed max log10(dl) spread)
	tb := -200.0
	switch {
	case pTier >= 100:
		tb = 380.0
	case pTier >= 92:
		tb = 300.0 + pSub*16.0
	case pTier >= 90:
		tb = 282.0 + pSub*14.0
	case pTier >= 84:
		tb = 232.0 + pSub*14.0
	case pTier >= 72:
		tb = 174.0 + pSub*12.0
	case pTier >= 64:
		tb = 136.0 + pSub*10.0
	case aTier >= 90:
		// Exact/prefix match to a fork analog (e.g. JEI when searching EMI) sits cleanly below direct title matches
		tb = 112.0 + aSub*8.0
	case aTier >= 72:
		tb = 84.0 + aSub*6.0
	case pTier >= 40:
		tb = 60.0 + pSub*8.0
	case pTier >= 30:
		tb = 42.0 + pSub*6.0
	case pTier > 0:
		tb = 18.0 + pSub*6.0
	case aTier > 0:
		tb = 6.0 + aSub*4.0
	}

	if sortMode == "relevance" {
		return tb + logDL*4.0
	}
	// Default ("downloads") sort with active search query: popularity strongly orders within the same relevance tier,
	// without allowing a 300M-download analog or unrelated mod to overtake an exact title match.
	return tb + logDL*7.5
}

func ComputeSearchScore(item domain.ProjectCard, canonicalQ string, extraQueries []string, sortMode string) float64 {
	var primary []string
	if canonicalQ != "" {
		primary = append(primary, canonicalQ)
	}
	return ComputeSearchScoreMulti(item, primary, extraQueries, sortMode)
}
