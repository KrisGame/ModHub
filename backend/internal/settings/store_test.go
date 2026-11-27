package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"modhub/internal/domain"
)

func TestConfigOverridesAndPacksPersistence(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"port":8099,"curseforge_api_key":"$2a$10$legacytestkeyvalue1234567890","merge_threshold":75,"proxy_url":"http://127.0.0.1:7890","auto_install_medium_deps":true}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	loaded := LoadAppConfig(dir)
	if loaded.Port != 8099 || loaded.MergeThreshold != 75 || !loaded.AutoInstallMediumDeps || loaded.CurseForgeAPIKey != "$2a$10$legacytestkeyvalue1234567890" {
		t.Fatalf("legacy config mismatch: %+v", loaded)
	}
	if err := SaveAppConfig(dir, loaded); err != nil {
		t.Fatalf("SaveAppConfig: %v", err)
	}
	reloaded := LoadAppConfig(dir)
	if reloaded.CurseForgeAPIKey != loaded.CurseForgeAPIKey {
		t.Fatalf("reloaded API key mismatch")
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(filepath.Join(dir, "config.json")); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("expected 0600 permissions, got err=%v mode=%v", err, info.Mode())
		}
	}

	ov := domain.OverridesData{Merges: [][]string{{"modrinth:a", "curseforge:1"}}, Splits: [][]string{{"modrinth:b", "curseforge:2"}}}
	if err := SaveOverrides(dir, ov); err != nil {
		t.Fatalf("SaveOverrides: %v", err)
	}
	if got := LoadOverrides(dir); len(got.Merges) != 1 || len(got.Splits) != 1 {
		t.Fatalf("LoadOverrides mismatch: %+v", got)
	}

	packs := []domain.SavedPack{{ID: "p1", Name: "Test", Items: []domain.CartItem{{Title: "Sodium", Folder: "mods", Filename: "sodium.jar"}}}}
	if err := SavePacksCompat(dir, packs); err != nil {
		t.Fatalf("SavePacksCompat: %v", err)
	}
	if got := LoadPacksCompat(dir); len(got) != 1 || got[0].ID != "p1" {
		t.Fatalf("LoadPacksCompat mismatch: %+v", got)
	}
}

func TestCurseForgeKeyEncryptedWithDataDir(t *testing.T) {
	dir := t.TempDir()
	key := "$2a$10$nonwindowssecretkeyvalue1234567890"
	if err := SaveAppConfig(dir, domain.AppConfig{CurseForgeAPIKey: key}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), key) {
		t.Fatal("config.json contains plaintext CurseForge key")
	}
	if got := LoadAppConfig(dir).CurseForgeAPIKey; got != key {
		t.Fatalf("decrypted key mismatch: %q", got)
	}
}

func TestSaveConfigRejectsUnrecoverableCiphertext(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"curseforge_api_key":"mh-aesgcm:broken"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var cfg domain.AppConfig
	rawConfig, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		t.Fatal(err)
	}
	if err := SaveAppConfig(dir, cfg); err == nil {
		t.Fatal("expected decryption error")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "mh-aesgcm:broken") {
		t.Fatal("failed save overwrote original ciphertext")
	}
}

func TestUnreadableCiphertextSurvivesUnrelatedEditAndExplicitReplacement(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	for _, ciphertext := range []string{"dpapi:broken", "mh-aesgcm:broken"} {
		if err := os.WriteFile(configPath, []byte(`{"curseforge_api_key":"`+ciphertext+`","merge_threshold":70}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := LoadAppConfig(dir)
		if cfg.CurseForgeAPIKey != "" || !HasUnreadableCFKey(dir) {
			t.Fatalf("unreadable secret exposed or not detected: %q", ciphertext)
		}
		cfg.MergeThreshold = 85
		if err := SaveAppConfig(dir, cfg); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		var saved domain.AppConfig
		if err := json.Unmarshal(raw, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.CurseForgeAPIKey != ciphertext || saved.MergeThreshold != 85 {
			t.Fatalf("unrelated edit lost ciphertext or threshold: %s", raw)
		}
		if err := SaveAppConfigReplacingKey(dir, cfg); err != nil {
			t.Fatal(err)
		}
		if HasUnreadableCFKey(dir) {
			t.Fatal("explicit clear retained old ciphertext")
		}
	}
}
