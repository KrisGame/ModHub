package netx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
)

const (
	MaxDownloadFileBytes     int64 = 256 * 1024 * 1024  // 256 MB per mod file
	MaxLauncherDownloadBytes int64 = 4 * 1024 * 1024 * 1024 // 4 GB per launcher/runtime file
	MaxPackTotalBytes        int64 = 4 * 1024 * 1024 * 1024 // 4 GB per modpack archive
	MaxAPIBodyBytes          int64 = 16 * 1024 * 1024   // 16 MB per JSON API response
	maxDownloadRedirects           = 5
)

var trustedDownloadHosts = []string{
	"cdn.modrinth.com",
	"cdn-raw.modrinth.com",
	"forgecdn.net",
	"edge.forgecdn.net",
	"mediafilez.forgecdn.net",
	"media.forgecdn.net",
	"mediafiles.forgecdn.net",
	"cdn.curseforge.com",
}

var (
	launcherHostsMu      sync.RWMutex
	trustedLauncherHosts = []string{
		// Official Mojang / Minecraft infrastructure (manifests, game, assets, Java runtimes)
		"piston-meta.mojang.com",
		"piston-data.mojang.com",
		"launchermeta.mojang.com",
		"launcher.mojang.com",
		"resources.download.minecraft.net",
		"libraries.minecraft.net",
		// Modrinth & CurseForge CDNs
		"cdn.modrinth.com",
		"cdn-raw.modrinth.com",
		"forgecdn.net",
		"edge.forgecdn.net",
		"mediafilez.forgecdn.net",
		"media.forgecdn.net",
		"mediafiles.forgecdn.net",
		"cdn.curseforge.com",
		// Official Modloader Maven & Meta repositories
		"meta.fabricmc.net",
		"maven.fabricmc.net",
		"meta.quiltmc.org",
		"maven.quiltmc.org",
		"maven.minecraftforge.net",
		"files.minecraftforge.net",
		"maven.neoforged.net",
		"repo1.maven.org",
		"maven.creeperhost.net",
		"maven.ow2.org",
	}
)

// RegisterTrustedLauncherHost safely adds a trusted HTTPS domain to the centralized download allowlist.
func RegisterTrustedLauncherHost(host string) {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if h == "" || h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") {
		return
	}
	if _, err := netip.ParseAddr(h); err == nil {
		return
	}
	launcherHostsMu.Lock()
	defer launcherHostsMu.Unlock()
	for _, existing := range trustedLauncherHosts {
		if existing == h {
			return
		}
	}
	trustedLauncherHosts = append(trustedLauncherHosts, h)
}

func getTrustedLauncherHosts() []string {
	launcherHostsMu.RLock()
	defer launcherHostsMu.RUnlock()
	return append([]string(nil), trustedLauncherHosts...)
}

var trustedIconHosts = []string{
	"cdn.modrinth.com",
	"cdn-raw.modrinth.com",
	"forgecdn.net",
	"media.forgecdn.net",
	"mediafilez.forgecdn.net",
	"edge.forgecdn.net",
	"mediafiles.forgecdn.net",
	"cdn.curseforge.com",
	"avatars.githubusercontent.com",
	"raw.githubusercontent.com",
	"textures.minecraft.net",
}

var (
	prefixCGNAT  = netip.MustParsePrefix("100.64.0.0/10")
	prefixBench  = netip.MustParsePrefix("198.18.0.0/15")
	prefixDoc1   = netip.MustParsePrefix("192.0.2.0/24")
	prefixDoc2   = netip.MustParsePrefix("198.51.100.0/24")
	prefixDoc3   = netip.MustParsePrefix("203.0.113.0/24")
	prefixDoc6   = netip.MustParsePrefix("2001:db8::/32")
	prefixNAT64  = netip.MustParsePrefix("64:ff9b::/96")
	prefix6to4   = netip.MustParsePrefix("2002::/16")
	prefixTeredo = netip.MustParsePrefix("2001::/32")
)

func IsBlockedIP(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	// Unmap IPv4-mapped IPv6 addresses (e.g. ::ffff:127.0.0.1 -> 127.0.0.1)
	addr = addr.Unmap()
	if addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified() ||
		addr.IsInterfaceLocalMulticast() {
		return true
	}
	if !addr.IsGlobalUnicast() {
		return true
	}
	if addr.Is4() {
		if prefixCGNAT.Contains(addr) ||
			prefixBench.Contains(addr) ||
			prefixDoc1.Contains(addr) ||
			prefixDoc2.Contains(addr) ||
			prefixDoc3.Contains(addr) {
			return true
		}
	} else {
		if prefixDoc6.Contains(addr) ||
			prefixNAT64.Contains(addr) ||
			prefix6to4.Contains(addr) ||
			prefixTeredo.Contains(addr) {
			return true
		}
	}
	return false
}

func hostMatchesAllowlist(host string, allowed []string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if h == "" {
		return false
	}
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") {
		return false
	}
	if _, err := netip.ParseAddr(h); err == nil {
		return false
	}
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if h == a || strings.HasSuffix(h, "."+a) {
			return true
		}
	}
	return false
}

func validateAllowedHTTPSURL(rawURL string, allowed []string) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, errors.New("пустой URL загрузки")
	}
	for _, r := range trimmed {
		if r < 0x20 || r == 0x7f || r == '\\' {
			return nil, errors.New("недопустимые символы в URL")
		}
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("некорректный URL: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return nil, errors.New("разрешены только HTTPS-ссылки")
	}
	if u.User != nil {
		return nil, errors.New("учётные данные в URL запрещены")
	}
	if u.Opaque != "" {
		return nil, errors.New("некорректный формат URL")
	}
	if port := u.Port(); port != "" && port != "443" {
		return nil, errors.New("разрешён только стандартный порт HTTPS (443)")
	}
	host := u.Hostname()
	if !hostMatchesAllowlist(host, allowed) {
		return nil, fmt.Errorf("хост %q не входит в список доверенных CDN", host)
	}
	if strings.Contains(u.Path, " ") {
		u.RawPath = ""
	}
	return u, nil
}

// ValidateDownloadURL enforces strict HTTPS allowlist & SSRF checks for mod/pack downloads.
func ValidateDownloadURL(rawURL string) (*url.URL, error) {
	return validateAllowedHTTPSURL(rawURL, trustedDownloadHosts)
}

// ValidateLauncherDownloadURL enforces strict HTTPS allowlist & SSRF checks for the centralized Download Manager
// (covering Mojang, Modrinth, CurseForge, and official modloader CDNs while blocking private/local addresses).
func ValidateLauncherDownloadURL(rawURL string) (*url.URL, error) {
	return validateAllowedHTTPSURL(rawURL, getTrustedLauncherHosts())
}

// ValidateIconURL enforces strict HTTPS allowlist & SSRF checks for icon proxy downloads.
func ValidateIconURL(rawURL string) (*url.URL, error) {
	return validateAllowedHTTPSURL(rawURL, trustedIconHosts)
}

func isAllowedHost(rawURL string, allowed []string) bool {
	_, err := validateAllowedHTTPSURL(rawURL, allowed)
	return err == nil
}

func MakeSafeRedirectChecker(validator func(string) (*url.URL, error)) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxDownloadRedirects {
			return fmt.Errorf("слишком много перенаправлений (%d)", len(via))
		}
		if req.URL == nil {
			return errors.New("пустой адрес перенаправления")
		}
		if _, err := validator(req.URL.String()); err != nil {
			return fmt.Errorf("заблокировано перенаправление: %w", err)
		}
		return nil
	}
}

// makeSafeDialContext wraps a net.Dialer to resolve DNS first and refuse connections
// to loopback, private, link-local, or otherwise non-global IPv4/IPv6 addresses.
func MakeSafeDialContext(base *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		// Permit dialing the exact configured local/system proxy endpoint;
		// the target download/icon URL itself has already passed strict HTTPS CDN validation.
		if IsActiveProxyAddr(addr) {
			return base.DialContext(ctx, network, addr)
		}
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("некорректный адрес назначения: %w", err)
		}
		if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
			if IsBlockedIP(ip) {
				return nil, fmt.Errorf("подключение к локальному или приватному IP %s запрещено", ip.String())
			}
			return base.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("не удалось разрешить адрес %q", host)
		}
		validIPs := make([]netip.Addr, 0, len(ips))
		for _, ip := range ips {
			if IsBlockedIP(ip) {
				return nil, fmt.Errorf("хост %q указывает на запрещённый IP %s", host, ip.String())
			}
			validIPs = append(validIPs, ip.Unmap())
		}
		var lastErr error
		for _, ip := range validIPs {
			conn, dialErr := base.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
}
