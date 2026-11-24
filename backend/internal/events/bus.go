package events

import (
	"sync"
	"sync/atomic"
	"time"
)

type EventName string

const (
	EventConfigUpdated      EventName = "config.updated"
	EventCacheCleared       EventName = "cache.cleared"
	EventIndexSyncStarted   EventName = "index.sync.started"
	EventIndexSyncProgress  EventName = "index.sync.progress"
	EventIndexSyncCompleted EventName = "index.sync.completed"
	EventOverridesUpdated   EventName = "overrides.updated"
	EventPacksUpdated       EventName = "packs.updated"

	// Download Manager events.
	EventDownloadCreated   EventName = "download.created"
	EventDownloadProgress  EventName = "download.progress"
	EventDownloadStatus    EventName = "download.status"
	EventDownloadPaused    EventName = "download.paused"
	EventDownloadResumed   EventName = "download.resumed"
	EventDownloadCompleted EventName = "download.completed"
	EventDownloadError     EventName = "download.error"
	EventDownloadCancelled EventName = "download.cancelled"
	EventDownloadRemoved   EventName = "download.removed"
	EventDownloadSummary   EventName = "download.summary"

	// Java Runtime Manager & reserved launcher events.
	EventJavaUpdated    EventName = "java.updated"
	EventInstallStatus    EventName = "install.status"
	EventInstallProgress  EventName = "install.progress"
	EventInstallCompleted EventName = "install.completed"
	EventInstallError     EventName = "install.error"
	EventInstallCancelled EventName = "install.cancelled"
	EventVersionsUpdated   EventName = "versions.updated"
	EventGameStatus        EventName = "game.status"
	EventGameLog           EventName = "game.log"
	EventGameExited        EventName = "game.exited"
	EventGameCrashDetected EventName = "game.crash_detected"
	EventInstanceCreated EventName = "instance.created"
	EventInstanceUpdated EventName = "instance.updated"
	EventInstanceDeleted EventName = "instance.deleted"
	EventInstanceStatus  EventName = "instance.status"

	// Microsoft Account & Minecraft Authentication events (zero secret tokens in payloads).
	EventAuthStarted          EventName = "auth.started"
	EventAuthBrowserOpened    EventName = "auth.browser_opened"
	EventAuthOAuthCompleted   EventName = "auth.oauth_completed"
	EventAuthXboxStarted      EventName = "auth.xbox_started"
	EventAuthXboxCompleted    EventName = "auth.xbox_completed"
	EventAuthMinecraftStarted EventName = "auth.minecraft_started"
	EventAuthLicenseChecked   EventName = "auth.license_checked"
	EventAuthProfileLoaded    EventName = "auth.profile_loaded"
	EventAuthError            EventName = "auth.error"
	EventAuthReauthRequired   EventName = "auth.reauth_required"
	EventAccountActiveChanged EventName = "account.active_changed"
	EventAccountUpdated       EventName = "account.updated"
	EventAccountRemoved       EventName = "account.removed"

	// Modpack Manager events.
	EventModpackImportProgress  EventName = "modpack.import.progress"
	EventModpackImportCompleted EventName = "modpack.import.completed"
	EventModpackImportError     EventName = "modpack.import.error"
	EventModpackUpdated         EventName = "modpack.updated"
	EventModpackDeleted         EventName = "modpack.deleted"
)

type Event struct {
	Name      EventName
	Timestamp time.Time
	Payload   any
}

type Handler func(Event)

type subscriber struct {
	id    uint64
	fn    Handler
	wild  bool
	event EventName
}

type Bus struct {
	mu     sync.RWMutex
	closed bool
	nextID atomic.Uint64
	subs   map[uint64]subscriber
	byName map[EventName]map[uint64]Handler
	wild   map[uint64]Handler
}

func NewBus() *Bus {
	return &Bus{
		subs:   make(map[uint64]subscriber),
		byName: make(map[EventName]map[uint64]Handler),
		wild:   make(map[uint64]Handler),
	}
}

func (b *Bus) Subscribe(name EventName, fn Handler) func() {
	if b == nil || fn == nil || name == "" {
		return func() {}
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return func() {}
	}
	id := b.nextID.Add(1)
	if b.byName[name] == nil {
		b.byName[name] = make(map[uint64]Handler)
	}
	b.byName[name][id] = fn
	b.subs[id] = subscriber{id: id, fn: fn, event: name}
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() { b.unsubscribe(id) })
	}
}

func (b *Bus) SubscribeAll(fn Handler) func() {
	if b == nil || fn == nil {
		return func() {}
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return func() {}
	}
	id := b.nextID.Add(1)
	b.wild[id] = fn
	b.subs[id] = subscriber{id: id, fn: fn, wild: true}
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() { b.unsubscribe(id) })
	}
}

func (b *Bus) unsubscribe(id uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sub, ok := b.subs[id]
	if !ok {
		return
	}
	delete(b.subs, id)
	if sub.wild {
		delete(b.wild, id)
	} else if m := b.byName[sub.event]; m != nil {
		delete(m, id)
		if len(m) == 0 {
			delete(b.byName, sub.event)
		}
	}
}

func (b *Bus) Publish(name EventName, payload any) {
	if b == nil || name == "" {
		return
	}
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return
	}
	m := b.byName[name]
	total := len(m) + len(b.wild)
	if total == 0 {
		b.mu.RUnlock()
		return
	}
	handlers := make([]Handler, 0, total)
	for _, h := range m {
		handlers = append(handlers, h)
	}
	for _, h := range b.wild {
		handlers = append(handlers, h)
	}
	b.mu.RUnlock()

	ev := Event{
		Name:      name,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	for _, h := range handlers {
		invokeSafe(h, ev)
	}
}

func invokeSafe(h Handler, ev Event) {
	defer func() { _ = recover() }()
	h(ev)
}

func (b *Bus) SubscriberCount() int {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}

func (b *Bus) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	clear(b.subs)
	clear(b.byName)
	clear(b.wild)
}
