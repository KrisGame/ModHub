package netx

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestValidateDownloadURLAndIPBlocking(t *testing.T) {
	for _, u := range []string{
		"https://cdn.modrinth.com/data/AANobbMI/versions/abc/sodium-0.6.jar",
		"https://cdn-raw.modrinth.com/data/abc/mod.jar",
		"https://edge.forgecdn.net/files/5000/123/jei.jar",
		"https://mediafilez.forgecdn.net/files/5000/123/create.jar",
		"https://cdn.curseforge.com/files/1234/567/mod.jar",
		"https://eu.forgecdn.net/files/1234/567/mod.jar",
	} {
		if _, err := ValidateDownloadURL(u); err != nil {
			t.Errorf("expected allowed URL %q, got error: %v", u, err)
		}
	}

	for _, u := range []string{
		"", "http://cdn.modrinth.com/data/mod.jar", "file:///etc/passwd",
		"https://localhost/mod.jar", "https://127.0.0.1/mod.jar", "https://[::1]/mod.jar",
		"https://169.254.169.254/latest/meta-data/", "https://10.0.0.1/mod.jar",
		"https://user:pass@cdn.modrinth.com/mod.jar", "https://cdn.modrinth.com:8443/mod.jar",
		"https://cdn.modrinth.com.evil.com/mod.jar", "https://cdn.modrinth.com/\r\nX-Injected: 1",
	} {
		if _, err := ValidateDownloadURL(u); err == nil {
			t.Errorf("expected blocked URL %q, but it was accepted", u)
		}
	}

	for _, s := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "10.1.2.3", "192.168.1.10", "169.254.169.254", "100.64.0.1"} {
		if !IsBlockedIP(netip.MustParseAddr(s)) {
			t.Errorf("expected IP %s to be blocked", s)
		}
	}
	for _, s := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if IsBlockedIP(netip.MustParseAddr(s)) {
			t.Errorf("expected public IP %s to be allowed", s)
		}
	}
}

func TestRedirectPolicyAndSafeProxyDial(t *testing.T) {
	evilSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("secret"))
	}))
	defer evilSrv.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evilSrv.URL+"/secret", http.StatusFound)
	}))
	defer redirector.Close()

	client := &http.Client{CheckRedirect: MakeSafeRedirectChecker(ValidateDownloadURL)}
	if resp, err := client.Get(redirector.URL); err == nil {
		_ = resp.Body.Close()
		t.Fatal("expected untrusted redirect to be blocked")
	} else if !strings.Contains(err.Error(), "заблокировано перенаправление") {
		t.Fatalf("unexpected error: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	proxyAddr := ln.Addr().String()
	dialFn := MakeSafeDialContext(&net.Dialer{Timeout: 2 * time.Second})
	SetManualProxyURL("direct")
	defer SetManualProxyURL("")
	if c, err := dialFn(context.Background(), "tcp", proxyAddr); err == nil {
		_ = c.Close()
		t.Fatal("expected loopback blocked when proxy=direct")
	}
	SetManualProxyURL("http://" + proxyAddr)
	c, err := dialFn(context.Background(), "tcp", proxyAddr)
	if err != nil {
		t.Fatalf("expected active proxy dial to succeed: %v", err)
	}
	_ = c.Close()
}

func TestDownloadSizeLimitAndStreaming(t *testing.T) {
	payload := bytes.Repeat([]byte("A"), 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write(payload)
		case "/chunked-big":
			fl, _ := w.(http.Flusher)
			for i := 0; i < 4; i++ {
				_, _ = w.Write(payload)
				if fl != nil {
					fl.Flush()
				}
			}
		}
	}))
	defer srv.Close()

	hc := NewHTTPClient(nil)
	hc.SetDownloadClient(srv.Client())
	hc.SetDownloadValidator(func(raw string) (*url.URL, error) { return url.Parse(raw) })

	tmpPath, sz, err := hc.DownloadToTempFile(context.Background(), srv.URL+"/ok", 8192)
	if err != nil || sz != int64(len(payload)) {
		t.Fatalf("DownloadToTempFile = (%q, %d, %v)", tmpPath, sz, err)
	}
	_ = os.Remove(tmpPath)

	if p, _, err := hc.DownloadToTempFile(context.Background(), srv.URL+"/chunked-big", 5000); err == nil {
		_ = os.Remove(p)
		t.Fatal("expected oversized chunked stream rejected")
	}
}
