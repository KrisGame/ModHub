package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const exportLifetime = 2 * time.Hour

type registeredExport struct {
	path    string
	base    string
	name    string
	expires time.Time
}

type exportRegistry struct {
	mu    sync.Mutex
	files map[string]registeredExport
}

func (s *Server) registerExport(base, filename, downloadName string) (string, error) {
	if err := ensurePathWithin(base, filename); err != nil {
		return "", err
	}
	var token [24]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	key := hex.EncodeToString(token[:])
	s.exports.mu.Lock()
	defer s.exports.mu.Unlock()
	if s.exports.files == nil {
		s.exports.files = make(map[string]registeredExport)
	}
	s.exports.files[key] = registeredExport{path: filename, base: base, name: downloadName, expires: time.Now().Add(exportLifetime)}
	time.AfterFunc(exportLifetime, func() {
		s.exports.mu.Lock()
		defer s.exports.mu.Unlock()
		s.removeRegisteredExport(key)
	})
	return key, nil
}

func (s *Server) removeRegisteredExport(key string) {
	record, ok := s.exports.files[key]
	if !ok {
		return
	}
	delete(s.exports.files, key)
	if ensurePathWithin(record.base, record.path) == nil {
		_ = os.Remove(record.path)
		if dir := filepath.Dir(record.path); dir != record.base {
			_ = os.Remove(dir)
		}
	}
}

func (s *Server) serveRegisteredExport(w http.ResponseWriter, r *http.Request, base string, allowedExt ...string) {
	key := r.URL.Query().Get("token")
	s.exports.mu.Lock()
	record, ok := s.exports.files[key]
	if ok && time.Now().After(record.expires) {
		s.removeRegisteredExport(key)
		ok = false
	}
	s.exports.mu.Unlock()
	if !ok || record.base != base || ensurePathWithin(base, record.path) != nil {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(filepath.Ext(record.path))
	valid := false
	for _, candidate := range allowedExt {
		if ext == candidate {
			valid = true
			break
		}
	}
	info, err := os.Stat(record.path)
	if err != nil || !info.Mode().IsRegular() || !valid {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": record.name}))
	http.ServeFile(w, r, record.path)
}
