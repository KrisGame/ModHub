package catalog

import (
	"context"
	"strings"
	"sync"
	"time"

	"modhub/internal/domain"
	"modhub/internal/netx"
	"modhub/internal/security"
	"modhub/internal/storage"
)

const negSiblingTTLSecs int64 = 4 * 3600

func negSiblingKey(plat, ct, normTitle string) string {
	if normTitle == "" {
		return ""
	}
	return "neg_sib::" + strings.ToLower(plat) + "::" + strings.ToLower(ct) + "::" + normTitle
}

func cardHasPlatform(item domain.ProjectCard, plat string) bool {
	for _, s := range item.Sources {
		if strings.EqualFold(s.Platform, plat) && s.ID != "" {
			return true
		}
	}
	return false
}

func cardToRawCards(item domain.ProjectCard) []domain.RawProjectCard {
	out := make([]domain.RawProjectCard, 0, len(item.Sources))
	ct := item.ContentType
	if ct == "" {
		ct = "mod"
	}
	for _, s := range item.Sources {
		if s.Platform == "" || s.ID == "" {
			continue
		}
		authors := s.Authors
		if len(authors) == 0 {
			authors = item.Authors
		}
		loaders := s.Loaders
		if len(loaders) == 0 {
			loaders = item.Loaders
		}
		title := s.Title
		if title == "" {
			title = item.Title
		}
		out = append(out, domain.RawProjectCard{
			Platform:     s.Platform,
			SourceKey:    s.SourceKey,
			ID:           s.ID,
			Slug:         s.Slug,
			ContentType:  ct,
			Title:        title,
			Summary:      item.Summary,
			Authors:      authors,
			IconURL:      item.IconURL,
			Downloads:    s.Downloads,
			DateModified: item.DateModified,
			DateCreated:  item.DateCreated,
			License:      item.License,
			Status:       item.Status,
			ClientSide:   item.ClientSide,
			ServerSide:   item.ServerSide,
			Loaders:      loaders,
			GameVersions: item.GameVersions,
			Categories:   item.Categories,
			URL:          s.URL,
			Links:        item.Links,
		})
	}
	return out
}

func AuthorsOverlap(a1, a2 []string) bool {
	if len(a1) == 0 || len(a2) == 0 {
		return false
	}
	set := map[string]struct{}{}
	for _, a := range a1 {
		n := domain.NormalizeCompactTitle(a)
		if len(n) >= 3 {
			set[n] = struct{}{}
		}
	}
	for _, a := range a2 {
		n := domain.NormalizeCompactTitle(a)
		if _, ok := set[n]; ok {
			return true
		}
	}
	return false
}

func candidateSlugVariants(item domain.ProjectCard) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if len(s) < 2 {
			return
		}
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	add(item.Slug)
	for _, src := range item.Sources {
		add(src.Slug)
	}
	cleanWords := strings.Fields(strings.ToLower(domain.CleanDisplayTitle(item.Title)))
	if len(cleanWords) > 0 {
		var cleanTokens []string
		for _, w := range cleanWords {
			t := domain.NormalizeCompactTitle(w)
			if t != "" {
				cleanTokens = append(cleanTokens, t)
			}
		}
		if len(cleanTokens) > 0 {
			add(strings.Join(cleanTokens, "-"))
			add(strings.Join(cleanTokens, ""))
			if len(cleanTokens) >= 2 && len(cleanTokens[0]) >= 4 {
				add(cleanTokens[0])
			}
		}
	}
	return out
}

func scoreSiblingCandidate(base domain.ProjectCard, cand domain.RawProjectCard) int {
	if base.ContentType != "" && cand.ContentType != "" && !strings.EqualFold(base.ContentType, cand.ContentType) {
		return 0
	}
	baseRepo := domain.ExtractNormalizedRepo(base.Links)
	candRepo := domain.ExtractNormalizedRepo(cand.Links)
	if baseRepo != "" && candRepo != "" && baseRepo == candRepo {
		return 100
	}

	baseTitleComp := domain.NormalizeCompactTitle(base.Title)
	candTitleComp := domain.NormalizeCompactTitle(cand.Title)
	baseNoAcr := domain.NormalizeTitleNoAcronym(base.Title)
	candNoAcr := domain.NormalizeTitleNoAcronym(cand.Title)
	baseSlugComp := domain.NormalizeCompactTitle(base.Slug)
	candSlugComp := domain.NormalizeCompactTitle(cand.Slug)

	if baseTitleComp == "" || candTitleComp == "" {
		return 0
	}

	authMatch := AuthorsOverlap(base.Authors, cand.Authors)
	slugExact := baseSlugComp != "" && candSlugComp != "" && baseSlugComp == candSlugComp
	slugContains := baseSlugComp != "" && candSlugComp != "" && len(baseSlugComp) >= 4 && len(candSlugComp) >= 4 &&
		(strings.Contains(candSlugComp, baseSlugComp) || strings.Contains(baseSlugComp, candSlugComp))

	score := 0
	switch {
	case baseTitleComp == candTitleComp:
		score += 82
	case baseNoAcr != "" && candNoAcr != "" && baseNoAcr == candNoAcr:
		score += 78
	case baseSlugComp != "" && candTitleComp == baseSlugComp || candSlugComp != "" && baseTitleComp == candSlugComp:
		score += 72
	default:
		sim := StrSimilarity(baseTitleComp, candTitleComp)
		if sim >= 0.88 {
			score += 68
		} else if sim >= 0.76 && (authMatch || slugExact || slugContains) {
			score += 60
		} else if (len(baseTitleComp) >= 4 && strings.Contains(candTitleComp, baseTitleComp)) ||
			(len(candTitleComp) >= 4 && strings.Contains(baseTitleComp, candTitleComp)) {
			if authMatch || slugExact || slugContains {
				score += 58
			}
		}
	}

	if slugExact {
		score += 28
	} else if slugContains {
		score += 16
	}
	if authMatch {
		score += 24
	}
	return score
}

// DiscoverAndMergeCard finds cross-platform twins (Modrinth <-> CurseForge) using:
// 1. Instant SQLite catalog cache
// 2. Negative lookup cache (4h TTL via lookup_log) to skip repeated misses
// 3. Context-aware relevance-sorted live lookup + slug variants
func DiscoverAndMergeCard(
	httpClient *netx.HTTPClient,
	db *storage.Database,
	cfKeyRaw string,
	item domain.ProjectCard,
	allowedPlatforms []string,
) (domain.ProjectCard, bool) {
	if item.Title == "" || len(item.Sources) == 0 {
		return item, false
	}
	ct := item.ContentType
	if ct == "" {
		ct = "mod"
	}
	if ct == "modpack" {
		return item, false
	}

	cfKey := security.SanitizeCFKey(cfKeyRaw)
	allowed := map[string]bool{"modrinth": true, "curseforge": true}
	if len(allowedPlatforms) > 0 {
		allowed = map[string]bool{}
		for _, p := range allowedPlatforms {
			allowed[strings.ToLower(p)] = true
		}
	}

	needMR := allowed["modrinth"] && !cardHasPlatform(item, "modrinth")
	needCF := allowed["curseforge"] && cfKey != "" && !cardHasPlatform(item, "curseforge")
	if !needMR && !needCF {
		return item, false
	}

	normTitle := domain.NormalizeCompactTitle(item.Title)
	cleanTitle := domain.CleanDisplayTitle(item.Title)
	slugVariants := candidateSlugVariants(item)
	var discovered []domain.RawProjectCard
	var mu sync.Mutex

	// 1. Instant SQLite catalog sibling lookup first
	if normTitle != "" && db != nil {
		for _, sib := range db.FindCatalogSiblings(ct, normTitle) {
			if sib.Platform == "modrinth" && needMR {
				discovered = append(discovered, sib)
				needMR = false
			} else if sib.Platform == "curseforge" && needCF {
				discovered = append(discovered, sib)
				needCF = false
			}
		}
	}

	// 2. Negative lookup cache check (skip live calls for known missing twins)
	negMRKey := negSiblingKey("modrinth", ct, normTitle)
	negCFKey := negSiblingKey("curseforge", ct, normTitle)
	if db != nil {
		if needMR && db.IsNegLookupCached(negMRKey, negSiblingTTLSecs) {
			needMR = false
		}
		if needCF && db.IsNegLookupCached(negCFKey, negSiblingTTLSecs) {
			needCF = false
		}
	}

	ctx := httpClient.ReqCtx()

	// 3. Live cross-platform search for still-needed platforms
	if (needMR || needCF) && ctx.Err() == nil {
		var wg sync.WaitGroup
		if needMR {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var cands []domain.RawProjectCard
				var searchErr error
				for idx, sv := range slugVariants {
					if idx >= 2 || ctx.Err() != nil {
						break
					}
					if exact, err := FetchModrinthProjectBySlug(httpClient, sv); err == nil && exact != nil && exact.ContentType == ct {
						cands = append(cands, *exact)
						if scoreSiblingCandidate(item, *exact) >= 80 {
							break
						}
					}
				}
				if ctx.Err() == nil {
					hits, _, err := SearchModrinth(httpClient, cleanTitle, ct, "", nil, nil, nil, nil, "and", "", "relevance", 10, 0)
					searchErr = err
					if err == nil {
						cands = append(cands, hits...)
					}
				}
				var best *domain.RawProjectCard
				bestScore := 0
				for i := range cands {
					sc := scoreSiblingCandidate(item, cands[i])
					if sc >= 70 && (sc > bestScore || (sc == bestScore && best != nil && cands[i].Downloads > best.Downloads)) {
						bestScore = sc
						cp := cands[i]
						best = &cp
					}
				}
				if best != nil {
					mu.Lock()
					discovered = append(discovered, *best)
					mu.Unlock()
				} else if db != nil && searchErr == nil && ctx.Err() == nil {
					db.MarkNegLookupAsync(negMRKey, negSiblingTTLSecs)
				}
			}()
		}

		if needCF {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var cands []domain.RawProjectCard
				var searchErr error
				for idx, sv := range slugVariants {
					if idx >= 2 || ctx.Err() != nil {
						break
					}
					if exact := FetchCFExactSlug(httpClient, cfKey, sv, ct); exact != nil {
						cands = append(cands, *exact)
						if scoreSiblingCandidate(item, *exact) >= 80 {
							break
						}
					}
				}
				if ctx.Err() == nil {
					hits, _, err := SearchCurseForge(httpClient, cfKey, cleanTitle, ct, "", "", "relevance", 10, 0)
					searchErr = err
					if err == nil {
						cands = append(cands, hits...)
					}
				}
				var best *domain.RawProjectCard
				bestScore := 0
				var validMatches []domain.RawProjectCard
				for i := range cands {
					sc := scoreSiblingCandidate(item, cands[i])
					if sc >= 70 {
						validMatches = append(validMatches, cands[i])
						if sc > bestScore || (sc == bestScore && best != nil && cands[i].Downloads > best.Downloads) {
							bestScore = sc
							cp := cands[i]
							best = &cp
						}
					}
				}
				if best != nil {
					mu.Lock()
					discovered = append(discovered, *best)
					for _, vm := range validMatches {
						if vm.SourceKey != best.SourceKey && domain.LoadersDisjoint(best.Loaders, vm.Loaders) && AuthorsOverlap(best.Authors, vm.Authors) {
							discovered = append(discovered, vm)
							break
						}
					}
					mu.Unlock()
				} else if db != nil && searchErr == nil && ctx.Err() == nil {
					db.MarkNegLookupAsync(negCFKey, negSiblingTTLSecs)
				}
			}()
		}
		wg.Wait()
	}

	if len(discovered) == 0 {
		return item, false
	}
	if db != nil {
		db.UpsertCatalogItemsAsync(discovered)
	}

	allRaw := cardToRawCards(item)
	allRaw = append(allRaw, discovered...)
	merged := forceMergeIntoSingleCard(allRaw, item)
	return merged, true
}

func forceMergeIntoSingleCard(rawItems []domain.RawProjectCard, fallback domain.ProjectCard) domain.ProjectCard {
	if len(rawItems) == 0 {
		return fallback
	}
	res := MergeItems(rawItems, true)
	if len(res) == 1 {
		return res[0]
	}
	seenSK := map[string]struct{}{}
	var combined []domain.RawProjectCard
	for _, it := range rawItems {
		if fallback.ContentType != "" && it.ContentType != "" && !strings.EqualFold(it.ContentType, fallback.ContentType) {
			continue
		}
		if _, dup := seenSK[it.SourceKey]; dup {
			continue
		}
		seenSK[it.SourceKey] = struct{}{}
		it.Title = domain.CleanDisplayTitle(fallback.Title)
		combined = append(combined, it)
	}
	res2 := MergeItems(combined, true)
	if len(res2) > 0 {
		return res2[0]
	}
	return fallback
}

// CrossEnrichVersionDependencies merges declared dependencies ONLY from the OTHER platform
// when MC version and loader match (or when a single-loader file on one platform omitted loader tags).
// Never copies across incompatible loaders (e.g. Fabric -> NeoForge).
func CrossEnrichVersionDependencies(versions []domain.VersionFile) []domain.VersionFile {
	if len(versions) < 2 {
		return versions
	}
	type depDonor struct {
		platform string
		loaders  []string
		deps     []domain.DependencyRef
	}
	donorsByMcLoader := map[string][]depDonor{}
	donorsByMc := map[string][]depDonor{}

	hasRequiredDep := func(list []domain.DependencyRef) bool {
		for _, d := range list {
			if domain.NormalizeDepType(d.Type) == "required" {
				return true
			}
		}
		return false
	}

	for _, v := range versions {
		if len(v.Dependencies) == 0 {
			continue
		}
		hasUseful := false
		for _, d := range v.Dependencies {
			if domain.NormalizeDepType(d.Type) != "" {
				hasUseful = true
				break
			}
		}
		if !hasUseful {
			continue
		}
		d := depDonor{platform: v.Platform, loaders: v.Loaders, deps: v.Dependencies}
		for _, gv := range v.GameVersions {
			donorsByMc[gv] = append(donorsByMc[gv], d)
			for _, ld := range v.Loaders {
				k := gv + "|" + strings.ToLower(ld)
				donorsByMcLoader[k] = append(donorsByMcLoader[k], d)
			}
		}
	}

	if len(donorsByMc) == 0 {
		return versions
	}

	// Count distinct known loaders across all versions to detect single-loader mods safely
	distinctLoaders := map[string]struct{}{}
	for _, v := range versions {
		for _, ld := range v.Loaders {
			low := strings.ToLower(ld)
			if domain.IsKnownLoader(low) {
				distinctLoaders[low] = struct{}{}
			}
		}
	}
	isSingleLoaderProject := len(distinctLoaders) <= 1

	findSiblingDeps := func(list []depDonor, selfPlat string, selfLoaders []string) []domain.DependencyRef {
		for _, d := range list {
			if d.platform == selfPlat {
				continue
			}
			// If both sides declare loaders, they MUST overlap (never cross-pollinate Fabric <-> NeoForge)
			if len(selfLoaders) > 0 && len(d.loaders) > 0 && domain.LoadersDisjoint(selfLoaders, d.loaders) {
				continue
			}
			// If one side has no loaders, only inherit when both have no loaders OR project is single-loader
			if (len(selfLoaders) == 0) != (len(d.loaders) == 0) && !isSingleLoaderProject {
				continue
			}
			return d.deps
		}
		return nil
	}

	for i := range versions {
		// Skip if this version already declares required dependencies
		if hasRequiredDep(versions[i].Dependencies) {
			continue
		}
		var inherited []domain.DependencyRef
		if len(versions[i].Loaders) > 0 {
			for _, gv := range versions[i].GameVersions {
				for _, ld := range versions[i].Loaders {
					if deps := findSiblingDeps(donorsByMcLoader[gv+"|"+strings.ToLower(ld)], versions[i].Platform, versions[i].Loaders); len(deps) > 0 {
						inherited = deps
						break
					}
				}
				if len(inherited) > 0 {
					break
				}
			}
		}
		if len(inherited) == 0 {
			for _, gv := range versions[i].GameVersions {
				if deps := findSiblingDeps(donorsByMc[gv], versions[i].Platform, versions[i].Loaders); len(deps) > 0 {
					inherited = deps
					break
				}
			}
		}
		if len(inherited) > 0 {
			if len(versions[i].Dependencies) == 0 {
				cp := make([]domain.DependencyRef, len(inherited))
				copy(cp, inherited)
				versions[i].Dependencies = cp
			} else {
				combined := make([]domain.DependencyRef, 0, len(versions[i].Dependencies)+len(inherited))
				combined = append(combined, versions[i].Dependencies...)
				combined = append(combined, inherited...)
				versions[i].Dependencies = domain.DedupeDependencyRefs(combined, versions[i].ModID)
			}
		}
	}
	return versions
}

// EnrichSearchBatchMerged pre-discovers & merges cross-platform twins BEFORE returning search results:
// 1. Instant SQLite sibling lookup across all cards
// 2. Single batch GET /v2/projects?ids=[...] for all cards missing Modrinth
// 3. Bounded concurrent live lookup under a strict 1.4s overall timeout budget

// EnrichSearchBatchLocalOnly performs instant O(N) local SQLite sibling enrichment (ZERO network I/O)
// so search cards render immediately without blocking on 1.4s live cross-platform lookups.
func EnrichSearchBatchLocalOnly(
	db *storage.Database,
	cards []domain.ProjectCard,
	allowedPlatforms []string,
) []domain.ProjectCard {
	if len(cards) == 0 || db == nil {
		return cards
	}
	allowMR, allowCF := false, false
	for _, p := range allowedPlatforms {
		switch strings.ToLower(p) {
		case "modrinth":
			allowMR = true
		case "curseforge":
			allowCF = true
		}
	}
	if !allowMR || !allowCF {
		return cards
	}

	out := make([]domain.ProjectCard, len(cards))
	copy(out, cards)

	for i := range out {
		if len(out[i].Platforms) >= 2 || out[i].ContentType == "modpack" {
			continue
		}
		ct := out[i].ContentType
		if ct == "" {
			ct = "mod"
		}
		nt := domain.NormalizeCompactTitle(out[i].Title)
		if nt == "" {
			continue
		}
		needMR := !cardHasPlatform(out[i], "modrinth")
		needCF := !cardHasPlatform(out[i], "curseforge")
		var found []domain.RawProjectCard
		for _, sib := range db.FindCatalogSiblings(ct, nt) {
			if sib.Platform == "modrinth" && needMR && scoreSiblingCandidate(out[i], sib) >= 75 {
				found = append(found, sib)
				needMR = false
			} else if sib.Platform == "curseforge" && needCF && scoreSiblingCandidate(out[i], sib) >= 75 {
				found = append(found, sib)
				needCF = false
			}
		}
		if len(found) > 0 {
			allRaw := append(cardToRawCards(out[i]), found...)
			out[i] = forceMergeIntoSingleCard(allRaw, out[i])
		}
	}
	return out
}

func EnrichSearchBatchMerged(
	httpClient *netx.HTTPClient,
	db *storage.Database,
	cfKeyRaw string,
	cards []domain.ProjectCard,
	allowedPlatforms []string,
) []domain.ProjectCard {
	if len(cards) == 0 {
		return cards
	}
	cfKey := security.SanitizeCFKey(cfKeyRaw)
	allowMR, allowCF := false, false
	for _, p := range allowedPlatforms {
		switch strings.ToLower(p) {
		case "modrinth":
			allowMR = true
		case "curseforge":
			allowCF = cfKey != ""
		}
	}
	if !allowMR || !allowCF {
		return cards
	}

	out := make([]domain.ProjectCard, len(cards))
	copy(out, cards)

	// Phase 1: Instant SQLite catalog sibling merge (0 network I/O)
	if db != nil {
		for i := range out {
			if len(out[i].Platforms) >= 2 || out[i].ContentType == "modpack" {
				continue
			}
			ct := out[i].ContentType
			if ct == "" {
				ct = "mod"
			}
			nt := domain.NormalizeCompactTitle(out[i].Title)
			if nt == "" {
				continue
			}
			needMR := !cardHasPlatform(out[i], "modrinth")
			needCF := !cardHasPlatform(out[i], "curseforge")
			var found []domain.RawProjectCard
			for _, sib := range db.FindCatalogSiblings(ct, nt) {
				if sib.Platform == "modrinth" && needMR {
					found = append(found, sib)
					needMR = false
				} else if sib.Platform == "curseforge" && needCF {
					found = append(found, sib)
					needCF = false
				}
			}
			if len(found) > 0 {
				allRaw := append(cardToRawCards(out[i]), found...)
				out[i] = forceMergeIntoSingleCard(allRaw, out[i])
			}
		}
	}

	// Enforce a strict 1.4s total enrichment budget so no single slow API call delays the catalog
	batchCtx, cancel := context.WithTimeout(httpClient.ReqCtx(), 1400*time.Millisecond)
	defer cancel()
	batchClient := httpClient.WithContext(batchCtx)

	// Phase 2: Single batch Modrinth /v2/projects?ids=[...] for all cards still missing Modrinth
	var batchSlugs []string
	var mrCandidateIdxs []int
	for i := range out {
		if len(out[i].Platforms) >= 2 || out[i].ContentType == "modpack" {
			continue
		}
		if cardHasPlatform(out[i], "modrinth") {
			continue
		}
		ct := out[i].ContentType
		if ct == "" {
			ct = "mod"
		}
		nt := domain.NormalizeCompactTitle(out[i].Title)
		if db != nil && db.IsNegLookupCached(negSiblingKey("modrinth", ct, nt), negSiblingTTLSecs) {
			continue
		}
		mrCandidateIdxs = append(mrCandidateIdxs, i)
		for idx, sv := range candidateSlugVariants(out[i]) {
			if idx >= 2 {
				break
			}
			batchSlugs = append(batchSlugs, sv)
		}
	}

	if len(batchSlugs) > 0 && batchCtx.Err() == nil {
		mrBatch := FetchModrinthProjectsBatch(batchClient, batchSlugs)
		if len(mrBatch) > 0 {
			if db != nil {
				db.UpsertCatalogItemsAsync(mrBatch)
			}
			for _, idx := range mrCandidateIdxs {
				ct := out[idx].ContentType
				if ct == "" {
					ct = "mod"
				}
				var best *domain.RawProjectCard
				bestScore := 0
				for j := range mrBatch {
					if mrBatch[j].ContentType != ct {
						continue
					}
					sc := scoreSiblingCandidate(out[idx], mrBatch[j])
					if sc >= 70 && (sc > bestScore || (sc == bestScore && best != nil && mrBatch[j].Downloads > best.Downloads)) {
						bestScore = sc
						cp := mrBatch[j]
						best = &cp
					}
				}
				if best != nil {
					allRaw := append(cardToRawCards(out[idx]), *best)
					out[idx] = forceMergeIntoSingleCard(allRaw, out[idx])
				}
			}
		}
	}

	// Phase 3: Bounded concurrent fallback for remaining unmatched cards within remaining 1.4s budget
	if batchCtx.Err() == nil {
		sem := make(chan struct{}, 6)
		var wg sync.WaitGroup
		for i := range out {
			if len(out[i].Platforms) >= 2 || out[i].ContentType == "modpack" {
				continue
			}
			if batchCtx.Err() != nil {
				break
			}
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				if !netx.AcquireSemCtx(batchCtx, sem) {
					return
				}
				defer func() { <-sem }()
				if merged, changed := DiscoverAndMergeCard(batchClient, db, cfKey, out[idx], allowedPlatforms); changed {
					out[idx] = merged
				}
			}(i)
		}
		wg.Wait()
	}

	// Deduplicate if two cards merged into the same cross-platform group
	dedup := make([]domain.ProjectCard, 0, len(out))
	seenGID := map[string]struct{}{}
	seenSK := map[string]struct{}{}
	for _, c := range out {
		if _, dup := seenGID[c.GroupID]; dup {
			continue
		}
		dupSrc := false
		for _, s := range c.Sources {
			if s.SourceKey != "" {
				if _, ok := seenSK[s.SourceKey]; ok {
					dupSrc = true
					break
				}
			}
		}
		if dupSrc {
			continue
		}
		seenGID[c.GroupID] = struct{}{}
		for _, s := range c.Sources {
			if s.SourceKey != "" {
				seenSK[s.SourceKey] = struct{}{}
			}
		}
		dedup = append(dedup, c)
	}
	return dedup
}
