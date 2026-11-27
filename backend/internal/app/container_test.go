package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"modhub/internal/domain"
	"modhub/internal/events"
	"modhub/internal/storage"
)

func TestContainerLifecycleConfigEventsAndCacheKeys(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.OpenDatabase(filepath.Join(dir, "app_test.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer db.Close()

	c := NewContainer(dir, db)
	defer c.Close()

	var cfgEvents, cacheEvents atomic.Int32
	c.Events.Subscribe(events.EventConfigUpdated, func(events.Event) { cfgEvents.Add(1) })
	c.Events.Subscribe(events.EventCacheCleared, func(events.Event) { cacheEvents.Add(1) })

	badProxy := "://not-a-valid-proxy"
	if _, err := c.UpdateConfig(ConfigUpdatePatch{ProxyURL: &badProxy}); err == nil {
		t.Fatal("expected invalid proxy URL to fail validation")
	}

	thresh := 82
	goodProxy := "direct"
	if _, err := c.UpdateConfig(ConfigUpdatePatch{MergeThreshold: &thresh, ProxyURL: &goodProxy}); err != nil {
		t.Fatalf("UpdateConfig failed: %v", err)
	}
	if c.GetConfig().MergeThreshold != 82 || cfgEvents.Load() != 1 {
		t.Fatalf("unexpected config state or event count: cfg=%+v events=%d", c.GetConfig(), cfgEvents.Load())
	}

	c.ClearAllCaches()
	if cacheEvents.Load() != 1 {
		t.Fatalf("expected 1 cache cleared event, got %d", cacheEvents.Load())
	}

	// Verify cache key isolation
	k1 := DependentsCacheKey(true, domain.DependentsRequest{Item: domain.ProjectCard{GroupID: "modrinth:aaa"}, ContentType: "mod", CFIndex: 0})
	k2 := DependentsCacheKey(true, domain.DependentsRequest{Item: domain.ProjectCard{GroupID: "modrinth:bbb"}, ContentType: "mod", CFIndex: 0})
	k3 := DependentsCacheKey(true, domain.DependentsRequest{Item: domain.ProjectCard{GroupID: "modrinth:aaa"}, ContentType: "resourcepack", CFIndex: 0})
	if k1 == k2 || k1 == k3 || k2 == k3 {
		t.Fatalf("DependentsCacheKey collision: %s / %s / %s", k1, k2, k3)
	}

	dt1 := DepTreeCacheKey(true, domain.DepTreeRequest{Platform: "modrinth", ProjectID: "aaa", MCVersion: "1.20.1", Loader: "forge"})
	dt2 := DepTreeCacheKey(true, domain.DepTreeRequest{Platform: "modrinth", ProjectID: "bbb", MCVersion: "1.20.1", Loader: "forge"})
	if dt1 == dt2 {
		t.Fatalf("DepTreeCacheKey collision: %s / %s", dt1, dt2)
	}
}

func TestContainerKeepsUnreadableCurseForgeCiphertextOnOtherEdits(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"curseforge_api_key":"dpapi:unreadable"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenDatabase(filepath.Join(dir, "app_test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := NewContainer(dir, db)
	defer c.Close()
	if c.GetConfig().CurseForgeAPIKey != "" {
		t.Fatal("unreadable ciphertext configured as API key")
	}
	if c.PublicConfigPayload()["curseforge_configured"] != false || c.PublicConfigPayload()["curseforge_key_unreadable"] != true {
		t.Fatal("unreadable key status incorrect")
	}
	threshold := 82
	if _, err := c.UpdateConfig(ConfigUpdatePatch{MergeThreshold: &threshold}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "dpapi:unreadable") {
		t.Fatal("unrelated edit lost encrypted key")
	}
	clear := ""
	if _, err := c.UpdateConfig(ConfigUpdatePatch{CurseForgeAPIKey: &clear}); err != nil {
		t.Fatal(err)
	}
	if c.PublicConfigPayload()["curseforge_key_unreadable"] != false {
		t.Fatal("explicit clear failed")
	}
}

func TestIndexStatusDistinguishesProgressCompletionAndProviderOutcomes(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.OpenDatabase(filepath.Join(dir, "app_test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := NewContainer(dir, db)
	defer c.Close()
	c.syncMu.Lock()
	c.syncProgress = 2
	c.syncTotal = 5
	c.lastCompleted = float64(time.Now().Unix())
	c.syncLastError = "Modrinth unavailable"
	c.syncProviders = map[string]string{"modrinth": "error", "curseforge": "not_configured"}
	c.syncMu.Unlock()
	status := c.IndexStatusPayload()
	if status["progress"] != 2 || status["total_steps"] != 5 || status["last_completed"] == float64(2) || status["last_error"] != "Modrinth unavailable" {
		t.Fatalf("invalid index status: %+v", status)
	}
	providers := status["provider_outcomes"].(map[string]string)
	providers["modrinth"] = "ok"
	if c.IndexStatusPayload()["provider_outcomes"].(map[string]string)["modrinth"] != "error" {
		t.Fatal("status leaked mutable provider map")
	}
}
