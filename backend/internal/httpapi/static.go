package httpapi

import (
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

func contentTypeForExt(ext string) string {
	switch ext {
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".html":
		return "text/html; charset=utf-8"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".json":
		return "application/json; charset=utf-8"
	default:
		return mime.TypeByExtension(ext)
	}
}

func serveAssetFromFS(w http.ResponseWriter, r *http.Request, fsys fs.FS, relPath string) bool {
	clean := strings.TrimPrefix(path.Clean("/"+relPath), "/")
	if clean == "" || clean == "." {
		clean = "index.html"
	}

	if _, err := fs.Stat(fsys, clean); err != nil {
		if !strings.Contains(path.Base(clean), ".") {
			clean = "index.html"
		} else {
			return false
		}
	}

	ext := strings.ToLower(path.Ext(clean))
	if ctype := contentTypeForExt(ext); ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	if strings.HasPrefix(clean, "assets/") && clean != "index.html" {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}

	ae := strings.ToLower(r.Header.Get("Accept-Encoding"))
	var data []byte
	var enc string
	if strings.Contains(ae, "br") {
		if b, err := fs.ReadFile(fsys, clean+".br"); err == nil && len(b) > 0 {
			data = b
			enc = "br"
		}
	}
	if data == nil && strings.Contains(ae, "gzip") {
		if g, err := fs.ReadFile(fsys, clean+".gz"); err == nil && len(g) > 0 {
			data = g
			enc = "gzip"
		}
	}
	if data == nil {
		raw, err := fs.ReadFile(fsys, clean)
		if err != nil {
			return false
		}
		data = raw
	}

	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	if ext == ".html" {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set(
			"Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; font-src 'self' data:; connect-src 'self' http://127.0.0.1:* http://localhost:* wails:; frame-ancestors 'none'; base-uri 'self'; object-src 'none'",
		)
	}
	if enc != "" {
		w.Header().Set("Content-Encoding", enc)
		w.Header().Set("Vary", "Accept-Encoding")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
	return true
}

func (s *Server) serveFrontend(w http.ResponseWriter, r *http.Request) {
	reqPath := strings.TrimPrefix(r.URL.Path, "/")
	if reqPath == "" {
		reqPath = "index.html"
	}

	if s.embeddedFS != nil {
		if serveAssetFromFS(w, r, s.embeddedFS, reqPath) {
			return
		}
	}

	if s.publicDir != "" {
		if serveAssetFromFS(w, r, os.DirFS(s.publicDir), reqPath) {
			return
		}
		cleanRel := filepath.Clean(reqPath)
		if !strings.HasPrefix(cleanRel, "..") {
			idx := filepath.Join(s.publicDir, "index.html")
			if _, err := os.Stat(idx); err == nil && !strings.Contains(filepath.Base(cleanRel), ".") {
				http.ServeFile(w, r, idx)
				return
			}
		}
	}

	http.NotFound(w, r)
}
