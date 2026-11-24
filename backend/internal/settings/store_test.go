package settings

import (
	"os"
	"path/filepath"
	"runtime"
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
