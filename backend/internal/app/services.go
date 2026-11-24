package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"modhub/internal/catalog"
	"modhub/internal/deps"
	"modhub/internal/domain"
	"modhub/internal/security"
)

func HumanizeSourceError(platform string, err error) string {
	if err == nil {
		return ""
	}
	name := "Modrinth"
	if strings.EqualFold(platform, "curseforge") {
		name = "CurseForge"
	}
	low := strings.ToLower(err.Error())
	switch {
	case strings.Contains(low, "401") || strings.Contains(low, "403") || strings.Contains(low, "unauthorized") || strings.Contains(low, "forbidden"):
		if name == "CurseForge" {
			return "API-ключ CurseForge недействителен или отклонён сервером"
		}
		return name + " временно отклонил запрос доступа"
	case strings.Contains(low, "429") || strings.Contains(low, "rate limit"):
		return name + " временно ограничил частоту запросов — попробуйте повторить через пару секунд"
	case strings.Contains(low, "timeout") || strings.Contains(low, "deadline exceeded"):
		return name + " не ответил вовремя — возможно, перегрузка или медленное соединение"
	case strings.Contains(low, "500") || strings.Contains(low, "502") || strings.Contains(low, "503") || strings.Contains(low, "504"):
		return "Серверы " + name + " временно недоступны"
	case strings.Contains(low, "proxy") || strings.Contains(low, "connection refused") || strings.Contains(low, "no such host") || strings.Contains(low, "eof") || strings.Contains(low, "reset by peer"):
		return "Не удалось подключиться к " + name + " — проверьте сеть или настройки прокси"
	default:
		return name + " временно недоступен — показаны результаты из доступных источников"
	}
}

func (c *Container) GetMCVersions() map[string]any {
	vers := catalog.FetchMCVersions(c.HTTP)
	if len(vers) > 75 {
		vers = vers[:75]
	}
	return map[string]any{
		"ok":        true,
		"versions":  vers,
		"by_loader": domain.BuildLoaderVersionMatrix(vers),
	}
}

func (c *Container) GetFilters(ct string) map[string]any {
	if ct != "mod" && ct != "resourcepack" && ct != "shader" && ct != "datapack" {
		ct = "mod"
	}
	return map[string]any{
		"ok":         true,
		"type":       ct,
		"categories": domain.BuildFiltersCategories(ct),
	}
}

func (c *Container) CheckInstalledHashes(ctx context.Context, hashes []string, mcVer, loader string) (map[string]any, error) {
	if len(hashes) > 600 {
		return nil, fmt.Errorf("%w: Слишком много хэшей за один запрос (максимум 600)", ErrValidation)
	}
	matched := catalog.LookupInstalledHashesModrinth(c.HTTP.WithContext(ctx), hashes, mcVer, loader)
	return map[string]any{
		"ok":            true,
		"installed":     matched,
		"matched_count": len(matched),
	}, nil
}

func (c *Container) Search(ctx context.Context, rawQueryKey string, params domain.SearchParams) ([]byte, bool, error) {
	cfg := c.GetConfig()
	cfKey := security.SanitizeCFKey(cfg.CurseForgeAPIKey)
	searchCacheKey := fmt.Sprintf("v19|cf=%t|%s", cfKey != "", rawQueryKey)
	if cachedJSON, ok := c.SearchCache.Get(searchCacheKey); ok {
		return cachedJSON, true, nil
	}

	hc := c.HTTP.WithContext(ctx)
	ct := params.ContentType
	if ct == "" {
		ct = "mod"
	}
	if params.Sort == "" {
		params.Sort = "downloads"
	}
	if params.CatMode == "" {
		params.CatMode = "and"
	}
	if params.Page < 1 {
		params.Page = 1
	}
	if params.Limit < 6 || params.Limit > 48 {
		params.Limit = 24
	}

	srcSet := map[string]bool{"modrinth": true, "curseforge": true}
	if len(params.Sources) > 0 {
		srcSet = map[string]bool{}
		for _, p := range params.Sources {
			srcSet[strings.ToLower(p)] = true
		}
	}
	useMR := srcSet["modrinth"]
	useCF := srcSet["curseforge"] && cfKey != ""
	cfStatus := "disabled"
	if srcSet["curseforge"] {
		if cfKey != "" {
			cfStatus = "ok"
		} else {
			cfStatus = "no_key"
		}
	}

	parsed := catalog.ParseSearchQuery(params.Query)
	effMC := params.Version
	if parsed.Prefixes.MCVersion != "" {
		effMC = parsed.Prefixes.MCVersion
	}
	for _, l := range parsed.Prefixes.LoadersIn {
		params.LoadersIn = domain.AppendUniqueCI(params.LoadersIn, l)
	}
	for _, l := range parsed.Prefixes.LoadersOut {
		params.LoadersOut = domain.AppendUniqueCI(params.LoadersOut, l)
	}
	for _, cat := range parsed.Prefixes.CategoriesIn {
		params.CategoriesIn = domain.AppendUniqueCI(params.CategoriesIn, cat)
	}
	for _, cat := range parsed.Prefixes.CategoriesOut {
		params.CategoriesOut = domain.AppendUniqueCI(params.CategoriesOut, cat)
	}
	if parsed.Prefixes.Env != "" {
		params.Env = parsed.Prefixes.Env
	}
	if len(parsed.Prefixes.Platforms) > 0 {
		pAllow := map[string]bool{}
		for _, p := range parsed.Prefixes.Platforms {
			pAllow[strings.ToLower(p)] = true
		}
		useMR = useMR && pAllow["modrinth"]
		useCF = useCF && pAllow["curseforge"]
	}

	if len(parsed.Prefixes.Deps) > 0 {
		var baseCards []domain.ProjectCard
		for _, term := range parsed.Prefixes.Deps {
			exp := catalog.ExpandSmartQuery(term, nil)
			qEff := term
			if exp.CanonicalQuery != "" {
				qEff = exp.CanonicalQuery
			}
			var candRaw []domain.RawProjectCard
			for _, k := range c.DB.FindSourceKeysByQuery(qEff, 6) {
				if item, ok := c.DB.GetCatalogItem(k); ok && item.ContentType == "mod" {
					candRaw = append(candRaw, *item)
				}
			}
			if len(candRaw) == 0 {
				if useMR {
					if mrHits, _, err := catalog.SearchModrinth(hc, qEff, "mod", "", nil, nil, nil, nil, "and", "", "downloads", 6, 0); err == nil {
						candRaw = append(candRaw, mrHits...)
					}
				}
				if useCF {
					if cfHits, _, err := catalog.SearchCurseForge(hc, cfKey, qEff, "mod", "", "", "downloads", 6, 0); err == nil {
						candRaw = append(candRaw, cfHits...)
					}
				}
				if len(candRaw) > 0 {
					c.DB.UpsertCatalogItemsAsync(candRaw)
				}
			}
			mergedCands := catalog.MergeItems(candRaw, true)
			if len(mergedCands) > 0 {
				primQ := []string{qEff}
				if !strings.EqualFold(term, qEff) {
					primQ = append(primQ, term)
				}
				bestIdx := 0
				bestScore := catalog.ComputeSearchScoreMulti(mergedCands[0], primQ, nil, "relevance")
				for i := 1; i < len(mergedCands); i++ {
					sc := catalog.ComputeSearchScoreMulti(mergedCands[i], primQ, nil, "relevance")
					if sc > bestScore {
						bestScore = sc
						bestIdx = i
					}
				}
				baseCards = append(baseCards, mergedCands[bestIdx])
			}
		}

		merged := []domain.ProjectCard{}
		if len(baseCards) > 0 {
			liveSources := []string{}
			if useMR {
				liveSources = append(liveSources, "modrinth")
			}
			if useCF {
				liveSources = append(liveSources, "curseforge")
			}
			seenG := map[string]struct{}{}
			for _, bc := range baseCards {
				depRes := catalog.GetProjectDependents(hc, c.DB, cfKey, domain.DependentsRequest{
					Item:          bc,
					Relation:      "all",
					ProjectFilter: "no_modpacks",
					ContentType:   ct,
					MCVersion:     effMC,
					LoadersIn:     params.LoadersIn,
					LoadersOut:    params.LoadersOut,
					CategoriesIn:  params.CategoriesIn,
					CategoriesOut: params.CategoriesOut,
					CatMode:       params.CatMode,
					Env:           params.Env,
					UpdatedWithin: params.UpdatedWithin,
					HideAbandoned: params.HideAbandoned,
					Query:         parsed.CleanText,
					Sort:          params.Sort,
					Sources:       liveSources,
					PageSize:      params.Limit,
				})
				for _, it := range depRes.Items {
					if it.ContentType != ct {
						continue
					}
					if _, dup := seenG[it.GroupID]; !dup {
						seenG[it.GroupID] = struct{}{}
						merged = append(merged, it)
					}
				}
			}
		}

		out := []domain.ProjectCard{}
		for _, m := range merged {
			if m.ContentType == ct &&
				catalog.ItemMatchesParsed(m, parsed) &&
				domain.ItemPassesLoaders(m.Loaders, ct, params.LoadersIn, params.LoadersOut) &&
				domain.ItemPassesCategories(m.Categories, ct, params.CategoriesIn, params.CategoriesOut, params.CatMode) &&
				(ct != "mod" || domain.ItemPassesEnv(m.ClientSide, m.ServerSide, params.Env)) {
				out = append(out, m)
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].Downloads > out[j].Downloads })
		if len(out) > params.Limit {
			out = out[:params.Limit]
		}
		rdepMerged := 0
		for _, m := range out {
			if m.IsMerged {
				rdepMerged++
			}
		}
		rawJSON, err := json.Marshal(map[string]any{
			"ok":                true,
			"source_mode":       "rdeps",
			"items":             out,
			"raw_count":         len(out),
			"merged_groups":     rdepMerged,
			"totals":            map[string]int64{"modrinth": int64(len(out)), "curseforge": 0},
			"page":              params.Page,
			"limit":             params.Limit,
			"has_more":          false,
			"next_mr_offset":    params.Limit,
			"next_cf_offset":    params.Limit,
			"curseforge_status": cfStatus,
			"fuzzy_suggestion":  "Зависимые от: " + strings.Join(parsed.Prefixes.Deps, ", "),
			"has_operators":     true,
			"errors":            map[string]string{},
		})
		return rawJSON, false, err
	}

	var apiQueries, expandedBranches, smartNotices, extraQs []string
	var catTitles []string
	if parsed.CleanText != "" && params.Page <= 1 {
		catTitles = c.DB.GetCatalogTitles(ct, 400)
	}

	seenAQ := map[string]struct{}{}
	addAQ := func(q string) {
		low := strings.ToLower(strings.TrimSpace(q))
		if _, dup := seenAQ[low]; dup {
			return
		}
		seenAQ[low] = struct{}{}
		apiQueries = append(apiQueries, q)
	}

	for _, branch := range parsed.APIQueries {
		if strings.TrimSpace(branch) == "" {
			addAQ("")
			continue
		}
		if len(parsed.ExactPhrases) > 0 {
			addAQ(branch)
			expandedBranches = append(expandedBranches, branch)
			continue
		}
		smart := catalog.ExpandSmartQuery(branch, catTitles)
		eff := branch
		if smart.Changed && smart.CanonicalQuery != "" {
			eff = smart.CanonicalQuery
			if smart.Notice != "" {
				smartNotices = append(smartNotices, smart.Notice)
			}
		}
		addAQ(eff)
		expandedBranches = append(expandedBranches, eff)
		for _, ex := range smart.ExtraQueries {
			extraQs = append(extraQs, ex)
			if params.Page <= 1 && len(apiQueries) < 4 {
				addAQ(ex)
			}
		}
	}
	if len(apiQueries) == 0 {
		apiQueries = []string{""}
	}
	canonicalQ := strings.Join(expandedBranches, " ")
	smartNotice := strings.Join(smartNotices, " • ")

	cfAPIQueries := apiQueries
	if canonicalQ == "" && len(parsed.Prefixes.Authors) > 0 {
		cfAPIQueries = append([]string(nil), parsed.Prefixes.Authors...)
	}

	cfSingleLoader := ""
	if len(params.LoadersIn) == 1 {
		cfSingleLoader = params.LoadersIn[0]
	}
	upstreamSort := params.Sort
	if parsed.Prefixes.MaxDownloads != nil && upstreamSort == "downloads" {
		upstreamSort = "relevance"
	}
	fetchLim := params.Limit
	if len(params.CategoriesIn) > 0 || len(params.CategoriesOut) > 0 || parsed.HasOperators {
		fetchLim = params.Limit * 2
		if fetchLim > 50 {
			fetchLim = 50
		}
	}

	searchT0 := time.Now()
	var (
		rawItems  []domain.RawProjectCard
		seenRawSK = map[string]struct{}{}
		errorsMap = map[string]string{}
		mrSubs    = make([]catalog.SubQueryPageStat, 0, len(apiQueries))
		cfSubs    = make([]catalog.SubQueryPageStat, 0, len(apiQueries))
		mrSucc    int
		cfSucc    int
		mrLastErr string
		cfLastErr string
		mrUnique  int
		cfUnique  int
		mu        sync.Mutex
		wg        sync.WaitGroup
	)

	appendUniqueRaw := func(items []domain.RawProjectCard) int {
		added := 0
		for _, it := range items {
			if it.SourceKey != "" {
				if _, dup := seenRawSK[it.SourceKey]; dup {
					continue
				}
				seenRawSK[it.SourceKey] = struct{}{}
			}
			rawItems = append(rawItems, it)
			added++
		}
		return added
	}

	if useMR {
		for _, qStr := range apiQueries {
			wg.Add(1)
			go func(q string) {
				defer wg.Done()
				items, tot, err := catalog.SearchModrinth(hc, q, ct, effMC, params.LoadersIn, params.LoadersOut, params.CategoriesIn, params.CategoriesOut, params.CatMode, params.Env, upstreamSort, fetchLim, params.MROffset)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					mrLastErr = HumanizeSourceError("modrinth", err)
					return
				}
				mrSucc++
				mrSubs = append(mrSubs, catalog.SubQueryPageStat{Fetched: len(items), Total: tot})
				mrUnique += appendUniqueRaw(items)
			}(qStr)
		}
	}
	if useCF {
		for _, qStr := range cfAPIQueries {
			wg.Add(1)
			go func(q string) {
				defer wg.Done()
				items, tot, err := catalog.SearchCurseForge(hc, cfKey, q, ct, effMC, cfSingleLoader, upstreamSort, fetchLim, params.CFOffset)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					cfLastErr = HumanizeSourceError("curseforge", err)
					return
				}
				cfSucc++
				cfSubs = append(cfSubs, catalog.SubQueryPageStat{Fetched: len(items), Total: tot})
				cfUnique += appendUniqueRaw(items)
			}(qStr)
		}
	}
	if params.Page <= 1 && useMR && len(parsed.Prefixes.Authors) > 0 {
		for _, auth := range parsed.Prefixes.Authors {
			wg.Add(1)
			go func(a string) {
				defer wg.Done()
				items := catalog.FetchModrinthByAuthor(hc, a, ct)
				if len(items) > 0 {
					mu.Lock()
					mrUnique += appendUniqueRaw(items)
					mu.Unlock()
				}
			}(auth)
		}
	}
	wg.Wait()

	if useMR && mrSucc == 0 && mrLastErr != "" {
		errorsMap["modrinth"] = mrLastErr
	}
	if useCF && cfSucc == 0 && cfLastErr != "" {
		errorsMap["curseforge"] = cfLastErr
	}

	upstreamMs := time.Since(searchT0).Milliseconds()
	mergeT0 := time.Now()
	c.DB.UpsertCatalogItemsAsync(rawItems)
	merged := catalog.MergeItemsWithOverrides(rawItems, params.AutoMerge, c.GetOverrides())
	if params.AutoMerge && useMR && useCF && len(merged) > 0 {
		merged = catalog.EnrichSearchBatchLocalOnly(c.DB, merged, []string{"modrinth", "curseforge"})
	}
	mergeMs := time.Since(mergeT0).Milliseconds()

	var updatedCut, abandonedCut string
	if params.UpdatedWithin > 0 {
		updatedCut = time.Now().UTC().AddDate(0, 0, -params.UpdatedWithin).Format(time.RFC3339)
	}
	if params.HideAbandoned {
		abandonedCut = time.Now().UTC().AddDate(0, 0, -730).Format(time.RFC3339)
	}

	filtered := make([]domain.ProjectCard, 0, len(merged))
	for _, m := range merged {
		if parsed.HasOperators && !catalog.ItemMatchesParsed(m, parsed) {
			continue
		}
		if !domain.ItemPassesLoaders(m.Loaders, ct, params.LoadersIn, params.LoadersOut) {
			continue
		}
		if !domain.ItemPassesCategories(m.Categories, ct, params.CategoriesIn, params.CategoriesOut, params.CatMode) {
			continue
		}
		if ct == "mod" && !domain.ItemPassesEnv(m.ClientSide, m.ServerSide, params.Env) {
			continue
		}
		if updatedCut != "" && m.DateModified != "" && m.DateModified < updatedCut {
			continue
		}
		if abandonedCut != "" && m.DateModified != "" && m.DateModified < abandonedCut {
			continue
		}
		filtered = append(filtered, m)
	}

	newestTS := func(card domain.ProjectCard) int64 {
		if ts := domain.ParseISO8601Millis(card.DateCreated); ts > 0 {
			return ts
		}
		return domain.ParseISO8601Millis(card.DateModified)
	}

	if len(filtered) > 1 {
		type rankedCard struct {
			card domain.ProjectCard
			key  int64
			fKey float64
		}
		ranked := make([]rankedCard, len(filtered))
		switch params.Sort {
		case "updated":
			for i, card := range filtered {
				ranked[i] = rankedCard{card: card, key: domain.ParseISO8601Millis(card.DateModified)}
			}
			sort.SliceStable(ranked, func(i, j int) bool {
				if ranked[i].key != ranked[j].key {
					return ranked[i].key > ranked[j].key
				}
				return ranked[i].card.Downloads > ranked[j].card.Downloads
			})
		case "newest":
			for i, card := range filtered {
				ranked[i] = rankedCard{card: card, key: newestTS(card)}
			}
			sort.SliceStable(ranked, func(i, j int) bool {
				if ranked[i].key != ranked[j].key {
					return ranked[i].key > ranked[j].key
				}
				return ranked[i].card.Downloads > ranked[j].card.Downloads
			})
		default:
			if canonicalQ != "" {
				primaryQueries := append([]string(nil), expandedBranches...)
				for _, rawBranch := range parsed.APIQueries {
					if strings.TrimSpace(rawBranch) != "" {
						primaryQueries = domain.AppendUniqueCI(primaryQueries, rawBranch)
					}
				}
				for i, card := range filtered {
					ranked[i] = rankedCard{card: card, fKey: catalog.ComputeSearchScoreMulti(card, primaryQueries, extraQs, params.Sort)}
				}
				sort.SliceStable(ranked, func(i, j int) bool {
					if ranked[i].fKey != ranked[j].fKey {
						return ranked[i].fKey > ranked[j].fKey
					}
					return ranked[i].card.Downloads > ranked[j].card.Downloads
				})
			} else {
				for i, card := range filtered {
					ranked[i] = rankedCard{card: card}
				}
				sort.SliceStable(ranked, func(i, j int) bool {
					return ranked[i].card.Downloads > ranked[j].card.Downloads
				})
			}
		}
		for i := range ranked {
			filtered[i] = ranked[i].card
		}
	}

	mergedGroups := 0
	for _, m := range filtered {
		if m.IsMerged {
			mergedGroups++
		}
	}
	mrPag := catalog.ComputeSearchPagination(params.MROffset, fetchLim, mrUnique, mrSubs)
	cfPag := catalog.ComputeSearchPagination(params.CFOffset, fetchLim, cfUnique, cfSubs)
	totals := map[string]int64{
		"modrinth":   mrPag.Total,
		"curseforge": cfPag.Total,
	}
	hasMore := (useMR && mrPag.HasMore) || (useCF && cfPag.HasMore)

	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	payload := map[string]any{
		"ok":                true,
		"source_mode":       "live",
		"items":             filtered,
		"raw_count":         len(rawItems),
		"merged_groups":     mergedGroups,
		"totals":            totals,
		"page":              params.Page,
		"limit":             params.Limit,
		"has_more":          hasMore,
		"next_mr_offset":    mrPag.NextOffset,
		"next_cf_offset":    cfPag.NextOffset,
		"curseforge_status": cfStatus,
		"fuzzy_suggestion":  smartNotice,
		"has_operators":     parsed.HasOperators,
		"errors":            errorsMap,
		"timing_ms": map[string]int64{
			"upstream": upstreamMs,
			"merge":    mergeMs,
			"total":    time.Since(searchT0).Milliseconds(),
		},
	}
	rawJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, false, err
	}
	if len(errorsMap) == 0 {
		c.SearchCache.Set(searchCacheKey, rawJSON, 45)
	}
	return rawJSON, false, nil
}

func (c *Container) ResolveURL(ctx context.Context, rawURL string) (domain.ProjectCard, error) {
	hc := c.HTTP.WithContext(ctx)
	parsed := catalog.ParseProjectURL(rawURL)
	if parsed == nil {
		return domain.ProjectCard{}, fmt.Errorf("%w: Неподдерживаемая ссылка (нужен URL Modrinth или CurseForge)", ErrValidation)
	}
	cfKey := security.SanitizeCFKey(c.GetConfig().CurseForgeAPIKey)
	var rawCard *domain.RawProjectCard
	if parsed.Platform == "modrinth" {
		card, err := catalog.FetchModrinthProjectBySlug(hc, parsed.Slug)
		if err != nil || card == nil {
			return domain.ProjectCard{}, fmt.Errorf("%w: Проект Modrinth не найден", ErrNotFound)
		}
		rawCard = card
	} else {
		if cfKey == "" {
			return domain.ProjectCard{}, fmt.Errorf("%w: Для открытия ссылок CurseForge укажите API-ключ в настройках", ErrValidation)
		}
		card := catalog.FetchCFExactSlug(hc, cfKey, parsed.Slug, parsed.ContentType)
		if card == nil {
			return domain.ProjectCard{}, fmt.Errorf("%w: Проект CurseForge не найден", ErrNotFound)
		}
		rawCard = card
	}
	c.DB.UpsertCatalogItemsAsync([]domain.RawProjectCard{*rawCard})
	merged := catalog.MergeItems([]domain.RawProjectCard{*rawCard}, true)
	return merged[0], nil
}

type ModDetailsInput struct {
	Item        domain.ProjectCard     `json:"item"`
	Sources     []domain.ProjectSource `json:"sources"`
	ContentType string                 `json:"content_type"`
}

func (c *Container) GetModDetails(ctx context.Context, body ModDetailsInput) ([]byte, bool, error) {
	hc := c.HTTP.WithContext(ctx)
	item := body.Item
	if len(item.Sources) == 0 && len(body.Sources) > 0 {
		item.Sources = body.Sources
	}
	ct := body.ContentType
	if ct == "" {
		ct = item.ContentType
	}
	if ct == "" {
		ct = "mod"
	}
	item.ContentType = ct
	cfKey := security.SanitizeCFKey(c.GetConfig().CurseForgeAPIKey)
	sks := make([]string, 0, len(item.Sources))
	for _, sc := range item.Sources {
		sks = append(sks, sc.SourceKey)
	}
	sort.Strings(sks)
	detailsCacheKey := fmt.Sprintf("det|cf=%t|%s|%s|%s", cfKey != "", ct, item.GroupID, strings.Join(sks, "+"))
	if cachedDet, ok := c.DetailsCache.Get(detailsCacheKey); ok {
		return cachedDet, true, nil
	}

	if enriched, changed := catalog.DiscoverAndMergeCard(hc, c.DB, cfKey, item, nil); changed {
		item = enriched
	}
	sources := item.Sources

	var (
		bestBody   string
		bestFormat = "markdown"
		gallery    = []domain.GalleryItem{}
		seenGal    = map[string]struct{}{}
		links      = item.Links
		loaderSet  = map[string]struct{}{}
		verSet     = map[string]struct{}{}
		chanSet    = map[string]struct{}{}
		platSet    = map[string]struct{}{}
		allVers    = []domain.VersionFile{}
		srcDetails = []map[string]any{}
	)
	for _, l := range item.Loaders {
		loaderSet[strings.ToLower(l)] = struct{}{}
	}
	for _, gv := range item.GameVersions {
		verSet[gv] = struct{}{}
	}

	type metaRes struct {
		src    domain.ProjectSource
		body   string
		fmt    string
		gal    []domain.GalleryItem
		links  domain.ProjectLinks
		vers   []domain.VersionFile
		err    error
		status string
	}
	results := make([]metaRes, len(sources))
	var mdWg sync.WaitGroup
	for i, src := range sources {
		mdWg.Add(1)
		go func(idx int, sc domain.ProjectSource) {
			defer mdWg.Done()
			var b, f string
			var g []domain.GalleryItem
			var l domain.ProjectLinks
			var v []domain.VersionFile
			var err error
			st := "ok"
			if sc.Platform == "modrinth" {
				b, g, l, v, err = catalog.GetModrinthProjectMeta(hc, sc.ID, sc.Slug, ct)
				f = "markdown"
				if err != nil {
					st = "error"
				}
			} else if sc.Platform == "curseforge" {
				f = "html"
				if cfKey == "" {
					st = "no_key"
				} else {
					b, g, l, v, err = catalog.GetCFProjectMeta(hc, cfKey, sc.ID, sc.URL, ct)
					if err != nil {
						st = "error"
					}
				}
			}
			results[idx] = metaRes{src: sc, body: b, fmt: f, gal: g, links: l, vers: v, err: err, status: st}
		}(i, src)
	}
	mdWg.Wait()

	detErrors := map[string]string{}
	sourceStatuses := map[string]string{}
	for _, r := range results {
		if r.status == "no_key" {
			sourceStatuses[r.src.Platform] = "no_key"
			detErrors[r.src.Platform] = "API-ключ CurseForge не задан — данные с CurseForge не загружены"
		} else if r.err != nil {
			sourceStatuses[r.src.Platform] = "error"
			detErrors[r.src.Platform] = HumanizeSourceError(r.src.Platform, r.err)
		} else if _, existed := sourceStatuses[r.src.Platform]; !existed {
			sourceStatuses[r.src.Platform] = "ok"
		}
		src := r.src
		platSet[src.Platform] = struct{}{}
		if r.body != "" {
			if r.fmt == "markdown" && (bestBody == "" || bestFormat != "markdown") {
				bestBody = r.body
				bestFormat = "markdown"
			} else if bestBody == "" {
				bestBody = r.body
				bestFormat = r.fmt
			}
		}
		for _, img := range r.gal {
			if _, dup := seenGal[img.URL]; !dup {
				seenGal[img.URL] = struct{}{}
				gallery = append(gallery, img)
			}
		}
		for _, v := range r.vers {
			allVers = append(allVers, v)
			for _, ld := range v.Loaders {
				loaderSet[strings.ToLower(ld)] = struct{}{}
			}
			for _, gv := range v.GameVersions {
				verSet[gv] = struct{}{}
			}
			if v.ReleaseType != "" {
				chanSet[v.ReleaseType] = struct{}{}
			}
		}
		if links.SourceURL == "" {
			links.SourceURL = r.links.SourceURL
		}
		if links.IssuesURL == "" {
			links.IssuesURL = r.links.IssuesURL
		}
		if links.WikiURL == "" {
			links.WikiURL = r.links.WikiURL
		}
		if links.DiscordURL == "" {
			links.DiscordURL = r.links.DiscordURL
		}
		srcDetails = append(srcDetails, map[string]any{
			"platform":   src.Platform,
			"source_key": src.SourceKey,
			"id":         src.ID,
			"slug":       src.Slug,
			"title":      src.Title,
			"url":        src.URL,
			"downloads":  src.Downloads,
			"links": map[string]any{
				"source_url":    r.links.SourceURL,
				"issues_url":    r.links.IssuesURL,
				"wiki_url":      r.links.WikiURL,
				"discord_url":   r.links.DiscordURL,
				"donation_urls": []any{},
			},
			"allow_dist": true,
		})
	}

	orderedLoaders := []string{}
	if ct != "shader" && ct != "resourcepack" {
		for _, kl := range domain.KnownLoaders {
			if _, ok := loaderSet[kl]; ok {
				orderedLoaders = append(orderedLoaders, kl)
			}
		}
	}
	sortedVers := make([]string, 0, len(verSet))
	for gv := range verSet {
		sortedVers = append(sortedVers, gv)
	}
	sort.SliceStable(sortedVers, func(i, j int) bool {
		return domain.MCTupleGreater(domain.ParseMCTuple(sortedVers[i]), domain.ParseMCTuple(sortedVers[j]))
	})
	plats := make([]string, 0, len(platSet))
	for p := range platSet {
		plats = append(plats, p)
	}
	sort.Strings(plats)

	allVers = catalog.CrossEnrichVersionDependencies(allVers)
	sort.SliceStable(allVers, func(i, j int) bool {
		ti, tj := domain.TopMCTuple(allVers[i].GameVersions), domain.TopMCTuple(allVers[j].GameVersions)
		if ti != tj {
			return domain.MCTupleGreater(ti, tj)
		}
		return allVers[i].DatePublished > allVers[j].DatePublished
	})
	channels := []string{}
	for _, ch := range []string{"release", "beta", "alpha"} {
		if _, ok := chanSet[ch]; ok {
			channels = append(channels, ch)
		}
	}

	item.Links = links
	item.Platforms = plats
	item.IsMerged = len(item.Sources) > 1
	if len(orderedLoaders) > 0 {
		item.Loaders = orderedLoaders
	}
	if len(sortedVers) > 0 {
		item.GameVersions = sortedVers
	}

	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	detPayload := map[string]any{
		"ok":          true,
		"merged_item": item,
		"links": map[string]any{
			"source_url":    links.SourceURL,
			"issues_url":    links.IssuesURL,
			"wiki_url":      links.WikiURL,
			"discord_url":   links.DiscordURL,
			"donation_urls": []any{},
		},
		"license":            item.License,
		"sources_details":    srcDetails,
		"body":               bestBody,
		"body_format":        bestFormat,
		"gallery":            gallery,
		"meta_loaders":       orderedLoaders,
		"meta_game_versions": sortedVers,
		"platforms":          plats,
		"versions":           allVers,
		"versions_facets": map[string]any{
			"loaders":       orderedLoaders,
			"game_versions": sortedVers,
			"channels":      channels,
		},
		"source_statuses": sourceStatuses,
		"errors":          detErrors,
	}
	rawDet, err := json.Marshal(detPayload)
	if err != nil {
		return nil, false, err
	}
	if len(detErrors) == 0 {
		c.DetailsCache.Set(detailsCacheKey, rawDet, 90)
	}
	return rawDet, false, nil
}

type ModVersionsInput struct {
	Item        *domain.ProjectCard    `json:"item,omitempty"`
	Sources     []domain.ProjectSource `json:"sources"`
	ContentType string                 `json:"content_type"`
	Loader      string                 `json:"loader"`
	MCVersion   string                 `json:"mc_version"`
	Channel     string                 `json:"channel"`
	Platform    string                 `json:"platform"`
	Sort        string                 `json:"sort"`
	PreloadAll  bool                   `json:"preload_all"`
	Page        int                    `json:"page"`
	Limit       int                    `json:"limit"`
}

func (c *Container) GetModVersions(ctx context.Context, body ModVersionsInput) map[string]any {
	hc := c.HTTP.WithContext(ctx)
	ct := body.ContentType
	if ct == "" && body.Item != nil {
		ct = body.Item.ContentType
	}
	if ct == "" {
		ct = "mod"
	}
	loader := strings.ToLower(strings.TrimSpace(body.Loader))
	if loader == "" {
		loader = "all"
	}
	channel := strings.ToLower(strings.TrimSpace(body.Channel))
	if channel == "" {
		channel = "all"
	}
	plat := strings.ToLower(strings.TrimSpace(body.Platform))
	if plat == "" {
		plat = "all"
	}
	page := body.Page
	if page < 1 {
		page = 1
	}
	limit := body.Limit
	if limit < 10 || limit > 50 {
		limit = 20
	}
	cfKey := security.SanitizeCFKey(c.GetConfig().CurseForgeAPIKey)

	sources := body.Sources
	var mergedOut *domain.ProjectCard
	if body.Item != nil {
		card := *body.Item
		if len(card.Sources) == 0 {
			card.Sources = sources
		}
		card.ContentType = ct
		if enriched, changed := catalog.DiscoverAndMergeCard(hc, c.DB, cfKey, card, nil); changed {
			sources = enriched.Sources
			mergedOut = &enriched
		} else if len(sources) == 0 {
			sources = card.Sources
		}
	}

	var allVers []domain.VersionFile
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, src := range sources {
		if plat != "all" && src.Platform != plat {
			continue
		}
		wg.Add(1)
		go func(sc domain.ProjectSource) {
			defer wg.Done()
			var list []domain.VersionFile
			if sc.Platform == "modrinth" {
				list = catalog.GetModrinthVersions(hc, sc.ID, sc.Slug, ct, "", "")
			} else if sc.Platform == "curseforge" && cfKey != "" {
				if body.PreloadAll {
					list = catalog.GetCFFilesMultiPage(hc, cfKey, sc.ID, sc.URL, ct, "", "")
				} else {
					list = catalog.GetCFFiles(hc, cfKey, sc.ID, sc.URL, ct, "", "", 50)
				}
			}
			if len(list) > 0 {
				mu.Lock()
				allVers = append(allVers, list...)
				mu.Unlock()
			}
		}(src)
	}
	wg.Wait()

	allVers = catalog.CrossEnrichVersionDependencies(allVers)

	loaderSet := map[string]struct{}{}
	verSet := map[string]struct{}{}
	chanSet := map[string]struct{}{}
	for _, v := range allVers {
		for _, l := range v.Loaders {
			loaderSet[strings.ToLower(l)] = struct{}{}
		}
		for _, gv := range v.GameVersions {
			verSet[gv] = struct{}{}
		}
		if v.ReleaseType != "" {
			chanSet[v.ReleaseType] = struct{}{}
		}
	}

	filtered := make([]domain.VersionFile, 0, len(allVers))
	for _, v := range allVers {
		if loader != "all" {
			ok := false
			for _, l := range v.Loaders {
				if strings.EqualFold(l, loader) {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		if body.MCVersion != "" {
			ok := false
			for _, gv := range v.GameVersions {
				if gv == body.MCVersion {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		if channel != "all" && !strings.EqualFold(v.ReleaseType, channel) {
			continue
		}
		filtered = append(filtered, v)
	}

	switch body.Sort {
	case "date_desc":
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].DatePublished > filtered[j].DatePublished })
	case "downloads_desc":
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Downloads > filtered[j].Downloads })
	default:
		sort.SliceStable(filtered, func(i, j int) bool {
			ti, tj := domain.TopMCTuple(filtered[i].GameVersions), domain.TopMCTuple(filtered[j].GameVersions)
			if ti != tj {
				return domain.MCTupleGreater(ti, tj)
			}
			return filtered[i].DatePublished > filtered[j].DatePublished
		})
	}

	total := len(filtered)
	pageItems := filtered
	hasMore := false
	if !body.PreloadAll {
		start := (page - 1) * limit
		if start >= total {
			pageItems = []domain.VersionFile{}
		} else {
			end := start + limit
			if end > total {
				end = total
			}
			pageItems = filtered[start:end]
			hasMore = end < total
		}
	}

	orderedLoaders := []string{}
	for _, kl := range domain.KnownLoaders {
		if _, ok := loaderSet[kl]; ok {
			orderedLoaders = append(orderedLoaders, kl)
		}
	}
	sortedVers := make([]string, 0, len(verSet))
	for gv := range verSet {
		sortedVers = append(sortedVers, gv)
	}
	sort.SliceStable(sortedVers, func(i, j int) bool {
		return domain.MCTupleGreater(domain.ParseMCTuple(sortedVers[i]), domain.ParseMCTuple(sortedVers[j]))
	})
	channels := []string{}
	for _, ch := range []string{"release", "beta", "alpha"} {
		if _, ok := chanSet[ch]; ok {
			channels = append(channels, ch)
		}
	}

	return map[string]any{
		"ok":          true,
		"items":       pageItems,
		"merged_item": mergedOut,
		"page":        page,
		"limit":       limit,
		"total":       total,
		"has_more":    hasMore,
		"facets": map[string]any{
			"loaders":       orderedLoaders,
			"game_versions": sortedVers,
			"channels":      channels,
		},
	}
}

func (c *Container) GetQuickFile(ctx context.Context, req domain.QuickFileRequest) (map[string]any, error) {
	if len(req.Item.Sources) == 0 && req.Item.GroupID == "" {
		return nil, fmt.Errorf("%w: Не переданы данные проекта (item)", ErrValidation)
	}
	cfg := c.GetConfig()
	res, mergedItem := deps.ResolveQuickFileWithPolicy(c.HTTP.WithContext(ctx), c.DB, cfg.CurseForgeAPIKey, cfg.AutoInstallMediumDeps, req)
	return map[string]any{
		"ok":                 res.OK,
		"file":               res.File,
		"resolved_deps":      res.ResolvedDeps,
		"unresolved_deps":    res.UnresolvedDeps,
		"needs_confirm_deps": res.NeedsConfirmDeps,
		"required_deps":      res.RequiredDeps,
		"dep_warnings":       res.DepWarnings,
		"dep_errors":         res.DepErrors,
		"dep_status":         res.DepStatus,
		"manifest_status":    res.ManifestStatus,
		"merged_item":        mergedItem,
		"reason":             res.Reason,
	}, nil
}

func DependentsCacheKey(cfConfigured bool, req domain.DependentsRequest) string {
	sks := make([]string, 0, len(req.Item.Sources))
	for _, sc := range req.Item.Sources {
		if sc.SourceKey != "" {
			sks = append(sks, sc.SourceKey)
		}
	}
	sort.Strings(sks)
	lIn := append([]string(nil), req.LoadersIn...)
	lOut := append([]string(nil), req.LoadersOut...)
	cIn := append([]string(nil), req.CategoriesIn...)
	cOut := append([]string(nil), req.CategoriesOut...)
	srcs := append([]string(nil), req.Sources...)
	sort.Strings(lIn)
	sort.Strings(lOut)
	sort.Strings(cIn)
	sort.Strings(cOut)
	sort.Strings(srcs)
	return fmt.Sprintf(
		"rdep_v2|cf=%t|gid=%s|sk=%s|rel=%s|pf=%s|ct=%s|mc=%s|li=%s|lo=%s|ci=%s|co=%s|cm=%s|env=%s|upd=%d|abn=%t|q=%s|sort=%s|src=%s|idx=%d|ps=%d",
		cfConfigured, req.Item.GroupID, strings.Join(sks, "+"),
		strings.ToLower(req.Relation), strings.ToLower(req.ProjectFilter), strings.ToLower(req.ContentType),
		req.MCVersion, strings.Join(lIn, ","), strings.Join(lOut, ","),
		strings.Join(cIn, ","), strings.Join(cOut, ","), req.CatMode, req.Env,
		req.UpdatedWithin, req.HideAbandoned, strings.TrimSpace(req.Query), req.Sort,
		strings.Join(srcs, ","), req.CFIndex, req.PageSize,
	)
}

func (c *Container) GetDependents(ctx context.Context, req domain.DependentsRequest) ([]byte, bool, error) {
	if len(req.Item.Sources) == 0 && req.Item.GroupID == "" {
		return nil, false, fmt.Errorf("%w: Не передан базовый проект (item)", ErrValidation)
	}
	if req.CFIndex < 0 {
		return nil, false, fmt.Errorf("%w: Некорректный индекс пагинации cf_index", ErrValidation)
	}
	cfKey := security.SanitizeCFKey(c.GetConfig().CurseForgeAPIKey)
	ck := DependentsCacheKey(cfKey != "", req)
	if cached, ok := c.DependentsCache.Get(ck); ok {
		return cached, true, nil
	}
	res := catalog.GetProjectDependents(c.HTTP.WithContext(ctx), c.DB, cfKey, req)
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return nil, false, err
	}
	if len(res.Errors) == 0 && !(res.CFWebBlocked && len(res.Items) == 0) {
		c.DependentsCache.Set(ck, raw, 90)
	}
	return raw, false, nil
}

func (c *Container) GetFileChangelog(ctx context.Context, platform, modID, fileID string) (map[string]any, error) {
	plat := strings.ToLower(strings.TrimSpace(platform))
	if plat == "" || strings.TrimSpace(modID) == "" || strings.TrimSpace(fileID) == "" {
		return nil, fmt.Errorf("%w: Требуются поля platform, mod_id и file_id", ErrValidation)
	}
	if plat == "curseforge" {
		html := catalog.GetCFFileChangelog(c.HTTP.WithContext(ctx), c.GetConfig().CurseForgeAPIKey, modID, fileID)
		return map[string]any{"ok": true, "changelog": html, "format": "html"}, nil
	}
	return map[string]any{"ok": true, "changelog": "", "format": "markdown"}, nil
}

func DepTreeCacheKey(cfConfigured bool, req domain.DepTreeRequest) string {
	depParts := make([]string, 0, len(req.Dependencies))
	for _, d := range req.Dependencies {
		depParts = append(depParts, strings.ToLower(d.Platform)+":"+d.ProjectID+":"+d.VersionID+":"+strings.ToLower(d.Type))
	}
	sort.Strings(depParts)
	textHash := deps.ComputeTextDepsHash(deps.TextDepSourceInput{
		Platform:  strings.ToLower(req.Platform),
		ProjectID: req.ProjectID,
		Title:     req.Title,
		Summary:   req.Summary,
		Body:      req.Body,
		Changelog: req.Changelog,
	}, cfConfigured)
	return fmt.Sprintf(
		"dtree_v4|cf=%t|p=%s|pid=%s|mc=%s|ld=%s|md=%d|fu=%s|th=%s|deps=%s",
		cfConfigured, strings.ToLower(req.Platform), req.ProjectID, req.MCVersion,
		strings.ToLower(req.Loader), req.MaxDepth, strings.TrimSpace(req.FileURL),
		textHash, strings.Join(depParts, ";"),
	)
}

func (c *Container) GetDependencyTree(ctx context.Context, req domain.DepTreeRequest) ([]byte, bool, error) {
	if len(req.Dependencies) > 100 {
		return nil, false, fmt.Errorf("%w: Слишком много зависимостей за один запрос (максимум 100)", ErrValidation)
	}
	cfKey := security.SanitizeCFKey(c.GetConfig().CurseForgeAPIKey)
	ck := DepTreeCacheKey(cfKey != "", req)
	if cached, ok := c.DepTreeCache.Get(ck); ok {
		return cached, true, nil
	}
	res := deps.BuildDependencyTreeResult(c.HTTP.WithContext(ctx), c.DB, cfKey, req)
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	raw, err := json.Marshal(map[string]any{
		"ok":              true,
		"tree":            res.Tree,
		"unresolved":      res.Unresolved,
		"warnings":        res.Warnings,
		"errors":          res.Errors,
		"manifest_status": res.ManifestStatus,
		"text_status":     res.TextStatus,
		"status":          res.Status,
	})
	if err != nil {
		return nil, false, err
	}
	if res.Status == "complete" || res.Status == "none" {
		c.DepTreeCache.Set(ck, raw, 180)
	}
	return raw, false, nil
}

func (c *Container) ResolveDependencies(ctx context.Context, req domain.DepResolveRequest) (map[string]any, error) {
	if len(req.Dependencies) > 100 {
		return nil, fmt.Errorf("%w: Слишком много зависимостей за один запрос (максимум 100)", ErrValidation)
	}
	cfg := c.GetConfig()
	if !req.AllowMediumConfidence {
		req.AllowMediumConfidence = cfg.AutoInstallMediumDeps
	}
	res := deps.ResolveRequiredDepsFull(c.HTTP.WithContext(ctx), c.DB, cfg.CurseForgeAPIKey, req)
	return map[string]any{
		"ok":              true,
		"resolved":        res.Resolved,
		"needs_confirm":   res.NeedsConfirm,
		"unresolved":      res.Unresolved,
		"warnings":        res.Warnings,
		"errors":          res.Errors,
		"manifest_status": res.ManifestStatus,
		"text_status":     res.TextStatus,
		"status":          res.Status,
		"items_status":    res.ItemsStatus,
		"diagnostics":     res.Diagnostics,
	}, nil
}
