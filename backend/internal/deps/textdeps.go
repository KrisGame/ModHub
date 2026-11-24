package deps

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"modhub/internal/catalog"
	"modhub/internal/domain"
	"modhub/internal/netx"
	"modhub/internal/security"
	"modhub/internal/storage"
)

type TextDepSourceInput struct {
	Platform  string
	ProjectID string
	Title     string
	Summary   string
	Body      string
	Changelog string
}

type TextDepCandidate struct {
	Name       string `json:"name"`
	Type       string `json:"type"`       // required | optional | recommended | integration | mention
	Confidence string `json:"confidence"` // high | medium | low
	Evidence   string `json:"evidence"`
	Origin     string `json:"origin"` // title | summary | description | changelog
	DirectURL  string `json:"direct_url,omitempty"`
}

// Known alternative names & library aliases (normalized compact key -> search/slug hints).
// Used solely to locate the real Modrinth/CurseForge project AFTER a mod name was extracted
// from an explicit dependency statement in the author's text.
var textDepAliases = map[string][]string{
	"bettercombat":        {"better-combat", "better-combat-by-daedelus", "Better Combat"},
	"cataclysm":           {"l-enders-cataclysm", "lendercataclysm", "L_Ender's Cataclysm"},
	"lenderscataclysm":    {"l-enders-cataclysm", "lendercataclysm", "L_Ender's Cataclysm"},
	"geckolib":            {"geckolib", "GeckoLib"},
	"architectury":        {"architectury-api", "Architectury API"},
	"architecturyapi":     {"architectury-api", "Architectury API"},
	"curios":              {"curios", "Curios API"},
	"curiosapi":           {"curios", "Curios API"},
	"clothconfig":         {"cloth-config", "Cloth Config API"},
	"clothconfigapi":      {"cloth-config", "Cloth Config API"},
	"playeranimator":      {"playeranimator", "player-animator", "playerAnimator"},
	"lionfishapi":         {"lionfish-api", "Lionfish API"},
	"citadel":             {"citadel", "Citadel"},
	"fabricapi":           {"fabric-api", "Fabric API"},
	"jei":                 {"jei", "Just Enough Items"},
	"justenoughitems":     {"jei", "Just Enough Items"},
	"rei":                 {"rei", "Roughly Enough Items"},
	"emi":                 {"emi", "EMI"},
	"create":              {"create", "Create"},
	"patchouli":           {"patchouli", "Patchouli"},
	"moonlight":           {"moonlight", "Moonlight Lib"},
	"moonlightlib":        {"moonlight", "Moonlight Lib"},
	"balm":                {"balm", "Balm"},
	"puzzleslib":          {"puzzles-lib", "Puzzles Lib"},
	"bookshelf":           {"bookshelf", "Bookshelf"},
	"collective":          {"collective", "Collective"},
	"azurelib":            {"azurelib", "AzureLib"},
	"terrablender":        {"terrablender", "TerraBlender"},
	"kotlinforforge":      {"kotlin-for-forge", "Kotlin for Forge"},
	"fabriclanguagekotlin": {"fabric-language-kotlin", "Fabric Language Kotlin"},
	"yungsapi":            {"yungs-api", "YUNG's API"},
	"caelus":              {"caelus", "Caelus API"},
	"caelusapi":           {"caelus", "Caelus API"},
	"smartbrainlib":       {"smartbrainlib", "SmartBrainLib"},
	"resourcefullib":      {"resourceful-lib", "Resourceful Lib"},
	"trinkets":            {"trinkets", "Trinkets"},
	"accessories":         {"accessories", "Accessories"},
	"flywheel":            {"flywheel", "Flywheel"},
	"epicfight":           {"epic-fight", "Epic Fight"},
	"ironsspellbooks":     {"irons-spells-n-spellbooks", "Iron's Spells 'n Spellbooks"},
	"farmersdelight":      {"farmers-delight", "Farmer's Delight"},
	"alexsmobs":           {"alexs-mobs", "Alex's Mobs"},
	"alexscaves":          {"alexs-caves", "Alex's Caves"},
	"iceandfire":          {"ice-and-fire-dragons", "Ice and Fire: Dragons"},
	"twilightforest":      {"the-twilight-forest", "The Twilight Forest"},
	"botania":             {"botania", "Botania"},
	"arsnouveau":          {"ars-nouveau", "Ars Nouveau"},
	"mekanism":            {"mekanism", "Mekanism"},
	"appliedenergistics2": {"ae2", "Applied Energistics 2"},
	"ae2":                 {"ae2", "Applied Energistics 2"},
}

var (
	reHTMLAnchor      = regexp.MustCompile(`(?is)<a\b[^>]*href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
	reHTMLBlockBreak  = regexp.MustCompile(`(?i)</?(?:p|div|br|li|ul|ol|tr|h[1-6]|hr)\b[^>]*>`)
	reHTMLHeading     = regexp.MustCompile(`(?is)<h[1-6]\b[^>]*>(.*?)</h[1-6]>`)
	reHTMLTagsStrip   = regexp.MustCompile(`<[^>]+>`)
	reMDLink          = regexp.MustCompile(`\[([^\]]{1,120})\]\((https?://[^)\s]+)\)`)
	reMDImage         = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)
	reMDHeading       = regexp.MustCompile(`^\s*#{1,6}\s+(.+?)\s*#*\s*$`)
	reBulletPrefix    = regexp.MustCompile(`^\s*(?:[-*•–—]+|\d+[.)])\s+`)
	reTrailingClause  = regexp.MustCompile(`(?i)(?:\b(?:to\s+work|to\s+run|to\s+function|to\s+use|installed|in\s+order\s+to|for\s+minecraft|for\s+mc|or\s+higher|or\s+newer|or\s+above|or\s+later|version\s+\d|v\d+\.\d+|1\.\d+(?:\.\d+)?|2\d\.\d+)|(?:\s+|^)(?:чтобы\s+работать|для\s+работы|для\s+запуска|установлен\w*|версии\s+\d|или\s+выше|или\s+новее)).*$`)
	reStripVersionTag = regexp.MustCompile(`(?i)\s*(?:\([^)]{0,40}\)|\[[^\]]{0,40}\]|\bv?\d+\.\d+(?:\.\d+)?\+?\s*)$`)

	// Negations (EN + RU)
	reNegation = regexp.MustCompile(`(?i)(?:\b(?:does\s+not\s+require|doesn['’]t\s+require|do\s+not\s+require|don['’]t\s+require|does\s+not\s+need|doesn['’]t\s+need|do\s+not\s+need|don['’]t\s+need|not\s+required|no\s+longer\s+requires?|without\s+requiring|no\s+dependency\s+on|not\s+depend\s+on|no\s+need\s+for)\b|(?:\s+|^)(?:не\s+требует(?:ся)?|не\s+нужен|не\s+нужна|не\s+нужно|не\s+обязательно|без\s+необходимости|больше\s+не\s+требует(?:ся)?|не\s+зависит\s+от)(?:\s+|$|[.,:;!?]))`)

	// Explicit required dependency patterns (EN + RU)
	reReqExplicit = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:this\s+(?:mod|addon|pack|plugin|extension)\s+)?(?:strictly\s+|hard\s+)?(?:requires?|depends\s+on|needs?)\s*[:\-–—]?\s*([^.!?\n;]{2,180})`),
		regexp.MustCompile(`(?i)\b(?:required\s+(?:dependencies?|mods?|libraries?)|hard\s+dependencies?|mandatory\s+dependencies?|dependencies?)\s*[:\-–—]\s*([^.!?\n;]{2,180})`),
		regexp.MustCompile(`(?i)\b(?:only\s+works?\s+with|will\s+not\s+work\s+without|won['’]t\s+work\s+without|must\s+have|must\s+install)\s+([^.!?\n;]{2,160})`),
		regexp.MustCompile(`(?i)(?:требует(?:ся)?|зависит\s+от|необходим[аоы]?|нужен|нужна|нужно|обязательн(?:ая|ые)\s+зависимост[ьи]|работает\s+только\s+с|не\s+работает\s+без)\s*[:\-–—]?\s*([^.!?\n;]{2,180})`),
	}
	reReqMedium = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:built\s+on\s+top\s+of|made\s+for|designed\s+for|addon\s+for|add-on\s+for|expansion\s+for)\s+([^.!?\n;]{2,140})`),
		regexp.MustCompile(`(?i)(?:построен\s+на\s+базе|создан\s+для|аддон\s+для|дополнение\s+для)\s+([^.!?\n;]{2,140})`),
	}
	reOptional = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:optional(?:ly)?\s+(?:requires?|depends\s+on|dependencies?|mods?|libraries?|supports?)|optional\s*[:\-–—])\s*([^.!?\n;]{2,160})`),
		regexp.MustCompile(`(?i)(?:опциональн(?:ая|ые|о)\s*(?:зависимост[ьи])?|необязательн(?:ая|ые|о))\s*[:\-–—]?\s*([^.!?\n;]{2,160})`),
	}
	reRecommended = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:highly\s+recommended|recommended(?:\s+mods?|\s+dependencies?)?|we\s+recommend|i\s+recommend|suggested)\s*[:\-–—]?\s*([^.!?\n;]{2,160})`),
		regexp.MustCompile(`(?i)(?:рекомендуе(?:тся|м)|советуем|желательно(?:\s+установить)?)\s*[:\-–—]?\s*([^.!?\n;]{2,160})`),
	}
	reIntegration = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:compatible\s+with|compatibility\s+(?:with|for)|integrates?\s+with|integration\s+(?:with|for)|works\s+(?:great\s+)?with|supports?\s+integration\s+with)\s*[:\-–—]?\s*([^.!?\n;]{2,160})`),
		regexp.MustCompile(`(?i)(?:совместим\w*\s+с|интеграция\s+с|работает\s+вместе\s+с|поддержка\s+интеграции\s+с)\s*[:\-–—]?\s*([^.!?\n;]{2,160})`),
	}

	// Title inference (strictly LOW confidence — never auto-installed)
	reTitleSuffix = regexp.MustCompile(`(?i)^(.{2,48}?)\s+(?:addon|add-on|integration|compatibility|compat|extension|bridge)\b`)
	reTitleFor    = regexp.MustCompile(`(?i)\b(?:addon|add-on|integration|compatibility|compat|extension|аддон|дополнение|интеграция)\s+(?:for|для)\s+([A-Za-zА-Яа-я0-9_'’:\-\s]{2,48})$`)

	// List splitters: `, `, ` and `, ` & `, ` + `, ` и `, ` / `
	reListSplit     = regexp.MustCompile(`(?i)\s*(?:,\s*(?:and\s+|и\s+)?|\s+and\s+|\s+и\s+|\s*[&+]\s*|\s+/\s+)\s*`)
	reLeadingFiller = regexp.MustCompile(`(?i)^(?:the\s+|a\s+|an\s+|mod\s+|library\s+|both\s+|мод\s+|моды\s+|библиотек[аиу]\s+)+`)
	reTrailingMod   = regexp.MustCompile(`(?i)\s+(?:mod|mods|library|plugin|мод|моды)$`)
	reSentenceSplit = regexp.MustCompile(`[.!?]+(?:\s+|$)`)
)

var stopDependencyNames = map[string]struct{}{
	"minecraft": {}, "mc": {}, "java": {}, "forge": {}, "neoforge": {}, "fabric": {}, "quilt": {},
	"liteloader": {}, "modloader": {}, "fml": {}, "mod": {}, "mods": {}, "thismod": {}, "themod": {},
	"base": {}, "basemod": {}, "game": {}, "client": {}, "server": {}, "both": {}, "loader": {},
	"version": {}, "latest": {}, "latestversion": {}, "anything": {}, "nothing": {}, "none": {},
	"other": {}, "othermods": {}, "allmods": {}, "vanilla": {}, "datapack": {}, "resourcepack": {},
	"optifine": {}, "dependencies": {}, "requirements": {}, "installation": {}, "instructions": {},
	"мод": {}, "моды": {}, "этотмод": {}, "игра": {}, "клиент": {}, "сервер": {}, "версия": {},
	"ничего": {}, "зависимости": {}, "требования": {}, "установка": {}, "библиотеки": {},
	"it": {}, "them": {}, "you": {}, "your": {}, "my": {}, "our": {}, "one": {}, "two": {},
	"bothmods": {}, "all": {}, "any": {}, "more": {}, "new": {}, "the": {}, "a": {}, "an": {},
}

func textDepTypePriority(t string) int {
	switch strings.ToLower(t) {
	case "required":
		return 6
	case "incompatible":
		return 5
	case "recommended":
		return 4
	case "optional":
		return 3
	case "integration":
		return 2
	case "mention":
		return 1
	default:
		return 0
	}
}

func confidenceRank(c string) int {
	switch strings.ToLower(c) {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func normalizeMarkupToLines(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	// Remove markdown images before link extraction
	s = reMDImage.ReplaceAllString(s, " ")
	// Convert HTML headings to markdown headings so heading detection works uniformly
	s = reHTMLHeading.ReplaceAllStringFunc(s, func(m string) string {
		sub := reHTMLHeading.FindStringSubmatch(m)
		if len(sub) == 2 {
			return "\n## " + strings.TrimSpace(reHTMLTagsStrip.ReplaceAllString(sub[1], " ")) + "\n"
		}
		return "\n"
	})
	// Convert HTML <a href="URL">Label</a> -> [Label](URL) so we keep both label and direct link!
	s = reHTMLAnchor.ReplaceAllStringFunc(s, func(m string) string {
		sub := reHTMLAnchor.FindStringSubmatch(m)
		if len(sub) == 3 {
			href := strings.TrimSpace(sub[1])
			label := strings.TrimSpace(reHTMLTagsStrip.ReplaceAllString(sub[2], " "))
			if label != "" && href != "" {
				return "[" + label + "](" + href + ")"
			}
			return label
		}
		return " "
	})
	// Convert HTML <li> to "- " bullet lines before stripping tags
	s = regexp.MustCompile(`(?i)<li\b[^>]*>`).ReplaceAllString(s, "\n- ")
	s = regexp.MustCompile(`(?i)</li>`).ReplaceAllString(s, "\n")
	s = reHTMLBlockBreak.ReplaceAllString(s, "\n")
	s = reHTMLTagsStrip.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)

	rawLines := strings.Split(s, "\n")
	out := make([]string, 0, len(rawLines))
	for _, ln := range rawLines {
		clean := strings.TrimSpace(ln)
		if clean != "" {
			out = append(out, clean)
		}
	}
	return out
}

func detectSectionKind(line string) (string, bool) {
	heading := ""
	if m := reMDHeading.FindStringSubmatch(line); len(m) == 2 {
		heading = strings.TrimSpace(m[1])
	} else {
		trimmed := strings.Trim(strings.TrimSpace(line), "*_:")
		if len(trimmed) >= 4 && len(trimmed) <= 44 && strings.HasSuffix(strings.TrimSpace(line), ":") && !strings.Contains(trimmed, "http") {
			heading = trimmed
		}
	}
	if heading == "" {
		return "", false
	}
	low := strings.ToLower(heading)
	switch {
	case strings.Contains(low, "credit") || strings.Contains(low, "thank") || strings.Contains(low, "license") || strings.Contains(low, "faq") || strings.Contains(low, "благодар") || strings.Contains(low, "лиценз"):
		return "credits", true
	case strings.Contains(low, "optional") || strings.Contains(low, "опциональ"):
		return "optional", true
	case strings.Contains(low, "recommend") || strings.Contains(low, "рекоменд"):
		return "recommended", true
	case strings.Contains(low, "compat") || strings.Contains(low, "integrat") || strings.Contains(low, "совместим") || strings.Contains(low, "интеграц"):
		return "integration", true
	case strings.Contains(low, "require") || strings.Contains(low, "dependenc") || strings.Contains(low, "требован") || strings.Contains(low, "зависимост"):
		return "required", true
	default:
		return "other", true
	}
}

func cleanCandidateModName(raw string) (string, string) {
	directURL := ""
	s := strings.TrimSpace(raw)
	if m := reMDLink.FindStringSubmatch(s); len(m) == 3 {
		s = strings.TrimSpace(m[1])
		directURL = strings.TrimSpace(m[2])
	}
	// Strip markdown bold/italic/code markers
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.Trim(s, "*_\"'«»()[]<>.,:;!?-–— ")
	s = reTrailingClause.ReplaceAllString(s, "")
	s = reStripVersionTag.ReplaceAllString(s, "")
	// Strip leading filler words ("the ", "mod ", "library ", "мод ", "библиотеку ")
	s = reLeadingFiller.ReplaceAllString(s, "")
	s = reTrailingMod.ReplaceAllString(s, "")
	s = strings.Trim(s, "*_\"'«»()[]<>.,:;!?-–— ")
	s = strings.Join(strings.Fields(s), " ")
	return s, directURL
}

func isValidCandidateName(name string, selfNormTitle string) bool {
	if len(name) < 2 || len(name) > 56 {
		return false
	}
	words := strings.Fields(name)
	if len(words) == 0 || len(words) > 6 {
		return false
	}
	if strings.Contains(name, "http://") || strings.Contains(name, "https://") {
		return false
	}
	norm := domain.NormalizeCompactTitle(name)
	if len(norm) < 2 {
		return false
	}
	if _, stop := stopDependencyNames[norm]; stop {
		return false
	}
	// Purely numeric or version string
	hasLetter := false
	for _, r := range norm {
		if (r >= 'a' && r <= 'z') || (r >= 'а' && r <= 'я') {
			hasLetter = true
			break
		}
	}
	if !hasLetter {
		return false
	}
	if selfNormTitle != "" && (norm == selfNormTitle || (len(selfNormTitle) >= 6 && strings.Contains(norm, selfNormTitle))) {
		return false
	}
	return true
}

func truncateEvidence(s string, maxLen int) string {
	clean := reMDLink.ReplaceAllString(s, "$1")
	clean = strings.Join(strings.Fields(clean), " ")
	runes := []rune(clean)
	if len(runes) <= maxLen {
		return clean
	}
	return string(runes[:maxLen-1]) + "…"
}

func extractNamesFromClause(clause string) []struct{ name, url string } {
	var out []struct{ name, url string }
	// Preserve markdown links before splitting by ", " / " and " / " & "
	placeholders := map[string]string{}
	idx := 0
	protected := reMDLink.ReplaceAllStringFunc(clause, func(m string) string {
		tok := "MHLINK" + strconv.Itoa(idx) + "TOK"
		idx++
		placeholders[tok] = m
		return tok
	})

	parts := reListSplit.Split(protected, -1)
	for _, p := range parts {
		restored := p
		for tok, orig := range placeholders {
			restored = strings.ReplaceAll(restored, tok, orig)
		}
		name, directURL := cleanCandidateModName(restored)
		if name != "" {
			out = append(out, struct{ name, url string }{name: name, url: directURL})
		}
	}
	return out
}

// AnalyzeTextDependencies extracts contextual dependency candidates from project title,
// summary, full description, and version changelog with strict negation handling and confidence scoring.
func AnalyzeTextDependencies(in TextDepSourceInput) []TextDepCandidate {
	selfNorm := domain.NormalizeCompactTitle(in.Title)
	byNorm := map[string]TextDepCandidate{}
	order := []string{}

	addCandidate := func(name, depType, conf, evidence, origin, directURL string) {
		if !isValidCandidateName(name, selfNorm) {
			return
		}
		norm := domain.NormalizeCompactTitle(name)
		cand := TextDepCandidate{
			Name:       name,
			Type:       depType,
			Confidence: conf,
			Evidence:   truncateEvidence(evidence, 150),
			Origin:     origin,
			DirectURL:  directURL,
		}
		if existing, ok := byNorm[norm]; ok {
			betterType := textDepTypePriority(cand.Type) > textDepTypePriority(existing.Type)
			sameTypeBetterConf := textDepTypePriority(cand.Type) == textDepTypePriority(existing.Type) && confidenceRank(cand.Confidence) > confidenceRank(existing.Confidence)
			if betterType || sameTypeBetterConf {
				if cand.DirectURL == "" {
					cand.DirectURL = existing.DirectURL
				}
				byNorm[norm] = cand
			} else if existing.DirectURL == "" && cand.DirectURL != "" {
				existing.DirectURL = cand.DirectURL
				byNorm[norm] = existing
			}
			return
		}
		byNorm[norm] = cand
		order = append(order, norm)
	}

	analyzeBlock := func(raw string, origin string) {
		lines := normalizeMarkupToLines(raw)
		section := "other"
		for _, line := range lines {
			if sec, isHead := detectSectionKind(line); isHead {
				section = sec
				continue
			}
			if section == "credits" {
				continue
			}

			// Split line into sentences for accurate per-clause negation & relation matching
			sentences := reSentenceSplit.Split(line, -1)
			for _, sent := range sentences {
				sent = strings.TrimSpace(sent)
				if len(sent) < 3 {
					continue
				}
				// Strictly reject negated sentences ("does not require X", "не требует X")
				if reNegation.MatchString(sent) {
					continue
				}

				matchedExplicit := false
				// 1. Optional / Recommended / Integration checked first when explicitly worded
				for _, rx := range reOptional {
					if m := rx.FindStringSubmatch(sent); len(m) == 2 {
						matchedExplicit = true
						for _, item := range extractNamesFromClause(m[1]) {
							addCandidate(item.name, "optional", "high", sent, origin, item.url)
						}
					}
				}
				for _, rx := range reRecommended {
					if m := rx.FindStringSubmatch(sent); len(m) == 2 {
						matchedExplicit = true
						for _, item := range extractNamesFromClause(m[1]) {
							addCandidate(item.name, "recommended", "medium", sent, origin, item.url)
						}
					}
				}
				for _, rx := range reIntegration {
					if m := rx.FindStringSubmatch(sent); len(m) == 2 {
						matchedExplicit = true
						for _, item := range extractNamesFromClause(m[1]) {
							addCandidate(item.name, "integration", "medium", sent, origin, item.url)
						}
					}
				}
				if matchedExplicit {
					continue
				}

				// 2. Strong explicit Required dependency triggers
				for _, rx := range reReqExplicit {
					if m := rx.FindStringSubmatch(sent); len(m) == 2 {
						matchedExplicit = true
						conf := "high"
						if origin == "changelog" {
							conf = "medium"
						}
						for _, item := range extractNamesFromClause(m[1]) {
							addCandidate(item.name, "required", conf, sent, origin, item.url)
						}
						break
					}
				}
				if matchedExplicit {
					continue
				}

				// 3. Contextual medium-confidence requirement phrases ("built on top of X", "addon for X")
				for _, rx := range reReqMedium {
					if m := rx.FindStringSubmatch(sent); len(m) == 2 {
						matchedExplicit = true
						for _, item := range extractNamesFromClause(m[1]) {
							addCandidate(item.name, "required", "medium", sent, origin, item.url)
						}
						break
					}
				}
				if matchedExplicit {
					continue
				}

				// 4. Bullet items directly under a dedicated section heading (e.g. ## Requirements -> "- Better Combat")
				if reBulletPrefix.MatchString(line) && (section == "required" || section == "optional" || section == "recommended" || section == "integration") {
					stripped := reBulletPrefix.ReplaceAllString(sent, "")
					conf := "high"
					if section == "recommended" || section == "integration" {
						conf = "medium"
					}
					for _, item := range extractNamesFromClause(stripped) {
						addCandidate(item.name, section, conf, line, origin, item.url)
					}
				}
			}
		}
	}

	analyzeBlock(in.Summary, "summary")
	analyzeBlock(in.Body, "description")
	analyzeBlock(in.Changelog, "changelog")

	// Title-only inference (strictly LOW confidence — NEVER auto-installed)
	cleanTitle := domain.CleanDisplayTitle(in.Title)
	if m := reTitleFor.FindStringSubmatch(cleanTitle); len(m) == 2 {
		name, _ := cleanCandidateModName(m[1])
		addCandidate(name, "integration", "low", "Из названия проекта: "+cleanTitle, "title", "")
	} else if m := reTitleSuffix.FindStringSubmatch(cleanTitle); len(m) == 2 {
		name, _ := cleanCandidateModName(m[1])
		addCandidate(name, "integration", "low", "Из названия проекта: "+cleanTitle, "title", "")
	}

	out := make([]TextDepCandidate, 0, len(order))
	for _, k := range order {
		out = append(out, byNorm[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := textDepTypePriority(out[i].Type), textDepTypePriority(out[j].Type)
		if pi != pj {
			return pi > pj
		}
		ci, cj := confidenceRank(out[i].Confidence), confidenceRank(out[j].Confidence)
		if ci != cj {
			return ci > cj
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func scoreTextDepCandidateCard(wantedName string, card domain.RawProjectCard) int {
	wantNorm := domain.NormalizeCompactTitle(wantedName)
	if len(wantNorm) < 2 {
		return 0
	}
	if card.ContentType != "" && card.ContentType != "mod" && card.ContentType != "datapack" {
		return 0
	}
	titleNorm := domain.NormalizeCompactTitle(card.Title)
	titleNoAcr := domain.NormalizeTitleNoAcronym(card.Title)
	slugNorm := domain.NormalizeCompactTitle(card.Slug)

	score := 0
	switch {
	case titleNorm == wantNorm || titleNoAcr == wantNorm || slugNorm == wantNorm:
		score = 100
	case func() bool {
		for _, alias := range textDepAliases[wantNorm] {
			an := domain.NormalizeCompactTitle(alias)
			if an != "" && (titleNorm == an || slugNorm == an || titleNoAcr == an) {
				return true
			}
		}
		return false
	}():
		score = 96
	case len(wantNorm) >= 5 && (strings.HasPrefix(titleNorm, wantNorm) || strings.HasPrefix(slugNorm, wantNorm)):
		// Avoid confusing "Create" with "Create: Steam 'n' Rails" unless suffix is purely API/Lib
		suffix := strings.TrimPrefix(titleNorm, wantNorm)
		if suffix == "api" || suffix == "lib" || suffix == "library" || suffix == "mod" || suffix == "core" {
			score = 90
		} else {
			score = 62
		}
	case len(wantNorm) >= 6 && (strings.Contains(titleNorm, wantNorm) || strings.Contains(slugNorm, wantNorm)):
		// e.g. "Cataclysm" inside "L_Ender's Cataclysm"
		score = 74
	default:
		return 0
	}

	// Popularity tie-breaker (small bonus so official libraries beat tiny forks/copies)
	if card.Downloads >= 1_000_000 {
		score += 4
	} else if card.Downloads >= 100_000 {
		score += 2
	}
	return score
}

func ComputeTextDepsHash(in TextDepSourceInput, cfConfigured bool) string {
	h := sha256.New()
	_, _ = h.Write([]byte(in.Platform + "|" + in.ProjectID + "|"))
	if cfConfigured {
		_, _ = h.Write([]byte("cf=1|"))
	} else {
		_, _ = h.Write([]byte("cf=0|"))
	}
	_, _ = h.Write([]byte(in.Title + "\x00" + in.Summary + "\x00" + in.Body + "\x00" + in.Changelog))
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// FetchProjectTextMetadata retrieves full description/summary/title from Modrinth or CurseForge
// (using cached netx.HTTPClient calls) when the caller didn't already supply full body text.
func FetchProjectTextMetadata(httpClient *netx.HTTPClient, db *storage.Database, cfKeyRaw, platform, projectID string) TextDepSourceInput {
	in := TextDepSourceInput{
		Platform:  strings.ToLower(strings.TrimSpace(platform)),
		ProjectID: strings.TrimSpace(projectID),
	}
	if in.Platform == "" || in.ProjectID == "" {
		return in
	}
	if db != nil {
		if c, ok := db.GetCatalogItem(in.Platform + ":" + in.ProjectID); ok {
			in.Title = c.Title
			in.Summary = c.Summary
		}
	}
	cfKey := security.SanitizeCFKey(cfKeyRaw)
	if in.Platform == "modrinth" {
		var p catalog.MRFullProject
		if httpClient.GetJSON(domain.ModrinthAPI+"/project/"+in.ProjectID, nil, 300, &p) == nil && p.ID != "" {
			if p.Title != "" {
				in.Title = p.Title
			}
			if p.Description != "" {
				in.Summary = p.Description
			}
			in.Body = p.Body
		}
	} else if in.Platform == "curseforge" && cfKey != "" {
		var descWrap struct {
			Data string `json:"data"`
		}
		var modWrap struct {
			Data catalog.CFMod `json:"data"`
		}
		hdr := map[string]string{"x-api-key": cfKey}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = httpClient.GetJSON(domain.CurseForgeAPI+"/mods/"+in.ProjectID+"/description", hdr, 300, &descWrap)
		}()
		go func() {
			defer wg.Done()
			if in.Title == "" || in.Summary == "" {
				_ = httpClient.GetJSON(domain.CurseForgeAPI+"/mods/"+in.ProjectID, hdr, 300, &modWrap)
			}
		}()
		wg.Wait()
		if modWrap.Data.Name != "" {
			in.Title = modWrap.Data.Name
		}
		if modWrap.Data.Summary != "" {
			in.Summary = modWrap.Data.Summary
		}
		in.Body = descWrap.Data
	}
	return in
}

// ResolveTextDepCandidates maps extracted textual candidates to verified Modrinth/CurseForge projects
// with strict disambiguation (never picking randomly when multiple distinct projects tie),
// and caches the result by SHA-256 hash of the source text.
func ResolveTextDepCandidates(
	httpClient *netx.HTTPClient,
	db *storage.Database,
	cfKeyRaw string,
	in TextDepSourceInput,
) ([]domain.DepTreeNode, []domain.UnresolvedDep) {
	cfKey := security.SanitizeCFKey(cfKeyRaw)
	cacheKey := "text_dep_v1|" + ComputeTextDepsHash(in, cfKey != "")
	if db != nil {
		if cached, ok := db.CacheGet(cacheKey); ok {
			var wrap struct {
				Nodes      []domain.DepTreeNode   `json:"nodes"`
				Unresolved []domain.UnresolvedDep `json:"unresolved"`
			}
			if json.Unmarshal([]byte(cached), &wrap) == nil {
				return wrap.Nodes, wrap.Unresolved
			}
		}
	}

	candidates := AnalyzeTextDependencies(in)
	if len(candidates) == 0 {
		if db != nil {
			if b, err := json.Marshal(map[string]any{"nodes": []domain.DepTreeNode{}, "unresolved": []domain.UnresolvedDep{}}); err == nil {
				db.CacheSet(cacheKey, string(b), 24*3600)
			}
		}
		return []domain.DepTreeNode{}, []domain.UnresolvedDep{}
	}

	// Cap live search candidates to 8 most important entries for predictable latency
	if len(candidates) > 8 {
		candidates = candidates[:8]
	}

	type resolveOut struct {
		node *domain.DepTreeNode
		unr  *domain.UnresolvedDep
	}
	results := make([]resolveOut, len(candidates))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)

	for i, c := range candidates {
		if c.Type == "mention" {
			continue
		}
		wg.Add(1)
		go func(idx int, cand TextDepCandidate) {
			defer wg.Done()
			if !netx.AcquireSemCtx(httpClient.ReqCtx(), sem) {
				return
			}
			defer func() { <-sem }()

			var pool []domain.RawProjectCard
			seenPool := map[string]struct{}{}
			addCard := func(rc domain.RawProjectCard) {
				if rc.ID == "" || (rc.Platform == in.Platform && rc.ID == in.ProjectID) {
					return
				}
				if _, dup := seenPool[rc.SourceKey]; dup {
					return
				}
				seenPool[rc.SourceKey] = struct{}{}
				pool = append(pool, rc)
			}

			// 1. Direct URL inside author's requirement sentence (strongest possible signal)
			if cand.DirectURL != "" {
				if parsed := catalog.ParseProjectURL(cand.DirectURL); parsed != nil {
					if parsed.Platform == "modrinth" {
						if card, err := catalog.FetchModrinthProjectBySlug(httpClient, parsed.Slug); err == nil && card != nil {
							addCard(*card)
						}
					} else if parsed.Platform == "curseforge" && cfKey != "" {
						if card := catalog.FetchCFExactSlug(httpClient, cfKey, parsed.Slug, parsed.ContentType); card != nil {
							addCard(*card)
						}
					}
				}
			}

			wantNorm := domain.NormalizeCompactTitle(cand.Name)
			slugCands := []string{strings.ToLower(strings.ReplaceAll(cand.Name, " ", "-"))}
			searchTerms := []string{cand.Name}
			if hints, ok := textDepAliases[wantNorm]; ok {
				for _, h := range hints {
					if !strings.Contains(h, " ") {
						slugCands = domain.AppendUniqueCI(slugCands, strings.ToLower(h))
					} else {
						searchTerms = domain.AppendUniqueCI(searchTerms, h)
					}
				}
			}

			// 2. Exact slug lookups + targeted search (only if direct URL didn't already give an exact match)
			if len(pool) == 0 {
				// Prefer same platform first, then sibling platform
				tryMR := func() {
					for _, sv := range slugCands {
						if card, err := catalog.FetchModrinthProjectBySlug(httpClient, sv); err == nil && card != nil {
							addCard(*card)
						}
					}
					for _, q := range searchTerms {
						if hits, _, err := catalog.SearchModrinth(httpClient, q, "mod", "", nil, nil, nil, nil, "and", "", "downloads", 5, 0); err == nil {
							for _, h := range hits {
								addCard(h)
							}
						}
					}
				}
				tryCF := func() {
					if cfKey == "" {
						return
					}
					for _, sv := range slugCands {
						if card := catalog.FetchCFExactSlug(httpClient, cfKey, sv, "mod"); card != nil {
							addCard(*card)
						}
					}
					for _, q := range searchTerms {
						if hits, _, err := catalog.SearchCurseForge(httpClient, cfKey, q, "mod", "", "", "downloads", 5, 0); err == nil {
							for _, h := range hits {
								addCard(h)
							}
						}
					}
				}
				if in.Platform == "curseforge" && cfKey != "" {
					tryCF()
					if len(pool) == 0 {
						tryMR()
					}
				} else {
					tryMR()
					if len(pool) == 0 {
						tryCF()
					}
				}
				// 3. Local SQLite catalog fallback
				if len(pool) == 0 && db != nil {
					for _, q := range searchTerms {
						for _, sk := range db.FindSourceKeysByQuery(q, 5) {
							if c, ok := db.GetCatalogItem(sk); ok {
								addCard(*c)
							}
						}
					}
				}
			}

			type scoredCard struct {
				card  domain.RawProjectCard
				score int
			}
			var scored []scoredCard
			for _, rc := range pool {
				sc := scoreTextDepCandidateCard(cand.Name, rc)
				if cand.DirectURL != "" && sc == 0 {
					sc = 95
				}
				if sc >= 72 {
					scored = append(scored, scoredCard{card: rc, score: sc})
				}
			}
			sort.SliceStable(scored, func(a, b int) bool {
				if scored[a].score != scored[b].score {
					return scored[a].score > scored[b].score
				}
				if scored[a].card.Platform == in.Platform && scored[b].card.Platform != in.Platform {
					return true
				}
				return scored[a].card.Downloads > scored[b].card.Downloads
			})

			if len(scored) == 0 {
				if cand.Type == "required" && cand.Confidence != "low" {
					results[idx].unr = &domain.UnresolvedDep{
						ModID:  cand.Name,
						Type:   cand.Type,
						Source: "text",
						Reason: "Упомянуто в описании (" + cand.Evidence + "), но однозначный проект не найден",
					}
				}
				return
			}

			// Disambiguation guard: if top-2 are DIFFERENT mods (different normalized title & slug) with close scores
			// and neither is an exact 100 match, refuse to guess randomly!
			if len(scored) >= 2 && scored[0].score < 98 {
				t0 := domain.NormalizeCompactTitle(scored[0].card.Title)
				t1 := domain.NormalizeCompactTitle(scored[1].card.Title)
				s0 := domain.NormalizeCompactTitle(scored[0].card.Slug)
				s1 := domain.NormalizeCompactTitle(scored[1].card.Slug)
				sameProject := t0 == t1 || s0 == s1
				if !sameProject && scored[0].score-scored[1].score <= 5 && scored[0].card.Downloads < scored[1].card.Downloads*5 {
					if cand.Type == "required" {
						results[idx].unr = &domain.UnresolvedDep{
							ModID:  cand.Name,
							Type:   cand.Type,
							Source: "text",
							Reason: "Неоднозначное совпадение в каталоге: «" + scored[0].card.Title + "» или «" + scored[1].card.Title + "»",
						}
					}
					return
				}
			}

			best := scored[0].card
			ct := best.ContentType
			if !domain.IsSupportedCatalogType(ct) {
				ct = "mod"
			}
			results[idx].node = &domain.DepTreeNode{
				Platform:    best.Platform,
				ProjectID:   best.ID,
				SourceKey:   best.SourceKey,
				Type:        cand.Type,
				Title:       best.Title,
				Slug:        best.Slug,
				IconURL:     best.IconURL,
				URL:         best.URL,
				Folder:      domain.GetContentTypeMeta(ct).Folder,
				ContentType: ct,
				Source:      "text",
				Confidence:  cand.Confidence,
				Evidence:    cand.Evidence,
				TextOrigin:  cand.Origin,
				Resolved:    true,
				Children:    []domain.DepTreeNode{},
			}
		}(i, c)
	}
	wg.Wait()

	nodes := make([]domain.DepTreeNode, 0, len(candidates))
	unresolved := make([]domain.UnresolvedDep, 0)
	seenSK := map[string]struct{}{}
	for _, r := range results {
		if r.node != nil {
			if _, dup := seenSK[r.node.SourceKey]; !dup {
				seenSK[r.node.SourceKey] = struct{}{}
				nodes = append(nodes, *r.node)
			}
		}
		if r.unr != nil {
			unresolved = append(unresolved, *r.unr)
		}
	}

	if db != nil {
		if b, err := json.Marshal(map[string]any{"nodes": nodes, "unresolved": unresolved}); err == nil {
			db.CacheSet(cacheKey, string(b), 24*3600)
		}
	}
	return nodes, unresolved
}
