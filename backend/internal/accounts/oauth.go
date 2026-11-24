package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"modhub/internal/domain"
	"modhub/internal/security"
)

var (
	ErrMissingClientID   = errors.New("не настроен Microsoft Client ID (зарегистрируйте публичное desktop-приложение в Microsoft Entra)")
	ErrAuthCancelled     = errors.New("авторизация отменена пользователем")
	ErrAuthTimeout       = errors.New("истекло время ожидания ответа от Microsoft")
	ErrInvalidState      = errors.New("несовпадение параметра OAuth state (возможная подмена ответа)")
	ErrInvalidRedirect   = errors.New("недопустимый Redirect URI (разрешён только loopback http://localhost или http://127.0.0.1)")
	ErrCallbackReplayed  = errors.New("повторное использование OAuth callback отклонено")
	ErrInvalidGrant      = errors.New("срок действия кода авторизации или refresh-токена истёк — требуется повторный вход")
)

// PKCEPair holds a fresh RFC 7636 S256 verifier and derived challenge.
type PKCEPair struct {
	Verifier  string
	Challenge string
	Method    string
}

func randomBase64URL(numBytes int) (string, error) {
	b := make([]byte, numBytes)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateState creates a cryptographically random 256-bit OAuth state parameter.
func GenerateState() (string, error) {
	return randomBase64URL(32)
}

// GeneratePKCE creates a cryptographically random RFC 7636 PKCE verifier (43 chars) and S256 challenge.
func GeneratePKCE() (PKCEPair, error) {
	verifier, err := randomBase64URL(32)
	if err != nil {
		return PKCEPair{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return PKCEPair{
		Verifier:  verifier,
		Challenge: challenge,
		Method:    "S256",
	}, nil
}

// ValidateLoopbackRedirectURI validates that a desktop OAuth redirect URI complies with RFC 8252 & Microsoft Entra rules.
func ValidateLoopbackRedirectURI(rawURI string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(rawURI))
	if err != nil {
		return nil, ErrInvalidRedirect
	}
	if !strings.EqualFold(u.Scheme, "http") {
		return nil, ErrInvalidRedirect
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, ErrInvalidRedirect
	}
	if u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return nil, ErrInvalidRedirect
	}
	if portStr := strings.TrimSpace(u.Port()); portStr != "" {
		var p int
		if _, err := fmt.Sscanf(portStr, "%d", &p); err != nil || p < 1 || p > 65535 || fmt.Sprintf("%d", p) != portStr {
			return nil, ErrInvalidRedirect
		}
	}
	if u.Path != "/callback" && u.Path != "/" && u.Path != "" {
		return nil, ErrInvalidRedirect
	}
	return u, nil
}

// BuildAuthorizeURL constructs the official Microsoft OAuth 2.0 public-client Authorization Code + PKCE URL.
func BuildAuthorizeURL(authorizeEndpoint, clientID, redirectURI, state string, pkce PKCEPair) (string, error) {
	cid := strings.TrimSpace(clientID)
	if cid == "" {
		return "", ErrMissingClientID
	}
	if _, err := ValidateLoopbackRedirectURI(redirectURI); err != nil {
		return "", err
	}
	if strings.TrimSpace(state) == "" || strings.TrimSpace(pkce.Challenge) == "" {
		return "", errors.New("отсутствуют параметры state или PKCE")
	}
	u, err := url.Parse(strings.TrimSpace(authorizeEndpoint))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("некорректный URL авторизации Microsoft")
	}
	q := u.Query()
	q.Set("client_id", cid)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI)
	q.Set("response_mode", "query")
	q.Set("scope", OfficialOAuthScope)
	q.Set("state", state)
	q.Set("code_challenge", pkce.Challenge)
	q.Set("code_challenge_method", pkce.Method)
	q.Set("prompt", "select_account")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

type OAuthCallbackResult struct {
	Code string
	Err  error
}

// LoopbackCallbackServer listens on an ephemeral localhost loopback port (IPv4 + IPv6 when available)
// for a single OAuth redirect, enforcing state verification, replay rejection, timeout, and teardown.
type LoopbackCallbackServer struct {
	listeners     []net.Listener
	srv           *http.Server
	redirectURI   string
	expectedState string

	mu       sync.Mutex
	consumed bool
	resultCh chan OAuthCallbackResult
	closeOne sync.Once
}

func StartLoopbackCallbackServer(expectedState string) (*LoopbackCallbackServer, error) {
	if strings.TrimSpace(expectedState) == "" {
		return nil, errors.New("expectedState is empty")
	}
	ln4, err := net.Listen("tcp4", "127.0.0.1:0")
	var listeners []net.Listener
	var port int
	if err == nil {
		port = ln4.Addr().(*net.TCPAddr).Port
		listeners = append(listeners, ln4)
		// Best-effort IPv6 loopback listener on the same ephemeral port so browsers resolving localhost to ::1 succeed immediately.
		if ln6, err6 := net.Listen("tcp6", fmt.Sprintf("[::1]:%d", port)); err6 == nil {
			listeners = append(listeners, ln6)
		}
	} else {
		ln6, err6 := net.Listen("tcp6", "[::1]:0")
		if err6 != nil {
			return nil, fmt.Errorf("не удалось открыть локальный порт для OAuth callback: %w", err)
		}
		port = ln6.Addr().(*net.TCPAddr).Port
		listeners = append(listeners, ln6)
	}
	if port < 1 || port > 65535 {
		for _, ln := range listeners {
			_ = ln.Close()
		}
		return nil, errors.New("некорректный динамический порт loopback callback")
	}

	// Microsoft Entra "Mobile and desktop applications" matches http://localhost with any dynamic port.
	redirectURI := fmt.Sprintf("http://localhost:%d", port)

	s := &LoopbackCallbackServer{
		listeners:     listeners,
		redirectURI:   redirectURI,
		expectedState: expectedState,
		resultCh:      make(chan OAuthCallbackResult, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleCallback)
	mux.HandleFunc("/callback", s.handleCallback)
	s.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
	}
	for _, ln := range listeners {
		listener := ln
		go func() {
			_ = s.srv.Serve(listener)
		}()
	}
	return s, nil
}

func (s *LoopbackCallbackServer) RedirectURI() string {
	return s.redirectURI
}

func (s *LoopbackCallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Ignore automatic browser/preflight requests (e.g. /favicon.ico, HEAD) so they never consume the one-shot callback.
	if r.URL.Path != "/" && r.URL.Path != "/callback" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	if q.Get("code") == "" && q.Get("error") == "" && q.Get("state") == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(renderCallbackHTML(true, "Ожидание ответа Microsoft", "Завершите вход в открытой вкладке Microsoft.")))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	s.mu.Lock()
	if s.consumed {
		s.mu.Unlock()
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(renderCallbackHTML(false, "Повторный запрос отклонён", "Этот код авторизации уже был обработан. Вы можете закрыть вкладку и вернуться в ModHub.")))
		return
	}
	s.consumed = true
	s.mu.Unlock()

	gotState := strings.TrimSpace(q.Get("state"))
	if subtle.ConstantTimeCompare([]byte(gotState), []byte(s.expectedState)) != 1 {
		s.deliver(OAuthCallbackResult{Err: ErrInvalidState})
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(renderCallbackHTML(false, "Ошибка проверки безопасности", "Параметр state не совпал. Авторизация прервана. Вернитесь в ModHub и попробуйте снова.")))
		return
	}

	if oauthErr := strings.TrimSpace(q.Get("error")); oauthErr != "" {
		desc := strings.TrimSpace(q.Get("error_description"))
		var resErr error
		if strings.EqualFold(oauthErr, "access_denied") {
			resErr = ErrAuthCancelled
		} else if desc != "" {
			resErr = fmt.Errorf("ошибка Microsoft OAuth (%s): %s", oauthErr, security.SanitizeLogText(desc))
		} else {
			resErr = fmt.Errorf("ошибка Microsoft OAuth: %s", oauthErr)
		}
		s.deliver(OAuthCallbackResult{Err: resErr})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(renderCallbackHTML(false, "Авторизация не завершена", resErr.Error())))
		return
	}

	code := strings.TrimSpace(q.Get("code"))
	if code == "" {
		err := errors.New("Microsoft не вернул код авторизации")
		s.deliver(OAuthCallbackResult{Err: err})
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(renderCallbackHTML(false, "Ошибка авторизации", err.Error())))
		return
	}

	s.deliver(OAuthCallbackResult{Code: code})
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(renderCallbackHTML(true, "Вход выполнен", "ModHub получил подтверждение Microsoft и завершает проверку Xbox и Minecraft. Эту вкладку можно закрыть.")))
}

func (s *LoopbackCallbackServer) deliver(res OAuthCallbackResult) {
	select {
	case s.resultCh <- res:
	default:
	}
}

func (s *LoopbackCallbackServer) Wait(ctx context.Context, timeout time.Duration) (string, error) {
	defer s.Close()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-s.resultCh:
		return res.Code, res.Err
	case <-ctx.Done():
		return "", ErrAuthCancelled
	case <-timer.C:
		return "", ErrAuthTimeout
	}
}

func (s *LoopbackCallbackServer) Close() {
	s.closeOne.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(ctx)
		for _, ln := range s.listeners {
			_ = ln.Close()
		}
	})
}

func renderCallbackHTML(ok bool, title, body string) string {
	accent := "#79b88b"
	if !ok {
		accent = "#d97b7b"
	}
	return fmt.Sprintf(`<!doctype html><html lang="ru"><head><meta charset="utf-8"><title>ModHub — %s</title>
<style>body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;background:#111214;color:#eceff3;font-family:-apple-system,Segoe UI,Roboto,sans-serif;}
.card{max-width:440px;padding:28px 30px;border-radius:14px;background:#181a1d;border:1px solid #2e333a;box-shadow:0 18px 40px rgba(0,0,0,.45);}
.dot{width:10px;height:10px;border-radius:50%%;background:%s;display:inline-block;margin-right:8px;}
h1{margin:0 0 10px;font-size:19px;display:flex;align-items:center;}p{margin:0;color:#b2b9c4;font-size:14px;line-height:1.5;}</style></head>
<body><div class="card"><h1><span class="dot"></span>%s</h1><p>%s</p></div></body></html>`,
		html.EscapeString(title), accent, html.EscapeString(title), html.EscapeString(body))
}

type msTokenResponse struct {
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	ExpiresIn        int64  `json:"expires_in"`
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	UserID           string `json:"user_id"`
	IDToken          string `json:"id_token"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func exchangeMSToken(ctx context.Context, client *http.Client, tokenURL string, form url.Values) (msTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return msTokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", domain.UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return msTokenResponse{}, fmt.Errorf("сетевая ошибка при обращении к Microsoft OAuth: %s", security.SanitizeLogText(err.Error()))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return msTokenResponse{}, fmt.Errorf("ошибка чтения ответа Microsoft OAuth: %v", err)
	}
	var tr msTokenResponse
	_ = json.Unmarshal(raw, &tr)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 || tr.Error != "" {
		if strings.EqualFold(tr.Error, "invalid_grant") || strings.EqualFold(tr.Error, "interaction_required") || strings.EqualFold(tr.Error, "login_required") || strings.EqualFold(tr.Error, "consent_required") {
			return msTokenResponse{}, fmt.Errorf("%w (%s)", ErrInvalidGrant, tr.Error)
		}
		if tr.Error != "" {
			return msTokenResponse{}, fmt.Errorf("ошибка Microsoft OAuth (%s): %s", tr.Error, security.SanitizeLogText(tr.ErrorDescription))
		}
		return msTokenResponse{}, fmt.Errorf("Microsoft OAuth вернул HTTP %d", resp.StatusCode)
	}
	if strings.TrimSpace(tr.AccessToken) == "" {
		return msTokenResponse{}, errors.New("Microsoft OAuth не вернул access_token")
	}
	return tr, nil
}

// ExchangeAuthCode exchanges a one-time OAuth authorization code + PKCE verifier for Microsoft tokens (public client, NO secret).
func ExchangeAuthCode(ctx context.Context, client *http.Client, tokenURL, clientID, code, redirectURI, codeVerifier string) (msTokenResponse, error) {
	if strings.TrimSpace(clientID) == "" {
		return msTokenResponse{}, ErrMissingClientID
	}
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(clientID))
	form.Set("grant_type", "authorization_code")
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", strings.TrimSpace(redirectURI))
	form.Set("code_verifier", strings.TrimSpace(codeVerifier))
	form.Set("scope", OfficialOAuthScope)
	return exchangeMSToken(ctx, client, tokenURL, form)
}

// RefreshMSToken exchanges a saved refresh_token for a new Microsoft access_token (and rotated refresh_token).
func RefreshMSToken(ctx context.Context, client *http.Client, tokenURL, clientID, refreshToken string) (msTokenResponse, error) {
	if strings.TrimSpace(clientID) == "" {
		return msTokenResponse{}, ErrMissingClientID
	}
	if strings.TrimSpace(refreshToken) == "" {
		return msTokenResponse{}, ErrInvalidGrant
	}
	form := url.Values{}
	form.Set("client_id", strings.TrimSpace(clientID))
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", strings.TrimSpace(refreshToken))
	form.Set("scope", OfficialOAuthScope)
	return exchangeMSToken(ctx, client, tokenURL, form)
}
