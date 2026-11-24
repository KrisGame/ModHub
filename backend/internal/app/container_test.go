package app

import (
	"path/filepath"
	"sync/atomic"
	"testing"

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
