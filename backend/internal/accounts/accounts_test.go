package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"modhub/internal/events"
	"modhub/internal/security"
	"modhub/internal/storage"
)

// =========================================================================
// OAuth Unit Tests (Scenarios 1..10)
// =========================================================================

func TestOAuthStatePKCEAuthorizeURLRedirectAndCallbackLifecycle(t *testing.T) {
	// 1 & 2: State & PKCE S256 generation
	st1, err := GenerateState()
	if err != nil || len(st1) < 32 {
		t.Fatalf("GenerateState failed: %q %v", st1, err)
	}
	st2, _ := GenerateState()
	if st1 == st2 {
		t.Fatal("GenerateState returned identical values")
	}

	pkce, err := GeneratePKCE()
	if err != nil || pkce.Method != "S256" || len(pkce.Verifier) < 43 {
		t.Fatalf("GeneratePKCE failed: %+v err=%v", pkce, err)
	}
	sum := sha256.Sum256([]byte(pkce.Verifier))
	wantChal := base64.RawURLEncoding.EncodeToString(sum[:])
	if pkce.Challenge != wantChal {
		t.Fatalf("PKCE S256 challenge mismatch: got %s want %s", pkce.Challenge, wantChal)
	}

	// 4: Redirect URI validation
	for _, okURI := range []string{"http://127.0.0.1:49152/callback", "http://localhost:50000/callback"} {
		if _, err := ValidateLoopbackRedirectURI(okURI); err != nil {
			t.Errorf("expected valid loopback redirect %q, got %v", okURI, err)
		}
	}
	for _, badURI := range []string{"https://evil.com/callback", "http://192.168.1.5:8080/callback", "http://127.0.0.1:8080/other", "http://localhost:0", "http://localhost:99999", "javascript:alert(1)"} {
		if _, err := ValidateLoopbackRedirectURI(badURI); err == nil {
			t.Errorf("expected invalid redirect URI %q to be rejected", badURI)
		}
	}

	// 10: Missing client ID MUST fail with ErrMissingClientID
	if _, err := BuildAuthorizeURL(OfficialMSAuthorizeURL, "", "http://localhost:50000", st1, pkce); !errors.Is(err, ErrMissingClientID) {
		t.Fatalf("expected ErrMissingClientID, got %v", err)
	}

	// 3: Build authorization URL contains required PKCE, state, scope, localhost redirect_uri, NO client_secret
	authURL, err := BuildAuthorizeURL(OfficialMSAuthorizeURL, "test-client-id", "http://localhost:50000", st1, pkce)
	if err != nil {
		t.Fatalf("BuildAuthorizeURL failed: %v", err)
	}
	u, _ := url.Parse(authURL)
	if u.Query().Get("code_challenge") != pkce.Challenge || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("state") != st1 || u.Query().Get("redirect_uri") != "http://localhost:50000" || u.Query().Get("client_secret") != "" {
		t.Fatalf("unexpected authorize URL query: %s", authURL)
	}

	// 5 & 9: Successful callback on localhost + replay callback MUST be rejected (409)
	cbSrv, err := StartLoopbackCallbackServer(st1)
	if err != nil {
		t.Fatalf("StartLoopbackCallbackServer: %v", err)
	}
	defer cbSrv.Close()
	if !strings.HasPrefix(cbSrv.RedirectURI(), "http://localhost:") {
		t.Fatalf("expected http://localhost:<port> redirect URI, got %q", cbSrv.RedirectURI())
	}

	go func() {
		// Verify /favicon.ico side request does not consume the one-shot OAuth callback
		if favResp, ferr := http.Get(cbSrv.RedirectURI() + "/favicon.ico"); ferr == nil {
			if favResp.StatusCode != http.StatusNoContent {
				t.Errorf("expected 204 for favicon.ico, got %d", favResp.StatusCode)
			}
			_ = favResp.Body.Close()
		}
		resp1, err := http.Get(cbSrv.RedirectURI() + "?code=AUTH_CODE_ONE&state=" + url.QueryEscape(st1))
		if err == nil {
			_ = resp1.Body.Close()
		}
		// 9: Replay callback attempt must get HTTP 409 Conflict
		resp2, err := http.Get(cbSrv.RedirectURI() + "?code=AUTH_CODE_REPLAY&state=" + url.QueryEscape(st1))
		if err == nil {
			if resp2.StatusCode != http.StatusConflict {
				t.Errorf("expected 409 on replayed callback, got %d", resp2.StatusCode)
			}
			_ = resp2.Body.Close()
		}
	}()
	code, err := cbSrv.Wait(context.Background(), 5*time.Second)
	if err != nil || code != "AUTH_CODE_ONE" {
		t.Fatalf("callback Wait = (%q, %v), want AUTH_CODE_ONE", code, err)
	}

	// 6: Callback error (state mismatch)
	cbErr, _ := StartLoopbackCallbackServer("expected-state")
	defer cbErr.Close()
	go func() {
		resp, err := http.Get(cbErr.RedirectURI() + "?code=abc&state=wrong-state")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	if _, err := cbErr.Wait(context.Background(), 5*time.Second); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState, got %v", err)
	}

	// 7: User cancelled (access_denied)
	cbCancel, _ := StartLoopbackCallbackServer("st-cancel")
	defer cbCancel.Close()
	go func() {
		resp, err := http.Get(cbCancel.RedirectURI() + "?error=access_denied&state=st-cancel")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	if _, err := cbCancel.Wait(context.Background(), 5*time.Second); !errors.Is(err, ErrAuthCancelled) {
		t.Fatalf("expected ErrAuthCancelled, got %v", err)
	}

	// 8: Expired authorization code -> ErrInvalidGrant
	expSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"AADSTS70000: code expired"}`))
	}))
	defer expSrv.Close()
	if _, err := ExchangeAuthCode(context.Background(), expSrv.Client(), expSrv.URL, "cid", "old-code", "http://127.0.0.1:50000/callback", pkce.Verifier); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("expected ErrInvalidGrant for expired auth code, got %v", err)
	}
}

// =========================================================================
// Xbox, XSTS, Minecraft Services, Entitlements, Profile & Storage/Refresh Tests
// =========================================================================

type mockAuthEnv struct {
	srv             *httptest.Server
	failXBL         atomic.Bool
	failXSTS        atomic.Int64 // 0 = ok, otherwise XErr code
	failMCAuth      atomic.Bool
	licenseMode     atomic.Value // "owned" | "not_owned" | "503" | "bad_json"
	noProfile       atomic.Bool
	revokeRefresh   atomic.Bool
	refreshHits     atomic.Int32
	currentUserIdx  atomic.Int32
}

func newMockAuthServer(t *testing.T) *mockAuthEnv {
	t.Helper()
	env := &mockAuthEnv{}
	env.licenseMode.Store("owned")

	env.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		idx := env.currentUserIdx.Load()
		switch r.URL.Path {
		case "/oauth2/token":
			_ = r.ParseForm()
			if r.Form.Get("grant_type") == "refresh_token" {
				env.refreshHits.Add(1)
				if env.revokeRefresh.Load() {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh token revoked"}`))
					return
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token_type":    "Bearer",
				"expires_in":    3600,
				"access_token":  "MS_SECRET_ACCESS_TOKEN_XYZ",
				"refresh_token": "MS_SECRET_REFRESH_TOKEN_XYZ",
				"user_id":       "ms_user_" + string(rune('A'+idx)),
			})
		case "/xbl":
			if env.failXBL.Load() {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Token":    "XBL_SECRET_TOKEN_XYZ",
				"NotAfter": time.Now().Add(12 * time.Hour).UTC().Format(time.RFC3339),
				"DisplayClaims": map[string]any{
					"xui": []map[string]any{{"uhs": "uhs_" + string(rune('A'+idx)), "xid": "xuid_" + string(rune('A'+idx)), "gtg": "Player" + string(rune('A'+idx))}},
				},
			})
		case "/xsts":
			if xerr := env.failXSTS.Load(); xerr != 0 {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{"XErr": xerr, "Message": "no xbox profile"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Token":    "XSTS_SECRET_TOKEN_XYZ",
				"NotAfter": time.Now().Add(12 * time.Hour).UTC().Format(time.RFC3339),
				"DisplayClaims": map[string]any{
					"xui": []map[string]any{{"uhs": "uhs_" + string(rune('A'+idx)), "xid": "xuid_" + string(rune('A'+idx)), "gtg": "Player" + string(rune('A'+idx))}},
				},
			})
		case "/mc/login":
			if env.failMCAuth.Load() {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"username":     "xuid_" + string(rune('A'+idx)),
				"access_token": "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJNT0NLIn0.MC_SECRET_JWT_XYZ",
				"expires_in":   86400,
			})
		case "/mc/entitlements":
			mode := env.licenseMode.Load().(string)
			switch mode {
			case "owned":
				_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"name": "game_minecraft", "signature": "sig"}}})
			case "not_owned":
				_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
			case "503":
				w.WriteHeader(http.StatusServiceUnavailable)
			case "bad_json":
				_, _ = w.Write([]byte(`{not-json`))
			}
		case "/mc/profile":
			if env.noProfile.Load() {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			uuid := "0000000000000000000000000000000" + string(rune('1'+idx))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   uuid,
				"name": "Steve" + string(rune('A'+idx)),
				"skins": []map[string]any{{"id": "s1", "state": "ACTIVE", "url": "https://textures.minecraft.net/texture/abc", "variant": "CLASSIC"}},
				"capes": []map[string]any{{"id": "c1", "state": "ACTIVE", "url": "https://textures.minecraft.net/texture/cape", "alias": "Migrator"}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(env.srv.Close)
	return env
}

func (e *mockAuthEnv) endpoints() Endpoints {
	return Endpoints{
		MSAuthorizeURL: e.srv.URL + "/oauth2/authorize",
		MSTokenURL:     e.srv.URL + "/oauth2/token",
		XBLAuthURL:     e.srv.URL + "/xbl",
		XSTSAuthURL:    e.srv.URL + "/xsts",
		MCLoginXboxURL: e.srv.URL + "/mc/login",
		MCEntitlements: e.srv.URL + "/mc/entitlements",
		MCProfileURL:   e.srv.URL + "/mc/profile",
	}
}

// Covers Xbox/MC scenarios 1..11: XBL ok/err, XSTS ok/err (2148916233), MC auth ok/err, Entitlements owned/not_owned/503, Profile ok/404
func TestXboxXSTSMinecraftChainAndErrorBranches(t *testing.T) {
	env := newMockAuthServer(t)
	ep := epOrFail(env)
	cl := env.srv.Client()
	ctx := context.Background()

	// 1 & 2: XBL success & failure
	xbl, err := AuthenticateXboxLive(ctx, cl, ep.XBLAuthURL, "MS_TOKEN")
	if err != nil || xbl.Token == "" || xbl.UserHash == "" {
		t.Fatalf("XBL success failed: %+v %v", xbl, err)
	}
	env.failXBL.Store(true)
	if _, err := AuthenticateXboxLive(ctx, cl, ep.XBLAuthURL, "MS_TOKEN"); !errors.Is(err, ErrXBLAuthFailed) {
		t.Fatalf("expected ErrXBLAuthFailed, got %v", err)
	}
	env.failXBL.Store(false)

	// 3 & 4: XSTS success & specific error 2148916233
	xsts, err := AuthorizeXSTS(ctx, cl, ep.XSTSAuthURL, xbl.Token)
	if err != nil || xsts.Token == "" {
		t.Fatalf("XSTS success failed: %+v %v", xsts, err)
	}
	env.failXSTS.Store(2148916233)
	if _, err := AuthorizeXSTS(ctx, cl, ep.XSTSAuthURL, xbl.Token); !errors.Is(err, ErrXSTSAuthFailed) || !strings.Contains(err.Error(), "профиля Xbox Live") {
		t.Fatalf("expected localized XSTS 2148916233 error, got %v", err)
	}
	env.failXSTS.Store(0)

	// 5 & 6: Minecraft Auth success & failure
	mcRes, err := AuthenticateMinecraft(ctx, cl, ep.MCLoginXboxURL, xsts.UserHash, xsts.Token)
	if err != nil || mcRes.AccessToken == "" {
		t.Fatalf("MC Auth failed: %+v %v", mcRes, err)
	}
	env.failMCAuth.Store(true)
	if _, err := AuthenticateMinecraft(ctx, cl, ep.MCLoginXboxURL, xsts.UserHash, xsts.Token); !errors.Is(err, ErrMCAuthFailed) {
		t.Fatalf("expected ErrMCAuthFailed, got %v", err)
	}
	env.failMCAuth.Store(false)

	// 7, 8, 9: Entitlements owned, not_owned, service unavailable (503)
	if st, err := CheckMinecraftEntitlements(ctx, cl, ep.MCEntitlements, mcRes.AccessToken); err != nil || st != LicenseOwned {
		t.Fatalf("expected LicenseOwned, got (%s, %v)", st, err)
	}
	env.licenseMode.Store("not_owned")
	if st, err := CheckMinecraftEntitlements(ctx, cl, ep.MCEntitlements, mcRes.AccessToken); err != nil || st != LicenseNotOwned {
		t.Fatalf("expected LicenseNotOwned, got (%s, %v)", st, err)
	}
	env.licenseMode.Store("503")
	if st, err := CheckMinecraftEntitlements(ctx, cl, ep.MCEntitlements, mcRes.AccessToken); err == nil || st != LicenseServiceUnavailable {
		t.Fatalf("expected LicenseServiceUnavailable on 503, got (%s, %v)", st, err)
	}
	env.licenseMode.Store("owned")

	// 10 & 11: Profile success & 404 missing profile
	prof, err := FetchMinecraftProfile(ctx, cl, ep.MCProfileURL, mcRes.AccessToken)
	if err != nil || prof.Username != "SteveA" || prof.SkinURL == "" || prof.CapeURL == "" {
		t.Fatalf("FetchMinecraftProfile mismatch: %+v %v", prof, err)
	}
	env.noProfile.Store(true)
	if _, err := FetchMinecraftProfile(ctx, cl, ep.MCProfileURL, mcRes.AccessToken); !errors.Is(err, ErrMCProfileNotCreated) {
		t.Fatalf("expected ErrMCProfileNotCreated on 404, got %v", err)
	}
}

func epOrFail(env *mockAuthEnv) Endpoints { return env.endpoints() }

// Covers Storage & Refresh scenarios 1..12:
// vault save/load, corrupted vault, refresh, concurrent refresh sync, revoked refresh token,
// multi-account switching, signout vs delete, zero secrets in SQLite/logs/events, LaunchCredentials gating.
func TestVaultRefreshMultiAccountSecurityAndNoSecretLeaks(t *testing.T) {
	env := newMockAuthServer(t)
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "acct_test.db")
	db, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer db.Close()

	bus := events.NewBus()
	defer bus.Close()

	// Capture every single event payload to assert ZERO tokens ever leak to React!
	var evMu sync.Mutex
	var eventJSONs []string
	bus.SubscribeAll(func(ev events.Event) {
		raw, _ := json.Marshal(ev)
		evMu.Lock()
		eventJSONs = append(eventJSONs, string(raw))
		evMu.Unlock()
	})

	// Automated loopback callback simulator for BrowserOpener
	simBrowser := func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		redir := u.Query().Get("redirect_uri")
		state := u.Query().Get("state")
		go func() {
			resp, err := http.Get(redir + "?code=SIM_CODE_123&state=" + url.QueryEscape(state))
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}

	mgr := NewManager(filepath.Join(dir, "vault"), db, bus, func() string { return "test-entra-client-id" }, simBrowser)
	mgr.SetHTTPClient(env.srv.Client())
	mgr.SetEndpoints(env.endpoints())
	defer mgr.Close()

	// Account 1 login (SteveA, licensed)
	env.currentUserIdx.Store(0)
	a1, err := mgr.LoginInteractiveSync(context.Background(), "")
	if err != nil || a1.MinecraftUsername != "SteveA" || a1.AuthStatus != AuthAuthorized || a1.LicenseStatus != LicenseOwned || !a1.HasVaultSecret {
		t.Fatalf("Account 1 login failed: view=%+v err=%v", a1, err)
	}

	// Account 2 login (SteveB, unlicensed -> ResolveLaunchCredentials MUST refuse!)
	env.currentUserIdx.Store(1)
	env.licenseMode.Store("not_owned")
	a2, err := mgr.LoginInteractiveSync(context.Background(), "")
	if err != nil || a2.LicenseStatus != LicenseNotOwned {
		t.Fatalf("Account 2 login failed: view=%+v err=%v", a2, err)
	}
	if _, err := mgr.ResolveLaunchCredentials(context.Background(), ""); !errors.Is(err, ErrLicenseNotConfirmed) {
		t.Fatalf("expected ErrLicenseNotConfirmed for unlicensed active account, got %v", err)
	}

	// 8 & 9: Switch active account back to SteveA -> LaunchCredentials succeeds and masks token in String()
	env.currentUserIdx.Store(0)
	env.licenseMode.Store("owned")
	if _, err := mgr.SetActive(a1.ID); err != nil {
		t.Fatalf("SetActive a1: %v", err)
	}
	creds, err := mgr.ResolveLaunchCredentials(context.Background(), "")
	if err != nil || creds.MinecraftUsername != "SteveA" || creds.MinecraftAccessToken == "" {
		t.Fatalf("ResolveLaunchCredentials failed: creds=%s err=%v", creds.String(), err)
	}
	if strings.Contains(creds.String(), "MC_SECRET_JWT_XYZ") {
		t.Fatal("LaunchCredentials.String() leaked Minecraft token!")
	}
	credsJSON, _ := json.Marshal(creds)
	if strings.Contains(string(credsJSON), "MC_SECRET_JWT_XYZ") {
		t.Fatal("LaunchCredentials JSON leaked Minecraft token!")
	}

	// 4 & 5: Force concurrent token refresh for SteveA -> synchronized per-account
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = mgr.RefreshAccount(context.Background(), a1.ID, true)
		}()
	}
	wg.Wait()

	// 6: Revoked refresh token transitions cleanly to AuthReauthRequired
	env.revokeRefresh.Store(true)
	vRev, err := mgr.RefreshAccount(context.Background(), a1.ID, true)
	if !errors.Is(err, ErrReauthRequired) || vRev.AuthStatus != AuthReauthRequired {
		t.Fatalf("expected ErrReauthRequired on revoked refresh token, got view=%+v err=%v", vRev, err)
	}
	env.revokeRefresh.Store(false)

	// 7: Re-authentication recovers account
	a1Re, err := mgr.LoginInteractiveSync(context.Background(), a1.ID)
	if err != nil || a1Re.AuthStatus != AuthAuthorized {
		t.Fatalf("re-auth failed: view=%+v err=%v", a1Re, err)
	}

	// 11 & 12: Verify ZERO secret tokens exist in SQLite file OR emitted events OR sanitized logs!
	dbBytes, _ := os.ReadFile(dbPath)
	for _, secret := range []string{"MS_SECRET_ACCESS_TOKEN_XYZ", "MS_SECRET_REFRESH_TOKEN_XYZ", "XBL_SECRET_TOKEN_XYZ", "XSTS_SECRET_TOKEN_XYZ", "MC_SECRET_JWT_XYZ"} {
		if strings.Contains(string(dbBytes), secret) {
			t.Fatalf("CRITICAL: secret %q found in SQLite database file!", secret)
		}
		evMu.Lock()
		for _, ej := range eventJSONs {
			if strings.Contains(ej, secret) {
				t.Fatalf("CRITICAL: secret %q leaked into EventBus event: %s", secret, ej)
			}
		}
		evMu.Unlock()
		if strings.Contains(security.SanitizeLogText("Bearer "+secret), secret) {
			t.Fatalf("SanitizeLogText failed to redact %q", secret)
		}
	}

	// 3: Corrupt vault file -> next Manager load marks account reauth_required without crashing
	vaultPath := filepath.Join(dir, "vault", "vk_"+a1.ID+".sec")
	_ = os.WriteFile(vaultPath, []byte("CORRUPTED-VAULT"), 0o600)
	mgrReload := NewManager(filepath.Join(dir, "vault"), db, nil, func() string { return "test-entra-client-id" }, simBrowser)
	defer mgrReload.Close()
	reloadedA1, err := mgrReload.GetAccount(a1.ID)
	if err != nil || reloadedA1.AuthStatus != AuthReauthRequired {
		t.Fatalf("expected corrupted vault to mark account reauth_required, got %+v err=%v", reloadedA1, err)
	}

	// 10: Delete account removes DB record AND vault file, while keeping second account intact
	if err := mgrReload.DeleteAccount(a1.ID); err != nil {
		t.Fatalf("DeleteAccount failed: %v", err)
	}
	if _, err := os.Stat(vaultPath); !os.IsNotExist(err) {
		t.Fatal("expected vault secret file to be deleted with account")
	}
	if snap := mgrReload.GetSnapshot(); len(snap.Accounts) != 1 || snap.ActiveAccountID != a2.ID {
		t.Fatalf("expected remaining account a2 promoted to active, got %+v", snap)
	}
}

func TestOfflineAccountManagementAndLaunch(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.OpenDatabase(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	defer db.Close()

	mgr := NewManager(filepath.Join(dir, "vault"), db, nil, nil, nil)
	defer mgr.Close()

	// 1. Validation checks
	if _, err := mgr.AddOfflineAccount("ab"); err == nil {
		t.Fatal("expected short nickname rejected")
	}
	if _, err := mgr.AddOfflineAccount("name with spaces"); err == nil {
		t.Fatal("expected space in nickname rejected")
	}
	if _, err := mgr.AddOfflineAccount("Toolong12345678901"); err == nil {
		t.Fatal("expected >16 chars rejected")
	}

	// 2. Add valid offline account
	off, err := mgr.AddOfflineAccount("Player_One")
	if err != nil {
		t.Fatalf("AddOfflineAccount failed: %v", err)
	}
	if off.MinecraftUsername != "Player_One" || off.AccountType != AccountTypeOffline || !off.Active {
		t.Fatalf("unexpected offline account view: %+v", off)
	}

	rawUUID, dashed := GenerateOfflinePlayerUUID("Player_One")
	if off.MinecraftUUID != rawUUID || off.FormattedUUID != dashed {
		t.Fatalf("UUID mismatch: expected %s / %s, got %s / %s", rawUUID, dashed, off.MinecraftUUID, off.FormattedUUID)
	}

	// 3. Resolve launch credentials without network or Microsoft tokens
	creds, err := mgr.ResolveLaunchCredentials(context.Background(), off.ID)
	if err != nil {
		t.Fatalf("ResolveLaunchCredentials for offline account failed: %v", err)
	}
	if creds.AccountType != AccountTypeOffline || creds.UserType != "mojang" || creds.MinecraftAccessToken != "0" {
		t.Fatalf("unexpected offline launch creds: %+v", creds)
	}
	if creds.MinecraftUsername != "Player_One" || creds.MinecraftUUID != rawUUID {
		t.Fatalf("offline creds identity mismatch: %+v", creds)
	}

	// 4. Persistence across DB reload
	mgrReload := NewManager(filepath.Join(dir, "vault"), db, nil, nil, nil)
	defer mgrReload.Close()
	snap := mgrReload.GetSnapshot()
	if len(snap.Accounts) != 1 || snap.ActiveAccountID != off.ID {
		t.Fatalf("expected 1 restored offline account, got %+v", snap)
	}
	if snap.Accounts[0].AccountType != AccountTypeOffline || snap.Accounts[0].MinecraftUsername != "Player_One" {
		t.Fatalf("restored offline account fields corrupt: %+v", snap.Accounts[0])
	}
}

func TestUpdateAccountSkin(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.OpenDatabase(filepath.Join(dir, "modhub.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	mgr := NewManager(filepath.Join(dir, "vault"), db, nil, nil, nil)
	defer mgr.Close()

	off, err := mgr.AddOfflineAccount("SkinPlayer")
	if err != nil {
		t.Fatalf("AddOfflineAccount failed: %v", err)
	}

	updated, err := mgr.UpdateAccountSkin(off.ID, "data:image/png;base64,iVBORw0KGgo...", "SLIM", "https://textures.minecraft.net/cape.png", "Migrator")
	if err != nil {
		t.Fatalf("UpdateAccountSkin failed: %v", err)
	}

	if updated.SkinURL != "data:image/png;base64,iVBORw0KGgo..." || updated.SkinVariant != "SLIM" {
		t.Fatalf("Skin fields mismatch: %+v", updated)
	}
	if updated.CapeURL != "https://textures.minecraft.net/cape.png" || updated.CapeAlias != "Migrator" {
		t.Fatalf("Cape fields mismatch: %+v", updated)
	}

	// Verify persistence
	reloaded, err := mgr.GetAccount(off.ID)
	if err != nil || reloaded.SkinVariant != "SLIM" || reloaded.CapeAlias != "Migrator" {
		t.Fatalf("Reload mismatch: %+v %v", reloaded, err)
	}
}

func TestSequentialMicrosoftLoginsAcrossDynamicLocalhostPorts(t *testing.T) {
	origDefaultClientID := DefaultClientID
	DefaultClientID = "shared-modhub-entra-client-id"
	t.Cleanup(func() { DefaultClientID = origDefaultClientID })

	var tokenMu sync.Mutex
	var recordedRedirects []string
	var recordedChallenges []string
	var lastAuthorizeRedirect string
	var lastCodeChallenge string

	env := newMockAuthServer(t)
	ep := env.endpoints()

	// Wrap mock token server to strictly enforce Entra PKCE + exact redirect_uri parity + NO client_secret
	strictSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			_ = r.ParseForm()
			if r.Form.Get("client_secret") != "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_request","error_description":"client_secret forbidden for public desktop client"}`))
				return
			}
			if r.Form.Get("client_id") != DefaultClientID {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"unexpected client_id"}`))
				return
			}
			if r.Form.Get("grant_type") == "authorization_code" {
				redir := r.Form.Get("redirect_uri")
				verifier := r.Form.Get("code_verifier")
				sum := sha256.Sum256([]byte(verifier))
				derivedChallenge := base64.RawURLEncoding.EncodeToString(sum[:])

				tokenMu.Lock()
				wantRedir := lastAuthorizeRedirect
				wantChal := lastCodeChallenge
				recordedRedirects = append(recordedRedirects, redir)
				recordedChallenges = append(recordedChallenges, derivedChallenge)
				tokenMu.Unlock()

				if !strings.HasPrefix(redir, "http://localhost:") {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"invalid_request","error_description":"redirect_uri must be http://localhost:<port>"}`))
					return
				}
				if redir != wantRedir {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"AADSTS50011: redirect_uri mismatch"}`))
					return
				}
				if verifier == "" || derivedChallenge != wantChal {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"PKCE code_verifier mismatch"}`))
					return
				}
			}
		}
		env.srv.Config.Handler.ServeHTTP(w, r)
	}))
	defer strictSrv.Close()

	ep.MSAuthorizeURL = strictSrv.URL + "/oauth2/authorize"
	ep.MSTokenURL = strictSrv.URL + "/oauth2/token"
	ep.XBLAuthURL = strictSrv.URL + "/xbl"
	ep.XSTSAuthURL = strictSrv.URL + "/xsts"
	ep.MCLoginXboxURL = strictSrv.URL + "/mc/login"
	ep.MCEntitlements = strictSrv.URL + "/mc/entitlements"
	ep.MCProfileURL = strictSrv.URL + "/mc/profile"

	dir := t.TempDir()
	db, err := storage.OpenDatabase(filepath.Join(dir, "seq_oauth.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer db.Close()

	var simErr error
	var cancelNext atomic.Bool
	simBrowser := func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		q := u.Query()
		if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("client_secret") != "" || q.Get("client_id") != DefaultClientID {
			simErr = errors.New("invalid authorize query parameters: " + authURL)
			return simErr
		}
		redir := q.Get("redirect_uri")
		if !strings.HasPrefix(redir, "http://localhost:") {
			simErr = errors.New("expected http://localhost:<port>, got " + redir)
			return simErr
		}
		tokenMu.Lock()
		lastAuthorizeRedirect = redir
		lastCodeChallenge = q.Get("code_challenge")
		tokenMu.Unlock()

		state := q.Get("state")
		go func() {
			if cancelNext.Load() {
				resp, err := http.Get(redir + "?error=access_denied&state=" + url.QueryEscape(state))
				if err == nil {
					_ = resp.Body.Close()
				}
				return
			}
			// Simulate browser hitting /favicon.ico first, then completing redirect on http://localhost:<port>
			if fav, err := http.Get(redir + "/favicon.ico"); err == nil {
				_ = fav.Body.Close()
			}
			resp, err := http.Get(redir + "?code=ENTRA_CODE_OK&state=" + url.QueryEscape(state))
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}

	mgr := NewManager(filepath.Join(dir, "vault"), db, nil, nil, simBrowser)
	mgr.SetHTTPClient(strictSrv.Client())
	mgr.SetEndpoints(ep)
	defer mgr.Close()

	cid, src := mgr.ResolveClientID()
	if cid != DefaultClientID || src != "build" {
		t.Fatalf("expected shared DefaultClientID via build source, got (%q, %q)", cid, src)
	}

	// Sequential run #1 -> succeeds
	env.currentUserIdx.Store(0)
	v1, err := mgr.LoginInteractiveSync(context.Background(), "")
	if err != nil || simErr != nil || v1.AuthStatus != AuthAuthorized || v1.LicenseStatus != LicenseOwned {
		t.Fatalf("sequential login #1 failed: view=%+v err=%v simErr=%v", v1, err, simErr)
	}

	// Sequential run #2 -> user cancels, port cleanly released
	cancelNext.Store(true)
	if _, err := mgr.LoginInteractiveSync(context.Background(), ""); !errors.Is(err, ErrAuthCancelled) {
		t.Fatalf("expected ErrAuthCancelled on run #2, got %v", err)
	}
	cancelNext.Store(false)

	// Sequential run #3 -> succeeds on a fresh dynamic localhost port
	env.currentUserIdx.Store(1)
	v3, err := mgr.LoginInteractiveSync(context.Background(), "")
	if err != nil || simErr != nil || v3.AuthStatus != AuthAuthorized || v3.MinecraftUsername != "SteveB" {
		t.Fatalf("sequential login #3 failed: view=%+v err=%v simErr=%v", v3, err, simErr)
	}

	// Sequential run #4 -> re-auth on another fresh dynamic localhost port
	env.currentUserIdx.Store(0)
	v4, err := mgr.LoginInteractiveSync(context.Background(), v1.ID)
	if err != nil || simErr != nil || v4.ID != v1.ID || v4.AuthStatus != AuthAuthorized {
		t.Fatalf("sequential login #4 failed: view=%+v err=%v simErr=%v", v4, err, simErr)
	}

	tokenMu.Lock()
	defer tokenMu.Unlock()
	if len(recordedRedirects) != 3 {
		t.Fatalf("expected 3 token exchanges, got %d (%v)", len(recordedRedirects), recordedRedirects)
	}
	seenPorts := map[string]bool{}
	for _, r := range recordedRedirects {
		if !strings.HasPrefix(r, "http://localhost:") {
			t.Fatalf("unexpected recorded redirect URI: %q", r)
		}
		seenPorts[r] = true
	}
	if len(seenPorts) < 2 {
		t.Fatalf("expected distinct dynamic ports across sequential runs, got %v", recordedRedirects)
	}
}
