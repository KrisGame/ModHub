package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"modhub/internal/app"
	"modhub/internal/domain"
	"modhub/internal/instances"
	"modhub/internal/storage"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newTestServer(t *testing.T) (*app.Container, http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.OpenDatabase(filepath.Join(dir, "http_test.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	container := app.NewContainer(dir, db)
	t.Cleanup(func() { container.Close() })
	srv := NewServer(container, dir, nil)
	return container, srv.Router(), dir
}

func TestLocalAPISecurityAndStrictJSONAndRollback(t *testing.T) {
	container, router, dir := newTestServer(t)

	// 1. Evil Host rejected
	req := httptest.NewRequest(http.MethodGet, "http://evil.com/api/health", nil)
	req.Host = "evil.com"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for evil Host, got %d", rec.Code)
	}

	// 2. Cross-site Origin rejected
	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/cache/clear", bytes.NewBufferString(`{}`))
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-site Origin, got %d", rec.Code)
	}

	// 3. Wrong Content-Type rejected
	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/config", bytes.NewBufferString(`a=b`))
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	req.Header.Set("Content-Type", "text/plain")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 for text/plain, got %d", rec.Code)
	}

	postJSON := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080"+path, bytes.NewBufferString(body))
		r.Host = "127.0.0.1:8080"
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}

	// 3b. Wails WebView2 origin (wails.localhost) is accepted cleanly
	req = httptest.NewRequest(http.MethodPost, "http://wails.localhost/api/cache/clear", bytes.NewBufferString(`{}`))
	req.Host = "wails.localhost"
	req.Header.Set("Origin", "http://wails.localhost")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for wails.localhost POST, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 4. Valid local POST succeeds & malformed JSON returns 400
	if w := postJSON("/api/cache/clear", `{}`); w.Code != http.StatusOK {
		t.Fatalf("expected 200 for cache/clear, got %d", w.Code)
	}
	for _, ep := range []string{"/api/config", "/api/packs/save", "/api/overrides/merge", "/api/mod/dependency-tree"} {
		if w := postJSON(ep, `{bad json`); w.Code != http.StatusBadRequest {
			t.Errorf("POST %s invalid JSON expected 400, got %d", ep, w.Code)
		}
	}

	// 5. Save valid pack then test disk persistence failure rollback
	if w := postJSON("/api/packs/save", `{"id":"p1","name":"TestPack","items":[{"title":"Sodium","folder":"mods","filename":"sodium.jar"}]}`); w.Code != http.StatusOK {
		t.Fatalf("valid pack save expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	blockerFile := filepath.Join(dir, "not_a_dir")
	_ = os.WriteFile(blockerFile, []byte("x"), 0o600)
	container.DataDir = blockerFile

	if w := postJSON("/api/packs/save", `{"id":"p2","name":"Rollback","items":[{"title":"Lithium","folder":"mods","filename":"lithium.jar"}]}`); w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on disk save failure, got %d", w.Code)
	}
	if packs := container.GetPacks(); len(packs) != 1 || packs[0].ID != "p1" {
		t.Fatalf("packs mutated despite save error: %+v", packs)
	}
}

func TestExportDownloadEndpointConstrainedToExportDir(t *testing.T) {
	container, router, dir := newTestServer(t)
	exports := filepath.Join(dir, "exports")
	if err := os.MkdirAll(exports, 0o700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(exports, "test.modhub")
	if err := os.WriteFile(archive, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(container, dir, nil)
	token, err := srv.registerExport(exports, archive, "test.modhub")
	if err != nil {
		t.Fatal(err)
	}
	request := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/modpacks/export-file?token="+url.QueryEscape(target), nil)
		req.Host = "127.0.0.1:8080"
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		return rec
	}
	_ = router
	if got := request(token); got.Code != http.StatusOK || got.Body.String() != "archive" {
		t.Fatalf("valid download: status=%d body=%q", got.Code, got.Body.String())
	}
	if got := request(token); got.Code != http.StatusOK {
		t.Fatalf("export should allow retry; got %d", got.Code)
	}
	outside := filepath.Join(dir, "config.json")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := request(outside); got.Code != http.StatusNotFound {
		t.Fatalf("outside path should be hidden; got %d", got.Code)
	}
	srv.exports.mu.Lock()
	record := srv.exports.files[token]
	record.expires = time.Now().Add(-time.Second)
	srv.exports.files[token] = record
	srv.exports.mu.Unlock()
	if got := request(token); got.Code != http.StatusNotFound {
		t.Fatalf("expired export should be hidden: %d", got.Code)
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Fatalf("expired archive should be removed: %v", err)
	}
}

func TestWebArchiveUploadAndPreview(t *testing.T) {
	_, router, dir := newTestServer(t)
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	entry, err := zw.Create("modrinth.index.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte(`{"formatVersion":1,"game":"minecraft","versionId":"1","name":"Web Upload","files":[],"dependencies":{"minecraft":"1.20.1"}}`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	upload := func(filename string, contents []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		part, err := mw.CreateFormFile("archive", filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(contents)
		_ = mw.Close()
		r := httptest.NewRequest(http.MethodPost, "/api/modpacks/upload-import", &body)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Host = "127.0.0.1:8080"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	good := upload("test.mrpack", archive.Bytes())
	if good.Code != http.StatusOK || !strings.Contains(good.Body.String(), "preview_id") {
		t.Fatalf("upload preview status=%d body=%s", good.Code, good.Body.String())
	}
	if bad := upload("../../test.txt", archive.Bytes()); bad.Code != http.StatusBadRequest {
		t.Fatalf("bad extension: %d", bad.Code)
	}
	if bad := upload("test.zip", []byte("not zip")); bad.Code != http.StatusBadRequest {
		t.Fatalf("bad zip: %d", bad.Code)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "modpacks_cache", "uploads"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("upload cleanup: %v %v", entries, err)
	}
}

func TestWebModpackExportRoundTrip(t *testing.T) {
	container, router, _ := newTestServer(t)
	inst, err := container.Instances.Create(instances.CreateInstanceRequest{Name: "Round Trip", InstanceType: instances.TypeVanilla, MCVersion: "1.20.1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inst.GameDir, "config", "smoke.toml"), []byte("value=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, ct string, body *bytes.Buffer) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, body)
		req.Host = "127.0.0.1:8080"
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	payload, err := json.Marshal(map[string]any{"source_type": "instance", "source_id": inst.ID, "format": "modhub", "name": "Round Trip", "include_configs": true})
	if err != nil {
		t.Fatal(err)
	}
	export := request(http.MethodPost, "/api/modpacks/export", "application/json", bytes.NewBuffer(payload))
	var result struct {
		URL      string `json:"download_url"`
		Path     string `json:"output_path"`
		Embedded int    `json:"embedded_files"`
	}
	if err := json.Unmarshal(export.Body.Bytes(), &result); err != nil || export.Code != http.StatusOK || !strings.HasPrefix(result.URL, "/api/modpacks/export-file?token=") || result.Path != "" || result.Embedded != 1 {
		t.Fatalf("export: %d %s (%v)", export.Code, export.Body.String(), err)
	}
	download := request(http.MethodGet, result.URL, "", bytes.NewBuffer(nil))
	if download.Code != http.StatusOK {
		t.Fatalf("download: %d %s", download.Code, download.Body.String())
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("archive", "round-trip.modhub")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(download.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	preview := request(http.MethodPost, "/api/modpacks/upload-import", mw.FormDataContentType(), &body)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), "smoke.toml") {
		t.Fatalf("re-upload: %d %s", preview.Code, preview.Body.String())
	}
}

func TestModDetailsSingleSourceFailureDiagnostics(t *testing.T) {
	container, router, _ := newTestServer(t)
	cfg := container.GetConfig()
	cfg.CurseForgeAPIKey = "$2a$10$mocktestkey1234567890"
	container.SetConfigForTest(cfg)
	container.HTTP.SetAPIClient(&http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		if strings.Contains(req.URL.String(), "api.modrinth.com") {
			rec.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(req.URL.Path, "/version") {
				_, _ = rec.WriteString(`[]`)
			} else {
				_, _ = rec.WriteString(`{"id":"abc","slug":"sodium","title":"Sodium","body":"Modrinth body text"}`)
			}
			return rec.Result(), nil
		}
		rec.WriteHeader(http.StatusServiceUnavailable)
		_, _ = rec.WriteString(`service unavailable`)
		return rec.Result(), nil
	})})

	payload := map[string]any{
		"item": domain.ProjectCard{
			GroupID: "m::mr:abc+cf:999", Title: "Sodium", ContentType: "mod",
			Sources: []domain.ProjectSource{
				{Platform: "modrinth", SourceKey: "mr:abc", ID: "abc", Slug: "sodium", Title: "Sodium"},
				{Platform: "curseforge", SourceKey: "cf:999", ID: "999", Slug: "sodium", Title: "Sodium"},
			},
		},
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/mod/details", bytes.NewReader(raw))
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with partial success, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		OK             bool              `json:"ok"`
		Body           string            `json:"body"`
		Errors         map[string]string `json:"errors"`
		SourceStatuses map[string]string `json:"source_statuses"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Body != "Modrinth body text" || resp.SourceStatuses["modrinth"] != "ok" || resp.SourceStatuses["curseforge"] != "error" || resp.Errors["curseforge"] == "" {
		t.Fatalf("unexpected partial failure response: %+v", resp)
	}
}

func TestPrincipalSecurityHeadersAndErrorSanitization(t *testing.T) {
	_, router, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/health", nil)
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("expected X-Frame-Options: DENY, got %q", rec.Header().Get("X-Frame-Options"))
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("expected Referrer-Policy: no-referrer, got %q", rec.Header().Get("Referrer-Policy"))
	}
	if rec.Header().Get("Permissions-Policy") == "" {
		t.Fatal("expected Permissions-Policy header")
	}

	rec2 := httptest.NewRecorder()
	writeErr(rec2, http.StatusBadRequest, "failed with Bearer SUPER_SECRET_TOKEN_999999")
	if strings.Contains(rec2.Body.String(), "SUPER_SECRET_TOKEN_999999") {
		t.Fatalf("secret leaked in writeErr JSON body: %s", rec2.Body.String())
	}
}
