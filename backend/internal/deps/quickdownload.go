package deps

import (
	"sort"
	"strings"
	"sync"

	"modhub/internal/catalog"
	"modhub/internal/domain"
	"modhub/internal/netx"
	"modhub/internal/security"
	"modhub/internal/storage"
)

func pickBestRelease(all []domain.VersionFile, mcVer, loader string, usesLoader bool) *domain.VersionFile {
	if len(all) == 0 {
		return nil
	}
	filter := func(reqMC, reqLD string) []domain.VersionFile {
		var out []domain.VersionFile
		for _, v := range all {
			if reqMC != "" {
				hasMC := false
				for _, g := range v.GameVersions {
					if g == reqMC {
						hasMC = true
						break
					}
				}
				if !hasMC {
					continue
				}
			}
			if usesLoader && reqLD != "" {
				hasLD := false
				for _, l := range v.Loaders {
					if strings.EqualFold(l, reqLD) {
						hasLD = true
						break
					}
				}
				if !hasLD {
					continue
				}
			}
			out = append(out, v)
		}
		return out
	}

	var pools [][]domain.VersionFile
	if mcVer != "" && loader != "" {
		pools = append(pools, filter(mcVer, loader))
	}
	if mcVer != "" {
		pools = append(pools, filter(mcVer, ""))
	}
	if loader != "" && mcVer == "" {
		pools = append(pools, filter("", loader))
	}
	pools = append(pools, append([]domain.VersionFile{}, all...))

	var fallback *domain.VersionFile
	for _, pool := range pools {
		if len(pool) == 0 {
			continue
		}
		sort.SliceStable(pool, func(i, j int) bool {
			return domain.CompareVersionRelease(pool[i], pool[j])
		})
		top := pool[0]
		if fallback == nil {
			cp := top
			fallback = &cp
		}
		if strings.ToLower(top.ReleaseType) == "release" {
			cp := top
			return &cp
		}
	}
	return fallback
}

func fetchAllSourceVersions(httpClient *netx.HTTPClient, cfKey string, item domain.ProjectCard, mcVer, loader string) []domain.VersionFile {
	ct := item.ContentType
	if ct == "" {
		ct = "mod"
	}
	var out []domain.VersionFile
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, src := range item.Sources {
		wg.Add(1)
		go func(s domain.ProjectSource) {
			defer wg.Done()
			var list []domain.VersionFile
			if s.Platform == "modrinth" {
				list = catalog.GetModrinthVersions(httpClient, s.ID, s.Slug, ct, mcVer, loader)
			} else if s.Platform == "curseforge" && cfKey != "" {
				list = catalog.GetCFFiles(httpClient, cfKey, s.ID, s.URL, ct, mcVer, loader, 30)
			}
			if len(list) > 0 {
				mu.Lock()
				out = append(out, list...)
				mu.Unlock()
			}
		}(src)
	}
	wg.Wait()
	return out
}

func ResolveQuickFile(httpClient *netx.HTTPClient, db *storage.Database, cfKeyRaw string, req domain.QuickFileRequest) (domain.QuickFileResponse, domain.ProjectCard) {
	return ResolveQuickFileWithPolicy(httpClient, db, cfKeyRaw, false, req)
}

func ResolveQuickFileWithPolicy(httpClient *netx.HTTPClient, db *storage.Database, cfKeyRaw string, allowMediumDeps bool, req domain.QuickFileRequest) (domain.QuickFileResponse, domain.ProjectCard) {
	cfKey := security.SanitizeCFKey(cfKeyRaw)
	// Auto-discover cross-platform sibling so quick-install checks BOTH Modrinth & CurseForge and grabs the newest release!
	if enriched, changed := catalog.DiscoverAndMergeCard(httpClient, db, cfKey, req.Item, nil); changed {
		req.Item = enriched
	}
	ct := req.Item.ContentType
	if ct == "" {
		ct = "mod"
	}
	meta := domain.GetContentTypeMeta(ct)
	effLoader := ""
	if meta.UsesLoader {
		effLoader = strings.ToLower(strings.TrimSpace(req.Loader))
	}
	mcVer := strings.TrimSpace(req.MCVersion)

	allVers := fetchAllSourceVersions(httpClient, cfKey, req.Item, mcVer, effLoader)
	best := pickBestRelease(allVers, mcVer, effLoader, meta.UsesLoader)

	if (best == nil || strings.ToLower(best.ReleaseType) != "release") && effLoader != "" {
		allVers = append(allVers, fetchAllSourceVersions(httpClient, cfKey, req.Item, mcVer, "")...)
		best = pickBestRelease(allVers, mcVer, effLoader, meta.UsesLoader)
	}
	if (best == nil || strings.ToLower(best.ReleaseType) != "release") && mcVer != "" {
		allVers = append(allVers, fetchAllSourceVersions(httpClient, cfKey, req.Item, "", "")...)
		best = pickBestRelease(allVers, mcVer, effLoader, meta.UsesLoader)
	}

	// Cross-enrich missing dependencies across Modrinth & CurseForge builds before resolving!
	if len(allVers) > 1 {
		allVers = catalog.CrossEnrichVersionDependencies(allVers)
		if best != nil && len(best.Dependencies) == 0 {
			for _, v := range allVers {
				if v.Platform == best.Platform && v.VersionID == best.VersionID && len(v.Dependencies) > 0 {
					best.Dependencies = v.Dependencies
					break
				}
			}
		}
	}

	if best == nil {
		return domain.QuickFileResponse{OK: true, File: nil, ResolvedDeps: []domain.CartItem{}, Reason: "Нет доступного файла для загрузки"}, req.Item
	}

	chosenLoader := ""
	if effLoader != "" {
		for _, l := range best.Loaders {
			if strings.EqualFold(l, effLoader) {
				chosenLoader = effLoader
				break
			}
		}
	}
	if chosenLoader == "" && len(best.Loaders) > 0 {
		chosenLoader = best.Loaders[0]
	}

	chosenMC := ""
	if mcVer != "" {
		for _, g := range best.GameVersions {
			if g == mcVer {
				chosenMC = mcVer
				break
			}
		}
	}
	if chosenMC == "" && len(best.GameVersions) > 0 {
		bestT := [3]int{}
		for _, g := range best.GameVersions {
			t := domain.ParseMCTuple(g)
			if domain.MCTupleGreater(t, bestT) {
				bestT = t
				chosenMC = g
			}
		}
	}

	allSKeys := make([]string, 0, len(req.Item.Sources))
	for _, s := range req.Item.Sources {
		if s.SourceKey != "" {
			allSKeys = append(allSKeys, s.SourceKey)
		}
	}

	mainFile := &domain.CartItem{
		Title:        req.Item.Title,
		IconURL:      req.Item.IconURL,
		Platform:     best.Platform,
		SourceKey:    best.SourceKey,
		SourceKeys:   allSKeys,
		GroupID:      req.Item.GroupID,
		Filename:     best.Filename,
		FileSize:     best.FileSize,
		DownloadURL:  best.DownloadURL,
		FallbackURL:  best.FallbackURL,
		IsAvailable:  best.IsAvailable,
		Folder:       meta.Folder,
		Loaders:      best.Loaders,
		GameVersions: best.GameVersions,
		MCVersion:    chosenMC,
		Loader:       chosenLoader,
		AutoDepFor:   []string{},
	}

	requiredDeps := make([]domain.DependencyRef, 0)
	for _, d := range best.Dependencies {
		if domain.NormalizeDepType(d.Type) == "required" && (d.ProjectID != "" || d.VersionID != "") {
			requiredDeps = append(requiredDeps, d)
		}
	}

	depRes := domain.DepResolveResult{
		Resolved:       []domain.CartItem{},
		Unresolved:     []domain.UnresolvedDep{},
		Warnings:       []string{},
		Errors:         map[string]string{},
		ManifestStatus: "skipped",
		Status:         "none",
	}
	if req.IncludeDeps {
		existingKeys := make([]string, 0, len(req.ExistingSourceKeys)+len(allSKeys))
		existingKeys = append(existingKeys, req.ExistingSourceKeys...)
		existingKeys = append(existingKeys, allSKeys...)
		existingFns := make([]string, 0, len(req.ExistingFilenames)+1)
		existingFns = append(existingFns, mainFile.Filename)
		existingFns = append(existingFns, req.ExistingFilenames...)
		fileURL := ""
		if ct == "mod" {
			fileURL = best.DownloadURL
		}
		depRes = ResolveRequiredDepsFull(httpClient, db, cfKey, domain.DepResolveRequest{
			Dependencies:          requiredDeps,
			MCVersion:             chosenMC,
			Loader:                chosenLoader,
			FileURL:               fileURL,
			Platform:              best.Platform,
			ProjectID:             best.ModID,
			Title:                 req.Item.Title,
			Summary:               req.Item.Summary,
			Changelog:             best.Changelog,
			AllowMediumConfidence: allowMediumDeps,
			ExistingSourceKeys:    existingKeys,
			ExistingFilenames:     existingFns,
		})
	}

	return domain.QuickFileResponse{
		OK:             true,
		File:           mainFile,
		ResolvedDeps:   depRes.Resolved,
		UnresolvedDeps:   depRes.Unresolved,
		NeedsConfirmDeps: depRes.NeedsConfirm,
		RequiredDeps:   requiredDeps,
		DepWarnings:    depRes.Warnings,
		DepErrors:      depRes.Errors,
		DepStatus:      depRes.Status,
		ManifestStatus: depRes.ManifestStatus,
	}, req.Item
}
