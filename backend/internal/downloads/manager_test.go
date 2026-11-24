package downloads

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"modhub/internal/domain"
	"modhub/internal/events"
	"modhub/internal/netx"
	"modhub/internal/storage"
)

func newTestManager(t *testing.T, srv *httptest.Server, maxConc int) (*Manager, *storage.Database, *events.Bus, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.OpenDatabase(filepath.Join(dir, "dl_test.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	hc := netx.NewHTTPClient(db)
	if srv != nil {
		hc.SetLauncherDownloadClient(srv.Client())
		hc.SetLauncherDownloadValidator(func(raw string) (*url.URL, error) { return url.Parse(raw) })
	}
	bus := events.NewBus()
	t.Cleanup(func() { bus.Close() })

	mgr := NewManager(hc, db, bus, maxConc)
	mgr.SetRetryBaseDelay(15 * time.Millisecond)
	t.Cleanup(func() { mgr.Close() })
	return mgr, db, bus, dir
}

func sha1Hex(b []byte) string {
	s := sha1.Sum(b)
	return hex.EncodeToString(s[:])
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// Covers scenarios 1, 2, 9, 10, 11: small file, large file (5 MB streamed), SHA-1, SHA-256, checksum mismatch
func TestDownloadSmallLargeChecksumsAndMismatch(t *testing.T) {
	smallData := []byte("hello-modhub-download-manager")
	largeData := bytes.Repeat([]byte("MODHUB0123456789"), 320*1024) // 5 MB

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/small":
			w.Header().Set("Content-Length", strconv.Itoa(len(smallData)))
			_, _ = w.Write(smallData)
		case "/large":
			w.Header().Set("Content-Length", strconv.Itoa(len(largeData)))
			_, _ = w.Write(largeData)
		}
	}))
	defer srv.Close()

	mgr, _, _, dir := newTestManager(t, srv, 4)

	// 1 + 9: Small file with SHA-1
	destSmall := filepath.Join(dir, "small.bin")
	v1, err := mgr.Enqueue(DownloadRequest{
		Name: "small.bin", URL: srv.URL + "/small", DestPath: destSmall,
		ExpectedSize: int64(len(smallData)), ChecksumAlgo: ChecksumSHA1, ChecksumValue: sha1Hex(smallData),
	})
	if err != nil {
		t.Fatalf("Enqueue small: %v", err)
	}
	fin1, err := mgr.Wait(context.Background(), v1.ID)
	if err != nil || fin1.Status != StatusCompleted || !fin1.Verified {
		t.Fatalf("small SHA-1 failed: view=%+v err=%v", fin1, err)
	}
	if got, _ := os.ReadFile(destSmall); !bytes.Equal(got, smallData) {
		t.Fatal("small file content mismatch")
	}

	// 2 + 10: Large 5 MB file with SHA-256
	destLarge := filepath.Join(dir, "large.bin")
	v2, err := mgr.Enqueue(DownloadRequest{
		Name: "large.bin", URL: srv.URL + "/large", DestPath: destLarge,
		ExpectedSize: int64(len(largeData)), ChecksumAlgo: ChecksumSHA256, ChecksumValue: sha256Hex(largeData),
	})
	if err != nil {
		t.Fatalf("Enqueue large: %v", err)
	}
	fin2, err := mgr.Wait(context.Background(), v2.ID)
	if err != nil || fin2.Status != StatusCompleted || !fin2.Verified || fin2.DownloadedBytes != int64(len(largeData)) {
		t.Fatalf("large SHA-256 failed: view=%+v err=%v", fin2, err)
	}

	// 11: Checksum mismatch MUST fail and NEVER overwrite existing working file
	destBad := filepath.Join(dir, "protected.bin")
	_ = os.WriteFile(destBad, []byte("ORIGINAL-GOOD"), 0o644)
	v3, err := mgr.Enqueue(DownloadRequest{
		Name: "bad.bin", URL: srv.URL + "/small", DestPath: destBad,
		ChecksumAlgo: ChecksumSHA256, ChecksumValue: strings.Repeat("0", 64),
	})
	if err != nil {
		t.Fatalf("Enqueue bad: %v", err)
	}
	fin3, _ := mgr.Wait(context.Background(), v3.ID)
	if fin3.Status != StatusError || !strings.Contains(fin3.Error, "несовпадение контрольной суммы") {
		t.Fatalf("expected checksum error, got %+v", fin3)
	}
	if got, _ := os.ReadFile(destBad); string(got) != "ORIGINAL-GOOD" {
		t.Fatalf("existing file was corrupted on checksum failure: %q", string(got))
	}
}

// Covers scenarios 3, 4, 6, 7: parallel downloads, cancel, connection drop + retry, retry after error
func TestParallelCancelDropAndRetry(t *testing.T) {
	var flakyCalls atomic.Int32
	var dropOnce atomic.Int32
	payload := bytes.Repeat([]byte("Z"), 64*1024)
	blockGate := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = w.Write(payload)
		case "/flaky":
			if flakyCalls.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = w.Write(payload)
		case "/drop":
			if dropOnce.Add(1) == 1 {
				w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
				_, _ = w.Write(payload[:4096])
				// Premature EOF (simulated dropped connection)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = w.Write(payload)
		case "/fail-first-run":
			if flakyCalls.Load() < 10 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = w.Write(payload)
		case "/slow":
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = w.Write(payload[:1024])
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			select {
			case <-blockGate:
			case <-r.Context().Done():
			}
		}
	}))
	defer func() {
		close(blockGate)
		srv.Close()
	}()

	mgr, _, _, dir := newTestManager(t, srv, 3)

	// 3: Parallel downloads (3 concurrent + 1 queued)
	var ids []string
	for i := 0; i < 4; i++ {
		u := srv.URL + "/ok"
		if i == 1 {
			u = srv.URL + "/flaky"
		} else if i == 2 {
			u = srv.URL + "/drop"
		}
		v, err := mgr.Enqueue(DownloadRequest{Name: fmt.Sprintf("f%d.bin", i), URL: u, DestPath: filepath.Join(dir, fmt.Sprintf("f%d.bin", i))})
		if err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
		ids = append(ids, v.ID)
	}
	for _, id := range ids {
		fin, err := mgr.Wait(context.Background(), id)
		if err != nil || fin.Status != StatusCompleted {
			t.Fatalf("parallel task %s failed: %+v err=%v", id, fin, err)
		}
	}

	// 4: Cancel active download
	vSlow, err := mgr.Enqueue(DownloadRequest{Name: "slow.bin", URL: srv.URL + "/slow", DestPath: filepath.Join(dir, "slow.bin")})
	if err != nil {
		t.Fatalf("Enqueue slow: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := mgr.Cancel(vSlow.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	finSlow, _ := mgr.Wait(context.Background(), vSlow.ID)
	if finSlow.Status != StatusCancelled {
		t.Fatalf("expected cancelled, got %+v", finSlow)
	}

	// 6: Retry after permanent error
	vErr, _ := mgr.Enqueue(DownloadRequest{Name: "retry.bin", URL: srv.URL + "/fail-first-run", DestPath: filepath.Join(dir, "retry.bin")})
	finErr, _ := mgr.Wait(context.Background(), vErr.ID)
	if finErr.Status != StatusError {
		t.Fatalf("expected initial error, got %+v", finErr)
	}
	flakyCalls.Store(20)
	if _, err := mgr.Retry(vErr.ID); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	finRetry, _ := mgr.Wait(context.Background(), vErr.ID)
	if finRetry.Status != StatusCompleted {
		t.Fatalf("expected completed after Retry, got %+v", finRetry)
	}
}

// Covers scenarios 5 & 8: Pause/Resume with Range support AND server without Range support
func TestPauseResumeWithAndWithoutHTTPRange(t *testing.T) {
	payload := bytes.Repeat([]byte("RANGEDATA!"), 20*1024) // 200 KB
	pauseGate := make(chan struct{})
	var rangeHeaderSeen atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/range":
			w.Header().Set("Accept-Ranges", "bytes")
			if rng := r.Header.Get("Range"); strings.HasPrefix(rng, "bytes=") {
				rangeHeaderSeen.Store(true)
				startStr := strings.TrimSuffix(strings.TrimPrefix(rng, "bytes="), "-")
				start, _ := strconv.Atoi(startStr)
				if start < 0 || start >= len(payload) {
					w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
					return
				}
				chunk := payload[start:]
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
				w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(chunk)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = w.Write(payload[:16384])
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			select {
			case <-pauseGate:
				_, _ = w.Write(payload[16384:])
			case <-r.Context().Done():
			}
		case "/no-range":
			// Always ignores Range and sends 200 OK full payload
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
		}
	}))
	defer func() {
		close(pauseGate)
		srv.Close()
	}()

	mgr, _, _, dir := newTestManager(t, srv, 2)

	// 5: Pause & Resume with Range
	destRange := filepath.Join(dir, "resumed.bin")
	v, err := mgr.Enqueue(DownloadRequest{
		Name: "resumed.bin", URL: srv.URL + "/range", DestPath: destRange,
		ChecksumAlgo: ChecksumSHA256, ChecksumValue: sha256Hex(payload),
	})
	if err != nil {
		t.Fatalf("Enqueue range: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := mgr.Pause(v.ID); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := mgr.Resume(v.ID); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	fin, err := mgr.Wait(context.Background(), v.ID)
	if err != nil || fin.Status != StatusCompleted || !fin.Verified {
		t.Fatalf("resumed download failed: %+v err=%v", fin, err)
	}
	if !rangeHeaderSeen.Load() {
		t.Fatal("expected HTTP Range header on resume")
	}

	// 8: Pre-seed partial .mhpart and download from server WITHOUT Range support -> must cleanly restart & not corrupt!
	destNoRange := filepath.Join(dir, "norange.bin")
	_ = os.WriteFile(destNoRange+".mhpart", []byte("STALE-PARTIAL-PREFIX"), 0o644)
	v2, err := mgr.Enqueue(DownloadRequest{
		Name: "norange.bin", URL: srv.URL + "/no-range", DestPath: destNoRange,
		ChecksumAlgo: ChecksumSHA256, ChecksumValue: sha256Hex(payload),
	})
	if err != nil {
		t.Fatalf("Enqueue no-range: %v", err)
	}
	fin2, err := mgr.Wait(context.Background(), v2.ID)
	if err != nil || fin2.Status != StatusCompleted || !fin2.Verified {
		t.Fatalf("no-range server download failed: %+v err=%v", fin2, err)
	}
	if got, _ := os.ReadFile(destNoRange); !bytes.Equal(got, payload) {
		t.Fatal("no-range file corrupted")
	}
}

// Covers scenario 12: Restart persistence (completed NOT re-run; incomplete recovered cleanly)
func TestRestartPersistenceAndRecovery(t *testing.T) {
	var hits atomic.Int32
	payload := []byte("PERSIST-QUEUE-DATA")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")
	db1, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}

	now := time.Now().Unix()
	_ = db1.UpsertDownloadTask(domain.DownloadTaskRecord{ID: "done_1", Name: "done.bin", Category: "generic", URL: srv.URL + "/done", DestPath: filepath.Join(dir, "done.bin"), TempPath: filepath.Join(dir, "done.bin.mhpart"), Status: string(StatusCompleted), CreatedAt: now - 10, UpdatedAt: now - 10})
	_ = db1.UpsertDownloadTask(domain.DownloadTaskRecord{ID: "rec_1", Name: "rec.bin", Category: "generic", URL: srv.URL + "/rec", DestPath: filepath.Join(dir, "rec.bin"), TempPath: filepath.Join(dir, "rec.bin.mhpart"), Status: string(StatusDownloading), MaxRetries: 2, CreatedAt: now - 5, UpdatedAt: now - 5})

	hc := netx.NewHTTPClient(db1)
	hc.SetLauncherDownloadClient(srv.Client())
	hc.SetLauncherDownloadValidator(func(raw string) (*url.URL, error) { return url.Parse(raw) })
	mgr := NewManager(hc, db1, nil, 2)
	defer func() {
		mgr.Close()
		_ = db1.Close()
	}()

	finRec, err := mgr.Wait(context.Background(), "rec_1")
	if err != nil || finRec.Status != StatusCompleted {
		t.Fatalf("expected recovered task to complete, got %+v err=%v", finRec, err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected exactly 1 HTTP request (completed task must NOT re-run), got %d", hits.Load())
	}
}

// Covers scenarios 13 & 14: Invalid/SSRF URL rejected AND disk write error handled cleanly
func TestSecurityURLRejectionAndDiskWriteError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("DATA"))
	}))
	defer srv.Close()

	// 13: Real default validator MUST block localhost / untrusted host / non-HTTPS
	mgrStrict, _, _, dir := newTestManager(t, nil, 2)
	for _, badURL := range []string{
		"http://cdn.modrinth.com/a.jar",
		"https://127.0.0.1/a.jar",
		"https://evil.example.com/a.jar",
		"file:///etc/passwd",
	} {
		if _, err := mgrStrict.Enqueue(DownloadRequest{Name: "bad", URL: badURL, DestPath: filepath.Join(dir, "x.bin")}); err == nil {
			t.Errorf("expected URL %q to be rejected by security allowlist", badURL)
		}
	}

	// 14: Disk write error (parent path is an existing regular file)
	mgrDisk, _, _, dir2 := newTestManager(t, srv, 2)
	blocker := filepath.Join(dir2, "not_a_dir")
	_ = os.WriteFile(blocker, []byte("x"), 0o600)
	v, err := mgrDisk.Enqueue(DownloadRequest{Name: "diskerr.bin", URL: srv.URL + "/ok", DestPath: filepath.Join(blocker, "child.bin")})
	if err != nil {
		t.Fatalf("Enqueue diskerr: %v", err)
	}
	fin, _ := mgrDisk.Wait(context.Background(), v.ID)
	if fin.Status != StatusError || fin.Error == "" {
		t.Fatalf("expected StatusError on disk write failure, got %+v", fin)
	}
}
