package catalog

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"modhub/internal/domain"
	"modhub/internal/netx"
	"modhub/internal/security"
	"modhub/internal/storage"
)

func RelToCFType(rel string) int {
	switch rel {
	case "all":
		return 0
	case "embedded":
		return 1
	case "optional":
		return 2
	case "required":
		return 3
	case "tool":
		return 4
	case "incompatible":
		return 5
	case "include":
		return 6
	default:
		return 3
	}
}

func RelLabelRU(rel string) string {
	switch rel {
	case "required":
		return "Обязательная"
	case "optional":
		return "Опциональная"
	case "embedded":
		return "Встроенная"
	case "tool":
		return "Инструмент"
	case "incompatible":
		return "Несовместимо"
	case "include":
		return "Включено"
	default:
		return "Зависимый"
	}
}

func GetProjectDependents(httpClient *netx.HTTPClient, db *storage.Database, cfKeyRaw string, req domain.DependentsRequest) domain.DependentsResponse {
	cfKey := security.SanitizeCFKey(cfKeyRaw)
	relNorm := strings.ToLower(strings.TrimSpace(req.Relation))
	switch relNorm {
	case "all", "required", "optional", "embedded", "tool", "incompatible", "include":
	default:
		relNorm = "required"
	}
	cfRelType := RelToCFType(relNorm)

	pf := strings.ToLower(strings.TrimSpace(req.ProjectFilter))
	switch pf {
	case "all", "no_modpacks", "mods_only", "modpacks_only":
	default:
		pf = "no_modpacks"
	}

	reqCT := strings.ToLower(strings.TrimSpace(req.ContentType))
	switch reqCT {
	case "mod", "resourcepack", "shader", "datapack", "modpack":
	default:
		reqCT = ""
	}
	if reqCT == "" && pf == "mods_only" {
		reqCT = "mod"
	} else if reqCT == "" && pf == "modpacks_only" {
		reqCT = "modpack"
	}

	allowed := map[string]bool{"modrinth": true, "curseforge": true}
	if len(req.Sources) > 0 {
		allowed = map[string]bool{}
		for _, s := range req.Sources {
			allowed[strings.ToLower(s)] = true
		}
	}

	allowedList := make([]string, 0, len(allowed))
	for p, ok := range allowed {
		if ok {
			allowedList = append(allowedList, p)
		}
	}
	if enriched, changed := DiscoverAndMergeCard(httpClient, db, cfKey, req.Item, allowedList); changed {
		req.Item = enriched
	}

	cfID, cfSlug, mrID := "", "", ""
	baseSKeys := map[string]struct{}{}
	for _, s := range req.Item.Sources {
		if s.SourceKey != "" {
			baseSKeys[s.SourceKey] = struct{}{}
		}
		if s.Platform == "curseforge" && cfID == "" && s.ID != "" {
			cfID = s.ID
			cfSlug = s.Slug
		} else if s.Platform == "modrinth" && mrID == "" && s.ID != "" {
			mrID = s.ID
		}
	}

	baseCT := req.Item.ContentType
	if baseCT == "" {
		baseCT = "mod"
	}
	cfDepURL := ""
	if cfSlug != "" {
		qPart := ""
		if cfRelType > 0 {
			qPart = fmt.Sprintf("?filter-related-dependents=%d", cfRelType)
		}
		cfDepURL = fmt.Sprintf("https://www.curseforge.com/minecraft/%s/%s/relations/dependents%s", domain.GetContentTypeMeta(baseCT).CFURLSlug, cfSlug, qPart)
	}

	isFirst := req.CFIndex == 0
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	singleLoader := ""
	if len(req.LoadersIn) == 1 {
		singleLoader = req.LoadersIn[0]
	}

	cfFetchPF := pf
	if reqCT == "modpack" {
		cfFetchPF = "modpacks_only"
	} else if reqCT != "" && cfFetchPF == "mods_only" {
		cfFetchPF = "no_modpacks"
	}

	depTypes := []string{relNorm}
	if relNorm == "all" {
		depTypes = []string{"required", "optional", "embedded", "incompatible", "tool", "include"}
	}

	var (
		cfRes     CFDependentsPageResult
		usedCore  bool
		cfCached  []domain.RawProjectCard
		mrCards   []domain.RawProjectCard
		respErrs  = map[string]string{}
		errMu     sync.Mutex
		wg        sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		if cfID == "" || !allowed["curseforge"] {
			return
		}

		// Try official CurseForge web dependents endpoint first (provides exact global TotalCount & full pagination)
		cfRes = FetchCFDependentsPage(httpClient, cfKey, cfID, cfSlug, baseCT, reqCT, cfRelType, req.CFIndex, pageSize, cfFetchPF)

		// If www.curseforge.com is blocked (e.g. Cloudflare 403) on ANY page (page 1 OR subsequent pages),
		// seamlessly paginate through the official CurseForge Core API v1 strict verifier!
		if len(cfRes.Items) == 0 && cfRes.WebError != "" {
			if cfKey != "" {
				webErr := cfRes.WebError
				coreRes := FetchCFCoreStrictDependentsPage(
					httpClient, cfKey, cfID, req.Item, cfRelType,
					req.MCVersion, singleLoader, cfFetchPF, reqCT, req.CFIndex,
				)
				coreRes.WebError = webErr
				cfRes = coreRes
				usedCore = true
			} else {
				errMu.Lock()
				respErrs["curseforge"] = "CurseForge ограничил доступ к списку зависимых без API-ключа — подключите Core API Key в настройках"
				errMu.Unlock()
			}
		}

		// On page 1, also merge any previously verified CurseForge dependents stored in local SQLite
		if isFirst && db != nil {
			edges := db.GetReverseDependents([]string{"curseforge:" + cfID}, depTypes, 2000)
			seenCF := map[string]struct{}{}
			for _, it := range cfRes.Items {
				seenCF[it.SourceKey] = struct{}{}
			}
			for _, e := range edges {
				if _, dup := seenCF[e.DependentKey]; dup {
					continue
				}
				if c, found := db.GetCatalogItem(e.DependentKey); found {
					if !domain.IsSupportedCatalogType(c.ContentType) && c.ContentType != "modpack" {
						continue
					}
					seenCF[e.DependentKey] = struct{}{}
					c.DependentRelation = e.DepType
					c.DependentLabel = RelLabelRU(e.DepType)
					cfCached = append(cfCached, *c)
				}
			}
		}
	}()

	go func() {
		defer wg.Done()
		if !isFirst || mrID == "" || !allowed["modrinth"] || pf == "modpacks_only" || reqCT == "modpack" || relNorm == "tool" || relNorm == "include" {
			return
		}
		seen := map[string]struct{}{mrID: {}}

		// 1. Load previously verified Modrinth reverse_deps edges from local SQLite
		if db != nil {
			edges := db.GetReverseDependents([]string{"modrinth:" + mrID}, depTypes, 2000)
			var missingIDs []string
			relMap := map[string]string{}
			for _, e := range edges {
				if pid, ok := strings.CutPrefix(e.DependentKey, "modrinth:"); ok {
					if cur, exists := relMap[pid]; !exists || domain.DepTypePriority(e.DepType) > domain.DepTypePriority(cur) {
						relMap[pid] = e.DepType
					}
					if c, found := db.GetCatalogItem(e.DependentKey); found {
						if !domain.IsSupportedCatalogType(c.ContentType) {
							continue
						}
						if _, dup := seen[c.ID]; !dup {
							seen[c.ID] = struct{}{}
							c.DependentRelation = relMap[pid]
							c.DependentLabel = RelLabelRU(relMap[pid])
							mrCards = append(mrCards, *c)
						}
					} else {
						missingIDs = append(missingIDs, pid)
					}
				}
			}
			if len(missingIDs) > 0 {
				batch := GetModrinthProjectsBatch(httpClient, missingIDs)
				for pid, p := range batch {
					if _, dup := seen[pid]; !dup {
						c := FormatMRFullProject(p, "")
						if !domain.IsSupportedCatalogType(c.ContentType) {
							continue
						}
						seen[pid] = struct{}{}
						dt := relMap[pid]
						if dt == "" {
							dt = "required"
						}
						c.DependentRelation = dt
						c.DependentLabel = RelLabelRU(dt)
						mrCards = append(mrCards, c)
					}
				}
			}
		}

		// 2. Live strictly-verified Modrinth discovery across Mods, Resource Packs, Data Packs & Shaders
		mrTypes := []string{"mod", "resourcepack", "datapack", "shader"}
		for _, vc := range DiscoverVerifiedMRDependents(httpClient, db, req.Item, mrID, req.MCVersion, req.LoadersIn, req.LoadersOut, relNorm, mrTypes) {
			if !domain.IsSupportedCatalogType(vc.ContentType) {
				continue
			}
			if _, dup := seen[vc.ID]; !dup {
				seen[vc.ID] = struct{}{}
				mrCards = append(mrCards, vc)
			}
		}
	}()
	wg.Wait()

	// Persist verified CF edges and catalog items
	if db != nil && cfID != "" && len(cfRes.Items) > 0 {
		var edges []domain.RDepEdge
		for _, c := range cfRes.Items {
			if c.DependentRelation != "" && c.DependentRelation != "dependent" {
				edges = append(edges, domain.RDepEdge{
					DepKey:       "curseforge:" + cfID,
					DependentKey: c.SourceKey,
					DepType:      c.DependentRelation,
				})
			}
		}
		db.UpsertCatalogItemsAsync(cfRes.Items)
		db.UpsertReverseDepsAsync(edges)
	}

	relBySK := map[string][2]string{}
	ordBySK := map[string]int{}
	combined := make([]domain.RawProjectCard, 0, len(cfRes.Items)+len(cfCached)+len(mrCards))
	allRaw := make([]domain.RawProjectCard, 0, len(cfRes.Items)+len(cfCached)+len(mrCards))
	allRaw = append(allRaw, cfRes.Items...)
	allRaw = append(allRaw, cfCached...)
	allRaw = append(allRaw, mrCards...)

	for idx, c := range allRaw {
		if _, isBase := baseSKeys[c.SourceKey]; isBase {
			continue
		}
		if c.ContentType == "" {
			continue
		}
		if c.ContentType == "modpack" && reqCT != "modpack" && pf != "all" && pf != "modpacks_only" {
			continue
		}
		if _, exists := ordBySK[c.SourceKey]; !exists {
			ordBySK[c.SourceKey] = idx
		}
		rk := c.DependentRelation
		if rk == "" {
			rk = "dependent"
		}
		rl := c.DependentLabel
		if rl == "" {
			rl = RelLabelRU(rk)
		}
		if cur, ok := relBySK[c.SourceKey]; !ok || domain.DepTypePriority(rk) > domain.DepTypePriority(cur[0]) {
			relBySK[c.SourceKey] = [2]string{rk, rl}
		}
		combined = append(combined, c)
	}

	merged := MergeItems(combined, true)
	qLow := strings.ToLower(strings.TrimSpace(req.Query))
	var updatedCut, abandonedCut string
	if req.UpdatedWithin > 0 {
		updatedCut = time.Now().UTC().AddDate(0, 0, -req.UpdatedWithin).Format(time.RFC3339)
	}
	if req.HideAbandoned {
		abandonedCut = time.Now().UTC().AddDate(0, 0, -730).Format(time.RFC3339)
	}

	countsByType := map[string]int{
		"mod":          0,
		"resourcepack": 0,
		"datapack":     0,
		"shader":       0,
	}

	filtered := []domain.ProjectCard{}
	for _, m := range merged {
		skipBase := false
		bestRel := [2]string{"dependent", "Зависимый"}
		bestOrd := 999999
		for _, s := range m.Sources {
			if _, isB := baseSKeys[s.SourceKey]; isB {
				skipBase = true
				break
			}
			if r, ok := relBySK[s.SourceKey]; ok && domain.DepTypePriority(r[0]) > domain.DepTypePriority(bestRel[0]) {
				bestRel = r
			}
			if o, ok := ordBySK[s.SourceKey]; ok && o < bestOrd {
				bestOrd = o
			}
		}
		if skipBase {
			continue
		}
		m.DependentInfo = &domain.DependentInfo{Relation: bestRel[0], Label: bestRel[1], Order: bestOrd}

		mCT := m.ContentType
		if mCT == "" {
			continue
		}

		if req.MCVersion != "" && len(m.GameVersions) > 0 {
			found := false
			for _, gv := range m.GameVersions {
				if gv == req.MCVersion {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if mCT == "mod" || mCT == "datapack" {
			if !domain.ItemPassesLoaders(m.Loaders, mCT, req.LoadersIn, req.LoadersOut) {
				continue
			}
		}
		if mCT != "modpack" && len(req.CategoriesIn)+len(req.CategoriesOut) > 0 {
			if !domain.ItemPassesCategories(m.Categories, mCT, req.CategoriesIn, req.CategoriesOut, req.CatMode) {
				continue
			}
		}
		if mCT == "mod" && !domain.ItemPassesEnv(m.ClientSide, m.ServerSide, req.Env) {
			continue
		}
		if updatedCut != "" && m.DateModified != "" && m.DateModified < updatedCut {
			continue
		}
		if abandonedCut != "" && m.DateModified != "" && m.DateModified < abandonedCut {
			continue
		}
		if qLow != "" {
			hay := strings.ToLower(m.Title + " " + m.Summary + " " + strings.Join(m.Authors, " "))
			if !strings.Contains(hay, qLow) {
				continue
			}
		}

		if domain.IsSupportedCatalogType(mCT) {
			countsByType[mCT]++
		}

		if reqCT != "" && mCT != reqCT {
			continue
		}
		filtered = append(filtered, m)
	}

	switch req.Sort {
	case "downloads":
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Downloads > filtered[j].Downloads })
	case "updated", "newest":
		sort.SliceStable(filtered, func(i, j int) bool {
			if filtered[i].DateModified != filtered[j].DateModified {
				return filtered[i].DateModified > filtered[j].DateModified
			}
			return filtered[i].Downloads > filtered[j].Downloads
		})
	default:
		sort.SliceStable(filtered, func(i, j int) bool {
			if filtered[i].DependentInfo.Order != filtered[j].DependentInfo.Order {
				return filtered[i].DependentInfo.Order < filtered[j].DependentInfo.Order
			}
			return filtered[i].Downloads > filtered[j].Downloads
		})
	}

	sourceMode := "cf_web"
	if usedCore {
		sourceMode = "cf_core_api"
	} else if cfID == "" {
		sourceMode = "mr_only"
	}
	cfStatus := "disabled"
	if allowed["curseforge"] {
		if cfKey != "" {
			cfStatus = "ok"
		} else {
			cfStatus = "no_key"
		}
	}

	return domain.DependentsResponse{
		OK:               true,
		Items:            filtered,
		TotalCF:          cfRes.TotalCount,
		CFScanned:        cfRes.ScannedCount,
		Count:            len(filtered),
		CountsByType:     countsByType,
		SkippedModpacks:  cfRes.SkippedModpacks,
		NextCFIndex:      cfRes.NextIndex,
		HasMore:          cfRes.HasMore,
		CFDependentsURL:  cfDepURL,
		Relation:         relNorm,
		ProjectFilter:    pf,
		SourceMode:       sourceMode,
		CFWebBlocked:     cfRes.WebError != "",
		CFConfigured:     cfKey != "",
		HasCFSource:      cfID != "",
		CurseForgeStatus: cfStatus,
		Errors:           respErrs,
	}
}
