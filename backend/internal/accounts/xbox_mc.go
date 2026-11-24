package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"modhub/internal/domain"
	"modhub/internal/security"
)

var (
	ErrXBLAuthFailed         = errors.New("ошибка авторизации Xbox Live")
	ErrXSTSAuthFailed        = errors.New("ошибка авторизации XSTS")
	ErrMCAuthFailed          = errors.New("ошибка авторизации Minecraft Services")
	ErrMCProfileNotCreated   = errors.New("профиль Minecraft Java Edition ещё не создан для этого аккаунта")
)

type xboxTokenResult struct {
	Token     string
	UserHash  string
	XboxUID   string
	Gamertag  string
	ExpiresAt int64
}

type mcLoginResult struct {
	AccessToken string
	XboxUserID  string
	ExpiresAt   int64
}

type mcProfileResult struct {
	UUID        string
	Username    string
	ProfileName string
	SkinURL     string
	SkinVariant string
	CapeURL     string
	CapeAlias   string
}

type xstsErrorBody struct {
	Identity string `json:"Identity"`
	XErr     int64  `json:"XErr"`
	Message  string `json:"Message"`
	Redirect string `json:"Redirect"`
}

func describeXSTSError(xerr int64, msg string) string {
	switch xerr {
	case 2148916233:
		return "У этого аккаунта Microsoft нет профиля Xbox Live. Создайте профиль Xbox на xbox.com и повторите вход."
	case 2148916235:
		return "Служба Xbox Live недоступна в стране/регионе этого аккаунта."
	case 2148916236, 2148916237:
		return "Требуется подтверждение возраста или учётной записи на сайте Xbox (xbox.com)."
	case 2148916238:
		return "Этот аккаунт отмечен как детский (младше 18 лет) — взрослый должен добавить его в семейную группу Microsoft."
	default:
		if strings.TrimSpace(msg) != "" {
			return fmt.Sprintf("ошибка XSTS (код %d): %s", xerr, security.SanitizeLogText(msg))
		}
		if xerr != 0 {
			return fmt.Sprintf("ошибка XSTS (код %d)", xerr)
		}
		return "сервис XSTS отклонил авторизацию"
	}
}

func parseXboxTimestamp(ts string, fallbackTTL time.Duration) int64 {
	s := strings.TrimSpace(ts)
	if s != "" {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.Unix()
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.Unix()
		}
	}
	return time.Now().Add(fallbackTTL).Unix()
}

func postJSONRequest(ctx context.Context, client *http.Client, targetURL string, payload any, extraHeaders map[string]string) (int, []byte, error) {
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", domain.UserAgent)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

// AuthenticateXboxLive exchanges a Microsoft OAuth access token for an Xbox Live user token & UHS.
func AuthenticateXboxLive(ctx context.Context, client *http.Client, endpoint, msAccessToken string) (xboxTokenResult, error) {
	if strings.TrimSpace(msAccessToken) == "" {
		return xboxTokenResult{}, fmt.Errorf("%w: отсутствует Microsoft access token", ErrXBLAuthFailed)
	}
	reqBody := map[string]any{
		"Properties": map[string]any{
			"AuthMethod": "RPS",
			"SiteName":   "user.auth.xboxlive.com",
			"RpsTicket":  "d=" + msAccessToken,
		},
		"RelyingParty": "http://auth.xboxlive.com",
		"TokenType":    "JWT",
	}
	status, raw, err := postJSONRequest(ctx, client, endpoint, reqBody, map[string]string{"x-xbl-contract-version": "1"})
	if err != nil {
		return xboxTokenResult{}, fmt.Errorf("%w: сетевая ошибка (%s)", ErrXBLAuthFailed, security.SanitizeLogText(err.Error()))
	}
	if status < 200 || status >= 300 {
		var xerr xstsErrorBody
		_ = json.Unmarshal(raw, &xerr)
		if xerr.XErr != 0 || strings.TrimSpace(xerr.Message) != "" {
			return xboxTokenResult{}, fmt.Errorf("%w: %s", ErrXBLAuthFailed, describeXSTSError(xerr.XErr, xerr.Message))
		}
		return xboxTokenResult{}, fmt.Errorf("%w: Xbox Live вернул HTTP %d", ErrXBLAuthFailed, status)
	}
	var resp struct {
		IssueInstant  string `json:"IssueInstant"`
		NotAfter      string `json:"NotAfter"`
		Token         string `json:"Token"`
		DisplayClaims struct {
			XUI []struct {
				UHS string `json:"uhs"`
				XID string `json:"xid"`
				GT  string `json:"gtg"`
			} `json:"xui"`
		} `json:"DisplayClaims"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return xboxTokenResult{}, fmt.Errorf("%w: некорректный JSON ответа Xbox Live", ErrXBLAuthFailed)
	}
	if strings.TrimSpace(resp.Token) == "" || len(resp.DisplayClaims.XUI) == 0 || strings.TrimSpace(resp.DisplayClaims.XUI[0].UHS) == "" {
		return xboxTokenResult{}, fmt.Errorf("%w: в ответе Xbox Live отсутствуют Token или uhs", ErrXBLAuthFailed)
	}
	return xboxTokenResult{
		Token:     resp.Token,
		UserHash:  resp.DisplayClaims.XUI[0].UHS,
		XboxUID:   resp.DisplayClaims.XUI[0].XID,
		Gamertag:  resp.DisplayClaims.XUI[0].GT,
		ExpiresAt: parseXboxTimestamp(resp.NotAfter, 12*time.Hour),
	}, nil
}

// AuthorizeXSTS exchanges an Xbox Live token for a Minecraft-scoped XSTS token.
func AuthorizeXSTS(ctx context.Context, client *http.Client, endpoint, xblToken string) (xboxTokenResult, error) {
	if strings.TrimSpace(xblToken) == "" {
		return xboxTokenResult{}, fmt.Errorf("%w: отсутствует Xbox Live token", ErrXSTSAuthFailed)
	}
	reqBody := map[string]any{
		"Properties": map[string]any{
			"SandboxId":  "RETAIL",
			"UserTokens": []string{xblToken},
		},
		"RelyingParty": "rp://api.minecraftservices.com/",
		"TokenType":    "JWT",
	}
	status, raw, err := postJSONRequest(ctx, client, endpoint, reqBody, map[string]string{"x-xbl-contract-version": "1"})
	if err != nil {
		return xboxTokenResult{}, fmt.Errorf("%w: сетевая ошибка (%s)", ErrXSTSAuthFailed, security.SanitizeLogText(err.Error()))
	}
	if status == http.StatusUnauthorized || status < 200 || status >= 300 {
		var xerr xstsErrorBody
		_ = json.Unmarshal(raw, &xerr)
		return xboxTokenResult{}, fmt.Errorf("%w: %s", ErrXSTSAuthFailed, describeXSTSError(xerr.XErr, xerr.Message))
	}
	var resp struct {
		NotAfter      string `json:"NotAfter"`
		Token         string `json:"Token"`
		DisplayClaims struct {
			XUI []struct {
				UHS string `json:"uhs"`
				XID string `json:"xid"`
				GT  string `json:"gtg"`
			} `json:"xui"`
		} `json:"DisplayClaims"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return xboxTokenResult{}, fmt.Errorf("%w: некорректный JSON ответа XSTS", ErrXSTSAuthFailed)
	}
	if strings.TrimSpace(resp.Token) == "" || len(resp.DisplayClaims.XUI) == 0 || strings.TrimSpace(resp.DisplayClaims.XUI[0].UHS) == "" {
		return xboxTokenResult{}, fmt.Errorf("%w: в ответе XSTS отсутствуют Token или uhs", ErrXSTSAuthFailed)
	}
	return xboxTokenResult{
		Token:     resp.Token,
		UserHash:  resp.DisplayClaims.XUI[0].UHS,
		XboxUID:   resp.DisplayClaims.XUI[0].XID,
		Gamertag:  resp.DisplayClaims.XUI[0].GT,
		ExpiresAt: parseXboxTimestamp(resp.NotAfter, 12*time.Hour),
	}, nil
}

// AuthenticateMinecraft exchanges XSTS (uhs + token) for a Minecraft Services JWT access token.
func AuthenticateMinecraft(ctx context.Context, client *http.Client, endpoint, userHash, xstsToken string) (mcLoginResult, error) {
	if strings.TrimSpace(userHash) == "" || strings.TrimSpace(xstsToken) == "" {
		return mcLoginResult{}, fmt.Errorf("%w: отсутствуют данные XSTS", ErrMCAuthFailed)
	}
	reqBody := map[string]any{
		"identityToken": fmt.Sprintf("XBL3.0 x=%s;%s", userHash, xstsToken),
	}
	status, raw, err := postJSONRequest(ctx, client, endpoint, reqBody, nil)
	if err != nil {
		return mcLoginResult{}, fmt.Errorf("%w: сетевая ошибка (%s)", ErrMCAuthFailed, security.SanitizeLogText(err.Error()))
	}
	if status < 200 || status >= 300 {
		var mcErr struct {
			Error        string `json:"error"`
			ErrorMessage string `json:"errorMessage"`
			DeveloperMsg string `json:"developerMessage"`
		}
		_ = json.Unmarshal(raw, &mcErr)
		msg := strings.TrimSpace(mcErr.ErrorMessage)
		if msg == "" {
			msg = strings.TrimSpace(mcErr.Error)
		}
		low := strings.ToLower(msg + " " + mcErr.DeveloperMsg)
		if status == http.StatusForbidden || strings.Contains(low, "invalid app registration") {
			return mcLoginResult{}, fmt.Errorf("%w: Minecraft Services отклонил Client ID (HTTP %d — Invalid app registration). Указанный Microsoft Entra Client ID не одобрен Mojang для доступа к API Minecraft.", ErrMCAuthFailed, status)
		}
		if msg != "" {
			return mcLoginResult{}, fmt.Errorf("%w: Minecraft Services вернул HTTP %d (%s)", ErrMCAuthFailed, status, security.SanitizeLogText(msg))
		}
		return mcLoginResult{}, fmt.Errorf("%w: Minecraft Services вернул HTTP %d", ErrMCAuthFailed, status)
	}
	var resp struct {
		Username    string `json:"username"`
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return mcLoginResult{}, fmt.Errorf("%w: некорректный JSON ответа Minecraft Services", ErrMCAuthFailed)
	}
	if strings.TrimSpace(resp.AccessToken) == "" {
		return mcLoginResult{}, fmt.Errorf("%w: отсутствует access_token", ErrMCAuthFailed)
	}
	expIn := resp.ExpiresIn
	if expIn <= 0 {
		expIn = 86400
	}
	return mcLoginResult{
		AccessToken: resp.AccessToken,
		XboxUserID:  strings.TrimSpace(resp.Username),
		ExpiresAt:   time.Now().Add(time.Duration(expIn) * time.Second).Unix(),
	}, nil
}

// CheckMinecraftEntitlements verifies Minecraft Java Edition ownership via /entitlements/mcstore.
// Strictly separates network errors, temporary service outages, invalid responses, and actual lack of license!
func CheckMinecraftEntitlements(ctx context.Context, client *http.Client, endpoint, mcAccessToken string) (LicenseStatus, error) {
	if strings.TrimSpace(mcAccessToken) == "" {
		return LicenseInvalidResponse, errors.New("отсутствует токен Minecraft Services")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return LicenseNetworkError, err
	}
	req.Header.Set("Authorization", "Bearer "+mcAccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", domain.UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return LicenseNetworkError, fmt.Errorf("сетевая ошибка при проверке лицензии Minecraft: %s", security.SanitizeLogText(err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return LicenseServiceUnavailable, fmt.Errorf("сервис проверки лицензии Minecraft временно недоступен (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return LicenseInvalidResponse, fmt.Errorf("%w: токен Minecraft отклонён при проверке лицензии (HTTP %d)", ErrInvalidGrant, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return LicenseInvalidResponse, fmt.Errorf("неожиданный ответ сервиса лицензий (HTTP %d)", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return LicenseNetworkError, fmt.Errorf("ошибка чтения ответа сервиса лицензий: %v", err)
	}
	var body struct {
		Items []struct {
			Name      string `json:"name"`
			Signature string `json:"signature"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return LicenseInvalidResponse, errors.New("некорректный JSON ответа сервиса лицензий Minecraft")
	}

	for _, item := range body.Items {
		name := strings.ToLower(strings.TrimSpace(item.Name))
		if name == "product_minecraft" || name == "game_minecraft" {
			return LicenseOwned, nil
		}
	}
	return LicenseNotOwned, nil
}

// FetchMinecraftProfile retrieves UUID, username, active skin, and active cape from /minecraft/profile.
func FetchMinecraftProfile(ctx context.Context, client *http.Client, endpoint, mcAccessToken string) (mcProfileResult, error) {
	if strings.TrimSpace(mcAccessToken) == "" {
		return mcProfileResult{}, errors.New("отсутствует токен Minecraft Services")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return mcProfileResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+mcAccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", domain.UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return mcProfileResult{}, fmt.Errorf("сетевая ошибка при получении профиля Minecraft: %s", security.SanitizeLogText(err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return mcProfileResult{}, ErrMCProfileNotCreated
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return mcProfileResult{}, fmt.Errorf("%w: токен Minecraft отклонён при запросе профиля (HTTP %d)", ErrInvalidGrant, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mcProfileResult{}, fmt.Errorf("Minecraft Profile API вернул HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return mcProfileResult{}, err
	}
	var prof struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Skins []struct {
			ID      string `json:"id"`
			State   string `json:"state"`
			URL     string `json:"url"`
			Variant string `json:"variant"`
		} `json:"skins"`
		Capes []struct {
			ID    string `json:"id"`
			State string `json:"state"`
			URL   string `json:"url"`
			Alias string `json:"alias"`
		} `json:"capes"`
	}
	if err := json.Unmarshal(raw, &prof); err != nil {
		return mcProfileResult{}, errors.New("некорректный JSON профиля Minecraft")
	}
	if strings.TrimSpace(prof.ID) == "" || strings.TrimSpace(prof.Name) == "" {
		return mcProfileResult{}, ErrMCProfileNotCreated
	}

	out := mcProfileResult{
		UUID:        strings.ReplaceAll(strings.TrimSpace(strings.ToLower(prof.ID)), "-", ""),
		Username:    strings.TrimSpace(prof.Name),
		ProfileName: strings.TrimSpace(prof.Name),
	}
	for _, sk := range prof.Skins {
		if strings.EqualFold(sk.State, "ACTIVE") || out.SkinURL == "" {
			out.SkinURL = strings.TrimSpace(sk.URL)
			out.SkinVariant = strings.TrimSpace(sk.Variant)
			if strings.EqualFold(sk.State, "ACTIVE") {
				break
			}
		}
	}
	for _, cp := range prof.Capes {
		if strings.EqualFold(cp.State, "ACTIVE") {
			out.CapeURL = strings.TrimSpace(cp.URL)
			out.CapeAlias = strings.TrimSpace(cp.Alias)
			break
		}
	}
	return out, nil
}
