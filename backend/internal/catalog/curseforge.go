package catalog

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"modhub/internal/domain"
	"modhub/internal/netx"
	"modhub/internal/security"
)

var reMCVer = regexp.MustCompile(`^(?:1|2\d)\.\d+(?:\.\d+)?$`)

type CFMod struct {
	ID            int    `json:"id"`
	ClassID       int    `json:"classId"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	Summary       string `json:"summary"`
	DownloadCount int64  `json:"downloadCount"`
	DateCreated   string `json:"dateCreated"`
	DateModified  string `json:"dateModified"`
	DateReleased  string `json:"dateReleased"`
	Links         struct {
		WebsiteURL string `json:"websiteUrl"`
		SourceURL  string `json:"sourceUrl"`
		IssuesURL  string `json:"issuesUrl"`
		WikiURL    string `json:"wikiUrl"`
	} `json:"links"`
	Logo struct {
		URL          string `json:"url"`
		ThumbnailURL string `json:"thumbnailUrl"`
	} `json:"logo"`
	Authors []struct {
		Name string `json:"name"`
	} `json:"authors"`
	Categories []struct {
		Name string `json:"name"`
	} `json:"categories"`
	Screenshots []struct {
		URL         string `json:"url"`
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"screenshots"`
	LatestFilesIndexes []struct {
		GameVersion string `json:"gameVersion"`
		ModLoader   int    `json:"modLoader"`
	} `json:"latestFilesIndexes"`
	LatestFiles []CFFile `json:"latestFiles"`
}

type CFFile struct {
	ID            int      `json:"id"`
	DisplayName   string   `json:"displayName"`
	FileName      string   `json:"fileName"`
	ReleaseType   int      `json:"releaseType"`
	FileDate      string   `json:"fileDate"`
	FileLength    int64    `json:"fileLength"`
	DownloadCount int64    `json:"downloadCount"`
	DownloadURL   string   `json:"downloadUrl"`
	GameVersions  []string `json:"gameVersions"`
	Dependencies  []struct {
		ModID        int `json:"modId"`
		RelationType int `json:"relationType"`
	} `json:"dependencies"`
}

func CFRelationMeta(rt int) (string, string) {
	switch rt {
	case 3:
		return "required", "Обязательная"
	case 2:
		return "optional", "Опциональная"
	case 1:
		return "embedded", "Встроенная"
	case 4:
		return "tool", "Инструмент"
	case 5:
		return "incompatible", "Несовместимо"
	case 6:
		return "include", "Включено"
	default:
		return "dependent", "Зависимый"
	}
}

func extractCFLoadersAndVersions(m CFMod) ([]string, []string) {
	loaders := []string{}
	versions := []string{}
	for _, idx := range m.LatestFilesIndexes {
		if idx.GameVersion != "" {
			versions = domain.AppendUniqueCI(versions, idx.GameVersion)
		}
		if name := domain.CFLoaderIDToName(idx.ModLoader); name != "" {
			loaders = domain.AppendUniqueCI(loaders, name)
		}
	}
	for _, lf := range m.LatestFiles {
		for _, gv := range lf.GameVersions {
			low := strings.ToLower(gv)
			if domain.IsKnownLoader(low) {
				loaders = domain.AppendUniqueCI(loaders, low)
			} else if reMCVer.MatchString(gv) {
				versions = domain.AppendUniqueCI(versions, gv)
			}
		}
	}
	return loaders, versions
}

func extractCFAllDepRels(m CFMod, mcVer, loader string) map[int]map[int]bool {
	rels := map[int]map[int]bool{}
	collect := func(files []CFFile) {
		for _, f := range files {
			for _, d := range f.Dependencies {
				if d.ModID <= 0 || d.ModID == m.ID || d.RelationType < 1 || d.RelationType > 6 {
					continue
				}
				if _, ok := rels[d.ModID]; !ok {
					rels[d.ModID] = map[int]bool{}
				}
				rels[d.ModID][d.RelationType] = true
			}
		}
	}
	if mcVer != "" || loader != "" {
		matched := make([]CFFile, 0, len(m.LatestFiles))
		for _, f := range m.LatestFiles {
			mcOK := mcVer == ""
			ldOK := loader == ""
			for _, gv := range f.GameVersions {
				if !mcOK && gv == mcVer {
					mcOK = true
				}
				if !ldOK && strings.EqualFold(gv, loader) {
					ldOK = true
				}
			}
			if mcOK && ldOK {
				matched = append(matched, f)
			}
		}
		if len(matched) > 0 {
			collect(matched)
			return rels
		}
	}
	collect(m.LatestFiles)
	return rels
}

func pickCFVerifiedRelType(relSet map[int]bool, wantedRel int) (int, bool) {
	if len(relSet) == 0 {
		return 0, false
	}
	if wantedRel > 0 {
		if relSet[wantedRel] {
			return wantedRel, true
		}
		return 0, false
	}
	prio := map[int]int{3: 6, 5: 5, 2: 4, 1: 3, 6: 2, 4: 1}
	best := 0
	bestP := 0
	for rt := range relSet {
		if p := prio[rt]; p > bestP {
			bestP = p
			best = rt
		}
	}
	return best, best > 0
}

func FormatCFModCard(m CFMod, ctHint string, allowModpacks bool) *domain.RawProjectCard {
	if m.ID <= 0 {
		return nil
	}
	effCT := domain.ClassifyCFClassID(m.ClassID)
	if effCT == "" {
		if m.ClassID > 0 {
			// Explicitly unsupported CurseForge class (e.g. Worlds=17, Plugins=5, Customization=4546)
			return nil
		}
		effCT = domain.ClassifyCFClassID(domain.GetContentTypeMeta(ctHint).CFClassID)
	}
	if effCT == "" {
		return nil
	}
	if effCT == "modpack" && !allowModpacks {
		return nil
	}
	urlSec := domain.GetContentTypeMeta(effCT).CFURLSlug
	if effCT == "modpack" {
		urlSec = "modpacks"
	}
	mid := strconv.Itoa(m.ID)
	slug := m.Slug
	if slug == "" {
		slug = mid
	}
	title := m.Name
	if title == "" {
		title = slug
	}
	loaders, versions := extractCFLoadersAndVersions(m)
	if effCT == "shader" || effCT == "resourcepack" || effCT == "modpack" {
		loaders = []string{}
	}
	authors := []string{}
	for _, a := range m.Authors {
		if a.Name != "" {
			authors = append(authors, a.Name)
		}
	}
	cats := []string{}
	for _, c := range m.Categories {
		if c.Name != "" {
			cats = append(cats, c.Name)
		}
	}
	icon := m.Logo.URL
	if icon == "" {
		icon = m.Logo.ThumbnailURL
	}
	webURL := m.Links.WebsiteURL
	if webURL == "" {
		webURL = fmt.Sprintf("https://www.curseforge.com/minecraft/%s/%s", urlSec, slug)
	}
	return &domain.RawProjectCard{
		Platform:     "curseforge",
		SourceKey:    "curseforge:" + mid,
		ID:           mid,
		Slug:         slug,
		ClassID:      m.ClassID,
		ContentType:  effCT,
		Title:        title,
		Summary:      m.Summary,
		Authors:      authors,
		IconURL:      icon,
		Downloads:    m.DownloadCount,
		DateModified: m.DateModified,
		DateCreated:  func() string { if m.DateReleased != "" { return m.DateReleased }; return m.DateCreated }(),
		Status:       "active",
		Loaders:      loaders,
		GameVersions: versions,
		Categories:   cats,
		URL:          webURL,
		Links: domain.ProjectLinks{
			SourceURL: m.Links.SourceURL,
			IssuesURL: m.Links.IssuesURL,
			WikiURL:   m.Links.WikiURL,
		},
	}
}

func TestCurseForgeKey(httpClient *netx.HTTPClient, rawKey string) error {
	key := security.SanitizeCFKey(rawKey)
	if key == "" {
		return fmt.Errorf("API-ключ пуст")
	}
	u := fmt.Sprintf("%s/mods/search?gameId=%d&classId=6&pageSize=1", domain.CurseForgeAPI, domain.CurseForgeGameID)
	var dummy map[string]any
	return httpClient.GetJSON(u, map[string]string{"x-api-key": key}, 0, &dummy)
}

func SearchCurseForge(
	httpClient *netx.HTTPClient,
	apiKey, query, ct, mcVer, loader, sortMode string,
	limit, offset int,
) ([]domain.RawProjectCard, int64, error) {
	key := security.SanitizeCFKey(apiKey)
	if key == "" {
		return nil, 0, nil
	}
	meta := domain.GetContentTypeMeta(ct)
	sortField := 6
	switch sortMode {
	case "relevance":
		sortField = 2
	case "updated":
		sortField = 3
	case "newest":
		sortField = 11
	}
	if limit < 1 {
		limit = 24
	}
	if limit > 50 {
		limit = 50
	}
	q := url.Values{}
	q.Set("gameId", strconv.Itoa(domain.CurseForgeGameID))
	q.Set("classId", strconv.Itoa(meta.CFClassID))
	q.Set("sortField", strconv.Itoa(sortField))
	q.Set("sortOrder", "desc")
	q.Set("pageSize", strconv.Itoa(limit))
	q.Set("index", strconv.Itoa(offset))
	if query != "" {
		q.Set("searchFilter", query)
	}
	if mcVer != "" {
		q.Set("gameVersion", mcVer)
	}
	if meta.UsesLoader && loader != "" {
		if lid := domain.CFLoaderNameToID(loader); lid > 0 {
			q.Set("modLoaderType", strconv.Itoa(lid))
		}
	}

	var resp struct {
		Data       []CFMod `json:"data"`
		Pagination struct {
			TotalCount int64 `json:"totalCount"`
		} `json:"pagination"`
	}
	if err := httpClient.GetJSON(domain.CurseForgeAPI+"/mods/search?"+q.Encode(), map[string]string{"x-api-key": key}, 90, &resp); err != nil {
		return nil, 0, err
	}
	out := make([]domain.RawProjectCard, 0, len(resp.Data))
	for _, m := range resp.Data {
		if c := FormatCFModCard(m, ct, false); c != nil {
			out = append(out, *c)
		}
	}
	return out, resp.Pagination.TotalCount, nil
}

func FetchCFExactSlug(httpClient *netx.HTTPClient, apiKey, slug, ct string) *domain.RawProjectCard {
	key := security.SanitizeCFKey(apiKey)
	if key == "" || slug == "" {
		return nil
	}
	meta := domain.GetContentTypeMeta(ct)
	q := url.Values{}
	q.Set("gameId", strconv.Itoa(domain.CurseForgeGameID))
	q.Set("classId", strconv.Itoa(meta.CFClassID))
	q.Set("slug", slug)
	var resp struct {
		Data []CFMod `json:"data"`
	}
	if err := httpClient.GetJSON(domain.CurseForgeAPI+"/mods/search?"+q.Encode(), map[string]string{"x-api-key": key}, 300, &resp); err != nil || len(resp.Data) == 0 {
		return nil
	}
	return FormatCFModCard(resp.Data[0], ct, false)
}

func GetCFModsBatch(httpClient *netx.HTTPClient, apiKey string, modIDs []int) map[int]CFMod {
	key := security.SanitizeCFKey(apiKey)
	out := map[int]CFMod{}
	if key == "" || len(modIDs) == 0 {
		return out
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < len(modIDs); i += 50 {
		end := i + 50
		if end > len(modIDs) {
			end = len(modIDs)
		}
		chunk := modIDs[i:end]
		wg.Add(1)
		go func(ids []int) {
			defer wg.Done()
			var resp struct {
				Data []CFMod `json:"data"`
			}
			err := httpClient.PostJSON(
				domain.CurseForgeAPI+"/mods",
				map[string]any{"modIds": ids},
				map[string]string{"x-api-key": key},
				&resp,
			)
			if err == nil {
				mu.Lock()
				for _, m := range resp.Data {
					if m.ID > 0 {
						out[m.ID] = m
					}
				}
				mu.Unlock()
			}
		}(chunk)
	}
	wg.Wait()
	return out
}

func parseCFFile(f CFFile, modID, webURL, ct string) domain.VersionFile {
	loaders := []string{}
	gvs := []string{}
	for _, gv := range f.GameVersions {
		low := strings.ToLower(gv)
		if domain.IsKnownLoader(low) {
			loaders = domain.AppendUniqueCI(loaders, low)
		} else if reMCVer.MatchString(gv) {
			gvs = domain.AppendUniqueCI(gvs, gv)
		}
	}
	if ct == "shader" || ct == "resourcepack" {
		loaders = []string{}
	}
	rt := "alpha"
	switch f.ReleaseType {
	case 1:
		rt = "release"
	case 2:
		rt = "beta"
	}
	deps := make([]domain.DependencyRef, 0, len(f.Dependencies))
	for _, d := range f.Dependencies {
		if d.ModID <= 0 {
			continue
		}
		dpid := strconv.Itoa(d.ModID)
		if dpid == modID {
			continue
		}
		dt := ""
		switch d.RelationType {
		case 3:
			dt = "required"
		case 2:
			dt = "optional"
		case 5:
			dt = "incompatible"
		case 1:
			dt = "embedded"
		case 4:
			dt = "tool"
		case 6:
			dt = "include"
		}
		if dt != "" {
			deps = append(deps, domain.DependencyRef{
				Platform:  "curseforge",
				ProjectID: dpid,
				Type:      dt,
			})
		}
	}
	deps = domain.DedupeDependencyRefs(deps, modID)
	fallback := fmt.Sprintf("https://www.curseforge.com/projects/%s/files/%d", modID, f.ID)
	if webURL != "" {
		fallback = fmt.Sprintf("%s/files/%d", strings.TrimRight(webURL, "/"), f.ID)
	}
	name := f.DisplayName
	if name == "" {
		name = f.FileName
	}
	return domain.VersionFile{
		Platform:      "curseforge",
		SourceKey:     "curseforge:" + modID,
		VersionID:     strconv.Itoa(f.ID),
		ModID:         modID,
		Name:          name,
		VersionNumber: f.DisplayName,
		ReleaseType:   rt,
		DatePublished: f.FileDate,
		Downloads:     f.DownloadCount,
		Loaders:       loaders,
		GameVersions:  gvs,
		Filename:      f.FileName,
		FileSize:      f.FileLength,
		DownloadURL:   f.DownloadURL,
		FallbackURL:   fallback,
		IsAvailable:   f.DownloadURL != "",
		Dependencies:  deps,
	}
}

func GetCFFilesMultiPageWithErr(httpClient *netx.HTTPClient, apiKey, modID, webURL, ct, mcVer, loader string) ([]domain.VersionFile, error) {
	key := security.SanitizeCFKey(apiKey)
	if key == "" || modID == "" {
		return nil, nil
	}
	fetchPage := func(index int) ([]domain.VersionFile, int, error) {
		q := url.Values{}
		q.Set("pageSize", "50")
		q.Set("index", strconv.Itoa(index))
		if mcVer != "" {
			q.Set("gameVersion", mcVer)
		}
		if loader != "" {
			if lid := domain.CFLoaderNameToID(loader); lid > 0 {
				q.Set("modLoaderType", strconv.Itoa(lid))
			}
		}
		var resp struct {
			Data       []CFFile `json:"data"`
			Pagination struct {
				TotalCount int `json:"totalCount"`
			} `json:"pagination"`
		}
		u := fmt.Sprintf("%s/mods/%s/files?%s", domain.CurseForgeAPI, url.PathEscape(modID), q.Encode())
		if err := httpClient.GetJSON(u, map[string]string{"x-api-key": key}, 240, &resp); err != nil {
			return nil, 0, err
		}
		out := make([]domain.VersionFile, 0, len(resp.Data))
		for _, f := range resp.Data {
			out = append(out, parseCFFile(f, modID, webURL, ct))
		}
		return out, resp.Pagination.TotalCount, nil
	}

	var (
		p1    []domain.VersionFile
		p2    []domain.VersionFile
		total int
		err1  error
		wg    sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		p1, total, err1 = fetchPage(0)
	}()
	go func() {
		defer wg.Done()
		p2, _, _ = fetchPage(50)
	}()
	wg.Wait()
	if total <= 50 || len(p1) < 50 || len(p2) == 0 {
		return p1, err1
	}
	return append(p1, p2...), err1
}

func GetCFFilesMultiPage(httpClient *netx.HTTPClient, apiKey, modID, webURL, ct, mcVer, loader string) []domain.VersionFile {
	files, _ := GetCFFilesMultiPageWithErr(httpClient, apiKey, modID, webURL, ct, mcVer, loader)
	return files
}

func GetCFFiles(httpClient *netx.HTTPClient, apiKey, modID, webURL, ct, mcVer, loader string, pageSize int) []domain.VersionFile {
	key := security.SanitizeCFKey(apiKey)
	if key == "" || modID == "" {
		return nil
	}
	if pageSize < 1 {
		pageSize = 30
	}
	if pageSize > 50 {
		pageSize = 50
	}
	q := url.Values{}
	q.Set("pageSize", strconv.Itoa(pageSize))
	if mcVer != "" {
		q.Set("gameVersion", mcVer)
	}
	if loader != "" {
		if lid := domain.CFLoaderNameToID(loader); lid > 0 {
			q.Set("modLoaderType", strconv.Itoa(lid))
		}
	}
	var resp struct {
		Data []CFFile `json:"data"`
	}
	u := fmt.Sprintf("%s/mods/%s/files?%s", domain.CurseForgeAPI, url.PathEscape(modID), q.Encode())
	if err := httpClient.GetJSON(u, map[string]string{"x-api-key": key}, 240, &resp); err != nil {
		return nil
	}
	out := make([]domain.VersionFile, 0, len(resp.Data))
	for _, f := range resp.Data {
		out = append(out, parseCFFile(f, modID, webURL, ct))
	}
	return out
}

func GetCFProjectMeta(httpClient *netx.HTTPClient, apiKey, modID, webURL, ct string) (string, []domain.GalleryItem, domain.ProjectLinks, []domain.VersionFile, error) {
	key := security.SanitizeCFKey(apiKey)
	if key == "" {
		return "", nil, domain.ProjectLinks{}, nil, nil
	}
	hdr := map[string]string{"x-api-key": key}
	var (
		modWrap struct {
			Data CFMod `json:"data"`
		}
		descWrap struct {
			Data string `json:"data"`
		}
		files    []domain.VersionFile
		modErr   error
		descErr  error
		filesErr error
		wg       sync.WaitGroup
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		modErr = httpClient.GetJSON(fmt.Sprintf("%s/mods/%s", domain.CurseForgeAPI, url.PathEscape(modID)), hdr, 300, &modWrap)
	}()
	go func() {
		defer wg.Done()
		descErr = httpClient.GetJSON(fmt.Sprintf("%s/mods/%s/description", domain.CurseForgeAPI, url.PathEscape(modID)), hdr, 900, &descWrap)
	}()
	go func() {
		defer wg.Done()
		files, filesErr = GetCFFilesMultiPageWithErr(httpClient, key, modID, webURL, ct, "", "")
	}()
	wg.Wait()

	gal := make([]domain.GalleryItem, 0, len(modWrap.Data.Screenshots))
	for _, sc := range modWrap.Data.Screenshots {
		if sc.URL != "" {
			gal = append(gal, domain.GalleryItem{URL: sc.URL, Title: sc.Title, Description: sc.Description})
		}
	}
	links := domain.ProjectLinks{
		SourceURL: modWrap.Data.Links.SourceURL,
		IssuesURL: modWrap.Data.Links.IssuesURL,
		WikiURL:   modWrap.Data.Links.WikiURL,
	}
	var retErr error
	if modErr != nil {
		retErr = modErr
	} else if filesErr != nil {
		retErr = filesErr
	} else if descErr != nil {
		retErr = descErr
	}
	return descWrap.Data, gal, links, files, retErr
}

func cfClassIDToContentType(classID int) string {
	switch classID {
	case 12:
		return "resourcepack"
	case 6552:
		return "shader"
	case 6945:
		return "datapack"
	default:
		return "mod"
	}
}

func ResolveCFDependencyRelease(httpClient *netx.HTTPClient, apiKey, modID, mcVer, loader string) (*domain.CartItem, []domain.DependencyRef) {
	key := security.SanitizeCFKey(apiKey)
	if key == "" || modID == "" {
		return nil, nil
	}
	var mWrap struct {
		Data CFMod `json:"data"`
	}
	if err := httpClient.GetJSON(fmt.Sprintf("%s/mods/%s", domain.CurseForgeAPI, url.PathEscape(modID)), map[string]string{"x-api-key": key}, 300, &mWrap); err != nil {
		return nil, nil
	}
	ct := cfClassIDToContentType(mWrap.Data.ClassID)
	card := FormatCFModCard(mWrap.Data, ct, false)
	if card == nil {
		return nil, nil
	}
	usesLoader := domain.GetContentTypeMeta(ct).UsesLoader
	effLoader := ""
	if usesLoader {
		effLoader = strings.ToLower(strings.TrimSpace(loader))
	}

	var best *domain.VersionFile
	if mcVer != "" && effLoader != "" {
		files := GetCFFiles(httpClient, key, modID, card.URL, ct, mcVer, effLoader, 30)
		if len(files) > 0 {
			best, _ = domain.PickBestDependencyVersion(files, mcVer, effLoader, usesLoader)
		}
	}
	if best == nil && mcVer != "" {
		files := GetCFFiles(httpClient, key, modID, card.URL, ct, mcVer, "", 40)
		if len(files) > 0 {
			best, _ = domain.PickBestDependencyVersion(files, mcVer, effLoader, usesLoader)
		}
	}
	if best == nil {
		files := GetCFFiles(httpClient, key, modID, card.URL, ct, "", effLoader, 50)
		if len(files) == 0 && effLoader != "" {
			files = GetCFFiles(httpClient, key, modID, card.URL, ct, "", "", 50)
		}
		if len(files) > 0 {
			best, _ = domain.PickBestDependencyVersion(files, mcVer, effLoader, usesLoader)
		}
	}
	if best == nil {
		// Return lightweight metadata partial so cross-platform Modrinth fallback knows Title & Slug.
		return &domain.CartItem{
			Title:      card.Title,
			IconURL:    card.IconURL,
			Platform:   "curseforge",
			SourceKey:  card.SourceKey,
			SourceKeys: []string{card.SourceKey},
			GroupID:    "curseforge:" + modID,
			Folder:     domain.GetContentTypeMeta(ct).Folder,
		}, nil
	}

	var reqChildren []domain.DependencyRef
	for _, d := range best.Dependencies {
		if d.Type == "required" && d.ProjectID != "" && d.ProjectID != modID {
			reqChildren = append(reqChildren, d)
		}
	}
	actualMC := domain.PickMatchedMCVersion(best.GameVersions, mcVer)
	actualLoader := domain.PickMatchedLoader(best.Loaders, effLoader)
	return &domain.CartItem{
		Title:        card.Title,
		IconURL:      card.IconURL,
		Platform:     "curseforge",
		SourceKey:    card.SourceKey,
		SourceKeys:   []string{card.SourceKey},
		GroupID:      "curseforge:" + modID,
		Filename:     best.Filename,
		FileSize:     best.FileSize,
		DownloadURL:  best.DownloadURL,
		FallbackURL:  best.FallbackURL,
		IsAvailable:  best.IsAvailable,
		Folder:       domain.GetContentTypeMeta(ct).Folder,
		Loaders:      best.Loaders,
		GameVersions: best.GameVersions,
		MCVersion:    actualMC,
		Loader:       actualLoader,
		AutoDepFor:   []string{},
	}, reqChildren
}

type cfWebDepRow struct {
	ID           int    `json:"id"`
	ModID        int    `json:"modId"`
	ClassID      int    `json:"classId"`
	RelationType int    `json:"relationType"`
	Type         int    `json:"type"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Summary      string `json:"summary"`
	AvatarURL    string `json:"avatarUrl"`
	Downloads    int64  `json:"downloads"`
	DownloadCnt  int64  `json:"downloadCount"`
	DateModified string `json:"dateModified"`
	Class        struct {
		ID int `json:"id"`
	} `json:"class"`
	Author struct {
		Name string `json:"name"`
	} `json:"author"`
	Logo struct {
		URL          string `json:"url"`
		ThumbnailURL string `json:"thumbnailUrl"`
	} `json:"logo"`
}

func formatCFWebDepRow(r cfWebDepRow, allowModpacks bool) *domain.RawProjectCard {
	rid := r.ID
	if rid <= 0 {
		rid = r.ModID
	}
	if rid <= 0 {
		return nil
	}
	cid := r.ClassID
	if cid <= 0 {
		cid = r.Class.ID
	}
	effCT := domain.ClassifyCFClassID(cid)
	if effCT == "" {
		return nil
	}
	if effCT == "modpack" && !allowModpacks {
		return nil
	}
	urlSec := domain.GetContentTypeMeta(effCT).CFURLSlug
	if effCT == "modpack" {
		urlSec = "modpacks"
	}
	slug := r.Slug
	if slug == "" {
		slug = strconv.Itoa(rid)
	}
	name := r.Name
	if name == "" {
		name = slug
	}
	icon := r.AvatarURL
	if icon == "" {
		icon = r.Logo.URL
	}
	if icon == "" {
		icon = r.Logo.ThumbnailURL
	}
	dl := r.Downloads
	if dl == 0 {
		dl = r.DownloadCnt
	}
	authors := []string{}
	if r.Author.Name != "" {
		authors = []string{r.Author.Name}
	}
	return &domain.RawProjectCard{
		Platform:     "curseforge",
		SourceKey:    fmt.Sprintf("curseforge:%d", rid),
		ID:           strconv.Itoa(rid),
		Slug:         slug,
		ClassID:      cid,
		ContentType:  effCT,
		Title:        name,
		Summary:      r.Summary,
		Authors:      authors,
		IconURL:      icon,
		Downloads:    dl,
		DateModified: r.DateModified,
		Status:       "active",
		Loaders:      []string{},
		GameVersions: []string{},
		Categories:   []string{},
		URL:          fmt.Sprintf("https://www.curseforge.com/minecraft/%s/%s", urlSec, slug),
	}
}

type CFDependentsPageResult struct {
	Items           []domain.RawProjectCard
	TotalCount      int64
	NextIndex       int
	HasMore         bool
	SkippedModpacks int
	ScannedCount    int
	WebError        string
}

func FetchCFDependentsPage(
	httpClient *netx.HTTPClient,
	apiKey, modID, slug, baseCT, targetCT string,
	relType, startIndex, pageSize int,
	projectFilter string,
) CFDependentsPageResult {
	key := security.SanitizeCFKey(apiKey)
	if pageSize < 10 {
		pageSize = 50
	}
	if pageSize > 50 {
		pageSize = 50
	}
	curIdx := startIndex
	if curIdx < 0 {
		curIdx = 0
	}
	allowModpacks := projectFilter == "all" || projectFilter == "modpacks_only" || targetCT == "modpack"
	referer := "https://www.curseforge.com/minecraft/mc-mods"
	if slug != "" {
		referer = fmt.Sprintf("https://www.curseforge.com/minecraft/%s/%s/relations/dependents", domain.GetContentTypeMeta(baseCT).CFURLSlug, slug)
	}

	var templates []string
	if relType == 0 {
		templates = []string{fmt.Sprintf("https://www.curseforge.com/api/v1/mods/%s/relations/dependents?index=%%d&pageSize=%d", modID, pageSize)}
	} else {
		templates = []string{
			fmt.Sprintf("https://www.curseforge.com/api/v1/mods/%s/relations/dependents?index=%%d&pageSize=%d&type=%d", modID, pageSize, relType),
			fmt.Sprintf("https://www.curseforge.com/api/v1/mods/%s/relations/dependents?index=%%d&pageSize=%d&filterType=%d", modID, pageSize, relType),
		}
	}

	var depIDs []int
	rawRows := map[int]cfWebDepRow{}
	relByID := map[int]int{}
	seen := map[int]struct{}{}
	var totalCount int64
	skippedModpacks := 0
	scannedRows := 0
	targetMatched := 0
	hasMore := false
	workingTpl := ""
	lastErr := ""
	anyOK := false

	// Allow enough slices per request so modpack-heavy popular mods (like JEI) still fill a full page of mods!
	maxSlices := 8
	if projectFilter == "no_modpacks" || projectFilter == "mods_only" || targetCT != "" {
		maxSlices = 18
	}

	for sliceI := 0; sliceI < maxSlices; sliceI++ {
		if httpClient.ReqCtx().Err() != nil {
			break
		}
		tryTpls := templates
		if workingTpl != "" {
			tryTpls = []string{workingTpl}
		}
		var rows []cfWebDepRow
		var sliceTotal int64
		var respPageSize int
		var respResultCount int
		sliceSuccess := false

		for _, tpl := range tryTpls {
			var resp struct {
				Data       []cfWebDepRow `json:"data"`
				Pagination struct {
					Index       int   `json:"index"`
					PageSize    int   `json:"pageSize"`
					ResultCount int   `json:"resultCount"`
					TotalCount  int64 `json:"totalCount"`
				} `json:"pagination"`
			}
			err := httpClient.GetBrowserJSON(fmt.Sprintf(tpl, curIdx), referer, 300, &resp)
			if err != nil {
				lastErr = err.Error()
				continue
			}
			anyOK = true
			sliceSuccess = true
			if len(resp.Data) > 0 || resp.Pagination.TotalCount > 0 {
				workingTpl = tpl
				rows = resp.Data
				sliceTotal = resp.Pagination.TotalCount
				respPageSize = resp.Pagination.PageSize
				respResultCount = resp.Pagination.ResultCount
				break
			}
		}
		if !sliceSuccess {
			// If we already fetched earlier slices and hit a transient error, allow retrying from curIdx
			if anyOK && totalCount > 0 && int64(curIdx) < totalCount {
				hasMore = true
			}
			break
		}
		if sliceTotal > totalCount {
			totalCount = sliceTotal
		}
		if len(rows) == 0 {
			hasMore = false
			break
		}

		step := len(rows)
		if respResultCount > step {
			step = respResultCount
		}
		curIdx += step
		scannedRows += len(rows)

		for _, r := range rows {
			cid := r.ClassID
			if cid <= 0 {
				cid = r.Class.ID
			}
			if cid == 4471 && !allowModpacks {
				skippedModpacks++
			}
			rowCT := domain.ClassifyCFClassID(cid)
			// When cid == 0, defer classification until GetCFModsBatch enriches official ClassID
			if cid > 0 && rowCT == "" {
				continue
			}
			passes := false
			switch projectFilter {
			case "all":
				passes = cid == 0 || rowCT != ""
			case "modpacks_only":
				passes = rowCT == "modpack"
			case "mods_only":
				passes = cid == 0 || rowCT == "mod"
			default:
				passes = cid == 0 || (rowCT != "" && rowCT != "modpack")
			}
			if !passes {
				continue
			}
			rid := r.ID
			if rid <= 0 {
				rid = r.ModID
			}
			if rid > 0 && strconv.Itoa(rid) != modID {
				if _, dup := seen[rid]; !dup {
					seen[rid] = struct{}{}
					depIDs = append(depIDs, rid)
					rawRows[rid] = r
					rt := r.RelationType
					if rt <= 0 {
						rt = r.Type
					}
					if rt <= 0 {
						rt = relType
					}
					relByID[rid] = rt
					if targetCT == "" || rowCT == targetCT || cid == 0 {
						targetMatched++
					}
				}
			}
		}

		// CRITICAL PAGINATION FIX:
		// CurseForge web API often caps actual pageSize to 20 even when 50 is requested.
		// Never stop solely because len(rows) < requested pageSize! Check totalCount first,
		// and only fall back to resp.Pagination.PageSize when totalCount is absent.
		reachedEnd := false
		if totalCount > 0 {
			reachedEnd = int64(curIdx) >= totalCount
		} else if respPageSize > 0 {
			reachedEnd = len(rows) < respPageSize
		} else {
			reachedEnd = len(rows) < pageSize
		}
		if reachedEnd {
			hasMore = false
			break
		}
		hasMore = true
		if targetMatched >= pageSize || len(depIDs) >= pageSize*2 {
			break
		}
	}

	if len(depIDs) == 0 {
		errOut := lastErr
		if anyOK {
			errOut = ""
		}
		return CFDependentsPageResult{
			Items:           []domain.RawProjectCard{},
			TotalCount:      totalCount,
			NextIndex:       curIdx,
			HasMore:         hasMore,
			SkippedModpacks: skippedModpacks,
			ScannedCount:    scannedRows,
			WebError:        errOut,
		}
	}

	modsMap := map[int]CFMod{}
	if key != "" {
		modsMap = GetCFModsBatch(httpClient, key, depIDs)
	}
	cards := make([]domain.RawProjectCard, 0, len(depIDs))
	for _, rid := range depIDs {
		var card *domain.RawProjectCard
		if m, ok := modsMap[rid]; ok {
			card = FormatCFModCard(m, "", allowModpacks)
		} else if rr, ok := rawRows[rid]; ok {
			card = formatCFWebDepRow(rr, allowModpacks)
		}
		if card == nil {
			continue
		}
		rk, rl := CFRelationMeta(relByID[rid])
		card.DependentRelation = rk
		card.DependentLabel = rl
		cards = append(cards, *card)
	}

	return CFDependentsPageResult{
		Items:           cards,
		TotalCount:      totalCount,
		NextIndex:       curIdx,
		HasMore:         hasMore,
		SkippedModpacks: skippedModpacks,
		ScannedCount:    scannedRows,
	}
}

// Official api.curseforge.com/v1 paginated strict fallback when www.curseforge.com is blocked (e.g. Cloudflare 403).
// Only keeps projects whose official latestFiles[].dependencies literally declare modId == modID.
// Supports true multi-page pagination via startIndex (each page scans targeted queries + popular catalog slices),
// so popular mods like JEI / Fabric API / Create return full paginated lists instead of stopping at ~12 items.
func FetchCFCoreStrictDependentsPage(
	httpClient *netx.HTTPClient,
	apiKey, modID string,
	baseItem domain.ProjectCard,
	relType int,
	mcVer, loader, projectFilter, targetCT string,
	startIndex int,
) CFDependentsPageResult {
	key := security.SanitizeCFKey(apiKey)
	midNum, err := strconv.Atoi(modID)
	if key == "" || err != nil || midNum <= 0 {
		return CFDependentsPageResult{Items: []domain.RawProjectCard{}}
	}
	if startIndex < 0 {
		startIndex = 0
	}

	queries := buildAddonCandidateQueries(baseItem)
	allowModpacks := projectFilter == "all" || projectFilter == "modpacks_only" || targetCT == "modpack"

	type searchJob struct {
		cid   int
		sf    int
		idx   int
		query string
	}

	// Build a deterministic, ordered list of search slices across targeted queries + broad popular catalog pages.
	// CurseForge /v1/mods/search supports index up to 9950 (pageSize=50 -> up to 200 pages per sort/class).
	var allJobs []searchJob
	classIDs := []int{6, 12, 6945, 6552}
	switch projectFilter {
	case "modpacks_only":
		classIDs = []int{4471}
	case "mods_only":
		classIDs = []int{6}
	case "all":
		classIDs = []int{6, 12, 6945, 6552, 4471}
	}

	// Phase 1: Targeted candidate queries (e.g. "JEI", "Just Enough Items") — high hit-rate for named addons
	for _, qStr := range queries {
		for _, cid := range classIDs {
			allJobs = append(allJobs, searchJob{cid: cid, sf: 6, idx: 0, query: qStr}) // TotalDownloads
			if cid == 6 {
				allJobs = append(allJobs,
					searchJob{cid: cid, sf: 2, idx: 0, query: qStr},   // Popularity
					searchJob{cid: cid, sf: 6, idx: 50, query: qStr},  // Downloads p2
					searchJob{cid: cid, sf: 6, idx: 100, query: qStr}, // Downloads p3
					searchJob{cid: cid, sf: 6, idx: 150, query: qStr}, // Downloads p4
					searchJob{cid: cid, sf: 3, idx: 0, query: qStr},   // LastUpdated
				)
			} else {
				allJobs = append(allJobs, searchJob{cid: cid, sf: 6, idx: 50, query: qStr})
			}
		}
	}

	// Phase 2: Broad top-catalog scan (empty searchFilter) — catches popular mods that depend on JEI/Create/GeckoLib
	// without putting the base mod name in their title (up to 1,500 top mods + top resourcepacks/datapacks)!
	broadClasses := []int{6}
	if targetCT == "resourcepack" {
		broadClasses = []int{12, 6}
	} else if targetCT == "datapack" {
		broadClasses = []int{6945, 6}
	} else if targetCT == "shader" {
		broadClasses = []int{6552, 6}
	} else if targetCT == "modpack" || projectFilter == "modpacks_only" {
		broadClasses = []int{4471}
	}
	for _, cid := range broadClasses {
		maxOff := 1500
		if cid != 6 && cid != 4471 {
			maxOff = 300
		}
		for off := 0; off < maxOff; off += 50 {
			allJobs = append(allJobs, searchJob{cid: cid, sf: 6, idx: off, query: ""})
		}
	}

	if startIndex >= len(allJobs) {
		return CFDependentsPageResult{
			Items:      []domain.RawProjectCard{},
			NextIndex:  len(allJobs),
			HasMore:    false,
		}
	}

	// Execute one paginated batch of search jobs (10 jobs = up to 500 CurseForge projects checked per request)
	const batchJobs = 10
	endIdx := startIndex + batchJobs
	if endIdx > len(allJobs) {
		endIdx = len(allJobs)
	}
	sliceJobs := allJobs[startIndex:endIdx]

	hdr := map[string]string{"x-api-key": key}
	rawMods := map[int]CFMod{}
	scannedCount := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, j := range sliceJobs {
		wg.Add(1)
		go func(jb searchJob) {
			defer wg.Done()
			q := url.Values{}
			q.Set("gameId", strconv.Itoa(domain.CurseForgeGameID))
			q.Set("classId", strconv.Itoa(jb.cid))
			if jb.query != "" {
				q.Set("searchFilter", jb.query)
			}
			q.Set("sortField", strconv.Itoa(jb.sf))
			q.Set("sortOrder", "desc")
			q.Set("pageSize", "50")
			q.Set("index", strconv.Itoa(jb.idx))
			if mcVer != "" {
				q.Set("gameVersion", mcVer)
			}
			if jb.cid == 6 && loader != "" {
				if lid := domain.CFLoaderNameToID(loader); lid > 0 {
					q.Set("modLoaderType", strconv.Itoa(lid))
				}
			}
			var resp struct {
				Data []CFMod `json:"data"`
			}
			if httpClient.GetJSON(domain.CurseForgeAPI+"/mods/search?"+q.Encode(), hdr, 300, &resp) == nil {
				mu.Lock()
				scannedCount += len(resp.Data)
				for _, m := range resp.Data {
					if m.ID > 0 && m.ID != midNum {
						if _, exists := rawMods[m.ID]; !exists {
							rawMods[m.ID] = m
						}
					}
				}
				mu.Unlock()
			}
		}(j)
	}
	wg.Wait()

	var verified []domain.RawProjectCard
	for _, m := range rawMods {
		if domain.ClassifyCFClassID(m.ClassID) == "" {
			continue
		}
		rels := extractCFAllDepRels(m, mcVer, loader)
		rtSet, ok := rels[midNum]
		if !ok {
			continue
		}
		foundRT, passes := pickCFVerifiedRelType(rtSet, relType)
		if !passes {
			continue
		}
		if c := FormatCFModCard(m, "", allowModpacks); c != nil {
			rk, rl := CFRelationMeta(foundRT)
			c.DependentRelation = rk
			c.DependentLabel = rl
			verified = append(verified, *c)
		}
	}
	sort.SliceStable(verified, func(i, j int) bool {
		return verified[i].Downloads > verified[j].Downloads
	})

	return CFDependentsPageResult{
		Items:        verified,
		TotalCount:   0, // Unknown global total in Core API fallback — never fabricate a fake total!
		NextIndex:    endIdx,
		HasMore:      endIdx < len(allJobs),
		ScannedCount: scannedCount,
	}
}

func GetCFFileChangelog(httpClient *netx.HTTPClient, apiKey, modID, fileID string) string {
	key := security.SanitizeCFKey(apiKey)
	if key == "" || modID == "" || fileID == "" {
		return ""
	}
	var resp struct {
		Data string `json:"data"`
	}
	u := fmt.Sprintf("%s/mods/%s/files/%s/changelog", domain.CurseForgeAPI, url.PathEscape(modID), url.PathEscape(fileID))
	if err := httpClient.GetJSON(u, map[string]string{"x-api-key": key}, 900, &resp); err != nil {
		return ""
	}
	return resp.Data
}
