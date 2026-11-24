package catalog

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"modhub/internal/domain"
	"modhub/internal/netx"
	"modhub/internal/storage"
)

type mrSearchResponse struct {
	Hits      []mrSearchHit `json:"hits"`
	TotalHits int64         `json:"total_hits"`
}

type mrSearchHit struct {
	ProjectID         string   `json:"project_id"`
	Slug              string   `json:"slug"`
	ProjectType       string   `json:"project_type"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Author            string   `json:"author"`
	IconURL           string   `json:"icon_url"`
	Downloads         int64    `json:"downloads"`
	DateModified      string   `json:"date_modified"`
	DateCreated       string   `json:"date_created"`
	License           string   `json:"license"`
	ClientSide        string   `json:"client_side"`
	ServerSide        string   `json:"server_side"`
	Categories        []string `json:"categories"`
	DisplayCategories []string `json:"display_categories"`
	Versions          []string `json:"versions"`
}

type MRFullProject struct {
	ID           string   `json:"id"`
	Slug         string   `json:"slug"`
	ProjectType  string   `json:"project_type"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Body         string   `json:"body"`
	Team         string   `json:"team"`
	IconURL      string   `json:"icon_url"`
	Downloads    int64    `json:"downloads"`
	Published    string   `json:"published"`
	Updated      string   `json:"updated"`
	Status       string   `json:"status"`
	ClientSide   string   `json:"client_side"`
	ServerSide   string   `json:"server_side"`
	Loaders      []string `json:"loaders"`
	GameVersions []string `json:"game_versions"`
	Categories   []string `json:"categories"`
	SourceURL    string   `json:"source_url"`
	IssuesURL    string   `json:"issues_url"`
	WikiURL      string   `json:"wiki_url"`
	DiscordURL   string   `json:"discord_url"`
	License      struct {
		ID string `json:"id"`
	} `json:"license"`
	Gallery []struct {
		URL         string `json:"url"`
		RawURL      string `json:"raw_url"`
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"gallery"`
}

type mrVersion struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	VersionNumber string   `json:"version_number"`
	VersionType   string   `json:"version_type"`
	DatePublished string   `json:"date_published"`
	Downloads     int64    `json:"downloads"`
	Loaders       []string `json:"loaders"`
	GameVersions  []string `json:"game_versions"`
	Changelog     string   `json:"changelog"`
	Files         []struct {
		URL      string `json:"url"`
		Filename string `json:"filename"`
		Primary  bool   `json:"primary"`
		Size     int64  `json:"size"`
	} `json:"files"`
	Dependencies []struct {
		ProjectID      string `json:"project_id"`
		VersionID      string `json:"version_id"`
		DependencyType string `json:"dependency_type"`
	} `json:"dependencies"`
}

func extractRealLoaders(raw []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, s := range raw {
		low := strings.ToLower(strings.TrimSpace(s))
		if domain.IsKnownLoader(low) {
			if _, ok := seen[low]; !ok {
				seen[low] = struct{}{}
				out = append(out, low)
			}
		}
	}
	return out
}

func formatMRSearchHit(h mrSearchHit, ct string) domain.RawProjectCard {
	slug := h.Slug
	if slug == "" {
		slug = h.ProjectID
	}
	title := h.Title
	if title == "" {
		title = slug
	}
	loaders := []string{}
	if ct != "shader" && ct != "resourcepack" {
		loaders = extractRealLoaders(h.Categories)
	}
	cats := h.DisplayCategories
	if len(cats) == 0 {
		cats = []string{}
		for _, c := range h.Categories {
			low := strings.ToLower(c)
			if !domain.IsKnownLoader(low) && low != "datapack" {
				cats = append(cats, c)
			}
		}
	}
	authors := []string{}
	if h.Author != "" {
		authors = []string{h.Author}
	}
	classified := domain.ClassifyMRProjectType(h.ProjectType)
	effCT := classified
	if effCT == "" {
		effCT = domain.ClassifyMRProjectType(ct)
	}
	pt := strings.ToLower(strings.TrimSpace(h.ProjectType))
	if pt == "" {
		pt = effCT
	}
	if effCT == "shader" || effCT == "resourcepack" || effCT == "modpack" {
		loaders = []string{}
	}
	versions := h.Versions
	if versions == nil {
		versions = []string{}
	}
	return domain.RawProjectCard{
		Platform:     "modrinth",
		SourceKey:    "modrinth:" + h.ProjectID,
		ID:           h.ProjectID,
		Slug:         slug,
		ContentType:  effCT,
		Title:        title,
		Summary:      h.Description,
		Authors:      authors,
		IconURL:      h.IconURL,
		Downloads:    h.Downloads,
		DateModified: h.DateModified,
		DateCreated:  h.DateCreated,
		License:      h.License,
		Status:       "active",
		ClientSide:   h.ClientSide,
		ServerSide:   h.ServerSide,
		Loaders:      loaders,
		GameVersions: versions,
		Categories:   cats,
		URL:          fmt.Sprintf("https://modrinth.com/%s/%s", pt, slug),
	}
}

func FormatMRFullProject(p MRFullProject, ctHint string) domain.RawProjectCard {
	ct := domain.ClassifyMRProjectType(p.ProjectType)
	if ct == "" && strings.TrimSpace(p.ProjectType) == "" {
		ct = domain.ClassifyMRProjectType(ctHint)
	}
	slug := p.Slug
	if slug == "" {
		slug = p.ID
	}
	title := p.Title
	if title == "" {
		title = slug
	}
	loaders := []string{}
	if ct != "shader" && ct != "resourcepack" {
		loaders = extractRealLoaders(p.Loaders)
	}
	cats := []string{}
	for _, c := range p.Categories {
		low := strings.ToLower(c)
		if !domain.IsKnownLoader(low) && low != "datapack" {
			cats = append(cats, c)
		}
	}
	gvs := p.GameVersions
	if gvs == nil {
		gvs = []string{}
	}
	return domain.RawProjectCard{
		Platform:     "modrinth",
		SourceKey:    "modrinth:" + p.ID,
		ID:           p.ID,
		Slug:         slug,
		ContentType:  ct,
		Title:        title,
		Summary:      p.Description,
		Authors:      []string{},
		IconURL:      p.IconURL,
		Downloads:    p.Downloads,
		DateModified: p.Updated,
		DateCreated:  p.Published,
		License:      p.License.ID,
		Status:       p.Status,
		ClientSide:   p.ClientSide,
		ServerSide:   p.ServerSide,
		Loaders:      loaders,
		GameVersions: gvs,
		Categories:   cats,
		URL: fmt.Sprintf("https://modrinth.com/%s/%s", func() string {
			if ct != "" {
				return ct
			}
			return "project"
		}(), slug),
		Links: domain.ProjectLinks{
			SourceURL:  p.SourceURL,
			IssuesURL:  p.IssuesURL,
			WikiURL:    p.WikiURL,
			DiscordURL: p.DiscordURL,
		},
	}
}

func SearchModrinth(
	httpClient *netx.HTTPClient,
	query, ct, mcVersion string,
	loadersIn, loadersOut, catsIn, catsOut []string,
	catMode, env, sortMode string,
	limit, offset int,
) ([]domain.RawProjectCard, int64, error) {
	meta := domain.GetContentTypeMeta(ct)
	var facets [][]string
	if ct == "datapack" {
		var modIn, modOut []string
		for _, l := range loadersIn {
			low := strings.ToLower(strings.TrimSpace(l))
			if domain.IsKnownLoader(low) {
				modIn = append(modIn, "categories:"+low)
			}
		}
		outSet := map[string]bool{}
		for _, lo := range loadersOut {
			low := strings.ToLower(strings.TrimSpace(lo))
			if domain.IsKnownLoader(low) {
				modOut = append(modOut, low)
				outSet[low] = true
			}
		}
		if outSet["fabric"] && outSet["forge"] && outSet["neoforge"] {
			facets = append(facets, []string{"project_type:datapack"})
		} else {
			facets = append(facets, []string{"project_type:datapack", "categories:datapack"})
		}
		if len(modIn) > 0 {
			facets = append(facets, modIn)
		}
		for _, lo := range modOut {
			facets = append(facets, []string{"categories!=" + lo})
		}
	} else {
		facets = append(facets, []string{"project_type:" + meta.MRType})
	}
	if mcVersion != "" {
		facets = append(facets, []string{"versions:" + mcVersion})
	}
	if meta.UsesLoader {
		var validIn []string
		for _, l := range loadersIn {
			low := strings.ToLower(strings.TrimSpace(l))
			if domain.IsKnownLoader(low) {
				validIn = append(validIn, "categories:"+low)
			}
		}
		if len(validIn) > 0 {
			facets = append(facets, validIn)
		}
		for _, lo := range loadersOut {
			low := strings.ToLower(strings.TrimSpace(lo))
			if domain.IsKnownLoader(low) {
				facets = append(facets, []string{"categories!=" + low})
			}
		}
	}

	catDefs := domain.CategoriesForType(ct)
	mrTagsFor := func(cid string) []string {
		for _, d := range catDefs {
			if d.ID == cid {
				return d.MRTags
			}
		}
		return []string{cid}
	}
	if len(catsIn) > 0 {
		if strings.ToLower(catMode) == "or" {
			var grp []string
			for _, cid := range catsIn {
				for _, t := range mrTagsFor(cid) {
					grp = append(grp, "categories:"+t)
				}
			}
			if len(grp) > 0 {
				facets = append(facets, grp)
			}
		} else {
			for _, cid := range catsIn {
				var grp []string
				for _, t := range mrTagsFor(cid) {
					grp = append(grp, "categories:"+t)
				}
				if len(grp) > 0 {
					facets = append(facets, grp)
				}
			}
		}
	}
	for _, cid := range catsOut {
		for _, t := range mrTagsFor(cid) {
			facets = append(facets, []string{"categories!=" + t})
		}
	}

	if ct == "mod" {
		switch env {
		case "client_only":
			facets = append(facets, []string{"client_side:required", "client_side:optional"}, []string{"server_side:unsupported"})
		case "server_only":
			facets = append(facets, []string{"server_side:required", "server_side:optional"}, []string{"client_side:unsupported"})
		case "both":
			facets = append(facets, []string{"client_side:required", "client_side:optional"}, []string{"server_side:required", "server_side:optional"})
		}
	}

	idxSort := "downloads"
	switch sortMode {
	case "relevance", "updated", "newest":
		idxSort = sortMode
	}
	if limit < 1 {
		limit = 24
	}
	if limit > 100 {
		limit = 100
	}

	facetsBytes, _ := json.Marshal(facets)
	q := url.Values{}
	q.Set("query", query)
	q.Set("facets", string(facetsBytes))
	q.Set("index", idxSort)
	q.Set("limit", fmt.Sprintf("%d", limit))
	q.Set("offset", fmt.Sprintf("%d", offset))

	var resp mrSearchResponse
	if err := httpClient.GetJSON(domain.ModrinthAPI+"/search?"+q.Encode(), nil, 90, &resp); err != nil {
		return nil, 0, err
	}
	items := make([]domain.RawProjectCard, 0, len(resp.Hits))
	for _, h := range resp.Hits {
		if h.ProjectID != "" {
			items = append(items, formatMRSearchHit(h, ct))
		}
	}
	return items, resp.TotalHits, nil
}

func FetchModrinthProjectBySlug(httpClient *netx.HTTPClient, idOrSlug string) (*domain.RawProjectCard, error) {
	var p MRFullProject
	u := fmt.Sprintf("%s/project/%s", domain.ModrinthAPI, url.PathEscape(idOrSlug))
	if err := httpClient.GetJSON(u, nil, 300, &p); err != nil {
		return nil, err
	}
	card := FormatMRFullProject(p, "")
	if p.Team != "" {
		var members []struct {
			User struct {
				Username string `json:"username"`
			} `json:"user"`
		}
		tu := fmt.Sprintf("%s/team/%s/members", domain.ModrinthAPI, url.PathEscape(p.Team))
		if httpClient.GetJSON(tu, nil, 600, &members) == nil {
			for _, m := range members {
				if m.User.Username != "" {
					card.Authors = append(card.Authors, m.User.Username)
					if len(card.Authors) >= 4 {
						break
					}
				}
			}
		}
	}
	return &card, nil
}

func FetchModrinthByAuthor(httpClient *netx.HTTPClient, username, ct string) []domain.RawProjectCard {
	var projs []MRFullProject
	u := fmt.Sprintf("%s/user/%s/projects", domain.ModrinthAPI, url.PathEscape(username))
	if err := httpClient.GetJSON(u, nil, 300, &projs); err != nil {
		return nil
	}
	var out []domain.RawProjectCard
	for _, p := range projs {
		if strings.ToLower(p.ProjectType) != ct {
			continue
		}
		card := FormatMRFullProject(p, ct)
		card.Authors = []string{username}
		out = append(out, card)
	}
	return out
}

func GetModrinthProjectsBatch(httpClient *netx.HTTPClient, ids []string) map[string]MRFullProject {
	out := map[string]MRFullProject{}
	for i := 0; i < len(ids); i += 60 {
		end := i + 60
		if end > len(ids) {
			end = len(ids)
		}
		b, _ := json.Marshal(ids[i:end])
		q := url.Values{}
		q.Set("ids", string(b))
		var list []MRFullProject
		if httpClient.GetJSON(domain.ModrinthAPI+"/projects?"+q.Encode(), nil, 300, &list) == nil {
			for _, p := range list {
				if p.ID != "" {
					out[p.ID] = p
				}
			}
		}
	}
	return out
}

// ResolveModrinthVersionProjectIDs batch-resolves Modrinth version_id -> project_id
// for dependencies that omitted project_id (allowed by Modrinth API v2).
func ResolveModrinthVersionProjectIDs(httpClient *netx.HTTPClient, versionIDs []string) map[string]string {
	out := map[string]string{}
	if len(versionIDs) == 0 {
		return out
	}
	uniq := make([]string, 0, len(versionIDs))
	seen := map[string]struct{}{}
	for _, id := range versionIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; !dup {
			seen[id] = struct{}{}
			uniq = append(uniq, id)
		}
	}
	for i := 0; i < len(uniq); i += 60 {
		end := i + 60
		if end > len(uniq) {
			end = len(uniq)
		}
		b, _ := json.Marshal(uniq[i:end])
		q := url.Values{}
		q.Set("ids", string(b))
		var list []struct {
			ID        string `json:"id"`
			ProjectID string `json:"project_id"`
		}
		if httpClient.GetJSON(domain.ModrinthAPI+"/versions?"+q.Encode(), nil, 300, &list) == nil {
			for _, v := range list {
				if v.ID != "" && v.ProjectID != "" {
					out[v.ID] = v.ProjectID
				}
			}
		}
	}
	return out
}

func parseMRVersion(v mrVersion, pid, slug, ct string) *domain.VersionFile {
	if len(v.Files) == 0 {
		return nil
	}
	primary := v.Files[0]
	for _, f := range v.Files {
		if f.Primary {
			primary = f
			break
		}
	}
	deps := make([]domain.DependencyRef, 0, len(v.Dependencies))
	for _, d := range v.Dependencies {
		dpid := strings.TrimSpace(d.ProjectID)
		dvid := strings.TrimSpace(d.VersionID)
		if (dpid == "" && dvid == "") || (dpid != "" && dpid == pid) {
			continue
		}
		dt := domain.NormalizeDepType(d.DependencyType)
		if dt == "" {
			continue
		}
		deps = append(deps, domain.DependencyRef{
			Platform:  "modrinth",
			ProjectID: dpid,
			VersionID: dvid,
			Type:      dt,
		})
	}
	deps = domain.DedupeDependencyRefs(deps, pid)
	loaders := []string{}
	if ct != "shader" && ct != "resourcepack" {
		loaders = extractRealLoaders(v.Loaders)
	}
	rt := strings.ToLower(v.VersionType)
	if rt == "" {
		rt = "release"
	}
	name := v.Name
	if name == "" {
		name = v.VersionNumber
	}
	pSlug := slug
	if pSlug == "" {
		pSlug = pid
	}
	gvs := v.GameVersions
	if gvs == nil {
		gvs = []string{}
	}
	return &domain.VersionFile{
		Platform:      "modrinth",
		SourceKey:     "modrinth:" + pid,
		VersionID:     v.ID,
		ModID:         pid,
		Name:          name,
		VersionNumber: v.VersionNumber,
		ReleaseType:   rt,
		DatePublished: v.DatePublished,
		Downloads:     v.Downloads,
		Loaders:       loaders,
		GameVersions:  gvs,
		Filename:      primary.Filename,
		FileSize:      primary.Size,
		DownloadURL:   primary.URL,
		FallbackURL:   fmt.Sprintf("https://modrinth.com/%s/%s/version/%s", ct, pSlug, v.ID),
		IsAvailable:   primary.URL != "",
		Changelog:     v.Changelog,
		Dependencies:  deps,
	}
}

func GetModrinthVersionsWithErr(httpClient *netx.HTTPClient, pid, slug, ct, mcVer, loader string) ([]domain.VersionFile, error) {
	q := url.Values{}
	if mcVer != "" {
		b, _ := json.Marshal([]string{mcVer})
		q.Set("game_versions", string(b))
	}
	if loader != "" && domain.IsKnownLoader(loader) {
		b, _ := json.Marshal([]string{strings.ToLower(loader)})
		q.Set("loaders", string(b))
	}
	u := fmt.Sprintf("%s/project/%s/version", domain.ModrinthAPI, url.PathEscape(pid))
	if encoded := q.Encode(); encoded != "" {
		u += "?" + encoded
	}
	var raw []mrVersion
	if err := httpClient.GetJSON(u, nil, 240, &raw); err != nil {
		return nil, err
	}
	out := make([]domain.VersionFile, 0, len(raw))
	missingVerIDs := []string{}
	seenVid := map[string]struct{}{}
	for _, rv := range raw {
		if vf := parseMRVersion(rv, pid, slug, ct); vf != nil {
			for _, d := range vf.Dependencies {
				if d.ProjectID == "" && d.VersionID != "" {
					if _, ok := seenVid[d.VersionID]; !ok {
						seenVid[d.VersionID] = struct{}{}
						missingVerIDs = append(missingVerIDs, d.VersionID)
					}
				}
			}
			out = append(out, *vf)
		}
	}
	if len(missingVerIDs) > 0 {
		vidToPid := ResolveModrinthVersionProjectIDs(httpClient, missingVerIDs)
		if len(vidToPid) > 0 {
			for i := range out {
				changed := false
				for j := range out[i].Dependencies {
					if out[i].Dependencies[j].ProjectID == "" && out[i].Dependencies[j].VersionID != "" {
						if resolvedPID := vidToPid[out[i].Dependencies[j].VersionID]; resolvedPID != "" {
							out[i].Dependencies[j].ProjectID = resolvedPID
							changed = true
						}
					}
				}
				if changed {
					out[i].Dependencies = domain.DedupeDependencyRefs(out[i].Dependencies, pid)
				}
			}
		}
	}
	return out, nil
}

func GetModrinthVersions(httpClient *netx.HTTPClient, pid, slug, ct, mcVer, loader string) []domain.VersionFile {
	vers, _ := GetModrinthVersionsWithErr(httpClient, pid, slug, ct, mcVer, loader)
	return vers
}

func GetModrinthProjectMeta(httpClient *netx.HTTPClient, pid, slugHint, ct string) (string, []domain.GalleryItem, domain.ProjectLinks, []domain.VersionFile, error) {
	var (
		p       MRFullProject
		vers    []domain.VersionFile
		projErr error
		versErr error
		wg      sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		u := fmt.Sprintf("%s/project/%s", domain.ModrinthAPI, url.PathEscape(pid))
		projErr = httpClient.GetJSON(u, nil, 300, &p)
	}()
	go func() {
		defer wg.Done()
		vers, versErr = GetModrinthVersionsWithErr(httpClient, pid, slugHint, ct, "", "")
	}()
	wg.Wait()

	if p.Slug != "" && p.Slug != slugHint {
		urlSec := domain.GetContentTypeMeta(ct).MRType
		for i := range vers {
			if vers[i].FallbackURL == "" || strings.Contains(vers[i].FallbackURL, "/"+pid+"/") {
				vers[i].FallbackURL = fmt.Sprintf("https://modrinth.com/%s/%s/version/%s", urlSec, p.Slug, vers[i].VersionID)
			}
		}
	}

	gal := make([]domain.GalleryItem, 0, len(p.Gallery))
	for _, g := range p.Gallery {
		fullURL := g.RawURL
		if fullURL == "" {
			fullURL = g.URL
		}
		if fullURL != "" {
			gal = append(gal, domain.GalleryItem{URL: fullURL, Title: g.Title, Description: g.Description})
		}
	}
	links := domain.ProjectLinks{
		SourceURL:  p.SourceURL,
		IssuesURL:  p.IssuesURL,
		WikiURL:    p.WikiURL,
		DiscordURL: p.DiscordURL,
	}
	var retErr error
	if projErr != nil {
		retErr = projErr
	} else if versErr != nil {
		retErr = versErr
	}
	return p.Body, gal, links, vers, retErr
}

// InspectMRDepRels inspects a Modrinth project's actual published versions and returns
// EVERY verified dependency relation: depProjectID -> set of relation types (required/optional/embedded/incompatible).
func InspectMRDepRels(httpClient *netx.HTTPClient, pid, ct, mcVer, loader string) map[string]map[string]bool {
	if ct == "" {
		ct = "mod"
	}
	effLoader := ""
	if domain.GetContentTypeMeta(ct).UsesLoader {
		effLoader = loader
	}
	vers := GetModrinthVersions(httpClient, pid, "", ct, mcVer, effLoader)
	if len(vers) == 0 && (mcVer != "" || effLoader != "") {
		vers = GetModrinthVersions(httpClient, pid, "", ct, "", "")
	}
	rels := map[string]map[string]bool{}
	limit := 12
	if len(vers) < limit {
		limit = len(vers)
	}
	for _, v := range vers[:limit] {
		for _, d := range v.Dependencies {
			dt := domain.NormalizeDepType(d.Type)
			if dt == "" || d.ProjectID == "" || d.ProjectID == pid {
				continue
			}
			if _, ok := rels[d.ProjectID]; !ok {
				rels[d.ProjectID] = map[string]bool{}
			}
			rels[d.ProjectID][dt] = true
		}
	}
	return rels
}

func pickVerifiedRelType(relSet map[string]bool, wantedRel string) (string, bool) {
	if len(relSet) == 0 {
		return "", false
	}
	if wantedRel != "" && wantedRel != "all" {
		if relSet[wantedRel] {
			return wantedRel, true
		}
		return "", false
	}
	best := ""
	bestP := 0
	for rt := range relSet {
		if p := domain.DepTypePriority(rt); p > bestP {
			bestP = p
			best = rt
		}
	}
	return best, best != ""
}

// buildAddonCandidateQueries derives targeted candidate-discovery queries from the base project's
// title, parenthetical acronym (e.g. "JEI" from "Just Enough Items (JEI)"), and slug.
// These queries are ONLY used to gather candidate IDs; every candidate MUST still pass strict
// version/file dependency verification before being accepted as an addon.
func buildAddonCandidateQueries(item domain.ProjectCard) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(raw string) {
		q := strings.TrimSpace(raw)
		if len(q) < 2 {
			return
		}
		low := strings.ToLower(q)
		if _, dup := seen[low]; dup {
			return
		}
		seen[low] = struct{}{}
		out = append(out, q)
	}

	// Extract acronym in parentheses if present (e.g. "Just Enough Items (JEI)" -> "JEI")
	if m := domain.ReParensShortCode.FindString(item.Title); m != "" {
		acr := strings.Trim(strings.TrimSpace(m), "()")
		add(acr)
	}
	// Check slugs across sources (e.g. slug "jei" -> "jei")
	for _, s := range item.Sources {
		if s.Slug != "" {
			cleanSlug := strings.ReplaceAll(strings.TrimSpace(s.Slug), "-", " ")
			add(cleanSlug)
		}
	}
	if item.Slug != "" {
		add(strings.ReplaceAll(strings.TrimSpace(item.Slug), "-", " "))
	}
	add(domain.CleanDisplayTitle(item.Title))
	// Prefer shortest distinctive terms + clean title, cap at 2 queries for latency
	if len(out) > 2 {
		cleanTitle := domain.CleanDisplayTitle(item.Title)
		first := out[0]
		if !strings.EqualFold(first, cleanTitle) && cleanTitle != "" {
			out = []string{first, cleanTitle}
		} else {
			out = out[:2]
		}
	}
	return out
}

// DiscoverVerifiedMRDependents discovers Modrinth dependents strictly by verifying
// each candidate's actual published version dependencies[]. No project is ever accepted
// solely by title, description, or unverified search facets.
func DiscoverVerifiedMRDependents(
	httpClient *netx.HTTPClient,
	db *storage.Database,
	baseItem domain.ProjectCard,
	mrID, mcVer string,
	loadersIn, loadersOut []string,
	relNorm string,
	targetTypes []string,
) []domain.RawProjectCard {
	if mrID == "" || len(targetTypes) == 0 {
		return nil
	}
	queries := buildAddonCandidateQueries(baseItem)
	if len(queries) == 0 {
		return nil
	}
	singleLoader := ""
	if len(loadersIn) == 1 {
		singleLoader = loadersIn[0]
	}

	type searchJob struct {
		ct    string
		query string
		limit int
	}
	var jobs []searchJob
	for _, ct := range targetTypes {
		lim := 14
		if ct == "mod" {
			lim = 26
		}
		for _, q := range queries {
			jobs = append(jobs, searchJob{ct: ct, query: q, limit: lim})
		}
	}

	candMap := map[string]domain.RawProjectCard{}
	candOrder := []string{}
	var candMu sync.Mutex
	var swg sync.WaitGroup
	for _, jb := range jobs {
		swg.Add(1)
		go func(j searchJob) {
			defer swg.Done()
			hits, _, err := SearchModrinth(httpClient, j.query, j.ct, mcVer, loadersIn, loadersOut, nil, nil, "and", "", "downloads", j.limit, 0)
			if err != nil || len(hits) == 0 {
				return
			}
			candMu.Lock()
			for _, h := range hits {
				// Strictly keep only items whose authoritative Modrinth project_type matches a supported type
				if h.ID == "" || h.ID == mrID || !domain.IsSupportedCatalogType(h.ContentType) {
					continue
				}
				if _, dup := candMap[h.ID]; !dup {
					candMap[h.ID] = h
					candOrder = append(candOrder, h.ID)
				}
			}
			candMu.Unlock()
		}(jb)
	}
	swg.Wait()

	if len(candOrder) == 0 {
		return nil
	}
	// Cap total live version probes to keep latency crisp while covering mods + resourcepacks + datapacks + shaders
	const maxProbes = 40
	if len(candOrder) > maxProbes {
		candOrder = candOrder[:maxProbes]
	}

	var verified []domain.RawProjectCard
	var verifiedRawToCache []domain.RawProjectCard
	var newEdges []domain.RDepEdge
	var mu sync.Mutex
	sem := make(chan struct{}, 8)
	var pwg sync.WaitGroup

	for _, id := range candOrder {
		cand := candMap[id]
		pwg.Add(1)
		go func(c domain.RawProjectCard) {
			defer pwg.Done()
			if !netx.AcquireSemCtx(httpClient.ReqCtx(), sem) {
				return
			}
			defer func() { <-sem }()

			relMap := InspectMRDepRels(httpClient, c.ID, c.ContentType, mcVer, singleLoader)
			if len(relMap) == 0 {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for dpid, rtSet := range relMap {
				for dt := range rtSet {
					newEdges = append(newEdges, domain.RDepEdge{
						DepKey:       "modrinth:" + dpid,
						DependentKey: c.SourceKey,
						DepType:      dt,
					})
				}
			}
			if rtSet, ok := relMap[mrID]; ok {
				if matchedRel, passes := pickVerifiedRelType(rtSet, relNorm); passes {
					c.DependentRelation = matchedRel
					c.DependentLabel = RelLabelRU(matchedRel)
					verified = append(verified, c)
					verifiedRawToCache = append(verifiedRawToCache, c)
				}
			}
		}(cand)
	}
	pwg.Wait()

	if db != nil {
		if len(verifiedRawToCache) > 0 {
			db.UpsertCatalogItemsAsync(verifiedRawToCache)
		}
		if len(newEdges) > 0 {
			db.UpsertReverseDepsAsync(newEdges)
		}
	}
	return verified
}

func ResolveMRDependencyRelease(httpClient *netx.HTTPClient, pid, mcVer, loader string) (*domain.CartItem, []domain.DependencyRef) {
	return ResolveMRDependencyReleasePinned(httpClient, pid, "", mcVer, loader)
}

func ResolveMRDependencyReleasePinned(httpClient *netx.HTTPClient, pid, versionIDHint, mcVer, loader string) (*domain.CartItem, []domain.DependencyRef) {
	if pid == "" && versionIDHint != "" {
		if resolved := ResolveModrinthVersionProjectIDs(httpClient, []string{versionIDHint}); resolved[versionIDHint] != "" {
			pid = resolved[versionIDHint]
		}
	}
	if pid == "" {
		return nil, nil
	}
	var proj MRFullProject
	u := fmt.Sprintf("%s/project/%s", domain.ModrinthAPI, url.PathEscape(pid))
	if err := httpClient.GetJSON(u, nil, 360, &proj); err != nil || proj.ID == "" {
		return nil, nil
	}
	pt := strings.ToLower(proj.ProjectType)
	if pt == "" {
		pt = "mod"
	}
	usesLoader := domain.GetContentTypeMeta(pt).UsesLoader
	effLoader := ""
	if usesLoader {
		effLoader = strings.ToLower(strings.TrimSpace(loader))
	}

	var best *domain.VersionFile
	if mcVer != "" && effLoader != "" {
		vers := GetModrinthVersions(httpClient, pid, proj.Slug, pt, mcVer, effLoader)
		if len(vers) > 0 {
			if versionIDHint != "" {
				for i := range vers {
					v := vers[i]
					if v.VersionID == versionIDHint && v.IsAvailable && strings.TrimSpace(v.DownloadURL) != "" {
						loaderOK := !usesLoader || effLoader == "" || len(v.Loaders) == 0
						if !loaderOK {
							for _, l := range v.Loaders {
								fl := strings.ToLower(strings.TrimSpace(l))
								if fl == effLoader || (effLoader == "quilt" && fl == "fabric") || (effLoader == "neoforge" && fl == "forge") {
									loaderOK = true
									break
								}
							}
						}
						if loaderOK {
							cp := v
							best = &cp
							break
						}
					}
				}
			}
			if best == nil {
				best, _ = domain.PickBestDependencyVersion(vers, mcVer, effLoader, usesLoader)
			}
		}
	}
	if best == nil {
		vers := GetModrinthVersions(httpClient, pid, proj.Slug, pt, "", effLoader)
		if len(vers) == 0 && effLoader != "" {
			vers = GetModrinthVersions(httpClient, pid, proj.Slug, pt, "", "")
		}
		if len(vers) > 0 {
			if versionIDHint != "" {
				for i := range vers {
					v := vers[i]
					if v.VersionID == versionIDHint && v.IsAvailable && strings.TrimSpace(v.DownloadURL) != "" {
						loaderOK := !usesLoader || effLoader == "" || len(v.Loaders) == 0
						if !loaderOK {
							for _, l := range v.Loaders {
								fl := strings.ToLower(strings.TrimSpace(l))
								if fl == effLoader || (effLoader == "quilt" && fl == "fabric") || (effLoader == "neoforge" && fl == "forge") {
									loaderOK = true
									break
								}
							}
						}
						if loaderOK {
							cp := v
							best = &cp
							break
						}
					}
				}
			}
			if best == nil {
				best, _ = domain.PickBestDependencyVersion(vers, mcVer, effLoader, usesLoader)
			}
		}
	}
	if best == nil {
		return nil, nil
	}

	var reqChildren []domain.DependencyRef
	for _, d := range best.Dependencies {
		if d.Type == "required" && (d.ProjectID != "" || d.VersionID != "") && d.ProjectID != pid {
			reqChildren = append(reqChildren, d)
		}
	}
	actualMC := domain.PickMatchedMCVersion(best.GameVersions, mcVer)
	actualLoader := domain.PickMatchedLoader(best.Loaders, effLoader)
	title := proj.Title
	if title == "" {
		title = pid
	}
	return &domain.CartItem{
		Title:        title,
		IconURL:      proj.IconURL,
		Platform:     "modrinth",
		SourceKey:    "modrinth:" + pid,
		SourceKeys:   []string{"modrinth:" + pid},
		GroupID:      "modrinth:" + pid,
		Filename:     best.Filename,
		FileSize:     best.FileSize,
		DownloadURL:  best.DownloadURL,
		FallbackURL:  best.FallbackURL,
		IsAvailable:  best.IsAvailable,
		Folder:       domain.GetContentTypeMeta(pt).Folder,
		Loaders:      best.Loaders,
		GameVersions: best.GameVersions,
		MCVersion:    actualMC,
		Loader:       actualLoader,
		AutoDepFor:   []string{},
	}, reqChildren
}

func FetchMCVersions(httpClient *netx.HTTPClient) []string {
	var raw []struct {
		Version     string `json:"version"`
		VersionType string `json:"version_type"`
	}
	if err := httpClient.GetJSON(domain.ModrinthAPI+"/tag/game_version", nil, 3600, &raw); err != nil {
		return []string{"1.21.4", "1.21.3", "1.21.1", "1.21", "1.20.4", "1.20.1", "1.19.4", "1.19.2", "1.18.2", "1.16.5", "1.12.2"}
	}
	seen := map[string]struct{}{}
	var out []string
	for _, item := range raw {
		if item.VersionType == "release" && item.Version != "" {
			if _, ok := seen[item.Version]; !ok {
				seen[item.Version] = struct{}{}
				out = append(out, item.Version)
			}
		}
	}
	return out
}


// FetchModrinthProjectsBatch resolves up to 50 Modrinth project IDs or slugs per single HTTP call
// via GET /v2/projects?ids=[...], drastically reducing per-card cross-match latency.
func FetchModrinthProjectsBatch(httpClient *netx.HTTPClient, idsOrSlugs []string) []domain.RawProjectCard {
	if len(idsOrSlugs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(idsOrSlugs))
	unique := make([]string, 0, len(idsOrSlugs))
	for _, s := range idsOrSlugs {
		s = strings.ToLower(strings.TrimSpace(s))
		if len(s) < 2 {
			continue
		}
		if _, dup := seen[s]; !dup {
			seen[s] = struct{}{}
			unique = append(unique, s)
		}
	}
	if len(unique) == 0 {
		return nil
	}

	var (
		out []domain.RawProjectCard
		mu  sync.Mutex
		wg  sync.WaitGroup
	)
	for i := 0; i < len(unique); i += 50 {
		end := i + 50
		if end > len(unique) {
			end = len(unique)
		}
		chunk := unique[i:end]
		wg.Add(1)
		go func(ids []string) {
			defer wg.Done()
			b, _ := json.Marshal(ids)
			q := url.Values{}
			q.Set("ids", string(b))
			var projs []MRFullProject
			if err := httpClient.GetJSON(domain.ModrinthAPI+"/projects?"+q.Encode(), nil, 300, &projs); err != nil {
				return
			}
			local := make([]domain.RawProjectCard, 0, len(projs))
			for _, p := range projs {
				if p.ID != "" {
					local = append(local, FormatMRFullProject(p, ""))
				}
			}
			if len(local) > 0 {
				mu.Lock()
				out = append(out, local...)
				mu.Unlock()
			}
		}(chunk)
	}
	wg.Wait()
	return out
}
