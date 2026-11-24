package deps

import (
	"fmt"
	"log"
	"sync"

	"modhub/internal/domain"
	"modhub/internal/security"
)

type DepTrace struct {
	mu    sync.Mutex
	scope string
	lines []string
	items []domain.DepInstallItemStatus
}

func NewDepTrace(scope string) *DepTrace {
	return &DepTrace{
		scope: security.SanitizeLogText(scope),
		lines: make([]string, 0, 16),
		items: make([]domain.DepInstallItemStatus, 0, 8),
	}
}

func (t *DepTrace) Logf(format string, args ...any) {
	if t == nil {
		return
	}
	msg := security.SanitizeLogText(fmt.Sprintf(format, args...))
	t.mu.Lock()
	if len(t.lines) < 120 {
		t.lines = append(t.lines, msg)
	}
	t.mu.Unlock()
	log.Printf("[ModHub][Deps][%s] %s", t.scope, msg)
}

func (t *DepTrace) RecordItem(it domain.DepInstallItemStatus) {
	if t == nil {
		return
	}
	it.Reason = security.SanitizeLogText(it.Reason)
	it.DownloadURL = security.SanitizeLogText(it.DownloadURL)
	t.mu.Lock()
	t.items = append(t.items, it)
	t.mu.Unlock()
}

func (t *DepTrace) Lines() []string {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.lines...)
}

func (t *DepTrace) Items() []domain.DepInstallItemStatus {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]domain.DepInstallItemStatus(nil), t.items...)
}
