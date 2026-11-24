package accounts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"modhub/internal/domain"
	"modhub/internal/events"
	"modhub/internal/netx"
	"modhub/internal/security"
)

const (
	defaultAuthTimeout = 5 * time.Minute
	tokenFreshMargin   = 5 * time.Minute
	msFreshMargin      = 2 * time.Minute
)

var (
	ErrAccountNotFound     = errors.New("аккаунт не найден")
	ErrAuthInProgress      = errors.New("авторизация Microsoft уже выполняется")
	ErrNoActiveAccount     = errors.New("не выбран активный аккаунт Microsoft")
	ErrLicenseNotConfirmed = errors.New("лицензия Minecraft Java Edition не подтверждена для этого аккаунта")
	ErrReauthRequired      = errors.New("требуется повторный вход в аккаунт Microsoft")
)

// ClientIDProvider resolves the currently configured Microsoft Entra public client ID.
type ClientIDProvider func() string

// Manager orchestrates Microsoft OAuth2 (PKCE), Xbox Live, XSTS, Minecraft Services,
// encrypted token vaulting, synchronized token refresh, multi-account management,
// and safe internal credential handoff to the future Game Launcher.
type Manager struct {
	vault         *SecretVault
	store         Store
	bus           *events.Bus
	clientIDFn    ClientIDProvider
	browserOpener BrowserOpener

	mu           sync.Mutex
	httpClient   *http.Client
	endpoints    Endpoints
	authTimeout  time.Duration
	accounts     map[string]*domain.AccountRecord
	refreshLocks map[string]*sync.Mutex
	authCancel   context.CancelFunc
	flow         AuthFlowProgress
	closeOnce    sync.Once
	wg           sync.WaitGroup
}

func defaultBrowserOpen(rawURL string) error {
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	case "darwin":
		cmd = exec.Command("open", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}

func newDefaultAuthHTTPClient() *http.Client {
	tr := &http.Transport{
		Proxy: netx.ResolveProxyFunc,
		DialContext: netx.MakeSafeDialContext(&net.Dialer{
			Timeout:   12 * time.Second,
			KeepAlive: 30 * time.Second,
		}),
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     45 * time.Second,
		TLSHandshakeTimeout: 12 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	return &http.Client{
		Timeout:   25 * time.Second,
		Transport: tr,
	}
}

func NewManager(vaultDir string, store Store, bus *events.Bus, clientIDFn ClientIDProvider, browserOpener BrowserOpener) *Manager {
	if browserOpener == nil {
		browserOpener = defaultBrowserOpen
	}
	m := &Manager{
		vault:         NewSecretVault(vaultDir),
		store:         store,
		bus:           bus,
		clientIDFn:    clientIDFn,
		browserOpener: browserOpener,
		httpClient:    newDefaultAuthHTTPClient(),
		endpoints:     OfficialEndpoints(),
		authTimeout:   defaultAuthTimeout,
		accounts:      make(map[string]*domain.AccountRecord),
		refreshLocks:  make(map[string]*sync.Mutex),
		flow:          AuthFlowProgress{Stage: "idle"},
	}
	m.loadFromStore()
	return m
}

func (m *Manager) SetHTTPClient(c *http.Client) {
	if c != nil {
		m.mu.Lock()
		m.httpClient = c
		m.mu.Unlock()
	}
}

func (m *Manager) SetEndpoints(ep Endpoints) {
	m.mu.Lock()
	m.endpoints = ep
	m.mu.Unlock()
}

func (m *Manager) SetAuthTimeout(d time.Duration) {
	if d > 0 {
		m.mu.Lock()
		m.authTimeout = d
		m.mu.Unlock()
	}
}

func (m *Manager) SetBrowserOpener(fn BrowserOpener) {
	if fn != nil {
		m.mu.Lock()
		m.browserOpener = fn
		m.mu.Unlock()
	}
}

func (m *Manager) ResetConnections() {
	m.mu.Lock()
	c := m.httpClient
	m.mu.Unlock()
	if c != nil {
		if tr, ok := c.Transport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
	}
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		m.CancelAuth()
		m.wg.Wait()
		m.ResetConnections()
	})
}

func (m *Manager) ResolveClientID() (string, string) {
	if envID := strings.TrimSpace(os.Getenv("MODHUB_MS_CLIENT_ID")); envID != "" {
		return envID, "env"
	}
	if m.clientIDFn != nil {
		if cfgID := strings.TrimSpace(m.clientIDFn()); cfgID != "" {
			return cfgID, "config"
		}
	}
	if buildID := strings.TrimSpace(DefaultClientID); buildID != "" {
		return buildID, "build"
	}
	return "", "none"
}

func (m *Manager) GetConfigStatus() AuthConfigStatus {
	cid, src := m.ResolveClientID()
	st := AuthConfigStatus{
		Configured:   cid != "",
		Source:       src,
		VaultBackend: security.VaultBackendName(),
		RedirectNote: "http://localhost (Mobile and desktop applications, публичный клиент без client_secret)",
	}
	if !st.Configured {
		st.Diagnostic = "Microsoft Client ID не задан. Зарегистрируйте публичное desktop-приложение в Microsoft Entra ID и укажите Client ID в Настройках или через переменную среды MODHUB_MS_CLIENT_ID."
	}
	return st
}

func (m *Manager) loadFromStore() {
	if m.store == nil {
		return
	}
	recs, err := m.store.ListAccounts()
	if err != nil {
		return
	}
	for _, rec := range recs {
		if strings.TrimSpace(rec.ID) == "" {
			continue
		}
		cp := rec
		// If an account was mid-auth when the app crashed, normalize honestly
		if AuthStatus(cp.AuthStatus) == AuthAuthenticating {
			cp.AuthStatus = string(AuthReauthRequired)
			cp.LastError = "Предыдущая авторизация была прервана"
			_ = m.store.UpsertAccount(cp)
		}
		// Detect missing or corrupted encrypted vault file without deleting other accounts
		if AuthStatus(cp.AuthStatus) == AuthAuthorized {
			if cp.VaultKey == "" || !m.vault.Exists(cp.VaultKey) {
				cp.AuthStatus = string(AuthReauthRequired)
				cp.LastError = "Защищённые токены недоступны — выполните повторный вход"
				_ = m.store.UpsertAccount(cp)
			} else if _, verr := m.vault.Load(cp.VaultKey); verr != nil {
				cp.AuthStatus = string(AuthReauthRequired)
				cp.LastError = "Защищённые данные токенов повреждены — выполните повторный вход"
				_ = m.store.UpsertAccount(cp)
			}
		}
		m.accounts[cp.ID] = &cp
	}
}

func (m *Manager) lockAccountRefresh(id string) func() {
	m.mu.Lock()
	lk, ok := m.refreshLocks[id]
	if !ok {
		lk = &sync.Mutex{}
		m.refreshLocks[id] = lk
	}
	m.mu.Unlock()
	lk.Lock()
	return lk.Unlock
}

func generateAccountID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("acct_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(b[:]))
}

func (m *Manager) GetSnapshot() AccountsSnapshot {
	m.mu.Lock()
	list := make([]AccountView, 0, len(m.accounts))
	activeID := ""
	for _, rec := range m.accounts {
		v := m.recordToView(*rec)
		if v.Active {
			activeID = v.ID
		}
		list = append(list, v)
	}
	flow := m.flow
	m.mu.Unlock()

	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Active != list[j].Active {
			return list[i].Active
		}
		if list[i].UpdatedAt != list[j].UpdatedAt {
			return list[i].UpdatedAt > list[j].UpdatedAt
		}
		return list[i].CreatedAt < list[j].CreatedAt
	})
	return AccountsSnapshot{
		Accounts:        list,
		ActiveAccountID: activeID,
		Config:          m.GetConfigStatus(),
		Flow:            flow,
	}
}

func (m *Manager) GetAccount(id string) (AccountView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.accounts[id]
	if !ok {
		return AccountView{}, ErrAccountNotFound
	}
	return m.recordToView(*rec), nil
}

// AddOfflineAccount creates a local offline profile without requiring Microsoft Entra ID or internet.
// Generates a standard deterministic Type 3 MD5-based UUID conforming to Minecraft Java Edition specification.
func (m *Manager) AddOfflineAccount(username string) (AccountView, error) {
	if err := ValidateMinecraftUsername(username); err != nil {
		return AccountView{}, err
	}
	trimmed := strings.TrimSpace(username)
	rawUUID, _ := GenerateOfflinePlayerUUID(trimmed)
	id := "offline_" + rawUUID

	m.mu.Lock()
	now := time.Now().Unix()
	rec, exists := m.accounts[id]
	if !exists {
		rec = &domain.AccountRecord{
			ID:                 id,
			AccountType:        AccountTypeOffline,
			MicrosoftAccountID: "offline",
			XboxUserID:         "0",
			XboxGamertag:       trimmed,
			MinecraftUUID:      rawUUID,
			MinecraftUsername:  trimmed,
			ProfileName:        trimmed,
			AuthStatus:         string(AuthAuthorized),
			LicenseStatus:      string(LicenseOwned),
			CreatedAt:          now,
		}
		m.accounts[id] = rec
	} else {
		rec.AccountType = AccountTypeOffline
		rec.MinecraftUsername = trimmed
		rec.ProfileName = trimmed
		rec.AuthStatus = string(AuthAuthorized)
		rec.LicenseStatus = string(LicenseOwned)
	}

	rec.UpdatedAt = now
	rec.LastAuthAt = now

	// Automatically make the newly added/updated offline account active
	for _, a := range m.accounts {
		a.Active = (a.ID == id)
	}

	if m.store != nil {
		if err := m.store.UpsertAccount(*rec); err != nil {
			m.mu.Unlock()
			return AccountView{}, err
		}
		_ = m.store.SetActiveAccount(id)
	}

	view := m.recordToView(*rec)
	m.mu.Unlock()

	if m.bus != nil {
		m.bus.Publish(events.EventAccountUpdated, view)
		m.bus.Publish(events.EventAccountActiveChanged, map[string]any{"account_id": id})
	}
	return view, nil
}

// StartMicrosoftLoginAsync launches the full official Microsoft OAuth2 PKCE + Xbox + XSTS + Minecraft flow in background.
// If reauthAccountID is non-empty, updates that specific account upon completion.
func (m *Manager) StartMicrosoftLoginAsync(reauthAccountID string) error {
	cid, _ := m.ResolveClientID()
	if cid == "" {
		return ErrMissingClientID
	}

	m.mu.Lock()
	if m.authCancel != nil {
		m.mu.Unlock()
		return ErrAuthInProgress
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.authCancel = cancel
	m.flow = AuthFlowProgress{
		Active:    true,
		AccountID: strings.TrimSpace(reauthAccountID),
		Stage:     "started",
		Message:   "Подготовка авторизации Microsoft OAuth 2.0 (PKCE)...",
		UpdatedAt: time.Now().Unix(),
	}
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		_, _ = m.runInteractiveLogin(ctx, cid, strings.TrimSpace(reauthAccountID))
	}()
	return nil
}

// LoginInteractiveSync runs the full official Microsoft OAuth2 PKCE + Xbox + XSTS + Minecraft flow synchronously
// (used by automated tests and internal orchestration).
func (m *Manager) LoginInteractiveSync(ctx context.Context, reauthAccountID string) (AccountView, error) {
	cid, _ := m.ResolveClientID()
	if cid == "" {
		return AccountView{}, ErrMissingClientID
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if m.authCancel != nil {
		m.mu.Unlock()
		return AccountView{}, ErrAuthInProgress
	}
	iCtx, cancel := context.WithCancel(ctx)
	m.authCancel = cancel
	m.flow = AuthFlowProgress{
		Active:    true,
		AccountID: strings.TrimSpace(reauthAccountID),
		Stage:     "started",
		Message:   "Подготовка авторизации Microsoft OAuth 2.0 (PKCE)...",
		UpdatedAt: time.Now().Unix(),
	}
	m.mu.Unlock()

	return m.runInteractiveLogin(iCtx, cid, strings.TrimSpace(reauthAccountID))
}

func (m *Manager) CancelAuth() bool {
	m.mu.Lock()
	cancel := m.authCancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
		return true
	}
	return false
}

func (m *Manager) runInteractiveLogin(ctx context.Context, clientID, reauthAccountID string) (AccountView, error) {
	defer func() {
		m.mu.Lock()
		if m.authCancel != nil {
			m.authCancel()
			m.authCancel = nil
		}
		m.flow.Active = false
		m.mu.Unlock()
	}()

	var currentAuthURL string
	emitStage := func(ev events.EventName, stage, msg, errStr, redirectURI, acctID string) {
		p := AuthFlowProgress{
			Active:      stage != "completed" && stage != "error" && stage != "cancelled",
			AccountID:   acctID,
			Stage:       stage,
			Message:     security.SanitizeLogText(msg),
			Error:       security.SanitizeLogText(errStr),
			RedirectURI: redirectURI,
			AuthURL:     currentAuthURL,
			UpdatedAt:   time.Now().Unix(),
		}
		m.mu.Lock()
		m.flow = p
		m.mu.Unlock()
		if m.bus != nil && ev != "" {
			m.bus.Publish(ev, p)
		}
	}

	failFlow := func(err error) (AccountView, error) {
		safeMsg := security.SanitizeLogText(err.Error())
		stage := "error"
		if errors.Is(err, ErrAuthCancelled) || errors.Is(err, context.Canceled) {
			stage = "cancelled"
		}
		emitStage(events.EventAuthError, stage, safeMsg, safeMsg, "", reauthAccountID)
		if errors.Is(err, context.Canceled) && !errors.Is(err, ErrAuthCancelled) {
			return AccountView{}, ErrAuthCancelled
		}
		if safeMsg == err.Error() {
			return AccountView{}, err
		}
		return AccountView{}, errors.New(safeMsg)
	}

	emitStage(events.EventAuthStarted, "started", "Генерация параметров PKCE и state...", "", "", reauthAccountID)

	state, err := GenerateState()
	if err != nil {
		return failFlow(fmt.Errorf("ошибка генерации OAuth state: %w", err))
	}
	pkce, err := GeneratePKCE()
	if err != nil {
		return failFlow(fmt.Errorf("ошибка генерации PKCE: %w", err))
	}

	cbSrv, err := StartLoopbackCallbackServer(state)
	if err != nil {
		return failFlow(err)
	}
	defer cbSrv.Close()

	m.mu.Lock()
	ep := m.endpoints
	timeout := m.authTimeout
	opener := m.browserOpener
	client := m.httpClient
	m.mu.Unlock()

	redirectURI := cbSrv.RedirectURI()
	authURL, err := BuildAuthorizeURL(ep.MSAuthorizeURL, clientID, redirectURI, state, pkce)
	if err != nil {
		return failFlow(err)
	}
	currentAuthURL = authURL

	emitStage(events.EventAuthBrowserOpened, "browser_opened", "Открытие системного браузера для входа на официальном сайте Microsoft...", "", redirectURI, reauthAccountID)
	if err := opener(authURL); err != nil {
		return failFlow(fmt.Errorf("не удалось открыть системный браузер: %w", err))
	}

	code, err := cbSrv.Wait(ctx, timeout)
	currentAuthURL = ""
	if err != nil {
		return failFlow(err)
	}

	emitStage(events.EventAuthOAuthCompleted, "oauth_completed", "Код авторизации получен. Обмен на токены Microsoft...", "", "", reauthAccountID)
	msResp, err := ExchangeAuthCode(ctx, client, ep.MSTokenURL, clientID, code, redirectURI, pkce.Verifier)
	if err != nil {
		return failFlow(err)
	}

	expIn := msResp.ExpiresIn
	if expIn <= 0 {
		expIn = 3600
	}
	bundle := SecretBundle{
		MSAccessToken:  msResp.AccessToken,
		MSRefreshToken: msResp.RefreshToken,
		MSExpiresAt:    time.Now().Add(time.Duration(expIn) * time.Second).Unix(),
	}

	view, err := m.completeXboxAndMinecraftChain(ctx, client, ep, bundle, msResp.UserID, reauthAccountID, emitStage)
	if err != nil {
		return failFlow(err)
	}
	emitStage(events.EventAccountUpdated, "completed", fmt.Sprintf("Авторизация завершена: %s", displayAccountName(view)), "", "", view.ID)
	return view, nil
}

func (m *Manager) completeXboxAndMinecraftChain(
	ctx context.Context,
	client *http.Client,
	ep Endpoints,
	bundle SecretBundle,
	msUserID string,
	targetAccountID string,
	emitStage func(ev events.EventName, stage, msg, errStr, redirectURI, acctID string),
) (AccountView, error) {
	// Step 2: Xbox Live User Authentication
	if emitStage != nil {
		emitStage(events.EventAuthXboxStarted, "xbox_started", "Авторизация Xbox Live...", "", "", targetAccountID)
	}
	xbl, err := AuthenticateXboxLive(ctx, client, ep.XBLAuthURL, bundle.MSAccessToken)
	if err != nil {
		return AccountView{}, err
	}
	bundle.XBLToken = xbl.Token
	bundle.XBLUserHash = xbl.UserHash
	bundle.XBLExpiresAt = xbl.ExpiresAt

	// Step 3: XSTS Authorization
	xsts, err := AuthorizeXSTS(ctx, client, ep.XSTSAuthURL, xbl.Token)
	if err != nil {
		return AccountView{}, err
	}
	bundle.XSTSToken = xsts.Token
	bundle.XSTSUserHash = xsts.UserHash
	bundle.XSTSExpiresAt = xsts.ExpiresAt
	if emitStage != nil {
		emitStage(events.EventAuthXboxCompleted, "xbox_completed", "Авторизация Xbox Live и XSTS успешно пройдена.", "", "", targetAccountID)
	}

	// Step 4: Minecraft Services Authentication
	if emitStage != nil {
		emitStage(events.EventAuthMinecraftStarted, "minecraft_started", "Получение токена Minecraft Services...", "", "", targetAccountID)
	}
	mcLogin, err := AuthenticateMinecraft(ctx, client, ep.MCLoginXboxURL, xsts.UserHash, xsts.Token)
	if err != nil {
		return AccountView{}, err
	}
	bundle.MCAccessToken = mcLogin.AccessToken
	bundle.MCExpiresAt = mcLogin.ExpiresAt

	xboxUID := mcLogin.XboxUserID
	if xboxUID == "" {
		xboxUID = xsts.XboxUID
	}
	if xboxUID == "" {
		xboxUID = xbl.XboxUID
	}
	gamertag := xsts.Gamertag
	if gamertag == "" {
		gamertag = xbl.Gamertag
	}

	// Step 5: Minecraft Java Edition Entitlements Check
	now := time.Now().Unix()
	licStatus, licErr := CheckMinecraftEntitlements(ctx, client, ep.MCEntitlements, bundle.MCAccessToken)
	licMsg := describeLicenseStatus(licStatus, licErr)
	if emitStage != nil {
		emitStage(events.EventAuthLicenseChecked, "license_checked", licMsg, "", "", targetAccountID)
	}

	// Step 6: Minecraft Java Profile (attempt even if entitlements check had network error; handle 404 cleanly)
	var prof mcProfileResult
	var profErr error
	var profUpdated int64
	if licStatus == LicenseOwned || licStatus == LicenseNetworkError || licStatus == LicenseServiceUnavailable {
		prof, profErr = FetchMinecraftProfile(ctx, client, ep.MCProfileURL, bundle.MCAccessToken)
		if profErr == nil {
			profUpdated = now
			if emitStage != nil {
				emitStage(events.EventAuthProfileLoaded, "profile_loaded", fmt.Sprintf("Профиль Minecraft загружен: %s", prof.Username), "", "", targetAccountID)
			}
		}
	}

	// Find existing account by explicit reauth ID, Minecraft UUID, Xbox UID, or Microsoft User ID (never email alone)
	m.mu.Lock()
	var existing *domain.AccountRecord
	if targetAccountID != "" {
		if r, ok := m.accounts[targetAccountID]; ok {
			existing = r
		}
	}
	if existing == nil {
		for _, r := range m.accounts {
			if prof.UUID != "" && r.MinecraftUUID != "" && strings.EqualFold(r.MinecraftUUID, prof.UUID) {
				existing = r
				break
			}
			if xboxUID != "" && r.XboxUserID != "" && r.XboxUserID == xboxUID {
				existing = r
				break
			}
			if msUserID != "" && r.MicrosoftAccountID != "" && r.MicrosoftAccountID == msUserID {
				existing = r
				break
			}
		}
	}

	var rec domain.AccountRecord
	if existing != nil {
		rec = *existing
	} else {
		rec = domain.AccountRecord{
			ID:        generateAccountID(),
			CreatedAt: now,
		}
	}
	if rec.VaultKey == "" {
		rec.VaultKey = "vk_" + rec.ID
	}

	// Save encrypted tokens into OS vault BEFORE persisting SQLite metadata!
	if err := m.vault.Save(rec.VaultKey, bundle); err != nil {
		m.mu.Unlock()
		return AccountView{}, err
	}

	if msUserID != "" {
		rec.MicrosoftAccountID = msUserID
	}
	if xboxUID != "" {
		rec.XboxUserID = xboxUID
	}
	if gamertag != "" {
		rec.XboxGamertag = gamertag
	}
	if profErr == nil && prof.UUID != "" {
		rec.MinecraftUUID = prof.UUID
		rec.MinecraftUsername = prof.Username
		rec.ProfileName = prof.ProfileName
		rec.SkinURL = prof.SkinURL
		rec.SkinVariant = prof.SkinVariant
		rec.CapeURL = prof.CapeURL
		rec.CapeAlias = prof.CapeAlias
		rec.ProfileUpdatedAt = profUpdated
	}

	rec.LicenseStatus = string(licStatus)
	rec.LastAuthAt = now
	rec.LastLicenseCheckAt = now
	rec.MSExpiresAt = bundle.MSExpiresAt
	rec.XBLExpiresAt = bundle.XBLExpiresAt
	rec.XSTSExpiresAt = bundle.XSTSExpiresAt
	rec.MCExpiresAt = bundle.MCExpiresAt
	rec.UpdatedAt = now

	// Set honest AuthStatus & diagnostics
	rec.AuthStatus = string(AuthAuthorized)
	rec.LastError = ""
	if licErr != nil {
		rec.LastError = security.SanitizeLogText(licErr.Error())
		if licStatus == LicenseNetworkError || licStatus == LicenseServiceUnavailable {
			rec.AuthStatus = string(AuthServiceError)
		}
	} else if errors.Is(profErr, ErrMCProfileNotCreated) {
		rec.LastError = ErrMCProfileNotCreated.Error()
	} else if profErr != nil {
		rec.LastError = security.SanitizeLogText(profErr.Error())
	}

	// Make active if no active account yet OR newly added/reauthenticated account
	rec.Active = true
	for _, other := range m.accounts {
		other.Active = false
	}
	recCopy := rec
	m.accounts[rec.ID] = &recCopy
	if m.store != nil {
		if err := m.store.UpsertAccount(recCopy); err != nil {
			m.mu.Unlock()
			return AccountView{}, fmt.Errorf("не удалось сохранить метаданные аккаунта: %w", err)
		}
	}
	view := m.recordToView(recCopy)
	m.mu.Unlock()

	if m.bus != nil {
		m.bus.Publish(events.EventAccountUpdated, view)
		m.bus.Publish(events.EventAccountActiveChanged, map[string]any{"account_id": view.ID})
	}
	return view, nil
}

// RefreshAccount refreshes expired tokens for a specific account with strict per-account mutex synchronization.
// Never loops infinitely on invalid/revoked refresh tokens; marks account reauth_required instead.
func (m *Manager) RefreshAccount(ctx context.Context, id string, force bool) (AccountView, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	unlock := m.lockAccountRefresh(id)
	defer unlock()

	m.mu.Lock()
	recPtr, ok := m.accounts[id]
	if !ok {
		m.mu.Unlock()
		return AccountView{}, ErrAccountNotFound
	}
	rec := *recPtr
	client := m.httpClient
	ep := m.endpoints
	m.mu.Unlock()

	if rec.AccountType == AccountTypeOffline || rec.MicrosoftAccountID == "offline" || strings.HasPrefix(rec.ID, "offline_") {
		return m.recordToView(rec), nil
	}

	if rec.VaultKey == "" {
		return m.markReauthRequired(id, "Отсутствует защищённое хранилище токенов — выполните вход заново")
	}
	bundle, err := m.vault.Load(rec.VaultKey)
	if err != nil {
		return m.markReauthRequired(id, err.Error())
	}

	now := time.Now().Unix()
	// If MC token is still valid and not forced, avoid unnecessary upstream traffic
	if !force && bundle.MCAccessToken != "" && bundle.MCExpiresAt > now+int64(tokenFreshMargin.Seconds()) && AuthStatus(rec.AuthStatus) == AuthAuthorized {
		return m.GetAccount(id)
	}

	// Step A: Refresh Microsoft token ONLY if missing/expired or forced
	if force || bundle.MSAccessToken == "" || bundle.MSExpiresAt <= now+int64(msFreshMargin.Seconds()) {
		cid, _ := m.ResolveClientID()
		if cid == "" {
			return AccountView{}, ErrMissingClientID
		}
		if strings.TrimSpace(bundle.MSRefreshToken) == "" {
			return m.markReauthRequired(id, "Отсутствует refresh_token — требуется повторный вход")
		}
		msResp, err := RefreshMSToken(ctx, client, ep.MSTokenURL, cid, bundle.MSRefreshToken)
		if err != nil {
			if errors.Is(err, ErrInvalidGrant) {
				return m.markReauthRequired(id, "Сессия Microsoft отозвана или истекла — требуется повторный вход")
			}
			return m.markServiceError(id, err.Error())
		}
		expIn := msResp.ExpiresIn
		if expIn <= 0 {
			expIn = 3600
		}
		bundle.MSAccessToken = msResp.AccessToken
		if strings.TrimSpace(msResp.RefreshToken) != "" {
			bundle.MSRefreshToken = msResp.RefreshToken
		}
		bundle.MSExpiresAt = time.Now().Add(time.Duration(expIn) * time.Second).Unix()
	}

	// Step B: Re-run Xbox Live -> XSTS -> Minecraft -> Entitlements -> Profile
	view, err := m.completeXboxAndMinecraftChain(ctx, client, ep, bundle, rec.MicrosoftAccountID, id, nil)
	if err != nil {
		if errors.Is(err, ErrInvalidGrant) {
			return m.markReauthRequired(id, err.Error())
		}
		return m.markServiceError(id, err.Error())
	}
	return view, nil
}

// CheckAccountLicense re-checks Minecraft Java Edition entitlements & profile for an account.
func (m *Manager) CheckAccountLicense(ctx context.Context, id string) (AccountView, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// First ensure MC token is valid via synchronized refresh
	view, err := m.RefreshAccount(ctx, id, false)
	if err != nil {
		return view, err
	}

	unlock := m.lockAccountRefresh(id)
	defer unlock()

	m.mu.Lock()
	recPtr, ok := m.accounts[id]
	if !ok {
		m.mu.Unlock()
		return AccountView{}, ErrAccountNotFound
	}
	rec := *recPtr
	client := m.httpClient
	ep := m.endpoints
	recPtr.LicenseStatus = string(LicenseChecking)
	m.mu.Unlock()

	bundle, err := m.vault.Load(rec.VaultKey)
	if err != nil {
		return m.markReauthRequired(id, err.Error())
	}

	now := time.Now().Unix()
	licStatus, licErr := CheckMinecraftEntitlements(ctx, client, ep.MCEntitlements, bundle.MCAccessToken)
	if errors.Is(licErr, ErrInvalidGrant) {
		return m.markReauthRequired(id, "Токен Minecraft истёк — требуется повторный вход")
	}

	var prof mcProfileResult
	var profErr error
	if licStatus == LicenseOwned {
		prof, profErr = FetchMinecraftProfile(ctx, client, ep.MCProfileURL, bundle.MCAccessToken)
	}

	m.mu.Lock()
	cur, exists := m.accounts[id]
	if !exists {
		m.mu.Unlock()
		return AccountView{}, ErrAccountNotFound
	}
	cur.LicenseStatus = string(licStatus)
	cur.LastLicenseCheckAt = now
	cur.UpdatedAt = now
	if licErr != nil {
		cur.LastError = security.SanitizeLogText(licErr.Error())
		if licStatus == LicenseNetworkError || licStatus == LicenseServiceUnavailable {
			cur.AuthStatus = string(AuthServiceError)
		}
	} else {
		cur.AuthStatus = string(AuthAuthorized)
		cur.LastError = ""
		if profErr == nil && prof.UUID != "" {
			cur.MinecraftUUID = prof.UUID
			cur.MinecraftUsername = prof.Username
			cur.ProfileName = prof.ProfileName
			cur.SkinURL = prof.SkinURL
			cur.SkinVariant = prof.SkinVariant
			cur.CapeURL = prof.CapeURL
			cur.CapeAlias = prof.CapeAlias
			cur.ProfileUpdatedAt = now
		} else if errors.Is(profErr, ErrMCProfileNotCreated) {
			cur.LastError = ErrMCProfileNotCreated.Error()
		}
	}
	if m.store != nil {
		_ = m.store.UpsertAccount(*cur)
	}
	out := m.recordToView(*cur)
	m.mu.Unlock()

	if m.bus != nil {
		m.bus.Publish(events.EventAuthLicenseChecked, AuthFlowProgress{
			AccountID: id,
			Stage:     "license_checked",
			Message:   describeLicenseStatus(licStatus, licErr),
			UpdatedAt: now,
		})
		m.bus.Publish(events.EventAccountUpdated, out)
	}
	return out, licErr
}

// SetActive switches the active account cleanly without deleting or mixing tokens of other accounts.
func (m *Manager) SetActive(id string) (AccountView, error) {
	m.mu.Lock()
	target, ok := m.accounts[id]
	if !ok {
		m.mu.Unlock()
		return AccountView{}, ErrAccountNotFound
	}
	for _, rec := range m.accounts {
		rec.Active = (rec.ID == id)
	}
	target.UpdatedAt = time.Now().Unix()
	if m.store != nil {
		if err := m.store.SetActiveAccount(id); err != nil {
			m.mu.Unlock()
			return AccountView{}, err
		}
	}
	view := m.recordToView(*target)
	m.mu.Unlock()

	if m.bus != nil {
		m.bus.Publish(events.EventAccountActiveChanged, map[string]any{"account_id": view.ID})
		m.bus.Publish(events.EventAccountUpdated, view)
		if view.AuthStatus == AuthReauthRequired {
			m.bus.Publish(events.EventAuthReauthRequired, AuthFlowProgress{
				AccountID: view.ID,
				Stage:     "reauth_required",
				Message:   "Активный аккаунт требует повторного входа Microsoft",
				UpdatedAt: time.Now().Unix(),
			})
		}
	}
	return view, nil
}

// SignOut ends the local ModHub session for an account and wipes its encrypted tokens from the vault,
// keeping the account entry visible as unauthorized for quick re-login.
func (m *Manager) SignOut(id string) (AccountView, error) {
	unlock := m.lockAccountRefresh(id)
	defer unlock()

	m.mu.Lock()
	rec, ok := m.accounts[id]
	if !ok {
		m.mu.Unlock()
		return AccountView{}, ErrAccountNotFound
	}
	if rec.VaultKey != "" {
		_ = m.vault.Delete(rec.VaultKey)
	}
	now := time.Now().Unix()
	rec.AuthStatus = string(AuthUnauthorized)
	rec.MSExpiresAt = 0
	rec.XBLExpiresAt = 0
	rec.XSTSExpiresAt = 0
	rec.MCExpiresAt = 0
	rec.LastError = "Выполнен выход из сессии ModHub"
	rec.UpdatedAt = now
	if m.store != nil {
		_ = m.store.UpsertAccount(*rec)
	}
	view := m.recordToView(*rec)
	m.mu.Unlock()

	if m.bus != nil {
		m.bus.Publish(events.EventAccountUpdated, view)
	}
	return view, nil
}

// DeleteAccount completely removes the account record from ModHub and deletes its encrypted vault tokens.
// Never deletes game instances, Minecraft files, saves, other accounts, or the user's Microsoft Account itself.
func (m *Manager) DeleteAccount(id string) error {
	unlock := m.lockAccountRefresh(id)
	defer unlock()

	m.mu.Lock()
	rec, ok := m.accounts[id]
	if !ok {
		m.mu.Unlock()
		return ErrAccountNotFound
	}
	wasActive := rec.Active
	vaultKey := rec.VaultKey
	delete(m.accounts, id)
	delete(m.refreshLocks, id)

	if vaultKey != "" {
		_ = m.vault.Delete(vaultKey)
	}
	if m.store != nil {
		if err := m.store.DeleteAccount(id); err != nil {
			m.mu.Unlock()
			return err
		}
	}

	// Promote another remaining account to active if we removed the active one
	nextActiveID := ""
	if wasActive && len(m.accounts) > 0 {
		var newest *domain.AccountRecord
		for _, r := range m.accounts {
			if newest == nil || r.UpdatedAt > newest.UpdatedAt {
				newest = r
			}
		}
		if newest != nil {
			newest.Active = true
			nextActiveID = newest.ID
			if m.store != nil {
				_ = m.store.SetActiveAccount(newest.ID)
			}
		}
	}
	m.mu.Unlock()

	if m.bus != nil {
		m.bus.Publish(events.EventAccountRemoved, map[string]any{"account_id": id, "next_active_id": nextActiveID})
		if nextActiveID != "" {
			m.bus.Publish(events.EventAccountActiveChanged, map[string]any{"account_id": nextActiveID})
		}
	}
	return nil
}

// UpdateAccountSkin updates skin texture URL/base64, model variant ("CLASSIC" or "SLIM"), cape texture URL, and alias.
func (m *Manager) UpdateAccountSkin(id, skinURL, skinVariant, capeURL, capeAlias string) (AccountView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, exists := m.accounts[id]
	if !exists {
		return AccountView{}, fmt.Errorf("%w (id=%s)", ErrAccountNotFound, id)
	}

	variant := strings.ToUpper(strings.TrimSpace(skinVariant))
	if variant != "SLIM" {
		variant = "CLASSIC"
	}

	rec.SkinURL = strings.TrimSpace(skinURL)
	rec.SkinVariant = variant
	rec.CapeURL = strings.TrimSpace(capeURL)
	rec.CapeAlias = strings.TrimSpace(capeAlias)
	rec.UpdatedAt = time.Now().Unix()

	if m.store != nil {
		if err := m.store.UpsertAccount(*rec); err != nil {
			return AccountView{}, err
		}
	}

	view := m.recordToView(*rec)
	if m.bus != nil {
		m.bus.Publish(events.EventAccountUpdated, view)
	}
	return view, nil
}

// ResolveLaunchCredentials prepares verified Minecraft launch credentials for the future Game Launcher (Stage 8+).
// Enforces confirmed Java Edition license, valid UUID/username, automatic synchronized token refresh,
// and keeps tokens strictly in Go memory (never exposed to React, SQLite, logs, or instance files).
func (m *Manager) ResolveLaunchCredentials(ctx context.Context, preferredAccountID string) (LaunchCredentials, error) {
	pref := strings.TrimSpace(preferredAccountID)
	m.mu.Lock()
	activeID := ""
	if pref != "" {
		if _, ok := m.accounts[pref]; ok {
			activeID = pref
		} else {
			m.mu.Unlock()
			return LaunchCredentials{}, fmt.Errorf("%w (id=%s)", ErrAccountNotFound, pref)
		}
	} else {
		for _, r := range m.accounts {
			if r.Active {
				activeID = r.ID
				break
			}
		}
	}

	if activeID == "" {
		m.mu.Unlock()
		return LaunchCredentials{}, ErrNoActiveAccount
	}

	recPtr, ok := m.accounts[activeID]
	if !ok {
		m.mu.Unlock()
		return LaunchCredentials{}, ErrAccountNotFound
	}
	rec := *recPtr
	m.mu.Unlock()

	// Fast-path: Offline account requires zero upstream network and zero tokens
	if rec.AccountType == AccountTypeOffline || rec.MicrosoftAccountID == "offline" || strings.HasPrefix(rec.ID, "offline_") {
		return LaunchCredentials{
			AccountID:            rec.ID,
			AccountType:          AccountTypeOffline,
			UserType:             "mojang",
			MinecraftUUID:        rec.MinecraftUUID,
			FormattedUUID:        FormatDashedUUID(rec.MinecraftUUID),
			MinecraftUsername:    rec.MinecraftUsername,
			MinecraftAccessToken: "0",
			XboxUserID:           "0",
			LicenseStatus:        LicenseOwned,
		}, nil
	}

	view, err := m.RefreshAccount(ctx, activeID, false)
	if err != nil {
		// Offline fallback for previously confirmed Microsoft accounts when network is down
		if rec.MinecraftUsername != "" && rec.MinecraftUUID != "" && rec.LicenseStatus == string(LicenseOwned) {
			return LaunchCredentials{
				AccountID:            rec.ID,
				AccountType:          AccountTypeMicrosoft,
				UserType:             "msa",
				MinecraftUUID:        rec.MinecraftUUID,
				FormattedUUID:        FormatDashedUUID(rec.MinecraftUUID),
				MinecraftUsername:    rec.MinecraftUsername,
				MinecraftAccessToken: "0",
				XboxUserID:           rec.XboxUserID,
				LicenseStatus:        LicenseOwned,
			}, nil
		}
		return LaunchCredentials{}, err
	}
	if view.AuthStatus != AuthAuthorized {
		return LaunchCredentials{}, ErrReauthRequired
	}
	if view.LicenseStatus != LicenseOwned {
		return LaunchCredentials{}, ErrLicenseNotConfirmed
	}
	if view.MinecraftUUID == "" || view.MinecraftUsername == "" {
		return LaunchCredentials{}, ErrMCProfileNotCreated
	}

	m.mu.Lock()
	recLatest, ok := m.accounts[activeID]
	if !ok {
		m.mu.Unlock()
		return LaunchCredentials{}, ErrAccountNotFound
	}
	vaultKey := recLatest.VaultKey
	m.mu.Unlock()

	bundle, err := m.vault.Load(vaultKey)
	if err != nil || strings.TrimSpace(bundle.MCAccessToken) == "" {
		return LaunchCredentials{}, ErrReauthRequired
	}

	return LaunchCredentials{
		AccountID:            view.ID,
		AccountType:          AccountTypeMicrosoft,
		UserType:             "msa",
		MinecraftUUID:        view.MinecraftUUID,
		FormattedUUID:        view.FormattedUUID,
		MinecraftUsername:    view.MinecraftUsername,
		XboxUserID:           view.XboxUserID,
		LicenseStatus:        view.LicenseStatus,
		ExpiresAt:            bundle.MCExpiresAt,
		MinecraftAccessToken: bundle.MCAccessToken,
	}, nil
}

func (m *Manager) markReauthRequired(id, reason string) (AccountView, error) {
	safe := security.SanitizeLogText(reason)
	m.mu.Lock()
	rec, ok := m.accounts[id]
	if !ok {
		m.mu.Unlock()
		return AccountView{}, ErrAccountNotFound
	}
	now := time.Now().Unix()
	rec.AuthStatus = string(AuthReauthRequired)
	rec.LastError = safe
	rec.UpdatedAt = now
	if m.store != nil {
		_ = m.store.UpsertAccount(*rec)
	}
	view := m.recordToView(*rec)
	m.mu.Unlock()

	if m.bus != nil {
		m.bus.Publish(events.EventAuthReauthRequired, AuthFlowProgress{
			AccountID: id,
			Stage:     "reauth_required",
			Message:   safe,
			Error:     safe,
			UpdatedAt: now,
		})
		m.bus.Publish(events.EventAccountUpdated, view)
	}
	return view, fmt.Errorf("%w: %s", ErrReauthRequired, safe)
}

func (m *Manager) markServiceError(id, reason string) (AccountView, error) {
	safe := security.SanitizeLogText(reason)
	m.mu.Lock()
	rec, ok := m.accounts[id]
	if !ok {
		m.mu.Unlock()
		return AccountView{}, ErrAccountNotFound
	}
	now := time.Now().Unix()
	rec.AuthStatus = string(AuthServiceError)
	rec.LastError = safe
	rec.UpdatedAt = now
	if m.store != nil {
		_ = m.store.UpsertAccount(*rec)
	}
	view := m.recordToView(*rec)
	m.mu.Unlock()

	if m.bus != nil {
		m.bus.Publish(events.EventAccountUpdated, view)
	}
	return view, errors.New(safe)
}

func (m *Manager) recordToView(rec domain.AccountRecord) AccountView {
	acctType := rec.AccountType
	if acctType == "" {
		if rec.MicrosoftAccountID == "offline" || strings.HasPrefix(rec.ID, "offline_") {
			acctType = AccountTypeOffline
		} else {
			acctType = AccountTypeMicrosoft
		}
	}
	return AccountView{
		ID:                 rec.ID,
		AccountType:        acctType,
		MicrosoftAccountID: rec.MicrosoftAccountID,
		XboxUserID:         rec.XboxUserID,
		XboxGamertag:       rec.XboxGamertag,
		MinecraftUUID:      rec.MinecraftUUID,
		FormattedUUID:      FormatDashedUUID(rec.MinecraftUUID),
		MinecraftUsername:  rec.MinecraftUsername,
		ProfileName:        rec.ProfileName,
		SkinURL:            rec.SkinURL,
		SkinVariant:        rec.SkinVariant,
		CapeURL:            rec.CapeURL,
		CapeAlias:          rec.CapeAlias,
		AuthStatus:         AuthStatus(rec.AuthStatus),
		LicenseStatus:      LicenseStatus(rec.LicenseStatus),
		ProfileCreated:     rec.MinecraftUUID != "" && rec.MinecraftUsername != "",
		LastAuthAt:         rec.LastAuthAt,
		LastLicenseCheckAt: rec.LastLicenseCheckAt,
		ProfileUpdatedAt:   rec.ProfileUpdatedAt,
		MSExpiresAt:        rec.MSExpiresAt,
		XBLExpiresAt:       rec.XBLExpiresAt,
		XSTSExpiresAt:      rec.XSTSExpiresAt,
		MCExpiresAt:        rec.MCExpiresAt,
		HasVaultSecret:     rec.VaultKey != "" && m.vault.Exists(rec.VaultKey),
		LastError:          rec.LastError,
		Active:             rec.Active,
		CreatedAt:          rec.CreatedAt,
		UpdatedAt:          rec.UpdatedAt,
	}
}

func describeLicenseStatus(st LicenseStatus, err error) string {
	switch st {
	case LicenseOwned:
		return "Лицензия Minecraft Java Edition подтверждена"
	case LicenseNotOwned:
		return "На этом аккаунте отсутствует лицензия Minecraft Java Edition"
	case LicenseNetworkError:
		return "Не удалось проверить лицензию из-за ошибки сети"
	case LicenseServiceUnavailable:
		return "Сервис проверки лицензий Minecraft временно недоступен"
	case LicenseInvalidResponse:
		if err != nil {
			return "Некорректный ответ сервиса лицензий: " + security.SanitizeLogText(err.Error())
		}
		return "Некорректный ответ сервиса лицензий"
	default:
		return "Лицензия не проверялась"
	}
}

func displayAccountName(v AccountView) string {
	if v.MinecraftUsername != "" {
		return v.MinecraftUsername
	}
	if v.XboxGamertag != "" {
		return v.XboxGamertag
	}
	return v.ID
}

// VaultDir exposes the vault directory path strictly for diagnostics/tests (never secrets).
func (m *Manager) VaultDir() string {
	return m.vault.Dir()
}

// CleanAvatarCacheDir removes any account-scoped cached personal data on deletion.
func (m *Manager) CleanAvatarCacheDir(cacheRoot, accountID string) {
	if cacheRoot == "" || accountID == "" {
		return
	}
	_ = os.Remove(filepath.Join(cacheRoot, "avatar_"+accountID+".png"))
}
