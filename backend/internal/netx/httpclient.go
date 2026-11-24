package netx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"modhub/internal/domain"
	"modhub/internal/security"
	"modhub/internal/storage"
)

type CacheStore interface {
	CacheGet(key string) (string, bool)
	CacheSet(key, payload string, ttlSecs int64)
}

type HTTPClient struct {
	ctx           context.Context
	transport     *http.Transport
	dlTransport   *http.Transport
	apiClient     *http.Client
	browserClient *http.Client
	dlClient         *http.Client
	launcherDLClient *http.Client
	dlValidator      func(string) (*url.URL, error)
	launcherDLVal    func(string) (*url.URL, error)
	db            CacheStore
	semMR         chan struct{}
	semCFAPI      chan struct{}
	semCFWeb      chan struct{}
	sfJSON        *storage.SingleFlight[[]byte]
}

func NewHTTPClient(db CacheStore) *HTTPClient {
	baseDialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy:                 ResolveProxyFunc,
		DialContext:           baseDialer.DialContext,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	dlTransport := &http.Transport{
		Proxy:                 ResolveProxyFunc,
		DialContext:           MakeSafeDialContext(baseDialer),
		MaxIdleConns:          24,
		MaxIdleConnsPerHost:   12,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &HTTPClient{
		transport:     transport,
		dlTransport:   dlTransport,
		apiClient:     &http.Client{Timeout: 16 * time.Second, Transport: transport},
		browserClient: &http.Client{Timeout: 14 * time.Second, Transport: transport},
		dlClient: &http.Client{
			Timeout:       120 * time.Second,
			Transport:     dlTransport,
			CheckRedirect: MakeSafeRedirectChecker(ValidateDownloadURL),
		},
		launcherDLClient: &http.Client{
			// No total Client.Timeout so large files don't abort mid-stream; dial/TLS/header timeouts are enforced on dlTransport.
			Transport:     dlTransport,
			CheckRedirect: MakeSafeRedirectChecker(ValidateLauncherDownloadURL),
		},
		dlValidator:   ValidateDownloadURL,
		launcherDLVal: ValidateLauncherDownloadURL,
		db:          db,
		semMR:       make(chan struct{}, 10),
		semCFAPI:    make(chan struct{}, 4),
		semCFWeb:    make(chan struct{}, 3),
		sfJSON:      storage.NewSingleFlight[[]byte](),
	}
}

func (c *HTTPClient) ReqCtx() context.Context {
	if c != nil && c.ctx != nil {
		return c.ctx
	}
	return context.Background()
}

// WithContext returns a lightweight shallow clone bound to `ctx` so all downstream API calls
// immediately cancel when the user switches filters/tabs or a batch timeout expires.
func (c *HTTPClient) WithContext(ctx context.Context) *HTTPClient {
	if c == nil || ctx == nil {
		return c
	}
	cp := *c
	cp.ctx = ctx
	return &cp
}

func (c *HTTPClient) ResetConnections() {
	if c.transport != nil {
		c.transport.CloseIdleConnections()
	}
	if c.dlTransport != nil {
		c.dlTransport.CloseIdleConnections()
	}
}

func (c *HTTPClient) semFor(rawURL string) chan struct{} {
	if strings.Contains(rawURL, "api.curseforge.com") {
		return c.semCFAPI
	}
	if strings.Contains(rawURL, "www.curseforge.com") {
		return c.semCFWeb
	}
	return c.semMR
}

func AcquireSemCtx(ctx context.Context, sem chan struct{}) bool {
	select {
	case sem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func SleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func retryDelayForResponse(resp *http.Response, attempt int) time.Duration {
	base := time.Duration(240*(1<<attempt)) * time.Millisecond
	if resp == nil {
		return base
	}
	if ra := strings.TrimSpace(resp.Header.Get("Retry-After")); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
			d := time.Duration(secs) * time.Second
			if d > 3*time.Second {
				d = 3 * time.Second
			}
			return d
		}
	}
	return base
}

func isRetryableHTTPStatus(code int) bool {
	return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500
}

func (c *HTTPClient) GetJSON(rawURL string, headers map[string]string, cacheTTL int64, dst any) error {
	ctx := c.ReqCtx()
	if err := ctx.Err(); err != nil {
		return err
	}
	cacheKey := rawURL
	if _, hasKey := headers["x-api-key"]; hasKey {
		cacheKey = "cfk|" + rawURL
	}
	if cacheTTL > 0 && c.db != nil {
		if cached, ok := c.db.CacheGet(cacheKey); ok {
			if err := json.Unmarshal([]byte(cached), dst); err == nil {
				return nil
			}
		}
	}

	rawBytes, err := c.sfJSON.DoCtx(ctx, cacheKey, func() ([]byte, error) {
		// Decouple shared SingleFlight execution from the first caller's cancellation
		// so cancelling one waiter never aborts other concurrent waiters for the same key.
		workCtx, cancel := context.WithTimeout(context.Background(), 22*time.Second)
		defer cancel()

		if cacheTTL > 0 && c.db != nil {
			if cached, ok := c.db.CacheGet(cacheKey); ok {
				return []byte(cached), nil
			}
		}
		var lastErr error
		for attempt := 0; attempt < 3; attempt++ {
			if err := workCtx.Err(); err != nil {
				return nil, err
			}
			sem := c.semFor(rawURL)
			if !AcquireSemCtx(workCtx, sem) {
				return nil, workCtx.Err()
			}
			req, err := http.NewRequestWithContext(workCtx, http.MethodGet, rawURL, nil)
			if err != nil {
				<-sem
				return nil, err
			}
			req.Header.Set("User-Agent", domain.UserAgent)
			req.Header.Set("Accept", "application/json")
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			resp, err := c.apiClient.Do(req)
			<-sem

			var delay time.Duration
			if err != nil {
				if workCtx.Err() != nil {
					return nil, workCtx.Err()
				}
				lastErr = fmt.Errorf("network error: %w", err)
				delay = retryDelayForResponse(nil, attempt)
			} else {
				body, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxAPIBodyBytes))
				_ = resp.Body.Close()
				if readErr != nil {
					lastErr = fmt.Errorf("read error: %w", readErr)
					delay = retryDelayForResponse(nil, attempt)
				} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					if json.Valid(body) {
						if cacheTTL > 0 && c.db != nil {
							c.db.CacheSet(cacheKey, string(body), cacheTTL)
						}
						return body, nil
					}
					lastErr = fmt.Errorf("invalid JSON response")
					delay = retryDelayForResponse(nil, attempt)
				} else {
					snippet := strings.TrimSpace(string(body))
					if len(snippet) > 160 {
						snippet = snippet[:160]
					}
					lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet)
					if !isRetryableHTTPStatus(resp.StatusCode) {
						return nil, lastErr
					}
					delay = retryDelayForResponse(resp, attempt)
				}
			}
			if attempt < 2 {
				if !SleepCtx(workCtx, delay) {
					return nil, workCtx.Err()
				}
			}
		}
		return nil, lastErr
	})
	if err != nil {
		return err
	}
	return json.Unmarshal(rawBytes, dst)
}

func (c *HTTPClient) PostJSON(rawURL string, payload any, headers map[string]string, dst any) error {
	ctx := c.ReqCtx()
	if err := ctx.Err(); err != nil {
		return err
	}
	rawBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		sem := c.semFor(rawURL)
		if !AcquireSemCtx(ctx, sem) {
			return ctx.Err()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(rawBody))
		if err != nil {
			<-sem
			return err
		}
		req.Header.Set("User-Agent", domain.UserAgent)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := c.apiClient.Do(req)
		<-sem
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = err
			if attempt == 0 && !SleepCtx(ctx, 250*time.Millisecond) {
				return ctx.Err()
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxAPIBodyBytes))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if attempt == 0 && !SleepCtx(ctx, 250*time.Millisecond) {
				return ctx.Err()
			}
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return json.Unmarshal(body, dst)
		}
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 160 {
			snippet = snippet[:160]
		}
		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet)
		if attempt == 0 && isRetryableHTTPStatus(resp.StatusCode) {
			if !SleepCtx(ctx, retryDelayForResponse(resp, attempt)) {
				return ctx.Err()
			}
			continue
		}
		return lastErr
	}
	return lastErr
}

func (c *HTTPClient) GetBrowserJSON(rawURL, referer string, cacheTTL int64, dst any) error {
	ctx := c.ReqCtx()
	if err := ctx.Err(); err != nil {
		return err
	}
	cacheKey := "br|" + rawURL
	if cacheTTL > 0 && c.db != nil {
		if cached, ok := c.db.CacheGet(cacheKey); ok {
			if err := json.Unmarshal([]byte(cached), dst); err == nil {
				return nil
			}
		}
	}

	rawBytes, err := c.sfJSON.DoCtx(ctx, cacheKey, func() ([]byte, error) {
		workCtx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
		defer cancel()

		if cacheTTL > 0 && c.db != nil {
			if cached, ok := c.db.CacheGet(cacheKey); ok {
				return []byte(cached), nil
			}
		}
		sem := c.semFor(rawURL)
		if !AcquireSemCtx(workCtx, sem) {
			return nil, workCtx.Err()
		}
		var lastErr error
		for attempt := 0; attempt < 2; attempt++ {
			req, err := http.NewRequestWithContext(workCtx, http.MethodGet, rawURL, nil)
			if err != nil {
				<-sem
				return nil, err
			}
			req.Header.Set("User-Agent", domain.BrowserUserAgent)
			req.Header.Set("Accept", "application/json, text/plain, */*")
			req.Header.Set("Accept-Language", "en-US,en;q=0.9")
			req.Header.Set("X-Requested-With", "XMLHttpRequest")
			req.Header.Set("Sec-Fetch-Dest", "empty")
			req.Header.Set("Sec-Fetch-Mode", "cors")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			if referer != "" {
				req.Header.Set("Referer", referer)
				if uRef, parseErr := url.Parse(referer); parseErr == nil && uRef.Scheme != "" && uRef.Host != "" {
					req.Header.Set("Origin", uRef.Scheme+"://"+uRef.Host)
				}
			}
			resp, err := c.browserClient.Do(req)
			if err != nil {
				lastErr = err
				if attempt == 0 && workCtx.Err() == nil {
					if !SleepCtx(workCtx, 250*time.Millisecond) {
						<-sem
						return nil, workCtx.Err()
					}
					continue
				}
				<-sem
				return nil, lastErr
			}
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxAPIBodyBytes))
			_ = resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
				if attempt == 0 && workCtx.Err() == nil {
					if !SleepCtx(workCtx, 250*time.Millisecond) {
						<-sem
						return nil, workCtx.Err()
					}
					continue
				}
				<-sem
				return nil, lastErr
			}
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				<-sem
				if !json.Valid(body) {
					return nil, fmt.Errorf("invalid JSON response")
				}
				if cacheTTL > 0 && c.db != nil {
					c.db.CacheSet(cacheKey, string(body), cacheTTL)
				}
				return body, nil
			}
			snippet := strings.TrimSpace(string(body))
			if len(snippet) > 120 {
				snippet = snippet[:120]
			}
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet)
			// Only retry on transient 429/5xx, never retry Cloudflare 403/404
			if attempt == 0 && (resp.StatusCode == 429 || resp.StatusCode >= 500) && workCtx.Err() == nil {
				if !SleepCtx(workCtx, retryDelayForResponse(resp, attempt)) {
					<-sem
					return nil, workCtx.Err()
				}
				continue
			}
			<-sem
			return nil, lastErr
		}
		<-sem
		return nil, lastErr
	})
	if err != nil {
		return err
	}
	return json.Unmarshal(rawBytes, dst)
}

func (c *HTTPClient) ValidateDLURL(rawURL string) error {
	if c.dlValidator != nil {
		_, err := c.dlValidator(rawURL)
		return err
	}
	_, err := ValidateDownloadURL(rawURL)
	return err
}

// OpenDownloadStream validates the URL (and every redirect), opens a streaming HTTP response,
// and rejects responses whose declared Content-Length exceeds maxBytes.
func (c *HTTPClient) OpenDownloadStream(ctx context.Context, rawURL string, maxBytes int64) (*http.Response, error) {
	if ctx == nil {
		ctx = c.ReqCtx()
	}
	if maxBytes <= 0 {
		maxBytes = MaxDownloadFileBytes
	}
	targetURL := strings.TrimSpace(rawURL)
	if c.dlValidator != nil {
		u, err := c.dlValidator(targetURL)
		if err != nil {
			return nil, err
		}
		if u != nil {
			targetURL = u.String()
		}
	} else {
		u, err := ValidateDownloadURL(targetURL)
		if err != nil {
			return nil, err
		}
		targetURL = u.String()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", domain.UserAgent)
	resp, err := c.dlClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("размер файла (%d МБ) превышает лимит (%d МБ)", resp.ContentLength/(1024*1024), maxBytes/(1024*1024))
	}
	return resp, nil
}

// DownloadToWriter streams a validated download directly into dst while enforcing maxBytes
// on actual bytes read (independent of Content-Length) and honoring context cancellation.
func (c *HTTPClient) DownloadToWriter(ctx context.Context, rawURL string, dst io.Writer, maxBytes int64) (int64, string, error) {
	if maxBytes <= 0 {
		maxBytes = MaxDownloadFileBytes
	}
	resp, err := c.OpenDownloadStream(ctx, rawURL, maxBytes)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	written, err := CopyWithLimit(ctx, dst, resp.Body, maxBytes)
	if err != nil {
		return written, resp.Header.Get("Content-Type"), err
	}
	return written, resp.Header.Get("Content-Type"), nil
}

// DownloadToTempFile streams a validated download straight to a temporary disk file
// so large files never accumulate in RAM. On any error, the temp file is removed automatically.
func (c *HTTPClient) DownloadToTempFile(ctx context.Context, rawURL string, maxBytes int64) (string, int64, error) {
	if maxBytes <= 0 {
		maxBytes = MaxDownloadFileBytes
	}
	resp, err := c.OpenDownloadStream(ctx, rawURL, maxBytes)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp("", "modhub-dl-*.tmp")
	if err != nil {
		return "", 0, err
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}

	written, err := CopyWithLimit(ctx, tmp, resp.Body, maxBytes)
	if err != nil {
		cleanup()
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", 0, err
	}
	return tmpPath, written, nil
}

func CopyWithLimit(ctx context.Context, dst io.Writer, src io.Reader, maxBytes int64) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return total, err
			}
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > maxBytes {
				return total, fmt.Errorf("превышен максимальный размер файла (%d МБ)", maxBytes/(1024*1024))
			}
			nw, werr := dst.Write(buf[:n])
			if werr != nil {
				return total, werr
			}
			if nw != n {
				return total, io.ErrShortWrite
			}
		}
		if rerr == io.EOF {
			return total, nil
		}
		if rerr != nil {
			return total, rerr
		}
	}
}

func (c *HTTPClient) DownloadBytes(rawURL string) ([]byte, error) {
	var buf bytes.Buffer
	_, _, err := c.DownloadToWriter(c.ReqCtx(), rawURL, &buf, MaxDownloadFileBytes)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}


func (c *HTTPClient) probeEndpoint(rawURL string, headers map[string]string) domain.EndpointProbe {
	start := time.Now()
	probeClient := &http.Client{Timeout: 8 * time.Second, Transport: c.transport}
	req, err := http.NewRequestWithContext(c.ReqCtx(), http.MethodGet, rawURL, nil)
	if err != nil {
		return domain.EndpointProbe{OK: false, Status: "error", Error: err.Error()}
	}
	req.Header.Set("User-Agent", domain.UserAgent)
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := probeClient.Do(req)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return domain.EndpointProbe{OK: false, Status: "unreachable", LatencyMs: ms, Error: err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return domain.EndpointProbe{OK: true, Status: "ok", LatencyMs: ms}
	}
	return domain.EndpointProbe{
		OK:        false,
		Status:    fmt.Sprintf("http_%d", resp.StatusCode),
		LatencyMs: ms,
		Error:     fmt.Sprintf("HTTP %d", resp.StatusCode),
	}
}

func (c *HTTPClient) CheckAPIConnectivity(cfKeyRaw string) domain.NetworkCheckResult {
	InvalidateProxyCache()
	c.ResetConnections()
	proxyInfo := GetProxyStatus()
	cfKey := security.SanitizeCFKey(cfKeyRaw)

	var (
		mrProbe domain.EndpointProbe
		cfProbe domain.EndpointProbe
		wg      sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		mrProbe = c.probeEndpoint(domain.ModrinthAPI+"/tag/loader", nil)
	}()

	if cfKey != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfProbe = c.probeEndpoint(fmt.Sprintf("%s/games/%d", domain.CurseForgeAPI, domain.CurseForgeGameID), map[string]string{"x-api-key": cfKey})
		}()
	} else {
		cfProbe = domain.EndpointProbe{OK: false, Status: "no_key", Error: "API-ключ CurseForge не задан"}
	}
	wg.Wait()

	return domain.NetworkCheckResult{
		OK:         mrProbe.OK || cfProbe.OK,
		Proxy:      proxyInfo,
		Modrinth:   mrProbe,
		CurseForge: cfProbe,
	}
}

func (c *HTTPClient) SetAPIClient(client *http.Client) {
	if c != nil && client != nil {
		c.apiClient = client
	}
}

func (c *HTTPClient) SetBrowserClient(client *http.Client) {
	if c != nil && client != nil {
		c.browserClient = client
	}
}

func (c *HTTPClient) SetDownloadClient(client *http.Client) {
	if c != nil && client != nil {
		c.dlClient = client
	}
}

func (c *HTTPClient) SetDownloadValidator(fn func(string) (*url.URL, error)) {
	if c != nil {
		c.dlValidator = fn
	}
}

func (c *HTTPClient) SetLauncherDownloadClient(client *http.Client) {
	if c != nil && client != nil {
		c.launcherDLClient = client
	}
}

func (c *HTTPClient) SetLauncherDownloadValidator(fn func(string) (*url.URL, error)) {
	if c != nil {
		c.launcherDLVal = fn
	}
}

func (c *HTTPClient) GetText(rawURL string, cacheTTL int64) (string, error) {
	ctx := c.ReqCtx()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := c.ValidateLauncherURL(rawURL); err != nil {
		return "", err
	}
	cacheKey := "txt|" + rawURL
	if cacheTTL > 0 && c.db != nil {
		if cached, ok := c.db.CacheGet(cacheKey); ok {
			return cached, nil
		}
	}
	rawBytes, err := c.sfJSON.DoCtx(ctx, cacheKey, func() ([]byte, error) {
		workCtx, cancel := context.WithTimeout(context.Background(), 22*time.Second)
		defer cancel()
		var lastErr error
		for attempt := 0; attempt < 3; attempt++ {
			if err := workCtx.Err(); err != nil {
				return nil, err
			}
			req, err := http.NewRequestWithContext(workCtx, http.MethodGet, rawURL, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", domain.UserAgent)
			resp, err := c.apiClient.Do(req)
			if err != nil {
				lastErr = fmt.Errorf("network error: %w", err)
			} else {
				body, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxAPIBodyBytes))
				_ = resp.Body.Close()
				if readErr != nil {
					lastErr = fmt.Errorf("read error: %w", readErr)
				} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					if cacheTTL > 0 && c.db != nil {
						c.db.CacheSet(cacheKey, string(body), cacheTTL)
					}
					return body, nil
				} else {
					lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
					if !isRetryableHTTPStatus(resp.StatusCode) {
						return nil, lastErr
					}
				}
			}
			if attempt < 2 && !SleepCtx(workCtx, retryDelayForResponse(nil, attempt)) {
				return nil, workCtx.Err()
			}
		}
		return nil, lastErr
	})
	if err != nil {
		return "", err
	}
	return string(rawBytes), nil
}

func (c *HTTPClient) ValidateLauncherURL(rawURL string) (*url.URL, error) {
	if c != nil && c.launcherDLVal != nil {
		return c.launcherDLVal(rawURL)
	}
	return ValidateLauncherDownloadURL(rawURL)
}

// OpenLauncherStream validates rawURL (and every redirect target) against the launcher allowlist
// and opens a streaming GET request with optional HTTP Range resumption (rangeStart > 0).
func (c *HTTPClient) OpenLauncherStream(ctx context.Context, rawURL string, rangeStart, maxBytes int64) (*http.Response, error) {
	if ctx == nil {
		ctx = c.ReqCtx()
	}
	if maxBytes <= 0 {
		maxBytes = MaxLauncherDownloadBytes
	}
	targetURL := strings.TrimSpace(rawURL)
	u, err := c.ValidateLauncherURL(targetURL)
	if err != nil {
		return nil, err
	}
	if u != nil {
		targetURL = u.String()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", domain.UserAgent)
	if rangeStart > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", rangeStart))
	}
	client := c.launcherDLClient
	if client == nil {
		client = c.dlClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return resp, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > 0 {
		expectedTotal := resp.ContentLength
		if resp.StatusCode == http.StatusPartialContent && rangeStart > 0 {
			expectedTotal += rangeStart
		}
		if expectedTotal > maxBytes {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("размер файла (%d МБ) превышает лимит (%d МБ)", expectedTotal/(1024*1024), maxBytes/(1024*1024))
		}
	}
	return resp, nil
}
