package accounts

import (
	"fmt"
	"strings"

	"modhub/internal/domain"
)

// DefaultClientID is the shared public Microsoft Entra client ID for ModHub Desktop.
// May also be overridden at build time via -ldflags "-X modhub/internal/accounts.DefaultClientID=..."
var DefaultClientID = "45024645-55b5-421d-ac73-bc5abb238378"

const (
	OfficialMSAuthorizeURL  = "https://login.microsoftonline.com/consumers/oauth2/v2.0/authorize"
	OfficialMSTokenURL      = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	OfficialMSLogoutURL     = "https://login.microsoftonline.com/consumers/oauth2/v2.0/logout"
	OfficialXBLAuthURL      = "https://user.auth.xboxlive.com/user/authenticate"
	OfficialXSTSAuthURL     = "https://xsts.auth.xboxlive.com/xsts/authorize"
	OfficialMCLoginXboxURL  = "https://api.minecraftservices.com/authentication/login_with_xbox"
	OfficialMCEntitlements  = "https://api.minecraftservices.com/entitlements/mcstore"
	OfficialMCProfileURL    = "https://api.minecraftservices.com/minecraft/profile"
	OfficialOAuthScope      = "XboxLive.signin offline_access"
)

const (
	AccountTypeMicrosoft = "microsoft"
	AccountTypeOffline   = "offline"
)

type AuthStatus string

const (
	AuthUnauthorized   AuthStatus = "unauthorized"
	AuthAuthenticating AuthStatus = "authenticating"
	AuthAuthorized     AuthStatus = "authorized"
	AuthReauthRequired AuthStatus = "reauth_required"
	AuthServiceError   AuthStatus = "service_error"
)

type LicenseStatus string

const (
	LicenseUnknown            LicenseStatus = "unknown"
	LicenseChecking           LicenseStatus = "checking"
	LicenseOwned              LicenseStatus = "owned"
	LicenseNotOwned           LicenseStatus = "not_owned"
	LicenseNetworkError       LicenseStatus = "network_error"
	LicenseServiceUnavailable LicenseStatus = "service_unavailable"
	LicenseInvalidResponse    LicenseStatus = "invalid_response"
)

// Endpoints holds official Microsoft / Xbox / Minecraft service URLs (overridable in unit tests only).
type Endpoints struct {
	MSAuthorizeURL string
	MSTokenURL     string
	XBLAuthURL     string
	XSTSAuthURL    string
	MCLoginXboxURL string
	MCEntitlements string
	MCProfileURL   string
}

func OfficialEndpoints() Endpoints {
	return Endpoints{
		MSAuthorizeURL: OfficialMSAuthorizeURL,
		MSTokenURL:     OfficialMSTokenURL,
		XBLAuthURL:     OfficialXBLAuthURL,
		XSTSAuthURL:    OfficialXSTSAuthURL,
		MCLoginXboxURL: OfficialMCLoginXboxURL,
		MCEntitlements: OfficialMCEntitlements,
		MCProfileURL:   OfficialMCProfileURL,
	}
}

// SecretBundle stores sensitive tokens EXCLUSIVELY inside the encrypted OS vault (never SQLite, never React, never logs).
type SecretBundle struct {
	MSAccessToken  string `json:"ms_access_token,omitempty"`
	MSRefreshToken string `json:"ms_refresh_token,omitempty"`
	MSExpiresAt    int64  `json:"ms_expires_at,omitempty"`

	XBLToken     string `json:"xbl_token,omitempty"`
	XBLUserHash  string `json:"xbl_user_hash,omitempty"`
	XBLExpiresAt int64  `json:"xbl_expires_at,omitempty"`

	XSTSToken     string `json:"xsts_token,omitempty"`
	XSTSUserHash  string `json:"xsts_user_hash,omitempty"`
	XSTSExpiresAt int64  `json:"xsts_expires_at,omitempty"`

	MCAccessToken string `json:"mc_access_token,omitempty"`
	MCExpiresAt   int64  `json:"mc_expires_at,omitempty"`
}

func (s SecretBundle) String() string   { return "[REDACTED_SECRET_BUNDLE]" }
func (s SecretBundle) GoString() string { return "[REDACTED_SECRET_BUNDLE]" }

// AccountView is the safe, token-free projection sent to React / Wails / HTTP API.
type AccountView struct {
	ID                 string        `json:"id"`
	AccountType        string        `json:"account_type"` // microsoft | offline
	MicrosoftAccountID string        `json:"microsoft_account_id,omitempty"`
	XboxUserID         string        `json:"xbox_user_id,omitempty"`
	XboxGamertag       string        `json:"xbox_gamertag,omitempty"`
	MinecraftUUID      string        `json:"minecraft_uuid,omitempty"`
	FormattedUUID      string        `json:"formatted_uuid,omitempty"`
	MinecraftUsername  string        `json:"minecraft_username,omitempty"`
	ProfileName        string        `json:"profile_name,omitempty"`
	SkinURL            string        `json:"skin_url,omitempty"`
	SkinVariant        string        `json:"skin_variant,omitempty"`
	CapeURL            string        `json:"cape_url,omitempty"`
	CapeAlias          string        `json:"cape_alias,omitempty"`
	AuthStatus         AuthStatus    `json:"auth_status"`
	LicenseStatus      LicenseStatus `json:"license_status"`
	ProfileCreated     bool          `json:"profile_created"`
	LastAuthAt         int64         `json:"last_auth_at"`
	LastLicenseCheckAt int64         `json:"last_license_check_at"`
	ProfileUpdatedAt   int64         `json:"profile_updated_at"`
	MSExpiresAt        int64         `json:"ms_expires_at"`
	XBLExpiresAt       int64         `json:"xbl_expires_at"`
	XSTSExpiresAt      int64         `json:"xsts_expires_at"`
	MCExpiresAt        int64         `json:"mc_expires_at"`
	HasVaultSecret     bool          `json:"has_vault_secret"`
	LastError          string        `json:"last_error,omitempty"`
	Active             bool          `json:"active"`
	CreatedAt          int64         `json:"created_at"`
	UpdatedAt          int64         `json:"updated_at"`
}

// AuthFlowProgress reports live, token-free Microsoft authentication stages to React.
type AuthFlowProgress struct {
	Active      bool   `json:"active"`
	AccountID   string `json:"account_id,omitempty"`
	Stage       string `json:"stage"` // idle | started | browser_opened | oauth_completed | xbox_started | xbox_completed | minecraft_started | license_checked | profile_loaded | completed | error | cancelled
	Message     string `json:"message"`
	Error       string `json:"error,omitempty"`
	RedirectURI string `json:"redirect_uri,omitempty"`
	AuthURL     string `json:"auth_url,omitempty"`
	UpdatedAt   int64  `json:"updated_at"`
}

// AuthConfigStatus informs the UI whether a real Microsoft Entra Client ID is configured.
type AuthConfigStatus struct {
	Configured   bool   `json:"configured"`
	Source       string `json:"source"` // env | config | build | none
	VaultBackend string `json:"vault_backend"`
	RedirectNote string `json:"redirect_note"`
	Diagnostic   string `json:"diagnostic,omitempty"`
}

// AccountsSnapshot is the top-level token-free state returned to UI.
type AccountsSnapshot struct {
	Accounts        []AccountView    `json:"accounts"`
	ActiveAccountID string           `json:"active_account_id,omitempty"`
	Config          AuthConfigStatus `json:"config"`
	Flow            AuthFlowProgress `json:"flow"`
}

// LaunchCredentials is an internal Go-only handoff structure for the future Game Launcher (Stage 8+).
// Secret tokens are tagged `json:"-"` and masked in String()/GoString() so they cannot leak to IPC, UI, or logs.
type LaunchCredentials struct {
	AccountID            string        `json:"account_id"`
	AccountType          string        `json:"account_type"` // microsoft | offline
	UserType             string        `json:"user_type"`    // msa | mojang
	MinecraftUUID        string        `json:"minecraft_uuid"`
	FormattedUUID        string        `json:"formatted_uuid"`
	MinecraftUsername    string        `json:"minecraft_username"`
	XboxUserID           string        `json:"xbox_user_id"`
	LicenseStatus        LicenseStatus `json:"license_status"`
	ExpiresAt            int64         `json:"expires_at"`
	MinecraftAccessToken string        `json:"-"`
}

func (c LaunchCredentials) String() string {
	return fmt.Sprintf("LaunchCredentials{account=%s, user=%s, uuid=%s, license=%s, token=[REDACTED]}", c.AccountID, c.MinecraftUsername, c.MinecraftUUID, c.LicenseStatus)
}

func (c LaunchCredentials) GoString() string {
	return c.String()
}

// FormatDashedUUID converts a 32-char hex Minecraft UUID into standard 8-4-4-4-12 format.
func FormatDashedUUID(raw string) string {
	s := strings.ReplaceAll(strings.TrimSpace(strings.ToLower(raw)), "-", "")
	if len(s) != 32 {
		return strings.TrimSpace(raw)
	}
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

// Store abstracts SQLite persistence for non-sensitive account metadata.
type Store interface {
	UpsertAccount(rec domain.AccountRecord) error
	ListAccounts() ([]domain.AccountRecord, error)
	SetActiveAccount(id string) error
	DeleteAccount(id string) error
}

// BrowserOpener launches an external URL in the user's system browser (never embedded WebView).
type BrowserOpener func(rawURL string) error
