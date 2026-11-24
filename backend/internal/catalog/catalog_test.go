package catalog

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"modhub/internal/domain"
	"modhub/internal/netx"
	"modhub/internal/storage"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMergeItemsSafetyOverridesAndCorroboration(t *testing.T) {
	conflicting := []domain.RawProjectCard{
		{Platform: "modrinth", SourceKey: "mr:1", ID: "1", ContentType: "mod", Title: "Waystones", Slug: "waystones", Authors: []string{"Alice"}, Links: domain.ProjectLinks{SourceURL: "https://github.com/alice/waystones"}},
		{Platform: "curseforge", SourceKey: "cf:2", ID: "2", ContentType: "mod", Title: "Waystones", Slug: "waystones", Authors: []string{"Bob"}, Links: domain.ProjectLinks{SourceURL: "https://github.com/bob/waystones"}},
	}
	if res := MergeItems(conflicting, true); len(res) != 2 {
		t.Fatalf("conflicting repos merged unexpectedly: %d", len(res))
	}

	raw := []domain.RawProjectCard{
		{Platform: "modrinth", SourceKey: "modrinth:sod", ID: "sod", Slug: "sodium", ContentType: "mod", Title: "Sodium", Authors: []string{"jellysquid3"}, Downloads: 1000},
		{Platform: "curseforge", SourceKey: "curseforge:394468", ID: "394468", Slug: "sodium", ContentType: "mod", Title: "Sodium", Authors: []string{"jellysquid3"}, Downloads: 2000},
		{Platform: "modrinth", SourceKey: "modrinth:customA", ID: "customA", Slug: "alpha-name", ContentType: "mod", Title: "Alpha Mod", Authors: []string{"Alice"}, Downloads: 100},
		{Platform: "curseforge", SourceKey: "curseforge:999001", ID: "999001", Slug: "beta-name", ContentType: "mod", Title: "Beta Mod", Authors: []string{"Bob"}, Downloads: 200},
	}
	ov := domain.OverridesData{
		Splits: [][]string{{"modrinth:sod", "curseforge:394468"}},
		Merges: [][]string{{"modrinth:customA", "curseforge:999001"}},
	}
	res := MergeItemsWithOverrides(raw, true, ov)
	if len(res) != 3 {
		t.Fatalf("expected 3 cards with split+merge overrides, got %d", len(res))
	}
}

func TestSearchOperatorsRankingAndPagination(t *testing.T) {
	pq := ParseSearchQuery(`create author:simibubi mc:1.21.1 dl:>=100k -fabric`)
	if !pq.HasOperators || pq.Prefixes.MCVersion != "1.21.1" || pq.Prefixes.MinDownloads == nil || *pq.Prefixes.MinDownloads != 100000 {
		t.Fatalf("unexpected parsed query: %+v", pq)
	}
	good := domain.ProjectCard{Title: "Create", Authors: []string{"simibubi"}, Downloads: 500000, Loaders: []string{"neoforge"}, GameVersions: []string{"1.21.1"}}
	if !ItemMatchesParsed(good, pq) {
		t.Fatal("expected good card to match")
	}

	emi := domain.ProjectCard{Title: "EMI", Slug: "emi", Downloads: 15_000_000}
	jei := domain.ProjectCard{Title: "Just Enough Items (JEI)", Slug: "jei", Downloads: 320_000_000}
	exp := ExpandSmartQuery("EMI", nil)
	if ComputeSearchScoreMulti(emi, []string{exp.CanonicalQuery}, exp.ExtraQueries, "downloads") <=
		ComputeSearchScoreMulti(jei, []string{exp.CanonicalQuery}, exp.ExtraQueries, "downloads") {
		t.Fatal("expected EMI to beat JEI when searching EMI")
	}

	p1 := ComputeSearchPagination(0, 24, 24, []SubQueryPageStat{{Fetched: 24, Total: 100}})
	if p1.Total != 100 || p1.NextOffset != 24 || !p1.HasMore {
		t.Fatalf("pagination mismatch: %+v", p1)
	}
}

func TestCrossEnrichAndDetectSafeIconMIME(t *testing.T) {
	versions := []domain.VersionFile{
		{Platform: "modrinth", VersionID: "mr-fab", GameVersions: []string{"1.21.1"}, Loaders: []string{"fabric"}, Dependencies: []domain.DependencyRef{{Platform: "modrinth", ProjectID: "P7dR8mSH", Type: "required"}}},
		{Platform: "curseforge", VersionID: "cf-fab", GameVersions: []string{"1.21.1"}, Loaders: []string{"fabric"}, Dependencies: []domain.DependencyRef{}},
		{Platform: "curseforge", VersionID: "cf-neo", GameVersions: []string{"1.21.1"}, Loaders: []string{"neoforge"}, Dependencies: []domain.DependencyRef{}},
	}
	out := CrossEnrichVersionDependencies(versions)
	if len(out[1].Dependencies) != 1 || len(out[2].Dependencies) != 0 {
		t.Fatalf("unexpected cross-enrich result: cf-fab=%+v cf-neo=%+v", out[1].Dependencies, out[2].Dependencies)
	}

	if m := DetectSafeIconMIME([]byte("<html><script>alert(1)</script></html>")); m != "" {
		t.Fatalf("HTML should be rejected, got %q", m)
	}
	if m := DetectSafeIconMIME([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 13}); m != "image/png" {
		t.Fatalf("expected image/png, got %q", m)
	}
}

func TestFastLocalSearchEnrichmentAndJEIDependents(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.OpenDatabase(filepath.Join(dir, "cat.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer db.Close()

	raw := make([]domain.RawProjectCard, 0, 52)
	for i := 0; i < 25; i++ {
		s := strconv.Itoa(i)
		raw = append(raw,
			domain.RawProjectCard{Platform: "modrinth", SourceKey: "modrinth:m" + s, ID: "m" + s, Slug: "shared-mod-" + s, ContentType: "mod", Title: "Shared Mod " + s, Authors: []string{"author" + s}, Downloads: 100000},
			domain.RawProjectCard{Platform: "curseforge", SourceKey: "curseforge:" + strconv.Itoa(1000+i), ID: strconv.Itoa(1000 + i), Slug: "shared-mod-" + s, ContentType: "mod", Title: "Shared Mod " + s, Authors: []string{"author" + s}, Downloads: 200000},
		)
	}
	t0 := time.Now()
	enriched := EnrichSearchBatchLocalOnly(db, MergeItems(raw, true), []string{"modrinth", "curseforge"})
	if len(enriched) != 25 || time.Since(t0) > 200*time.Millisecond {
		t.Fatalf("local enrichment unexpected: len=%d elapsed=%v", len(enriched), time.Since(t0))
	}

	mockAPI := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		u := req.URL.String()
		switch {
		case strings.Contains(u, "api.modrinth.com/v2/search") && (strings.Contains(u, "project_type:mod") || strings.Contains(u, "project_type%3Amod")):
			_, _ = rec.WriteString(`{"total_hits":1,"hits":[{"project_id":"jer_mr","project_type":"mod","slug":"just-enough-resources","title":"Just Enough Resources","author":"way2muchnoise","downloads":5000000,"categories":["neoforge"],"versions":["1.21.1"]}]}`)
		case strings.Contains(u, "api.modrinth.com/v2/project/jer_mr/version"):
			_, _ = rec.WriteString(`[{"id":"v1","version_type":"release","loaders":["neoforge"],"game_versions":["1.21.1"],"files":[{"url":"https://cdn.modrinth.com/jer.jar","filename":"jer.jar","primary":true}],"dependencies":[{"project_id":"u6dRKJwZ","dependency_type":"required"}]}]`)
		default:
			_, _ = rec.WriteString(`{"total_hits":0,"hits":[],"data":[]}`)
		}
		return rec.Result(), nil
	})

	hc := netx.NewHTTPClient(db)
	hc.SetAPIClient(&http.Client{Transport: mockAPI})
	hc.SetBrowserClient(&http.Client{Transport: mockAPI})

	jei := domain.ProjectCard{
		GroupID: "modrinth:u6dRKJwZ", ContentType: "mod", Title: "Just Enough Items (JEI)", Slug: "jei",
		Sources: []domain.ProjectSource{{Platform: "modrinth", SourceKey: "modrinth:u6dRKJwZ", ID: "u6dRKJwZ", Slug: "jei"}},
	}
	res := GetProjectDependents(hc, db, "", domain.DependentsRequest{
		Item: jei, Relation: "required", ProjectFilter: "no_modpacks", ContentType: "mod", Sources: []string{"modrinth"},
	})
	if len(res.Items) != 1 || res.Items[0].Title != "Just Enough Resources" {
		t.Fatalf("expected JER verified dependent, got %+v", res.Items)
	}
}
