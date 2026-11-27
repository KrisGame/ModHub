package httpapi

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"modhub/internal/modpacks"
)

const maxImportUploadBytes = int64(4) << 30

// handleUploadModpackImport streams one local archive into the private import cache.
// The existing PreviewImport path performs archive validation and registers the preview.
func (s *Server) handleUploadModpackImport(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		writeErr(w, http.StatusUnsupportedMediaType, "multipart/form-data required")
		return
	}
	if r.ContentLength > maxImportUploadBytes+1024*1024 {
		writeErr(w, http.StatusRequestEntityTooLarge, "archive too large")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImportUploadBytes+1024*1024)
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid multipart request")
		return
	}
	part, err := mr.NextPart()
	if err != nil || part.FormName() != "archive" {
		writeErr(w, http.StatusBadRequest, "archive field required")
		return
	}
	name := part.FileName()
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".mrpack" && ext != ".modhub" && ext != ".zip" {
		writeErr(w, http.StatusBadRequest, "unsupported archive extension")
		return
	}
	dir := filepath.Join(s.container.DataDir, "modpacks_cache", "uploads")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot create upload directory")
		return
	}
	// Clean orphaned uploads left by a previous process. Active previews expire
	// after two hours; keep an additional margin for imports already in progress.
	if entries, err := os.ReadDir(dir); err == nil {
		cutoff := time.Now().Add(-24 * time.Hour)
		for _, entry := range entries {
			if info, err := entry.Info(); err == nil && info.Mode().IsRegular() && strings.HasPrefix(entry.Name(), "import-") && info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(dir, entry.Name()))
			}
		}
	}
	// Uploaded archives outlive the preview request for asynchronous execution;
	// prune abandoned archives only after the preview cache has expired.
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "import-") {
				continue
			}
			if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
				_ = os.Remove(filepath.Join(dir, entry.Name()))
			}
		}
	}
	f, err := os.CreateTemp(dir, "import-*")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot create upload file")
		return
	}
	path := f.Name()
	defer part.Close()
	defer func() { _ = f.Close() }()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()
	n, copyErr := io.Copy(f, io.LimitReader(part, maxImportUploadBytes+1))
	if closeErr := f.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil || n > maxImportUploadBytes {
		var sizeErr *http.MaxBytesError
		if n > maxImportUploadBytes || errors.As(copyErr, &sizeErr) {
			writeErr(w, http.StatusRequestEntityTooLarge, "archive too large")
		} else {
			writeErr(w, http.StatusBadRequest, "cannot read archive")
		}
		return
	}
	if n == 0 {
		writeErr(w, http.StatusBadRequest, "empty archive")
		return
	}
	if extra, err := mr.NextPart(); err != io.EOF || extra != nil {
		if extra != nil {
			_ = extra.Close()
		}
		writeErr(w, http.StatusBadRequest, "only one archive allowed")
		return
	}
	// zip.OpenReader accepts any extension; the filename is private and not user-controlled.
	preview, err := s.container.Modpacks.PreviewImport(r.Context(), modpacks.PreviewImportRequest{SourceType: "file", SourceRef: path})
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("archive preview failed: %v", err))
		return
	}
	keep = true
	preview.ResolvedArchivePath = ""
	preview.SourceRef = filepath.Base(name)
	writeJSON(w, http.StatusOK, preview)
}
