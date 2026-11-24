package catalog

import (
	"sort"
	"strings"
	"time"

	"modhub/internal/domain"
	"modhub/internal/netx"
	"modhub/internal/storage"
)

func LookupInstalledHashesModrinth(httpClient *netx.HTTPClient, hashes []string, mcVer, loader string) map[string]any {
	clean := []string{}
	seen := map[string]struct{}{}
	for _, h := range hashes {
		s := strings.ToLower(strings.TrimSpace(h))
		if len(s) == 40 {
			if _, ok := seen[s]; !ok {
				seen[s] = struct{}{}
				clean = append(clean, s)
				if len(clean) >= 500 {
					break
				}
			}
		}
	}
	if len(clean) == 0 {
		return map[string]any{}
	}
	var matched map[string]struct {
		ID            string `json:"id"`
		ProjectID     string `json:"project_id"`
		VersionNumber string `json:"version_number"`
		Files         []struct {
			Filename string `json:"filename"`
			Primary  bool   `json:"primary"`
		} `json:"files"`
	}
	if err := httpClient.PostJSON(domain.ModrinthAPI+"/version_files", map[string]any{
		"hashes":    clean,
		"algorithm": "sha1",
	}, nil, &matched); err != nil || len(matched) == 0 {
		return map[string]any{}
	}
	latestByHash := map[string]struct {
		ID            string `json:"id"`
		VersionNumber string `json:"version_number"`
	}{}
	if mcVer != "" && loader != "" {
		keys := make([]string, 0, len(matched))
		for k := range matched {
			keys = append(keys, k)
		}
		_ = httpClient.PostJSON(domain.ModrinthAPI+"/version_files/update", map[string]any{
			"hashes":        keys,
			"algorithm":     "sha1",
			"loaders":       []string{strings.ToLower(loader)},
			"game_versions": []string{mcVer},
		}, nil, &latestByHash)
	}
	byProject := map[string]any{}
	for h, ver := range matched {
		if ver.ProjectID == "" {
			continue
		}
		fname := ""
		for _, f := range ver.Files {
			if f.Primary || fname == "" {
				fname = f.Filename
			}
		}
		hasUpdate := false
		latestNum := ver.VersionNumber
		if lat, ok := latestByHash[h]; ok && lat.ID != "" && lat.ID != ver.ID {
			hasUpdate = true
			if lat.VersionNumber != "" {
				latestNum = lat.VersionNumber
			}
		}
		byProject[ver.ProjectID] = map[string]any{
			"installed_version_id":     ver.ID,
			"installed_version_number": ver.VersionNumber,
			"has_update":               hasUpdate,
			"latest_version_number":    latestNum,
			"filename":                 fname,
		}
	}
	return byProject
}

func BuildBrowserSnapshot(db *storage.Database, indexVersion int64) map[string]any {
	typesMap := map[string][]domain.ProjectCard{}
	mergedCounts := map[string]int{}
	for _, ct := range []string{"mod", "resourcepack", "shader", "datapack"} {
		raw := db.GetCatalogItemsByType(ct, 600)
		merged := MergeItems(raw, true)
		sort.SliceStable(merged, func(i, j int) bool { return merged[i].Downloads > merged[j].Downloads })
		typesMap[ct] = merged
		mergedCounts[ct] = len(merged)
	}
	return map[string]any{
		"ok":              true,
		"delta":           false,
		"index_version":   indexVersion,
		"generated_at":    float64(time.Now().Unix()),
		"counts":          mergedCounts,
		"platform_counts": db.PlatformCountsByType(),
		"types":           typesMap,
	}
}
