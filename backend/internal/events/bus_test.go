package events

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestBusSubscribePublishUnsubscribe(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var gotA, gotB, gotAll atomic.Int32
	unsubA := bus.Subscribe(EventConfigUpdated, func(ev Event) {
		if ev.Name != EventConfigUpdated {
			t.Errorf("unexpected event name: %s", ev.Name)
		}
		gotA.Add(1)
	})
	bus.Subscribe(EventConfigUpdated, func(Event) { gotB.Add(1) })
	bus.SubscribeAll(func(Event) { gotAll.Add(1) })

	bus.Publish(EventConfigUpdated, map[string]any{"ok": true})
	bus.Publish(EventCacheCleared, nil)

	if gotA.Load() != 1 || gotB.Load() != 1 || gotAll.Load() != 2 {
		t.Fatalf("unexpected counts: A=%d B=%d All=%d", gotA.Load(), gotB.Load(), gotAll.Load())
	}

	unsubA()
	unsubA()
	bus.Publish(EventConfigUpdated, nil)
	if gotA.Load() != 1 || gotB.Load() != 2 {
		t.Fatalf("unsubscribe failed: A=%d B=%d", gotA.Load(), gotB.Load())
	}
}

func TestBusPanicIsolationAndConcurrency(t *testing.T) {
	bus := NewBus()
	var okCount atomic.Int32

	bus.Subscribe(EventDownloadProgress, func(Event) { panic("subscriber failure") })
	bus.Subscribe(EventDownloadProgress, func(Event) { okCount.Add(1) })

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unsub := bus.Subscribe(EventDownloadProgress, func(Event) {})
			bus.Publish(EventDownloadProgress, 42)
			unsub()
		}()
	}
	wg.Wait()

	if okCount.Load() != 16 {
		t.Fatalf("expected 16 deliveries despite panicking subscriber, got %d", okCount.Load())
	}
	bus.Close()
	if bus.SubscriberCount() != 0 {
		t.Fatalf("expected 0 subscribers after Close, got %d", bus.SubscriberCount())
	}
	bus.Publish(EventDownloadProgress, 99)
	if okCount.Load() != 16 {
		t.Fatalf("expected no deliveries after Close, got %d", okCount.Load())
	}
}
