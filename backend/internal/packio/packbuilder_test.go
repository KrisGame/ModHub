package packio

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"modhub/internal/domain"
	"modhub/internal/netx"
)

func TestSanitizeZIPPathAndWindowsReserved(t *testing.T) {
	for _, tc := range [][3]string{
		{"mods", "sodium-0.6.jar", "mods/sodium-0.6.jar"},
		{"", "lithium.jar", "mods/lithium.jar"},
		{"resourcepacks", "Faithful 32x.zip", "resourcepacks/Faithful 32x.zip"},
	} {
		if got, err := SanitizeZIPPath(tc[0], tc[1]); err != nil || got != tc[2] {
			t.Errorf("SanitizeZIPPath(%q,%q) = (%q,%v), want %q", tc[0], tc[1], got, err, tc[2])
		}
	}
	for _, tc := range [][2]string{
		{"mods", "../evil.jar"}, {"mods", "sub/evil.jar"}, {"mods", `sub\evil.jar`},
		{"mods", "manifest.txt"}, {"mods", ".hidden.jar"}, {"plugins", "foo.jar"},
		{"mods", "CON"}, {"mods", "con.jar"}, {"mods", "NUL.zip"}, {"mods", "COM1.jar"},
		{"mods", "mod*.jar"}, {"mods", "mod|pipe.jar"},
	} {
		if got, err := SanitizeZIPPath(tc[0], tc[1]); err == nil {
			t.Errorf("expected SanitizeZIPPath(%q,%q) rejected, got %q", tc[0], tc[1], got)
		}
	}
}

func TestStreamPackZIPIntegrityAndDeduplication(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.jar":
			_, _ = w.Write([]byte("JAR-DATA-A"))
		case "/b.jar":
			_, _ = w.Write([]byte("JAR-DATA-B"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	hc := netx.NewHTTPClient(nil)
	hc.SetDownloadClient(srv.Client())
	hc.SetDownloadValidator(func(raw string) (*url.URL, error) { return url.Parse(raw) })

	items := []domain.CartItem{
		{Title: "Mod A", Folder: "mods", Filename: "same.jar", DownloadURL: srv.URL + "/a.jar"},
		{Title: "Mod B", Folder: "mods", Filename: "same.jar", DownloadURL: srv.URL + "/b.jar"},
		{Title: "Manual Mod", Folder: "mods", Filename: "manual.jar", DownloadURL: ""},
	}
	var out bytes.Buffer
	if err := StreamPackZIP(context.Background(), &out, hc, items); err != nil {
		t.Fatalf("StreamPackZIP failed: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("invalid ZIP: %v", err)
	}
	names := map[string]bool{}
	var manifestText string
	for _, f := range zr.File {
		names[f.Name] = true
		if f.Name == "manifest.txt" {
			rc, _ := f.Open()
			var mb bytes.Buffer
			_, _ = mb.ReadFrom(rc)
			_ = rc.Close()
			manifestText = mb.String()
		}
	}
	if !names["mods/same.jar"] || !names["mods/same (2).jar"] || !names["manifest.txt"] || !strings.Contains(manifestText, "Manual Mod") {
		t.Fatalf("unexpected ZIP contents: %+v", names)
	}
}
