package netx

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"modhub/internal/domain"
)


var (
	proxyMu       sync.RWMutex
	manualProxy   string
	sysCache      winSystemProxy
	sysCacheUntil time.Time
)

func SetManualProxyURL(raw string) {
	proxyMu.Lock()
	manualProxy = strings.TrimSpace(raw)
	sysCacheUntil = time.Time{}
	proxyMu.Unlock()
}

func InvalidateProxyCache() {
	proxyMu.Lock()
	sysCacheUntil = time.Time{}
	proxyMu.Unlock()
}

func getCachedSystemProxy() winSystemProxy {
	now := time.Now()
	proxyMu.RLock()
	if now.Before(sysCacheUntil) {
		val := sysCache
		proxyMu.RUnlock()
		return val
	}
	proxyMu.RUnlock()

	fresh := readSystemProxyFromOS()
	proxyMu.Lock()
	sysCache = fresh
	sysCacheUntil = now.Add(4 * time.Second)
	proxyMu.Unlock()
	return fresh
}

func NormalizeProxyURL(raw, defaultScheme string) (*url.URL, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	low := strings.ToLower(s)
	if low == "direct" || low == "none" || low == "off" {
		return nil, nil
	}
	if strings.HasPrefix(low, "socks5h://") {
		s = "socks5://" + s[len("socks5h://"):]
	} else if strings.HasPrefix(low, "socks://") {
		s = "socks5://" + s[len("socks://"):]
	} else if !strings.Contains(s, "://") {
		if defaultScheme == "" {
			defaultScheme = "http"
		}
		s = defaultScheme + "://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return nil, err
	}
	return u, nil
}

func maskProxyURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	cp := *u
	if cp.User != nil {
		cp.User = url.User(cp.User.Username())
	}
	return cp.String()
}

func isHostBypassed(host, overrideList string) bool {
	if overrideList == "" || host == "" {
		return false
	}
	h := strings.ToLower(strings.TrimSpace(host))
	if stripped, _, err := net.SplitHostPort(h); err == nil {
		h = stripped
	}
	if h == "localhost" || h == "127.0.0.1" || h == "::1" {
		return true
	}
	for _, rawEntry := range strings.Split(overrideList, ";") {
		pat := strings.ToLower(strings.TrimSpace(rawEntry))
		if pat == "" {
			continue
		}
		if pat == "<local>" {
			if !strings.Contains(h, ".") {
				return true
			}
			continue
		}
		if pat == h {
			return true
		}
		if strings.HasPrefix(pat, "*.") && strings.HasSuffix(h, pat[1:]) {
			return true
		}
		if strings.HasSuffix(pat, "*") && strings.HasPrefix(h, strings.TrimSuffix(pat, "*")) {
			return true
		}
	}
	return false
}

func parseWinProxyServer(serverStr, targetScheme string) *url.URL {
	s := strings.TrimSpace(serverStr)
	if s == "" {
		return nil
	}
	// Format 1: single proxy for all protocols (e.g. "127.0.0.1:7890" or "http://127.0.0.1:7890")
	if !strings.Contains(s, "=") {
		u, err := NormalizeProxyURL(s, "http")
		if err == nil {
			return u
		}
		return nil
	}
	// Format 2: per-protocol entries (e.g. "http=127.0.0.1:7890;https=127.0.0.1:7890;socks=127.0.0.1:7891")
	protoMap := map[string]string{}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		v := strings.TrimSpace(kv[1])
		if k != "" && v != "" {
			protoMap[k] = v
		}
	}
	order := []string{strings.ToLower(targetScheme), "https", "http", "socks5", "socks"}
	for _, k := range order {
		if addr, ok := protoMap[k]; ok {
			defScheme := "http"
			if k == "socks" || k == "socks5" {
				defScheme = "socks5"
			}
			if u, err := NormalizeProxyURL(addr, defScheme); err == nil && u != nil {
				return u
			}
		}
	}
	return nil
}

func envAllProxy() *url.URL {
	for _, key := range []string{"ALL_PROXY", "all_proxy"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			if u, err := NormalizeProxyURL(v, "http"); err == nil && u != nil {
				return u
			}
		}
	}
	return nil
}

// ResolveProxyFunc resolves proxies for http.Transport in priority order:
// 1. Manual proxy in data/config.json ("direct" forces no proxy)
// 2. Environment variables (HTTP_PROXY / HTTPS_PROXY / ALL_PROXY / NO_PROXY)
// 3. Windows System Proxy from Registry (HKCU\...\Internet Settings)
func ResolveProxyFunc(req *http.Request) (*url.URL, error) {
	if req == nil || req.URL == nil {
		return nil, nil
	}
	host := req.URL.Hostname()
	if host == "127.0.0.1" || host == "localhost" || host == "::1" {
		return nil, nil
	}

	// 1. Manual proxy override
	proxyMu.RLock()
	manual := manualProxy
	proxyMu.RUnlock()
	if manual != "" {
		low := strings.ToLower(manual)
		if low == "direct" || low == "none" || low == "off" {
			return nil, nil
		}
		return NormalizeProxyURL(manual, "http")
	}

	// 2. Environment variables (HTTP_PROXY / HTTPS_PROXY / NO_PROXY)
	if envURL, err := http.ProxyFromEnvironment(req); err == nil && envURL != nil {
		return envURL, nil
	}
	if ap := envAllProxy(); ap != nil {
		return ap, nil
	}

	// 3. Windows System Proxy (auto-refreshed every 4s)
	sys := getCachedSystemProxy()
	if sys.Enabled && sys.Server != "" && !isHostBypassed(req.URL.Host, sys.Override) {
		if u := parseWinProxyServer(sys.Server, req.URL.Scheme); u != nil {
			return u, nil
		}
	}

	return nil, nil
}

func GetProxyStatus() domain.ProxyInfo {
	sampleReq, _ := http.NewRequest(http.MethodGet, domain.ModrinthAPI+"/tag/loader", nil)

	proxyMu.RLock()
	manual := manualProxy
	proxyMu.RUnlock()
	if manual != "" {
		low := strings.ToLower(manual)
		if low == "direct" || low == "none" || low == "off" {
			return domain.ProxyInfo{Mode: "direct", Address: "", Label: "Прямое подключение (принудительно)"}
		}
		if u, err := NormalizeProxyURL(manual, "http"); err == nil && u != nil {
			addr := maskProxyURL(u)
			return domain.ProxyInfo{Mode: "manual", Address: addr, Label: "Ручной прокси: " + addr}
		}
		return domain.ProxyInfo{Mode: "manual", Address: manual, Label: "Некорректный адрес прокси: " + manual}
	}

	if sampleReq != nil {
		if envURL, err := http.ProxyFromEnvironment(sampleReq); err == nil && envURL != nil {
			addr := maskProxyURL(envURL)
			return domain.ProxyInfo{Mode: "env", Address: addr, Label: "Переменная среды: " + addr}
		}
	}
	if ap := envAllProxy(); ap != nil {
		addr := maskProxyURL(ap)
		return domain.ProxyInfo{Mode: "env", Address: addr, Label: "Переменная ALL_PROXY: " + addr}
	}

	sys := getCachedSystemProxy()
	if sys.Enabled && sys.Server != "" {
		if u := parseWinProxyServer(sys.Server, "https"); u != nil {
			addr := maskProxyURL(u)
			return domain.ProxyInfo{Mode: "windows_system", Address: addr, Label: "Системный прокси Windows: " + addr}
		}
	}

	return domain.ProxyInfo{Mode: "direct", Address: "", Label: "Прямое подключение (системный прокси не активен)"}
}


func proxyDefaultPort(scheme string) string {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "https":
		return "443"
	case "socks", "socks5", "socks5h":
		return "1080"
	default:
		return "80"
	}
}

func sameProxyEndpoint(dialAddr string, proxyURL *url.URL) bool {
	if proxyURL == nil {
		return false
	}
	dh, dp, err := net.SplitHostPort(strings.TrimSpace(dialAddr))
	if err != nil || dp == "" {
		return false
	}
	ph := strings.ToLower(strings.Trim(proxyURL.Hostname(), "[]"))
	pp := proxyURL.Port()
	if pp == "" {
		pp = proxyDefaultPort(proxyURL.Scheme)
	}
	if dp != pp {
		return false
	}
	dh = strings.ToLower(strings.Trim(dh, "[]"))
	if dh == ph {
		return true
	}
	// Treat localhost / 127.0.0.1 / ::1 as equivalent loopback endpoints on the same port
	isLoop := func(h string) bool {
		return h == "localhost" || h == "127.0.0.1" || h == "::1"
	}
	return isLoop(dh) && isLoop(ph)
}

// isActiveProxyAddr reports whether dialAddr matches the currently active configured proxy
// (manual config, HTTP_PROXY/HTTPS_PROXY/ALL_PROXY env, or Windows system proxy).
func IsActiveProxyAddr(dialAddr string) bool {
	if strings.TrimSpace(dialAddr) == "" {
		return false
	}

	proxyMu.RLock()
	manual := manualProxy
	proxyMu.RUnlock()
	if manual != "" {
		low := strings.ToLower(manual)
		if low == "direct" || low == "none" || low == "off" {
			return false
		}
		if u, err := NormalizeProxyURL(manual, "http"); err == nil && sameProxyEndpoint(dialAddr, u) {
			return true
		}
		return false
	}

	for _, sampleURL := range []string{"https://cdn.modrinth.com/", "http://cdn.modrinth.com/"} {
		if req, err := http.NewRequest(http.MethodGet, sampleURL, nil); err == nil {
			if envURL, err := http.ProxyFromEnvironment(req); err == nil && sameProxyEndpoint(dialAddr, envURL) {
				return true
			}
		}
	}
	if ap := envAllProxy(); sameProxyEndpoint(dialAddr, ap) {
		return true
	}

	sys := getCachedSystemProxy()
	if sys.Enabled && sys.Server != "" {
		for _, scheme := range []string{"https", "http", "socks5"} {
			if u := parseWinProxyServer(sys.Server, scheme); sameProxyEndpoint(dialAddr, u) {
				return true
			}
		}
	}
	return false
}
