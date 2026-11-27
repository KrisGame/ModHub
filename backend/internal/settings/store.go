package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"modhub/internal/domain"
	"modhub/internal/netx"
	"modhub/internal/security"
	"modhub/internal/storage"
)

func DefaultAppConfig() domain.AppConfig {
	return domain.AppConfig{Port: 8080, MergeThreshold: 70, MaxConcurrentDownloads: 4}
}

func LoadAppConfig(dataDir string) domain.AppConfig {
	cfg := DefaultAppConfig()
	raw, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if err == nil {
		_ = json.Unmarshal(raw, &cfg)
	}
	if cfg.Port <= 0 {
		cfg.Port = 8080
	}
	if cfg.MergeThreshold < 40 || cfg.MergeThreshold > 95 {
		cfg.MergeThreshold = 70
	}
	if cfg.MaxConcurrentDownloads < 1 || cfg.MaxConcurrentDownloads > 32 {
		cfg.MaxConcurrentDownloads = 4
	}
	cfg.CurseForgeAPIKey = usableCFKey(dataDir, cfg.CurseForgeAPIKey)
	cfg.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
	cfg.MicrosoftClientID = strings.TrimSpace(cfg.MicrosoftClientID)
	return cfg
}

func SaveAppConfig(dataDir string, cfg domain.AppConfig) error {
	return saveAppConfig(dataDir, cfg, false)
}

// SaveAppConfigReplacingKey permits an explicit replacement (including clearing a
// broken encrypted key). Other settings edits retain unreadable ciphertext.
func SaveAppConfigReplacingKey(dataDir string, cfg domain.AppConfig) error {
	return saveAppConfig(dataDir, cfg, true)
}

func encryptedCFKey(stored string) bool {
	return strings.HasPrefix(stored, "dpapi:") || strings.HasPrefix(stored, "mh-aesgcm:")
}

func usableCFKey(dataDir, stored string) string {
	decoded := security.UnprotectSecretWithDir(dataDir, stored)
	if encryptedCFKey(stored) && (decoded == "" || decoded == stored || encryptedCFKey(decoded)) {
		return ""
	}
	return security.SanitizeCFKey(decoded)
}

// HasUnreadableCFKey never exposes the stored secret to callers.
func HasUnreadableCFKey(dataDir string) bool {
	raw, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if err != nil {
		return false
	}
	var stored struct {
		CurseForgeAPIKey string `json:"curseforge_api_key"`
	}
	return json.Unmarshal(raw, &stored) == nil && encryptedCFKey(stored.CurseForgeAPIKey) && usableCFKey(dataDir, stored.CurseForgeAPIKey) == ""
}

func saveAppConfig(dataDir string, cfg domain.AppConfig, replacingKey bool) error {
	toStore := cfg
	cleanKey := usableCFKey(dataDir, cfg.CurseForgeAPIKey)
	if strings.TrimSpace(cfg.CurseForgeAPIKey) != "" && cleanKey == "" {
		return errors.New("не удалось расшифровать CurseForge API key; исходный config сохранён без изменений")
	}
	if !replacingKey && cleanKey == "" && HasUnreadableCFKey(dataDir) {
		raw, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
		if err != nil {
			return err
		}
		var stored struct {
			CurseForgeAPIKey string `json:"curseforge_api_key"`
		}
		if err := json.Unmarshal(raw, &stored); err != nil {
			return err
		}
		toStore.CurseForgeAPIKey = stored.CurseForgeAPIKey
	} else if cleanKey != "" {
		protected, err := protectConfigSecret(dataDir, cleanKey)
		if err != nil {
			return err
		}
		toStore.CurseForgeAPIKey = protected
	} else {
		toStore.CurseForgeAPIKey = ""
	}
	toStore.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
	toStore.MicrosoftClientID = strings.TrimSpace(cfg.MicrosoftClientID)
	return storage.WriteJSONFileAtomic(dataDir, "config.json", toStore, 0o600)
}

func protectConfigSecret(dataDir, cleanKey string) (string, error) {
	// MODHUB_DATA_DIR is already the canonical data-dir override in all modes.
	return security.ProtectSecretWithDir(dataDir, cleanKey)
}

func ToPublicConfig(cfg domain.AppConfig) domain.PublicConfig {
	clean := security.SanitizeCFKey(cfg.CurseForgeAPIKey)
	return domain.PublicConfig{
		CurseForgeConfigured:   clean != "",
		CurseForgeKeyMasked:    security.MaskCFKey(clean),
		MergeThreshold:         cfg.MergeThreshold,
		ProxyURL:               strings.TrimSpace(cfg.ProxyURL),
		ProxyDetected:          netx.GetProxyStatus(),
		AutoInstallMediumDeps:  cfg.AutoInstallMediumDeps,
		MaxConcurrentDownloads: cfg.MaxConcurrentDownloads,
		MicrosoftConfigured:    strings.TrimSpace(cfg.MicrosoftClientID) != "",
		MicrosoftClientID:      strings.TrimSpace(cfg.MicrosoftClientID),
	}
}

func LoadOverrides(dataDir string) domain.OverridesData {
	ov := domain.OverridesData{Merges: [][]string{}, Splits: [][]string{}}
	raw, err := os.ReadFile(filepath.Join(dataDir, "overrides.json"))
	if err == nil {
		_ = json.Unmarshal(raw, &ov)
	}
	if ov.Merges == nil {
		ov.Merges = [][]string{}
	}
	if ov.Splits == nil {
		ov.Splits = [][]string{}
	}
	return ov
}

func SaveOverrides(dataDir string, ov domain.OverridesData) error {
	return storage.WriteJSONFileAtomic(dataDir, "overrides.json", ov, 0o600)
}

func LoadPacksCompat(dataDir string) []domain.SavedPack {
	raw, err := os.ReadFile(filepath.Join(dataDir, "packs.json"))
	if err != nil {
		return []domain.SavedPack{}
	}
	var wrap struct {
		Packs []domain.SavedPack `json:"packs"`
	}
	if json.Unmarshal(raw, &wrap) == nil && wrap.Packs != nil {
		return wrap.Packs
	}
	var direct []domain.SavedPack
	if json.Unmarshal(raw, &direct) == nil && direct != nil {
		return direct
	}
	return []domain.SavedPack{}
}

func SavePacksCompat(dataDir string, packs []domain.SavedPack) error {
	return storage.WriteJSONFileAtomic(dataDir, "packs.json", map[string]any{"packs": packs}, 0o600)
}
