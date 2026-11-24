package deps

import (
	"fmt"
	"net/url"
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

func BuildDependencyTree(httpClient *netx.HTTPClient, db *storage.Database, cfKey string, deps []domain.DependencyRef) ([]domain.DepTreeNode, map[string]string) {
	res := BuildDependencyTreeResult(httpClient, db, cfKey, domain.DepTreeRequest{
		Dependencies: deps,
		MaxDepth:     1,
	})
	return res.Tree, res.Errors
}

func BuildDependencyTreeFull(httpClient *netx.HTTPClient, db *storage.Database, cfKey string, req domain.DepTreeRequest) ([]domain.DepTreeNode, map[string]string) {
	res := BuildDependencyTreeResult(httpClient, db, cfKey, req)
	return res.Tree, res.Errors
}

func resolveDirectDepNodes(
	httpClient *netx.HTTPClient,
	db *storage.Database,
	cfKey string,
	deps []domain.DependencyRef,
	selfPlatform, selfProjectID string,
) ([]domain.DepTreeNode, []domain.UnresolvedDep, map[string]string) {
	errs := map[string]string{}
	unresolved := []domain.UnresolvedDep{}
	if len(deps) == 0 {
		return []domain.DepTreeNode{}, unresolved, errs
	}

	missingVids := []string{}
	for _, d := range deps {
		if strings.EqualFold(d.Platform, "modrinth") && strings.TrimSpace(d.ProjectID) == "" && strings.TrimSpace(d.VersionID) != "" {
			missingVids = append(missingVids, strings.TrimSpace(d.VersionID))
		}
	}
	var vidToPid map[string]string
	if len(missingVids) > 0 {
		vidToPid = catalog.ResolveModrinthVersionProjectIDs(httpClient, missingVids)
	}

	expanded := make([]domain.DependencyRef, 0, len(deps))
	for _, d := range deps {
		pid := strings.TrimSpace(d.ProjectID)
		vid := strings.TrimSpace(d.VersionID)
		if pid == "" && vid != "" && vidToPid != nil {
			pid = vidToPid[vid]
		}
		if pid == "" {
			if vid != "" {
				unresolved = append(unresolved, domain.UnresolvedDep{
					ModID:    vid,
					Platform: strings.ToLower(strings.TrimSpace(d.Platform)),
					Type:     domain.NormalizeDepType(d.Type),
					Source:   "api",
					Reason:   "Не удалось определить проект по идентификатору версии",
				})
			}
			continue
		}
		if selfProjectID != "" && strings.EqualFold(d.Platform, selfPlatform) && pid == selfProjectID {
			continue
		}
		d.ProjectID = pid
		d.VersionID = vid
		expanded = append(expanded, d)
	}

	valid := domain.DedupeDependencyRefs(expanded, "")
	if len(valid) == 0 {
		return []domain.DepTreeNode{}, unresolved, errs
	}

	var mrIDs []string
	var cfIDs []int
	seenMR := map[string]struct{}{}
	seenCF := map[int]struct{}{}
	for _, d := range valid {
		if d.Platform == "modrinth" {
			if _, ok := seenMR[d.ProjectID]; !ok {
				seenMR[d.ProjectID] = struct{}{}
				mrIDs = append(mrIDs, d.ProjectID)
			}
		} else if d.Platform == "curseforge" {
			if n, err := strconv.Atoi(d.ProjectID); err == nil && n > 0 {
				if _, ok := seenCF[n]; !ok {
					seenCF[n] = struct{}{}
					cfIDs = append(cfIDs, n)
				}
			}
		}
	}

	cleanCFKey := security.SanitizeCFKey(cfKey)
	if len(cfIDs) > 0 && cleanCFKey == "" {
		errs["curseforge"] = "API-ключ CurseForge не задан — для зависимостей CurseForge показаны базовые ссылки"
	}

	var (
		mrMeta map[string]catalog.MRFullProject
		cfMeta map[int]catalog.CFMod
		wg     sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		if len(mrIDs) > 0 {
			mrMeta = catalog.GetModrinthProjectsBatch(httpClient, mrIDs)
		}
	}()
	go func() {
		defer wg.Done()
		if len(cfIDs) > 0 && cleanCFKey != "" {
			cfMeta = catalog.GetCFModsBatch(httpClient, cleanCFKey, cfIDs)
		}
	}()
	wg.Wait()

	if len(mrIDs) > 0 && len(mrMeta) == 0 {
		errs["modrinth"] = "Не удалось загрузить метаданные зависимостей с Modrinth"
	}
	if len(cfIDs) > 0 && cleanCFKey != "" && len(cfMeta) == 0 {
		errs["curseforge"] = "Не удалось загрузить метаданные зависимостей с CurseForge"
	}

	nodes := make([]domain.DepTreeNode, 0, len(valid))
	for _, d := range valid {
		sk := d.Platform + ":" + d.ProjectID
		node := domain.DepTreeNode{
			Platform:    d.Platform,
			ProjectID:   d.ProjectID,
			SourceKey:   sk,
			Type:        d.Type,
			Title:       d.ProjectID,
			Folder:      "mods",
			ContentType: "mod",
			Source:      "api",
			Resolved:    false,
			Children:    []domain.DepTreeNode{},
		}
		if d.Platform == "modrinth" {
			if p, ok := mrMeta[d.ProjectID]; ok {
				node.Title = p.Title
				node.Slug = p.Slug
				node.IconURL = p.IconURL
				pt := p.ProjectType
				if pt == "" {
					pt = "mod"
				}
				node.URL = fmt.Sprintf("https://modrinth.com/%s/%s", pt, p.Slug)
				if cct := domain.ClassifyMRProjectType(pt); cct != "" {
					node.ContentType = cct
					node.Folder = domain.GetContentTypeMeta(cct).Folder
				}
				node.Resolved = true
			}
		} else if d.Platform == "curseforge" {
			if n, err := strconv.Atoi(d.ProjectID); err == nil {
				if m, ok := cfMeta[n]; ok {
					node.Title = m.Name
					node.Slug = m.Slug
					node.IconURL = m.Logo.URL
					if node.IconURL == "" {
						node.IconURL = m.Logo.ThumbnailURL
					}
					node.URL = m.Links.WebsiteURL
					if cct := domain.ClassifyCFClassID(m.ClassID); cct != "" {
						node.ContentType = cct
						node.Folder = domain.GetContentTypeMeta(cct).Folder
					}
					node.Resolved = true
				}
			}
		}

		if !node.Resolved && db != nil {
			if cached, ok := db.GetCatalogItem(sk); ok {
				if cached.Title != "" {
					node.Title = cached.Title
				}
				node.Slug = cached.Slug
				node.IconURL = cached.IconURL
				if cached.URL != "" {
					node.URL = cached.URL
				}
				if domain.IsSupportedCatalogType(cached.ContentType) {
					node.ContentType = cached.ContentType
					node.Folder = domain.GetContentTypeMeta(cached.ContentType).Folder
				}
				node.Resolved = true
			}
		}

		if node.URL == "" {
			if d.Platform == "modrinth" {
				node.URL = fmt.Sprintf("https://modrinth.com/project/%s", url.PathEscape(d.ProjectID))
			} else if d.Platform == "curseforge" {
				node.URL = fmt.Sprintf("https://www.curseforge.com/projects/%s", url.PathEscape(d.ProjectID))
				if node.Title == d.ProjectID {
					node.Title = "CurseForge #" + d.ProjectID
				}
			}
		}
		if !node.Resolved {
			unresolved = append(unresolved, domain.UnresolvedDep{
				ModID:     d.ProjectID,
				Platform:  d.Platform,
				ProjectID: d.ProjectID,
				Type:      d.Type,
				Source:    "api",
				Reason:    "Не удалось получить метаданные проекта из API",
			})
		}
		nodes = append(nodes, node)
	}

	sort.SliceStable(nodes, func(i, j int) bool {
		pi, pj := domain.DepTypePriority(nodes[i].Type), domain.DepTypePriority(nodes[j].Type)
		if pi != pj {
			return pi > pj
		}
		return strings.ToLower(nodes[i].Title) < strings.ToLower(nodes[j].Title)
	})
	return nodes, unresolved, errs
}

func nodesRepresentSameMod(a, b domain.DepTreeNode) bool {
	if a.SourceKey != "" && a.SourceKey == b.SourceKey {
		return true
	}
	if a.Slug != "" && b.Slug != "" && strings.EqualFold(a.Slug, b.Slug) {
		return true
	}
	na, nb := domain.NormalizeCompactTitle(a.Title), domain.NormalizeCompactTitle(b.Title)
	if len(na) >= 4 && na == nb {
		return true
	}
	if a.Slug != "" && modIDMatchesCandidate(b.ProjectID, a.Title, a.Slug) {
		return true
	}
	if b.Slug != "" && modIDMatchesCandidate(a.ProjectID, b.Title, b.Slug) {
		return true
	}
	return false
}

// mergeAPIAndManifestNodes ALWAYS combines API dependencies with manifest dependencies
// without duplicates, preserving the stronger dependency type and recording source="api+manifest".
func mergeAPIAndManifestNodes(
	apiNodes, manifestNodes []domain.DepTreeNode,
	selfPlatform, selfProjectID string,
) ([]domain.DepTreeNode, []domain.UnresolvedDep) {
	out := append([]domain.DepTreeNode(nil), apiNodes...)
	var unresolvedManifest []domain.UnresolvedDep

	for _, mn := range manifestNodes {
		if selfProjectID != "" && strings.EqualFold(mn.Platform, selfPlatform) && mn.ProjectID == selfProjectID {
			continue
		}
		matchedIdx := -1
		for i := range out {
			if nodesRepresentSameMod(out[i], mn) {
				matchedIdx = i
				break
			}
		}
		if matchedIdx >= 0 {
			if out[matchedIdx].Source == "api" {
				out[matchedIdx].Source = "api+manifest"
			}
			if out[matchedIdx].ManifestFile == "" {
				out[matchedIdx].ManifestFile = mn.ManifestFile
			}
			if domain.DepTypePriority(mn.Type) > domain.DepTypePriority(out[matchedIdx].Type) {
				out[matchedIdx].Type = mn.Type
			}
			continue
		}
		if !mn.Resolved {
			unresolvedManifest = append(unresolvedManifest, domain.UnresolvedDep{
				ModID:    mn.ProjectID,
				Platform: mn.Platform,
				Type:     mn.Type,
				Source:   "manifest",
				Reason:   "Мод указан в манифесте (" + mn.ManifestFile + "), но не найден в каталогах Modrinth/CurseForge",
			})
		}
		out = append(out, mn)
	}

	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := domain.DepTypePriority(out[i].Type), domain.DepTypePriority(out[j].Type)
		if pi != pj {
			return pi > pj
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out, unresolvedManifest
}

func fetchChildDependenciesForNode(
	httpClient *netx.HTTPClient,
	db *storage.Database,
	cfKey, mcVer, loader string,
	node domain.DepTreeNode,
) ([]domain.DependencyRef, string) {
	if node.ProjectID == "" {
		return nil, ""
	}
	effLoader := ""
	if domain.GetContentTypeMeta(node.ContentType).UsesLoader {
		effLoader = strings.ToLower(strings.TrimSpace(loader))
	}
	var best *domain.VersionFile
	switch node.Platform {
	case "modrinth":
		vers := catalog.GetModrinthVersions(httpClient, node.ProjectID, node.Slug, node.ContentType, mcVer, effLoader)
		if len(vers) == 0 && mcVer != "" && effLoader != "" {
			vers = catalog.GetModrinthVersions(httpClient, node.ProjectID, node.Slug, node.ContentType, "", effLoader)
		}
		if len(vers) == 0 && (mcVer != "" || effLoader != "") {
			vers = catalog.GetModrinthVersions(httpClient, node.ProjectID, node.Slug, node.ContentType, "", "")
		}
		if len(vers) > 0 {
			sort.SliceStable(vers, func(i, j int) bool { return domain.CompareVersionRelease(vers[i], vers[j]) })
			cp := vers[0]
			best = &cp
		}
	case "curseforge":
		if cleanKey := security.SanitizeCFKey(cfKey); cleanKey != "" {
			vers := catalog.GetCFFiles(httpClient, cleanKey, node.ProjectID, node.URL, node.ContentType, mcVer, effLoader, 20)
			if len(vers) == 0 && mcVer != "" && effLoader != "" {
				vers = catalog.GetCFFiles(httpClient, cleanKey, node.ProjectID, node.URL, node.ContentType, "", effLoader, 20)
			}
			if len(vers) == 0 && (mcVer != "" || effLoader != "") {
				vers = catalog.GetCFFiles(httpClient, cleanKey, node.ProjectID, node.URL, node.ContentType, "", "", 20)
			}
			if len(vers) > 0 {
				sort.SliceStable(vers, func(i, j int) bool { return domain.CompareVersionRelease(vers[i], vers[j]) })
				cp := vers[0]
				best = &cp
			}
		}
	}
	if best == nil {
		return nil, ""
	}
	var reqOnly []domain.DependencyRef
	for _, d := range best.Dependencies {
		if domain.NormalizeDepType(d.Type) == "required" && (d.ProjectID != "" || d.VersionID != "") && d.ProjectID != node.ProjectID {
			reqOnly = append(reqOnly, d)
		}
	}
	return reqOnly, best.DownloadURL
}

// BuildDependencyTreeResult is the SINGLE unified dependency discovery engine for ModHub:
// 1. Resolves direct dependencies from API metadata (Modrinth / CurseForge).
// 2. ALWAYS also inspects official .jar modloader manifests (mods.toml / neoforge.mods.toml /
//    fabric.mod.json / quilt.mod.json) when FileURL is provided, merging both sources without duplicates.
// 3. Recursively expands nested required dependencies (up to maxDepth, bounded concurrency, cycle-free).
// 4. Reports explicit unresolved items, warnings, per-source errors, manifest_status, and completeness status.
func BuildDependencyTreeResult(httpClient *netx.HTTPClient, db *storage.Database, cfKey string, req domain.DepTreeRequest) domain.DepTreeResult {
	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 3
	}
	if maxDepth > 4 {
		maxDepth = 4
	}

	apiNodes, unresolved, errs := resolveDirectDepNodes(httpClient, db, cfKey, req.Dependencies, req.Platform, req.ProjectID)
	warnings := []string{}
	manifestStatus := "skipped"

	// Run .jar manifest inspection AND intelligent text/description analysis concurrently in parallel!
	// Neither is skipped just because API already returned >= 1 dependency.
	textStatus := "skipped"
	var (
		mNodes []domain.DepTreeNode
		mErr   error
		tNodes []domain.DepTreeNode
		tUnres []domain.UnresolvedDep
		anWg   sync.WaitGroup
	)
	if strings.TrimSpace(req.FileURL) != "" {
		anWg.Add(1)
		go func() {
			defer anWg.Done()
			mdeps, mStat, err := InspectJarManifestDepsWithStatus(httpClient.ReqCtx(), httpClient, db, req.FileURL)
			manifestStatus = mStat
			mErr = err
			if err == nil && len(mdeps) > 0 {
				mNodes = ResolveManifestDepsToNodes(httpClient, db, cfKey, req.Platform, mdeps)
			}
		}()
	}

	hasTextSource := strings.TrimSpace(req.Body) != "" || strings.TrimSpace(req.Summary) != "" ||
		strings.TrimSpace(req.Title) != "" || strings.TrimSpace(req.Changelog) != "" ||
		(strings.TrimSpace(req.Platform) != "" && strings.TrimSpace(req.ProjectID) != "")
	if hasTextSource {
		anWg.Add(1)
		go func() {
			defer anWg.Done()
			textIn := TextDepSourceInput{
				Platform:  strings.ToLower(strings.TrimSpace(req.Platform)),
				ProjectID: strings.TrimSpace(req.ProjectID),
				Title:     req.Title,
				Summary:   req.Summary,
				Body:      req.Body,
				Changelog: req.Changelog,
			}
			// Fetch full description from Modrinth/CurseForge if caller didn't pass Body
			if strings.TrimSpace(textIn.Body) == "" && textIn.Platform != "" && textIn.ProjectID != "" {
				fetched := FetchProjectTextMetadata(httpClient, db, cfKey, textIn.Platform, textIn.ProjectID)
				if textIn.Title == "" {
					textIn.Title = fetched.Title
				}
				if textIn.Summary == "" {
					textIn.Summary = fetched.Summary
				}
				textIn.Body = fetched.Body
			}
			tNodes, tUnres = ResolveTextDepCandidates(httpClient, db, cfKey, textIn)
			if len(tNodes) > 0 || len(tUnres) > 0 {
				textStatus = "checked"
			} else {
				textStatus = "empty"
			}
		}()
	}
	anWg.Wait()

	if mErr != nil {
		warnings = append(warnings, "Не удалось прочитать манифест .jar-файла — показаны данные API и анализа описания")
	} else if len(mNodes) > 0 {
		var mUnres []domain.UnresolvedDep
		apiNodes, mUnres = mergeAPIAndManifestNodes(apiNodes, mNodes, req.Platform, req.ProjectID)
		unresolved = append(unresolved, mUnres...)
	}
	if len(tNodes) > 0 {
		for _, tn := range tNodes {
			if req.ProjectID != "" && strings.EqualFold(tn.Platform, req.Platform) && tn.ProjectID == req.ProjectID {
				continue
			}
			matchedIdx := -1
			for i := range apiNodes {
				if nodesRepresentSameMod(apiNodes[i], tn) {
					matchedIdx = i
					break
				}
			}
			if matchedIdx >= 0 {
				// Official API / Manifest node takes precedence; attach text evidence as corroboration
				if apiNodes[matchedIdx].Evidence == "" && tn.Evidence != "" {
					apiNodes[matchedIdx].Evidence = tn.Evidence
				}
				continue
			}
			apiNodes = append(apiNodes, tn)
		}
		sort.SliceStable(apiNodes, func(i, j int) bool {
			pi, pj := textDepTypePriority(apiNodes[i].Type), textDepTypePriority(apiNodes[j].Type)
			if pi != pj {
				return pi > pj
			}
			return strings.ToLower(apiNodes[i].Title) < strings.ToLower(apiNodes[j].Title)
		})
	}
	if len(tUnres) > 0 {
		// Only include text unresolved items that weren't already satisfied by API or manifest
		for _, tu := range tUnres {
			alreadyHave := false
			for _, n := range apiNodes {
				if modIDMatchesCandidate(tu.ModID, n.Title, n.Slug) || domain.NormalizeCompactTitle(n.Title) == domain.NormalizeCompactTitle(tu.ModID) {
					alreadyHave = true
					break
				}
			}
			if !alreadyHave {
				unresolved = append(unresolved, tu)
			}
		}
	}

	nodes := apiNodes
	if maxDepth > 1 && len(nodes) > 0 {
		rootPath := map[string]bool{}
		if req.Platform != "" && req.ProjectID != "" {
			rootPath[strings.ToLower(req.Platform)+":"+req.ProjectID] = true
		}

		var resMu sync.Mutex
		var expandChildren func(list []domain.DepTreeNode, depth int, path map[string]bool)
		expandChildren = func(list []domain.DepTreeNode, depth int, path map[string]bool) {
			if depth >= maxDepth || httpClient.ReqCtx().Err() != nil {
				return
			}
			var wg sync.WaitGroup
			sem := make(chan struct{}, 4)
			for i := range list {
				if list[i].Type != "required" || !list[i].Resolved || (list[i].Source == "text" && list[i].Confidence == "low") || (list[i].Platform != "modrinth" && list[i].Platform != "curseforge") {
					continue
				}
				sk := list[i].SourceKey
				if path[sk] {
					continue
				}
				wg.Add(1)
				go func(idx int, node domain.DepTreeNode) {
					defer wg.Done()
					if !netx.AcquireSemCtx(httpClient.ReqCtx(), sem) {
						return
					}
					defer func() { <-sem }()

					childPath := make(map[string]bool, len(path)+1)
					for k, v := range path {
						childPath[k] = v
					}
					childPath[node.SourceKey] = true

					childRefs, childFileURL := fetchChildDependenciesForNode(httpClient, db, cfKey, req.MCVersion, req.Loader, node)
					filteredRefs := make([]domain.DependencyRef, 0, len(childRefs))
					for _, cr := range childRefs {
						csk := strings.ToLower(cr.Platform) + ":" + cr.ProjectID
						if cr.ProjectID != "" && childPath[csk] {
							continue
						}
						filteredRefs = append(filteredRefs, cr)
					}

					var childAPI []domain.DepTreeNode
					var childUnres []domain.UnresolvedDep
					if len(filteredRefs) > 0 {
						childAPI, childUnres, _ = resolveDirectDepNodes(httpClient, db, cfKey, filteredRefs, node.Platform, node.ProjectID)
					}
					if childFileURL != "" {
						if mdeps, _, mErr := InspectJarManifestDepsWithStatus(httpClient.ReqCtx(), httpClient, db, childFileURL); mErr == nil && len(mdeps) > 0 {
							var reqM []ManifestDep
							for _, md := range mdeps {
								if md.Type == "required" {
									reqM = append(reqM, md)
								}
							}
							if len(reqM) > 0 {
								mChildren := ResolveManifestDepsToNodes(httpClient, db, cfKey, node.Platform, reqM)
								var mu2 []domain.UnresolvedDep
								childAPI, mu2 = mergeAPIAndManifestNodes(childAPI, mChildren, node.Platform, node.ProjectID)
								childUnres = append(childUnres, mu2...)
							}
						}
					}

					safeChildren := make([]domain.DepTreeNode, 0, len(childAPI))
					for _, ch := range childAPI {
						if !childPath[ch.SourceKey] {
							safeChildren = append(safeChildren, ch)
						}
					}
					if len(childUnres) > 0 {
						resMu.Lock()
						unresolved = append(unresolved, childUnres...)
						resMu.Unlock()
					}
					if len(safeChildren) > 0 {
						expandChildren(safeChildren, depth+1, childPath)
						list[idx].Children = safeChildren
					}
				}(i, list[i])
			}
			wg.Wait()
		}

		expandChildren(nodes, 1, rootPath)
	}

	status := "complete"
	if len(nodes) == 0 && len(unresolved) == 0 {
		if len(errs) > 0 || manifestStatus == "failed" {
			status = "error"
		} else {
			status = "none"
		}
	} else if len(unresolved) > 0 || len(errs) > 0 || len(warnings) > 0 {
		status = "partial"
	}

	return domain.DepTreeResult{
		Tree:           nodes,
		Unresolved:     unresolved,
		Warnings:       warnings,
		Errors:         errs,
		ManifestStatus: manifestStatus,
		TextStatus:     textStatus,
		Status:         status,
	}
}

func canonicalVisitedKey(platform, projectID string) string {
	return strings.ToLower(strings.TrimSpace(platform)) + ":" + strings.ToLower(strings.TrimSpace(projectID))
}

func tryCrossPlatformDepFallback(
	httpClient *netx.HTTPClient,
	db *storage.Database,
	cfKey string,
	d domain.DependencyRef,
	partial *domain.CartItem,
	mcVer, loader string,
	trace *DepTrace,
) (*domain.CartItem, []domain.DependencyRef) {
	title := ""
	slug := ""
	if partial != nil {
		title = partial.Title
	}
	if title == "" && db != nil {
		if c, ok := db.GetCatalogItem(d.Platform + ":" + d.ProjectID); ok {
			title = c.Title
			slug = c.Slug
		}
	}
	if title == "" {
		return nil, nil
	}
	seed := domain.ProjectCard{
		GroupID:     d.Platform + ":" + d.ProjectID,
		ContentType: "mod",
		Title:       title,
		Slug:        slug,
		Sources: []domain.ProjectSource{{
			Platform:  d.Platform,
			SourceKey: d.Platform + ":" + d.ProjectID,
			ID:        d.ProjectID,
			Slug:      slug,
			Title:     title,
		}},
	}
	targetPlat := "modrinth"
	if d.Platform == "modrinth" {
		if cfKey == "" {
			return nil, nil
		}
		targetPlat = "curseforge"
	}
	merged, changed := catalog.DiscoverAndMergeCard(httpClient, db, cfKey, seed, []string{targetPlat})
	if !changed {
		return nil, nil
	}
	for _, s := range merged.Sources {
		if s.Platform != targetPlat || s.ID == "" {
			continue
		}
		trace.Logf("Кросс-платформенный поиск %s:%s (%s) -> %s:%s", d.Platform, d.ProjectID, title, targetPlat, s.ID)
		if targetPlat == "modrinth" {
			return catalog.ResolveMRDependencyReleasePinned(httpClient, s.ID, "", mcVer, loader)
		}
		return catalog.ResolveCFDependencyRelease(httpClient, cfKey, s.ID, mcVer, loader)
	}
	return nil, nil
}

func ResolveRequiredDepsFull(httpClient *netx.HTTPClient, db *storage.Database, cfKeyRaw string, req domain.DepResolveRequest) domain.DepResolveResult {
	cfKey := security.SanitizeCFKey(cfKeyRaw)
	trace := NewDepTrace(fmt.Sprintf("%s:%s (%s/%s)", req.Platform, req.ProjectID, req.MCVersion, req.Loader))
	trace.Logf("Старт разрешения зависимостей: api_refs=%d file_url=%t mc=%s loader=%s",
		len(req.Dependencies), strings.TrimSpace(req.FileURL) != "", req.MCVersion, req.Loader)

	errs := map[string]string{}
	warnings := []string{}
	unresolved := []domain.UnresolvedDep{}
	manifestStatus := "skipped"

	visited := map[string]struct{}{}
	existingSet := map[string]struct{}{}
	for _, k := range req.ExistingSourceKeys {
		norm := strings.ToLower(strings.TrimSpace(k))
		if norm != "" {
			visited[norm] = struct{}{}
			existingSet[norm] = struct{}{}
		}
	}
	if req.Platform != "" && req.ProjectID != "" {
		visited[canonicalVisitedKey(req.Platform, req.ProjectID)] = struct{}{}
	}
	seenFns := map[string]struct{}{}
	for _, fn := range req.ExistingFilenames {
		low := strings.ToLower(strings.TrimSpace(fn))
		if low != "" {
			seenFns[low] = struct{}{}
		}
	}

	type taggedRef struct {
		ref    domain.DependencyRef
		source string
	}
	allDirect := make([]taggedRef, 0, len(req.Dependencies)+6)
	for _, d := range req.Dependencies {
		allDirect = append(allDirect, taggedRef{ref: d, source: "api"})
	}

	if strings.TrimSpace(req.FileURL) != "" {
		mdeps, mStat, mErr := InspectJarManifestDepsWithStatus(httpClient.ReqCtx(), httpClient, db, req.FileURL)
		manifestStatus = mStat
		if mErr != nil {
			trace.Logf("Ошибка чтения манифеста .jar: %v", mErr)
			warnings = append(warnings, "Не удалось прочитать манифест .jar для проверки зависимостей")
		} else {
			trace.Logf("Манифест .jar проверен (status=%s): найдено записей=%d", mStat, len(mdeps))
			if len(mdeps) > 0 {
				var reqAndIncM []ManifestDep
				for _, md := range mdeps {
					if md.Type == "required" || md.Type == "incompatible" {
						reqAndIncM = append(reqAndIncM, md)
					}
				}
				if len(reqAndIncM) > 0 {
					mNodes := ResolveManifestDepsToNodes(httpClient, db, cfKey, req.Platform, reqAndIncM)
					for _, mn := range mNodes {
						if !mn.Resolved {
							if mn.Type == "required" {
								trace.Logf("Зависимость из манифеста не найдена в каталогах: %s (%s)", mn.ProjectID, mn.ManifestFile)
								warnings = append(warnings, fmt.Sprintf("Зависимость из манифеста «%s» не найдена автоматически — если мод не запустится, установите её вручную", mn.ProjectID))
								trace.RecordItem(domain.DepInstallItemStatus{
									ProjectID: mn.ProjectID,
									Platform:  mn.Platform,
									Title:     mn.Title,
									Type:      "required",
									Source:    "manifest",
									Stage:     "unresolved",
									Reason:    "Не найден проект в Modrinth/CurseForge",
								})
							}
							continue
						}
						trace.Logf("Манифест -> каталог: %s -> %s:%s (%s, %s)", mn.ManifestFile, mn.Platform, mn.ProjectID, mn.Title, mn.Type)
						for _, dr := range ManifestNodesToDepRefs([]domain.DepTreeNode{mn}) {
							allDirect = append(allDirect, taggedRef{ref: dr, source: "manifest"})
						}
					}
				}
			}
		}
	}

	// Step 1B: Intelligent textual analysis of Title/Summary/Body/Changelog (ALWAYS runs alongside API & manifest!)
	textStatus := "skipped"
	needsConfirm := []domain.DepTreeNode{}
	if strings.TrimSpace(req.Body) != "" || strings.TrimSpace(req.Summary) != "" ||
		strings.TrimSpace(req.Title) != "" || strings.TrimSpace(req.Changelog) != "" ||
		(strings.TrimSpace(req.Platform) != "" && strings.TrimSpace(req.ProjectID) != "") {
		textIn := TextDepSourceInput{
			Platform:  strings.ToLower(strings.TrimSpace(req.Platform)),
			ProjectID: strings.TrimSpace(req.ProjectID),
			Title:     req.Title,
			Summary:   req.Summary,
			Body:      req.Body,
			Changelog: req.Changelog,
		}
		if strings.TrimSpace(textIn.Body) == "" && textIn.Platform != "" && textIn.ProjectID != "" {
			fetched := FetchProjectTextMetadata(httpClient, db, cfKey, textIn.Platform, textIn.ProjectID)
			if textIn.Title == "" {
				textIn.Title = fetched.Title
			}
			if textIn.Summary == "" {
				textIn.Summary = fetched.Summary
			}
			textIn.Body = fetched.Body
		}
		tNodes, tUnres := ResolveTextDepCandidates(httpClient, db, cfKey, textIn)
		if len(tNodes) > 0 || len(tUnres) > 0 {
			textStatus = "checked"
		} else {
			textStatus = "empty"
		}
		trace.Logf("Интеллектуальный анализ описания (status=%s): найдено=%d неразрешено=%d", textStatus, len(tNodes), len(tUnres))
		for _, tn := range tNodes {
			if tn.Type != "required" || !tn.Resolved {
				continue
			}
			switch tn.Confidence {
			case "high":
				trace.Logf("Текст [high]: автодобавление %s:%s (%s) — %s", tn.Platform, tn.ProjectID, tn.Title, tn.Evidence)
				for _, dr := range ManifestNodesToDepRefs([]domain.DepTreeNode{tn}) {
					allDirect = append(allDirect, taggedRef{ref: dr, source: "text"})
				}
			case "medium":
				if req.AllowMediumConfidence {
					trace.Logf("Текст [medium, разрешено настройкой]: автодобавление %s:%s (%s)", tn.Platform, tn.ProjectID, tn.Title)
					for _, dr := range ManifestNodesToDepRefs([]domain.DepTreeNode{tn}) {
						allDirect = append(allDirect, taggedRef{ref: dr, source: "text"})
					}
				} else {
					ck := canonicalVisitedKey(tn.Platform, tn.ProjectID)
					if _, already := visited[ck]; !already {
						needsConfirm = append(needsConfirm, tn)
						trace.Logf("Текст [medium]: требуется подтверждение для %s:%s (%s)", tn.Platform, tn.ProjectID, tn.Title)
					}
				}
			default:
				// Low confidence is NEVER auto-installed
				trace.Logf("Текст [low]: пропуск автоустановки для %s:%s (%s)", tn.Platform, tn.ProjectID, tn.Title)
			}
		}
		for _, tu := range tUnres {
			if tu.Type == "required" {
				alreadyQueued := false
				for _, tr := range allDirect {
					if strings.EqualFold(tr.ref.ProjectID, tu.ModID) {
						alreadyQueued = true
						break
					}
				}
				if !alreadyQueued {
					unresolved = append(unresolved, tu)
				}
			}
		}
	}


	missingVids := []string{}
	for _, tr := range allDirect {
		d := tr.ref
		if strings.EqualFold(d.Platform, "modrinth") && strings.TrimSpace(d.ProjectID) == "" && strings.TrimSpace(d.VersionID) != "" {
			missingVids = append(missingVids, strings.TrimSpace(d.VersionID))
		}
	}
	var vidToPid map[string]string
	if len(missingVids) > 0 {
		vidToPid = catalog.ResolveModrinthVersionProjectIDs(httpClient, missingVids)
	}

	var frontier []taggedRef
	for _, tr := range allDirect {
		d := tr.ref
		dt := domain.NormalizeDepType(d.Type)
		pid := strings.TrimSpace(d.ProjectID)
		vid := strings.TrimSpace(d.VersionID)
		if pid == "" && vid != "" && vidToPid != nil {
			pid = vidToPid[vid]
		}
		plat := strings.ToLower(strings.TrimSpace(d.Platform))

		if dt == "incompatible" && pid != "" {
			ck := canonicalVisitedKey(plat, pid)
			if _, conflict := existingSet[ck]; conflict {
				msg := fmt.Sprintf("Обнаружена несовместимая зависимость уже в сборке: %s:%s", plat, pid)
				warnings = append(warnings, msg)
				trace.Logf("Конфликт несовместимости: %s", msg)
				trace.RecordItem(domain.DepInstallItemStatus{
					ProjectID: pid,
					Platform:  plat,
					Title:     pid,
					Type:      "incompatible",
					Source:    tr.source,
					Stage:     "incompatible",
					Reason:    "Несовместимый проект уже присутствует в сборке",
				})
			}
			continue
		}
		if dt != "required" {
			continue
		}
		if pid == "" {
			if vid != "" {
				unresolved = append(unresolved, domain.UnresolvedDep{
					ModID:    vid,
					Platform: plat,
					Type:     "required",
					Source:   tr.source,
					Reason:   "Не удалось определить проект по version_id",
				})
				trace.RecordItem(domain.DepInstallItemStatus{
					ProjectID: vid,
					Platform:  plat,
					Title:     vid,
					Type:      "required",
					Source:    tr.source,
					Stage:     "unresolved",
					Reason:    "Не удалось определить project_id по version_id",
				})
			}
			continue
		}
		ck := canonicalVisitedKey(plat, pid)
		if _, already := visited[ck]; already {
			if _, inExisting := existingSet[ck]; inExisting {
				trace.Logf("Пропуск %s:%s — уже установлен/добавлен в сборку", plat, pid)
				trace.RecordItem(domain.DepInstallItemStatus{
					ProjectID: pid,
					Platform:  plat,
					Title:     pid,
					Type:      "required",
					Source:    tr.source,
					Stage:     "already_installed",
					Reason:    "Уже присутствует в сборке",
				})
			}
			continue
		}
		visited[ck] = struct{}{}
		frontier = append(frontier, taggedRef{
			ref:    domain.DependencyRef{Platform: plat, ProjectID: pid, VersionID: vid, Type: "required"},
			source: tr.source,
		})
	}

	resolved := []domain.CartItem{}
	for depth := 0; depth < 4 && len(frontier) > 0 && len(resolved) < 30; depth++ {
		trace.Logf("Уровень рекурсии %d: кандидатов=%d", depth+1, len(frontier))
		var nextFrontier []taggedRef
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)

		for _, itemRef := range frontier {
			wg.Add(1)
			go func(tr taggedRef) {
				defer wg.Done()
				if !netx.AcquireSemCtx(httpClient.ReqCtx(), sem) {
					return
				}
				defer func() { <-sem }()

				d := tr.ref
				var ci *domain.CartItem
				var children []domain.DependencyRef
				usedFallback := false

				if d.Platform == "modrinth" {
					ci, children = catalog.ResolveMRDependencyReleasePinned(httpClient, d.ProjectID, d.VersionID, req.MCVersion, req.Loader)
					if (ci == nil || ci.DownloadURL == "") && cfKey != "" {
						if alt, altCh := tryCrossPlatformDepFallback(httpClient, db, cfKey, d, ci, req.MCVersion, req.Loader, trace); alt != nil && alt.DownloadURL != "" {
							ci, children, usedFallback = alt, altCh, true
						}
					}
				} else if d.Platform == "curseforge" {
					if cfKey != "" {
						ci, children = catalog.ResolveCFDependencyRelease(httpClient, cfKey, d.ProjectID, req.MCVersion, req.Loader)
					}
					if ci == nil || ci.DownloadURL == "" {
						if alt, altCh := tryCrossPlatformDepFallback(httpClient, db, cfKey, d, ci, req.MCVersion, req.Loader, trace); alt != nil && alt.DownloadURL != "" {
							ci, children, usedFallback = alt, altCh, true
						}
					}
					if ci == nil && cfKey == "" {
						mu.Lock()
						errs["curseforge"] = "API-ключ CurseForge не задан — часть зависимостей CurseForge недоступна"
						unresolved = append(unresolved, domain.UnresolvedDep{
							ModID:     d.ProjectID,
							Platform:  "curseforge",
							ProjectID: d.ProjectID,
							Type:      "required",
							Source:    tr.source,
							Reason:    "Требуется API-ключ CurseForge (и не найден аналог на Modrinth)",
						})
						mu.Unlock()
						trace.Logf("Ошибка %s:%s — отсутствует API-ключ CurseForge", d.Platform, d.ProjectID)
						trace.RecordItem(domain.DepInstallItemStatus{
							ProjectID: d.ProjectID,
							Platform:  "curseforge",
							Title:     "CurseForge #" + d.ProjectID,
							Type:      "required",
							Source:    tr.source,
							Stage:     "unresolved",
							Reason:    "Требуется API-ключ CurseForge",
						})
						return
					}
				}

				if ci == nil || (ci.Filename == "" && strings.TrimSpace(ci.DownloadURL) == "") {
					reason := fmt.Sprintf("Не найден совместимый файл для MC %s / %s", req.MCVersion, req.Loader)
					mu.Lock()
					unresolved = append(unresolved, domain.UnresolvedDep{
						ModID:     d.ProjectID,
						Platform:  d.Platform,
						ProjectID: d.ProjectID,
						Type:      "required",
						Source:    tr.source,
						Reason:    reason,
					})
					mu.Unlock()
					trace.Logf("Не удалось подобрать версию для %s:%s: %s", d.Platform, d.ProjectID, reason)
					trace.RecordItem(domain.DepInstallItemStatus{
						ProjectID: d.ProjectID,
						Platform:  d.Platform,
						Title:     d.ProjectID,
						Type:      "required",
						Source:    tr.source,
						Stage:     "unresolved",
						Reason:    reason,
					})
					return
				}

				if strings.TrimSpace(ci.DownloadURL) == "" {
					reason := fmt.Sprintf("Прямая ссылка на скачивание файла %s ограничена автором (%s)", ci.Filename, ci.Platform)
					mu.Lock()
					unresolved = append(unresolved, domain.UnresolvedDep{
						ModID:     ci.Title,
						Platform:  ci.Platform,
						ProjectID: d.ProjectID,
						Type:      "required",
						Source:    tr.source,
						Reason:    reason,
					})
					mu.Unlock()
					trace.Logf("Нет прямой ссылки для %s (%s): %s", ci.Title, ci.Filename, reason)
					trace.RecordItem(domain.DepInstallItemStatus{
						ProjectID: d.ProjectID,
						Platform:  ci.Platform,
						Title:     ci.Title,
						Type:      "required",
						Source:    tr.source,
						Stage:     "failed",
						Filename:  ci.Filename,
						Folder:    ci.Folder,
						Reason:    reason,
					})
					return
				}

				if usedFallback {
					ci.SourceKeys = domain.AppendUniqueCI(ci.SourceKeys, d.Platform+":"+d.ProjectID)
				}
				trace.Logf("Готово к загрузке [%s]: %s -> %s/%s (url=%s, source=%s)",
					ci.Platform, ci.Title, ci.Folder, ci.Filename, ci.DownloadURL, tr.source)
				trace.RecordItem(domain.DepInstallItemStatus{
					ProjectID:   d.ProjectID,
					Platform:    ci.Platform,
					Title:       ci.Title,
					Type:        "required",
					Source:      tr.source,
					Stage:       "ready",
					Filename:    ci.Filename,
					DownloadURL: ci.DownloadURL,
					Folder:      ci.Folder,
				})

				childTagged := make([]taggedRef, 0, len(children))
				for _, ch := range children {
					childTagged = append(childTagged, taggedRef{ref: ch, source: "api"})
				}
				if ci.DownloadURL != "" && ci.Folder == "mods" {
					if mdeps, _, mErr := InspectJarManifestDepsWithStatus(httpClient.ReqCtx(), httpClient, db, ci.DownloadURL); mErr == nil && len(mdeps) > 0 {
						var reqM []ManifestDep
						for _, md := range mdeps {
							if md.Type == "required" {
								reqM = append(reqM, md)
							}
						}
						if len(reqM) > 0 {
							mNodes := ResolveManifestDepsToNodes(httpClient, db, cfKey, ci.Platform, reqM)
							for _, dr := range ManifestNodesToDepRefs(mNodes) {
								childTagged = append(childTagged, taggedRef{ref: dr, source: "manifest"})
							}
						}
					}
				}

				mu.Lock()
				defer mu.Unlock()
				visited[strings.ToLower(ci.SourceKey)] = struct{}{}
				for _, chT := range childTagged {
					ch := chT.ref
					if domain.NormalizeDepType(ch.Type) == "required" && ch.ProjectID != "" {
						ck := canonicalVisitedKey(ch.Platform, ch.ProjectID)
						if _, ok := visited[ck]; !ok {
							visited[ck] = struct{}{}
							nextFrontier = append(nextFrontier, taggedRef{
								ref:    domain.DependencyRef{Platform: strings.ToLower(ch.Platform), ProjectID: ch.ProjectID, VersionID: ch.VersionID, Type: "required"},
								source: chT.source,
							})
						}
					}
				}
				fnLow := strings.ToLower(strings.TrimSpace(ci.Filename))
				if fnLow != "" {
					if _, dup := seenFns[fnLow]; !dup && len(resolved) < 30 {
						seenFns[fnLow] = struct{}{}
						resolved = append(resolved, *ci)
					}
				}
			}(itemRef)
		}
		wg.Wait()
		frontier = nextFrontier
	}

	status := "complete"
	if len(resolved) == 0 && len(unresolved) == 0 {
		if len(errs) > 0 || manifestStatus == "failed" {
			status = "error"
		} else {
			status = "none"
		}
	} else if len(unresolved) > 0 || len(errs) > 0 || len(warnings) > 0 {
		status = "partial"
	}

	trace.Logf("Завершено: status=%s resolved=%d unresolved=%d warnings=%d", status, len(resolved), len(unresolved), len(warnings))

	// Filter out any medium-confidence confirmation candidate that was already resolved via API/manifest
	filteredConfirm := make([]domain.DepTreeNode, 0, len(needsConfirm))
	for _, nc := range needsConfirm {
		ck := canonicalVisitedKey(nc.Platform, nc.ProjectID)
		if _, done := visited[ck]; done {
			continue
		}
		alreadyByTitle := false
		for _, rci := range resolved {
			if domain.NormalizeCompactTitle(rci.Title) == domain.NormalizeCompactTitle(nc.Title) {
				alreadyByTitle = true
				break
			}
		}
		if !alreadyByTitle {
			filteredConfirm = append(filteredConfirm, nc)
		}
	}

	return domain.DepResolveResult{
		Resolved:       resolved,
		NeedsConfirm:   filteredConfirm,
		Unresolved:     unresolved,
		Warnings:       warnings,
		Errors:         errs,
		ManifestStatus: manifestStatus,
		TextStatus:     textStatus,
		Status:         status,
		ItemsStatus:    trace.Items(),
		Diagnostics:    trace.Lines(),
	}
}

func ResolveRequiredDepsList(httpClient *netx.HTTPClient, cfKeyRaw string, deps []domain.DependencyRef, mcVer, loader string, existingKeys, existingFns []string) []domain.CartItem {
	res := ResolveRequiredDepsFull(httpClient, nil, cfKeyRaw, domain.DepResolveRequest{
		Dependencies:       deps,
		MCVersion:          mcVer,
		Loader:             loader,
		ExistingSourceKeys: existingKeys,
		ExistingFilenames:  existingFns,
	})
	return res.Resolved
}
