package deps

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"modhub/internal/domain"
	"modhub/internal/netx"
	"modhub/internal/packio"
	"modhub/internal/storage"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func makeTestModJAR(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		_, _ = w.Write([]byte(content))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func TestManifestParsersAllLoaders(t *testing.T) {
	forgeToml := `
[[mods]]
modId="cataclysmiccombat"
[[dependencies.cataclysmiccombat]]
modId="forge"
mandatory=true
[[dependencies.cataclysmiccombat]]
modId="bettercombat"
mandatory=true
[[dependencies.cataclysmiccombat]]
modId="jei"
mandatory=false
`
	fDeps := ParseModsTomlDeps(forgeToml, "META-INF/mods.toml")
	if len(fDeps) != 2 || fDeps[0].ModID != "bettercombat" || fDeps[0].Type != "required" {
		t.Fatalf("unexpected Forge manifest deps: %+v", fDeps)
	}

	fabJSON := []byte(`{"id":"myfab","depends":{"minecraft":"1.21.1","fabric-api":"*"},"breaks":{"badmod":"*"}}`)
	fabDeps := ParseFabricModJSONDeps(fabJSON, "fabric.mod.json")
	if len(fabDeps) != 2 {
		t.Fatalf("unexpected Fabric manifest deps: %+v", fabDeps)
	}

	quiltJSON := []byte(`{"quilt_loader":{"id":"myq","depends":["minecraft",{"id":"bettercombat"},{"id":"rei","optional":true}]}}`)
	qDeps := ParseQuiltModJSONDeps(quiltJSON, "quilt.mod.json")
	if len(qDeps) != 2 {
		t.Fatalf("unexpected Quilt manifest deps: %+v", qDeps)
	}
}

func TestAnalyzeTextDependenciesNegationsAndCategories(t *testing.T) {
	in := TextDepSourceInput{
		Platform: "curseforge", ProjectID: "999001", Title: "Create Extension",
		Summary: "Requires Better Combat and L_Ender's Cataclysm to work. Does not require GeckoLib.",
		Body: `<h2>Requirements</h2><ul><li><a href="https://modrinth.com/mod/architectury-api">Architectury API</a></li><li>Требуется Curios API для работы</li></ul>
<p>Не требует Citadel.</p><p>Built on top of Epic Fight.</p><p>Optional: Cloth Config API.</p><p>Рекомендуется установить JEI.</p><h2>Credits</h2><p>Inspired by Botania.</p>`,
	}
	cands := AnalyzeTextDependencies(in)
	byNorm := map[string]TextDepCandidate{}
	for _, c := range cands {
		byNorm[domain.NormalizeCompactTitle(c.Name)] = c
	}
	for _, want := range []string{"bettercombat", "lenderscataclysm", "architecturyapi", "curiosapi"} {
		if c, ok := byNorm[want]; !ok || c.Type != "required" || c.Confidence != "high" {
			t.Fatalf("expected %s required/high, got %+v", want, c)
		}
	}
	if ef, ok := byNorm["epicfight"]; !ok || ef.Type != "required" || ef.Confidence != "medium" {
		t.Fatalf("expected Epic Fight required/medium, got %+v", ef)
	}
	for _, forbidden := range []string{"geckolib", "citadel", "botania"} {
		if c, exists := byNorm[forbidden]; exists {
			t.Fatalf("negated/credit mod %q wrongly extracted: %+v", forbidden, c)
		}
	}
}

func TestUnifiedDepEngineAPIPlusManifestAndRecursiveZIP(t *testing.T) {
	jarA := makeTestModJAR(t, map[string]string{
		"META-INF/mods.toml": "[[mods]]\nmodId=\"moda\"\n[[dependencies.moda]]\nmodId=\"modc\"\nmandatory=true\n",
	})
	jarB := makeTestModJAR(t, map[string]string{"META-INF/mods.toml": "[[mods]]\nmodId=\"modb\"\n"})
	jarC := makeTestModJAR(t, map[string]string{"META-INF/mods.toml": "[[mods]]\nmodId=\"modc\"\n"})

	jarSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.jar":
			_, _ = w.Write(jarA)
		case "/b.jar":
			_, _ = w.Write(jarB)
		case "/c.jar":
			_, _ = w.Write(jarC)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer jarSrv.Close()

	mockAPI := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		p := req.URL.Path
		switch {
		case strings.HasSuffix(p, "/v2/project/modB"):
			_, _ = rec.WriteString(`{"id":"modB","slug":"modb","title":"Mod B","project_type":"mod"}`)
		case strings.HasSuffix(p, "/v2/project/modB/version"):
			_, _ = rec.WriteString(`[{"id":"vb","version_type":"release","loaders":["neoforge"],"game_versions":["1.21.1"],"files":[{"url":"` + jarSrv.URL + `/b.jar","filename":"b.jar","primary":true}],"dependencies":[]}]`)
		case strings.HasSuffix(p, "/v2/project/modc") || strings.HasSuffix(p, "/v2/project/modC"):
			_, _ = rec.WriteString(`{"id":"modC","slug":"modc","title":"Mod C","project_type":"mod"}`)
		case strings.HasSuffix(p, "/v2/project/modC/version"):
			_, _ = rec.WriteString(`[{"id":"vc","version_type":"release","loaders":["neoforge"],"game_versions":["1.21.1"],"files":[{"url":"` + jarSrv.URL + `/c.jar","filename":"c.jar","primary":true}],"dependencies":[]}]`)
		default:
			rec.WriteHeader(http.StatusNotFound)
			_, _ = rec.WriteString(`{}`)
		}
		return rec.Result(), nil
	})

	dir := t.TempDir()
	db, err := storage.OpenDatabase(filepath.Join(dir, "deps.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer db.Close()

	hc := netx.NewHTTPClient(db)
	hc.SetAPIClient(&http.Client{Transport: mockAPI})
	hc.SetDownloadClient(jarSrv.Client())
	hc.SetDownloadValidator(func(raw string) (*url.URL, error) { return url.Parse(raw) })

	res := ResolveRequiredDepsFull(hc, db, "", domain.DepResolveRequest{
		Dependencies:       []domain.DependencyRef{{Platform: "modrinth", ProjectID: "modB", Type: "required"}},
		MCVersion:          "1.21.1",
		Loader:             "neoforge",
		FileURL:            jarSrv.URL + "/a.jar",
		Platform:           "modrinth",
		ProjectID:          "modA",
		ExistingSourceKeys: []string{"modrinth:modA"},
		ExistingFilenames:  []string{"a.jar"},
	})
	if res.Status != "complete" || len(res.Resolved) != 2 {
		t.Fatalf("expected 2 resolved deps (B from API + C from manifest), got status=%s resolved=%+v unresolved=%+v", res.Status, res.Resolved, res.Unresolved)
	}

	all := append([]domain.CartItem{{Title: "Mod A", Platform: "modrinth", Folder: "mods", Filename: "a.jar", DownloadURL: jarSrv.URL + "/a.jar"}}, res.Resolved...)
	var zbuf bytes.Buffer
	sum, err := packio.StreamPackZIPWithReport(context.Background(), &zbuf, hc, all)
	if err != nil || sum.WrittenCount != 3 {
		t.Fatalf("StreamPackZIPWithReport = (%+v, %v)", sum, err)
	}
}

func TestEmbeddedJarInJarIgnoredFromExternalDeps(t *testing.T) {
	innerJar := makeTestModJAR(t, map[string]string{
		"fabric.mod.json": `{"id":"bundled-lib","name":"Bundled Lib"}`,
	})
	outerFiles := map[string]string{
		"fabric.mod.json": `{"id":"outer-mod","depends":{"minecraft":"1.21.1","bundled-lib":"*","external-lib":"*"}}`,
		"META-INF/jars/bundled-lib-1.0.jar": string(innerJar),
	}
	outerJar := makeTestModJAR(t, outerFiles)
	tmpJar := filepath.Join(t.TempDir(), "outer.jar")
	if err := os.WriteFile(tmpJar, outerJar, 0o600); err != nil {
		t.Fatalf("write jar: %v", err)
	}
	deps := ExtractManifestDepsFromJARFile(tmpJar)
	if len(deps) != 1 || deps[0].ModID != "external-lib" {
		t.Fatalf("expected only external-lib after filtering bundled JiJ, got %+v", deps)
	}
}

func TestPickBestDependencyVersionCompatLoaderAndDownloadURL(t *testing.T) {
	vers := []domain.VersionFile{
		{VersionID: "q1", Loaders: []string{"quilt"}, GameVersions: []string{"1.21.1"}, DownloadURL: "", IsAvailable: false, DatePublished: "2025-05-01T00:00:00Z"},
		{VersionID: "f1", Loaders: []string{"fabric"}, GameVersions: []string{"1.21.1"}, DownloadURL: "https://cdn.example/f1.jar", IsAvailable: true, ReleaseType: "release", DatePublished: "2025-04-01T00:00:00Z"},
	}
	best, reason := domain.PickBestDependencyVersion(vers, "1.21.1", "quilt", true)
	if best == nil || best.VersionID != "f1" {
		t.Fatalf("expected fabric downloadable fallback f1 for quilt, got %+v (%s)", best, reason)
	}

	both := []domain.VersionFile{
		{VersionID: "unavailable", Loaders: []string{"fabric"}, GameVersions: []string{"1.21.1"}, DownloadURL: "", ReleaseType: "release", DatePublished: "2025-06-01T00:00:00Z"},
		{VersionID: "available", Loaders: []string{"fabric"}, GameVersions: []string{"1.21.1"}, DownloadURL: "https://cdn.example/ok.jar", ReleaseType: "release", DatePublished: "2025-05-01T00:00:00Z"},
	}
	bestAvail, _ := domain.PickBestDependencyVersion(both, "1.21.1", "fabric", true)
	if bestAvail == nil || bestAvail.VersionID != "available" {
		t.Fatalf("expected available file with DownloadURL over metadata-only entry, got %+v", bestAvail)
	}
}
