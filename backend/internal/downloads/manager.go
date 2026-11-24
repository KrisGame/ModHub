package downloads

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"modhub/internal/domain"
	"modhub/internal/events"
	"modhub/internal/netx"
)

const (
	defaultMaxConcurrent   = 4
	minConcurrent          = 1
	maxConcurrentCap       = 32
	defaultMaxRetries      = 3
	maxQueuedTasksCap      = 50000
	progressEmitInterval   = 140 * time.Millisecond
	diskMarginBytes        = 4 * 1024 * 1024 // 4 MB safety headroom
)

var (
	ErrTaskNotFound     = errors.New("задача загрузки не найдена")
	ErrDuplicateDest    = errors.New("другая активная загрузка уже записывает в этот файл")
	ErrInvalidRequest   = errors.New("некорректные параметры загрузки")
	ErrManagerClosed    = errors.New("менеджер загрузок остановлен")
	ErrQueueLimit       = errors.New("превышен лимит задач в очереди загрузок")
	ErrInsufficientDisk = errors.New("недостаточно свободного места на диске")
)

type taskEntry struct {
	rec        domain.DownloadTaskRecord
	speedBps   float64
	etaSecs    int64
	verified   bool
	lastEmit   time.Time
	activeCtx  context.Context
	cancelFn   context.CancelFunc
	stopReason Status // StatusPaused or StatusCancelled when stopped externally
	doneCh     chan struct{}
}

// Manager is the centralized, content-agnostic Download Manager shared by all ModHub subsystems.
type Manager struct {
	httpClient *netx.HTTPClient
	store      Store
	bus        *events.Bus

	baseCtx    context.Context
	baseCancel context.CancelFunc

	mu              sync.Mutex
	closed          bool
	maxConcurrent   int
	activeCount     int
	tasks           map[string]*taskEntry
	queue           []string
	activeDestPaths map[string]string // normalized dest_path -> task ID
	lastSummaryEmit time.Time
	idSeq           atomic.Uint64
	baseRetryDelay  time.Duration

	wg        sync.WaitGroup
	closeOnce sync.Once
}

func NewManager(httpClient *netx.HTTPClient, store Store, bus *events.Bus, maxConcurrent int) *Manager {
	if maxConcurrent < minConcurrent || maxConcurrent > maxConcurrentCap {
		maxConcurrent = defaultMaxConcurrent
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		httpClient:      httpClient,
		store:           store,
		bus:             bus,
		baseCtx:         ctx,
		baseCancel:      cancel,
		maxConcurrent:   maxConcurrent,
		tasks:           make(map[string]*taskEntry),
		queue:           make([]string, 0, 32),
		activeDestPaths: make(map[string]string),
		baseRetryDelay:  500 * time.Millisecond,
	}
	m.restoreFromStore()
	return m
}

// SetRetryBaseDelay overrides retry backoff base duration (primarily for fast unit tests).
func (m *Manager) SetRetryBaseDelay(d time.Duration) {
	if d > 0 {
		m.mu.Lock()
		m.baseRetryDelay = d
		m.mu.Unlock()
	}
}

func normalizeDestKey(destPath string) string {
	abs, err := filepath.Abs(destPath)
	if err != nil {
		abs = filepath.Clean(destPath)
	}
	return strings.ToLower(filepath.Clean(abs))
}

func isTerminalStatus(st Status) bool {
	return st == StatusCompleted || st == StatusCancelled || st == StatusError
}

func isLiveStatus(st Status) bool {
	return st == StatusPending || st == StatusQueued || st == StatusDownloading || st == StatusVerifying || st == StatusPaused
}

func (m *Manager) restoreFromStore() {
	if m.store == nil {
		return
	}
	records, err := m.store.ListDownloadTasks()
	if err != nil || len(records) == 0 {
		return
	}

	now := time.Now().Unix()
	toSchedule := false
	for _, rec := range records {
		// Clean up corrupt or incomplete records without losing healthy tasks
		if strings.TrimSpace(rec.ID) == "" || strings.TrimSpace(rec.URL) == "" || strings.TrimSpace(rec.DestPath) == "" {
			_ = m.store.DeleteDownloadTask(rec.ID)
			continue
		}
		st := Status(rec.Status)
		switch st {
		case StatusCompleted, StatusCancelled, StatusError:
			// Completed/cancelled/error tasks never auto-restart; clean any leftover temp files
			if rec.TempPath != "" {
				_ = os.Remove(rec.TempPath)
			}
			te := &taskEntry{
				rec:      rec,
				etaSecs:  -1,
				verified: st == StatusCompleted && rec.ChecksumAlgo != "",
			}
			m.tasks[rec.ID] = te

		case StatusPaused:
			// Keep explicitly paused tasks paused, reconciling downloaded_bytes with actual .mhpart file
			if rec.TempPath == "" {
				rec.TempPath = rec.DestPath + ".mhpart"
			}
			if info, err := os.Stat(rec.TempPath); err == nil && !info.IsDir() {
				rec.DownloadedBytes = info.Size()
			} else {
				rec.DownloadedBytes = 0
			}
			destKey := normalizeDestKey(rec.DestPath)
			if _, busy := m.activeDestPaths[destKey]; busy {
				rec.Status = string(StatusError)
				rec.ErrorMsg = ErrDuplicateDest.Error()
				rec.UpdatedAt = now
				_ = m.store.UpsertDownloadTask(rec)
				m.tasks[rec.ID] = &taskEntry{rec: rec, etaSecs: -1}
				continue
			}
			m.activeDestPaths[destKey] = rec.ID
			_ = m.store.UpsertDownloadTask(rec)
			m.tasks[rec.ID] = &taskEntry{rec: rec, etaSecs: -1, doneCh: make(chan struct{})}

		case StatusPending, StatusQueued, StatusDownloading, StatusVerifying:
			// Validate URL security before safely recovering interrupted task
			if _, err := m.httpClient.ValidateLauncherURL(rec.URL); err != nil {
				if rec.TempPath != "" {
					_ = os.Remove(rec.TempPath)
				}
				rec.Status = string(StatusError)
				rec.ErrorMsg = "Восстановление отклонено проверкой URL: " + err.Error()
				rec.UpdatedAt = now
				_ = m.store.UpsertDownloadTask(rec)
				m.tasks[rec.ID] = &taskEntry{rec: rec, etaSecs: -1}
				continue
			}
			destKey := normalizeDestKey(rec.DestPath)
			if _, busy := m.activeDestPaths[destKey]; busy {
				rec.Status = string(StatusError)
				rec.ErrorMsg = ErrDuplicateDest.Error()
				rec.UpdatedAt = now
				_ = m.store.UpsertDownloadTask(rec)
				m.tasks[rec.ID] = &taskEntry{rec: rec, etaSecs: -1}
				continue
			}
			if rec.TempPath == "" {
				rec.TempPath = rec.DestPath + ".mhpart"
			}
			if info, err := os.Stat(rec.TempPath); err == nil && !info.IsDir() {
				rec.DownloadedBytes = info.Size()
			} else {
				rec.DownloadedBytes = 0
			}
			rec.Status = string(StatusQueued)
			rec.ErrorMsg = ""
			rec.UpdatedAt = now
			m.activeDestPaths[destKey] = rec.ID
			_ = m.store.UpsertDownloadTask(rec)
			m.tasks[rec.ID] = &taskEntry{rec: rec, etaSecs: -1, doneCh: make(chan struct{})}
			m.queue = append(m.queue, rec.ID)
			toSchedule = true

		default:
			// Unknown/corrupt status -> prune cleanly
			_ = m.store.DeleteDownloadTask(rec.ID)
		}
	}

	if toSchedule {
		m.mu.Lock()
		m.scheduleLocked()
		m.mu.Unlock()
	}
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		for _, te := range m.tasks {
			if te.cancelFn != nil {
				te.cancelFn()
			}
		}
		m.mu.Unlock()
		if m.baseCancel != nil {
			m.baseCancel()
		}
		m.wg.Wait()
	})
}

func (m *Manager) SetMaxConcurrent(n int) int {
	if n < minConcurrent {
		n = minConcurrent
	} else if n > maxConcurrentCap {
		n = maxConcurrentCap
	}
	m.mu.Lock()
	m.maxConcurrent = n
	m.scheduleLocked()
	sum := m.buildSummaryLocked()
	m.mu.Unlock()
	m.emitEvent(events.EventDownloadSummary, sum)
	return n
}

func (m *Manager) MaxConcurrent() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.maxConcurrent
}

func (m *Manager) Enqueue(req DownloadRequest) (TaskView, error) {
	urlStr := strings.TrimSpace(req.URL)
	destPath := strings.TrimSpace(req.DestPath)
	if urlStr == "" || destPath == "" {
		return TaskView{}, fmt.Errorf("%w: требуется URL и путь назначения", ErrInvalidRequest)
	}
	if _, err := m.httpClient.ValidateLauncherURL(urlStr); err != nil {
		return TaskView{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	cleanDest, err := filepath.Abs(destPath)
	if err != nil {
		return TaskView{}, fmt.Errorf("%w: некорректный путь назначения", ErrInvalidRequest)
	}
	cleanDest = filepath.Clean(cleanDest)

	algo := ChecksumAlgo(strings.ToLower(strings.TrimSpace(string(req.ChecksumAlgo))))
	val := strings.ToLower(strings.TrimSpace(req.ChecksumValue))
	if algo != ChecksumNone && algo != ChecksumSHA1 && algo != ChecksumSHA256 {
		return TaskView{}, fmt.Errorf("%w: неподдерживаемый алгоритм контрольной суммы %q (поддерживаются sha1, sha256)", ErrInvalidRequest, algo)
	}
	if algo != ChecksumNone && val == "" {
		return TaskView{}, fmt.Errorf("%w: не указано значение контрольной суммы", ErrInvalidRequest)
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = filepath.Base(cleanDest)
	}
	cat := strings.TrimSpace(req.Category)
	if cat == "" {
		cat = "generic"
	}
	maxRetries := req.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = netx.MaxLauncherDownloadBytes
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return TaskView{}, ErrManagerClosed
	}
	if len(m.tasks) >= maxQueuedTasksCap {
		// Prune finished tasks to make room in queue
		m.pruneFinishedTasksLocked()
		if len(m.tasks) >= maxQueuedTasksCap {
			m.mu.Unlock()
			return TaskView{}, ErrQueueLimit
		}
	}
	destKey := normalizeDestKey(cleanDest)
	if existingID, busy := m.activeDestPaths[destKey]; busy {
		m.mu.Unlock()
		return TaskView{}, fmt.Errorf("%w (задача %s)", ErrDuplicateDest, existingID)
	}

	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = fmt.Sprintf("dl_%d_%d", time.Now().UnixMilli(), m.idSeq.Add(1))
	}
	if _, dup := m.tasks[id]; dup {
		id = fmt.Sprintf("%s_%d", id, m.idSeq.Add(1))
	}

	now := time.Now().Unix()
	rec := domain.DownloadTaskRecord{
		ID:            id,
		Name:          name,
		Category:      cat,
		URL:           urlStr,
		DestPath:      cleanDest,
		TempPath:      cleanDest + ".mhpart",
		TotalBytes:    req.ExpectedSize,
		ChecksumAlgo:  string(algo),
		ChecksumValue: val,
		Status:        string(StatusQueued),
		MaxRetries:    maxRetries,
		MaxBytes:      maxBytes,
		GroupID:       strings.TrimSpace(req.GroupID),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	te := &taskEntry{
		rec:     rec,
		etaSecs: -1,
		doneCh:  make(chan struct{}),
	}
	m.tasks[id] = te
	m.activeDestPaths[destKey] = id
	m.queue = append(m.queue, id)
	m.persistLocked(te)
	view := te.snapshot()
	sum := m.buildSummaryLocked()
	m.scheduleLocked()
	m.mu.Unlock()

	m.emitEvent(events.EventDownloadCreated, view)
	m.emitEvent(events.EventDownloadSummary, sum)
	return view, nil
}

// Wait blocks until the task reaches a terminal status (completed, cancelled, error) or ctx is done.
func (m *Manager) Wait(ctx context.Context, id string) (TaskView, error) {
	m.mu.Lock()
	te, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return TaskView{}, ErrTaskNotFound
	}
	if isTerminalStatus(Status(te.rec.Status)) {
		v := te.snapshot()
		m.mu.Unlock()
		return v, nil
	}
	doneCh := te.doneCh
	m.mu.Unlock()

	if doneCh == nil {
		return m.GetTask(id)
	}
	select {
	case <-doneCh:
		return m.GetTask(id)
	case <-ctx.Done():
		return TaskView{}, ctx.Err()
	}
}

func (m *Manager) GetTask(id string) (TaskView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	te, ok := m.tasks[id]
	if !ok {
		return TaskView{}, ErrTaskNotFound
	}
	return te.snapshot(), nil
}

func (m *Manager) GetSnapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	views := make([]TaskView, 0, len(m.tasks))
	for _, te := range m.tasks {
		views = append(views, te.snapshot())
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].CreatedAt != views[j].CreatedAt {
			return views[i].CreatedAt < views[j].CreatedAt
		}
		return views[i].ID < views[j].ID
	})
	return Snapshot{
		Tasks:   views,
		Summary: m.buildSummaryLocked(),
	}
}

func (m *Manager) Pause(id string) (TaskView, error) {
	m.mu.Lock()
	te, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return TaskView{}, ErrTaskNotFound
	}
	st := Status(te.rec.Status)
	switch st {
	case StatusQueued, StatusPending:
		m.removeFromQueueLocked(id)
		te.rec.Status = string(StatusPaused)
		te.rec.UpdatedAt = time.Now().Unix()
		te.speedBps = 0
		te.etaSecs = -1
		m.persistLocked(te)
		v := te.snapshot()
		sum := m.buildSummaryLocked()
		m.mu.Unlock()
		m.emitEvent(events.EventDownloadPaused, v)
		m.emitEvent(events.EventDownloadStatus, v)
		m.emitEvent(events.EventDownloadSummary, sum)
		return v, nil
	case StatusDownloading:
		te.stopReason = StatusPaused
		te.rec.Status = string(StatusPaused)
		te.rec.UpdatedAt = time.Now().Unix()
		te.speedBps = 0
		te.etaSecs = -1
		if te.cancelFn != nil {
			te.cancelFn()
		}
		m.persistLocked(te)
		v := te.snapshot()
		sum := m.buildSummaryLocked()
		m.mu.Unlock()
		m.emitEvent(events.EventDownloadPaused, v)
		m.emitEvent(events.EventDownloadStatus, v)
		m.emitEvent(events.EventDownloadSummary, sum)
		return v, nil
	case StatusPaused:
		v := te.snapshot()
		m.mu.Unlock()
		return v, nil
	default:
		v := te.snapshot()
		m.mu.Unlock()
		return v, fmt.Errorf("нельзя приостановить загрузку в статусе %s", st)
	}
}

func (m *Manager) Resume(id string) (TaskView, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return TaskView{}, ErrManagerClosed
	}
	te, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return TaskView{}, ErrTaskNotFound
	}
	if Status(te.rec.Status) != StatusPaused {
		v := te.snapshot()
		m.mu.Unlock()
		return v, fmt.Errorf("загрузка не находится на паузе (статус: %s)", te.rec.Status)
	}
	destKey := normalizeDestKey(te.rec.DestPath)
	if owner, busy := m.activeDestPaths[destKey]; busy && owner != id {
		m.mu.Unlock()
		return TaskView{}, ErrDuplicateDest
	}
	m.activeDestPaths[destKey] = id
	te.stopReason = ""
	te.rec.Status = string(StatusQueued)
	te.rec.ErrorMsg = ""
	te.rec.UpdatedAt = time.Now().Unix()
	m.queue = append(m.queue, id)
	m.persistLocked(te)
	v := te.snapshot()
	sum := m.buildSummaryLocked()
	m.scheduleLocked()
	m.mu.Unlock()

	m.emitEvent(events.EventDownloadResumed, v)
	m.emitEvent(events.EventDownloadStatus, v)
	m.emitEvent(events.EventDownloadSummary, sum)
	return v, nil
}

func (m *Manager) Cancel(id string) (TaskView, error) {
	m.mu.Lock()
	te, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return TaskView{}, ErrTaskNotFound
	}
	st := Status(te.rec.Status)
	if st == StatusCompleted {
		v := te.snapshot()
		m.mu.Unlock()
		return v, fmt.Errorf("загрузка уже завершена")
	}
	if st == StatusCancelled {
		v := te.snapshot()
		m.mu.Unlock()
		return v, nil
	}
	m.removeFromQueueLocked(id)
	te.stopReason = StatusCancelled
	te.rec.Status = string(StatusCancelled)
	te.rec.ErrorMsg = ""
	te.rec.UpdatedAt = time.Now().Unix()
	te.speedBps = 0
	te.etaSecs = -1
	cancelFn := te.cancelFn
	if cancelFn != nil {
		cancelFn()
	} else {
		// Not currently running in a worker -> clean up immediately
		m.releaseDestLocked(te)
		if te.rec.TempPath != "" {
			_ = os.Remove(te.rec.TempPath)
		}
		te.rec.DownloadedBytes = 0
		m.closeDoneLocked(te)
	}
	m.persistLocked(te)
	v := te.snapshot()
	sum := m.buildSummaryLocked()
	m.mu.Unlock()

	m.emitEvent(events.EventDownloadCancelled, v)
	m.emitEvent(events.EventDownloadStatus, v)
	m.emitEvent(events.EventDownloadSummary, sum)
	return v, nil
}

// CancelGroup cancels every non-completed task sharing groupID (used e.g. when aborting a Java install).
func (m *Manager) CancelGroup(groupID string) {
	if strings.TrimSpace(groupID) == "" {
		return
	}
	m.mu.Lock()
	ids := make([]string, 0)
	for id, te := range m.tasks {
		if te.rec.GroupID == groupID && !isTerminalStatus(Status(te.rec.Status)) {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		_, _ = m.Cancel(id)
	}
}

func (m *Manager) Retry(id string) (TaskView, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return TaskView{}, ErrManagerClosed
	}
	te, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return TaskView{}, ErrTaskNotFound
	}
	st := Status(te.rec.Status)
	if st != StatusError && st != StatusCancelled {
		v := te.snapshot()
		m.mu.Unlock()
		return v, fmt.Errorf("повторить можно только ошибочную или отменённую загрузку (статус: %s)", st)
	}
	destKey := normalizeDestKey(te.rec.DestPath)
	if owner, busy := m.activeDestPaths[destKey]; busy && owner != id {
		m.mu.Unlock()
		return TaskView{}, ErrDuplicateDest
	}
	if te.rec.TempPath != "" {
		_ = os.Remove(te.rec.TempPath)
	}
	m.activeDestPaths[destKey] = id
	te.stopReason = ""
	te.rec.Status = string(StatusQueued)
	te.rec.ErrorMsg = ""
	te.rec.DownloadedBytes = 0
	te.rec.Attempts = 0
	te.rec.IntegrityNote = ""
	te.rec.UpdatedAt = time.Now().Unix()
	te.speedBps = 0
	te.etaSecs = -1
	te.verified = false
	te.doneCh = make(chan struct{})
	m.queue = append(m.queue, id)
	m.persistLocked(te)
	v := te.snapshot()
	sum := m.buildSummaryLocked()
	m.scheduleLocked()
	m.mu.Unlock()

	m.emitEvent(events.EventDownloadStatus, v)
	m.emitEvent(events.EventDownloadSummary, sum)
	return v, nil
}

func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	te, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return ErrTaskNotFound
	}
	st := Status(te.rec.Status)
	if !isTerminalStatus(st) {
		m.mu.Unlock()
		return fmt.Errorf("нельзя удалить активную загрузку (сначала отмените её)")
	}
	delete(m.tasks, id)
	m.releaseDestLocked(te)
	if m.store != nil {
		_ = m.store.DeleteDownloadTask(id)
	}
	sum := m.buildSummaryLocked()
	m.mu.Unlock()

	m.emitEvent(events.EventDownloadRemoved, map[string]any{"id": id})
	m.emitEvent(events.EventDownloadSummary, sum)
	return nil
}

func (m *Manager) pruneFinishedTasksLocked() int {
	removed := 0
	for id, te := range m.tasks {
		st := Status(te.rec.Status)
		if st == StatusCompleted || st == StatusCancelled || st == StatusError {
			delete(m.tasks, id)
			m.releaseDestLocked(te)
			removed++
		}
	}
	if m.store != nil && removed > 0 {
		_ = m.store.DeleteDownloadTasksByStatuses([]string{string(StatusCompleted), string(StatusCancelled), string(StatusError)})
	}
	return removed
}

func (m *Manager) ClearFinished() int {
	m.mu.Lock()
	removed := m.pruneFinishedTasksLocked()
	sum := m.buildSummaryLocked()
	m.mu.Unlock()
	if removed > 0 {
		m.emitEvent(events.EventDownloadSummary, sum)
	}
	return removed
}

func (m *Manager) removeFromQueueLocked(id string) {
	for i, qid := range m.queue {
		if qid == id {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			return
		}
	}
}

func (m *Manager) releaseDestLocked(te *taskEntry) {
	destKey := normalizeDestKey(te.rec.DestPath)
	if owner, ok := m.activeDestPaths[destKey]; ok && owner == te.rec.ID {
		delete(m.activeDestPaths, destKey)
	}
}

func (m *Manager) closeDoneLocked(te *taskEntry) {
	if te.doneCh != nil {
		select {
		case <-te.doneCh:
		default:
			close(te.doneCh)
		}
	}
}

func (m *Manager) scheduleLocked() {
	if m.closed {
		return
	}
	for m.activeCount < m.maxConcurrent && len(m.queue) > 0 {
		id := m.queue[0]
		m.queue = m.queue[1:]
		te, ok := m.tasks[id]
		if !ok || Status(te.rec.Status) != StatusQueued {
			continue
		}
		taskCtx, cancel := context.WithCancel(m.baseCtx)
		te.activeCtx = taskCtx
		te.cancelFn = cancel
		te.stopReason = ""
		te.rec.Status = string(StatusDownloading)
		te.rec.UpdatedAt = time.Now().Unix()
		m.activeCount++
		m.persistLocked(te)
		m.wg.Add(1)
		go m.runWorker(te, taskCtx)
	}
}

func (m *Manager) runWorker(te *taskEntry, taskCtx context.Context) {
	defer func() {
		m.mu.Lock()
		if te.cancelFn != nil {
			te.cancelFn()
			te.cancelFn = nil
		}
		te.activeCtx = nil
		te.speedBps = 0
		te.etaSecs = -1
		m.activeCount--
		m.scheduleLocked()
		sum := m.buildSummaryLocked()
		m.mu.Unlock()
		m.emitEvent(events.EventDownloadSummary, sum)
		m.wg.Done()
	}()

	m.emitSnapshot(te, events.EventDownloadStatus)

	m.mu.Lock()
	maxRetries := te.rec.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	baseDelay := m.baseRetryDelay
	m.mu.Unlock()

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if taskCtx.Err() != nil {
			m.handleInterruption(te)
			return
		}

		m.mu.Lock()
		te.rec.Attempts = attempt + 1
		te.rec.Status = string(StatusDownloading)
		te.rec.UpdatedAt = time.Now().Unix()
		m.persistLocked(te)
		m.mu.Unlock()

		retryable, err := m.executeAttempt(taskCtx, te)
		if err == nil {
			return
		}
		if taskCtx.Err() != nil {
			m.handleInterruption(te)
			return
		}
		lastErr = err
		if !retryable || attempt >= maxRetries {
			break
		}

		// Exponential backoff between transient failures
		delay := baseDelay * time.Duration(1<<attempt)
		if delay > 8*time.Second {
			delay = 8 * time.Second
		}
		if !netx.SleepCtx(taskCtx, delay) {
			m.handleInterruption(te)
			return
		}
	}

	m.failTask(te, lastErr)
}

func (m *Manager) handleInterruption(te *taskEntry) {
	m.mu.Lock()
	reason := te.stopReason
	if m.closed && reason == "" {
		// Graceful shutdown while downloading: keep .mhpart & persist state for safe recovery on next launch
		te.rec.UpdatedAt = time.Now().Unix()
		m.persistLocked(te)
		m.mu.Unlock()
		return
	}
	if reason == StatusPaused {
		te.rec.Status = string(StatusPaused)
		te.rec.UpdatedAt = time.Now().Unix()
		m.persistLocked(te)
		v := te.snapshot()
		m.mu.Unlock()
		m.emitEvent(events.EventDownloadPaused, v)
		m.emitEvent(events.EventDownloadStatus, v)
		return
	}
	// Cancelled
	te.rec.Status = string(StatusCancelled)
	te.rec.ErrorMsg = ""
	te.rec.DownloadedBytes = 0
	te.rec.UpdatedAt = time.Now().Unix()
	if te.rec.TempPath != "" {
		_ = os.Remove(te.rec.TempPath)
	}
	m.releaseDestLocked(te)
	m.persistLocked(te)
	m.closeDoneLocked(te)
	v := te.snapshot()
	m.mu.Unlock()
	m.emitEvent(events.EventDownloadCancelled, v)
	m.emitEvent(events.EventDownloadStatus, v)
}

func (m *Manager) failTask(te *taskEntry, err error) {
	msg := "неизвестная ошибка загрузки"
	if err != nil {
		msg = err.Error()
	}
	m.mu.Lock()
	te.rec.Status = string(StatusError)
	te.rec.ErrorMsg = msg
	te.rec.UpdatedAt = time.Now().Unix()
	te.speedBps = 0
	te.etaSecs = -1
	m.releaseDestLocked(te)
	m.persistLocked(te)
	m.closeDoneLocked(te)
	v := te.snapshot()
	m.mu.Unlock()

	m.emitEvent(events.EventDownloadError, v)
	m.emitEvent(events.EventDownloadStatus, v)
}

// executeAttempt runs one streaming download + verification + atomic commit attempt.
// Returns (retryable, err).
func (m *Manager) executeAttempt(ctx context.Context, te *taskEntry) (bool, error) {
	m.mu.Lock()
	rawURL := te.rec.URL
	destPath := te.rec.DestPath
	tempPath := te.rec.TempPath
	expectedSize := te.rec.TotalBytes
	maxBytes := te.rec.MaxBytes
	checksumAlgo := ChecksumAlgo(te.rec.ChecksumAlgo)
	checksumVal := te.rec.ChecksumValue
	m.mu.Unlock()

	if maxBytes <= 0 {
		maxBytes = netx.MaxLauncherDownloadBytes
	}
	parentDir := filepath.Dir(destPath)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return false, fmt.Errorf("ошибка создания директории назначения: %w", err)
	}

	// Inspect existing .mhpart for safe HTTP Range resumption
	var existingBytes int64
	if info, err := os.Stat(tempPath); err == nil && !info.IsDir() {
		existingBytes = info.Size()
		if expectedSize > 0 && existingBytes > expectedSize {
			_ = os.Remove(tempPath)
			existingBytes = 0
		}
	}

	// Pre-check disk space when total size is already known
	if expectedSize > 0 {
		needed := expectedSize - existingBytes
		if needed < 0 {
			needed = expectedSize
		}
		if free, ok := availableDiskBytes(parentDir); ok && uint64(needed)+diskMarginBytes > free {
			return false, fmt.Errorf("%w (требуется %d МБ, свободно %d МБ)", ErrInsufficientDisk, needed/(1024*1024)+1, free/(1024*1024))
		}
	}

	// If partial file already equals expected size and checksum is provided, try verifying before network call
	if expectedSize > 0 && existingBytes == expectedSize && checksumAlgo != ChecksumNone {
		if ok, _ := verifyFileIntegrity(tempPath, expectedSize, checksumAlgo, checksumVal); ok {
			return m.commitVerifiedFile(te, tempPath, destPath, expectedSize, checksumAlgo)
		}
		_ = os.Remove(tempPath)
		existingBytes = 0
	}

	resp, err := m.httpClient.OpenLauncherStream(ctx, rawURL, existingBytes, maxBytes)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		// Permanent HTTP 4xx (except 408/429) or URL security errors must NOT retry
		low := strings.ToLower(err.Error())
		permanent := strings.Contains(low, "http 400") ||
			strings.Contains(low, "http 401") ||
			strings.Contains(low, "http 403") ||
			strings.Contains(low, "http 404") ||
			strings.Contains(low, "http 410") ||
			strings.Contains(low, "не входит в список") ||
			strings.Contains(low, "разрешены только https") ||
			strings.Contains(low, "превышает лимит")
		return !permanent, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		// Existing .mhpart may already be complete or mismatched -> discard and restart cleanly from 0
		_ = resp.Body.Close()
		if existingBytes > 0 && checksumAlgo != ChecksumNone {
			if ok, _ := verifyFileIntegrity(tempPath, expectedSize, checksumAlgo, checksumVal); ok {
				return m.commitVerifiedFile(te, tempPath, destPath, existingBytes, checksumAlgo)
			}
		}
		_ = os.Remove(tempPath)
		return true, fmt.Errorf("сервер отклонил диапазон (HTTP 416), перезапуск загрузки")
	}

	// Determine whether server honored HTTP Range (206) or fell back to full 200 OK
	resumed := false
	supportsRange := strings.EqualFold(strings.TrimSpace(resp.Header.Get("Accept-Ranges")), "bytes")
	if existingBytes > 0 && resp.StatusCode == http.StatusPartialContent {
		resumed = true
		supportsRange = true
	} else if existingBytes > 0 && resp.StatusCode == http.StatusOK {
		// Server ignored Range header: restart cleanly from byte 0 so file is NEVER corrupted
		existingBytes = 0
		resumed = false
		supportsRange = false
	}

	totalBytes := expectedSize
	if resp.ContentLength > 0 {
		if resumed {
			totalBytes = existingBytes + resp.ContentLength
		} else {
			totalBytes = resp.ContentLength
		}
	}
	if totalBytes > maxBytes {
		return false, fmt.Errorf("размер файла (%d МБ) превышает лимит (%d МБ)", totalBytes/(1024*1024), maxBytes/(1024*1024))
	}
	if totalBytes > 0 {
		needed := totalBytes - existingBytes
		if needed < 0 {
			needed = totalBytes
		}
		if free, ok := availableDiskBytes(parentDir); ok && uint64(needed)+diskMarginBytes > free {
			return false, fmt.Errorf("%w (требуется %d МБ, свободно %d МБ)", ErrInsufficientDisk, needed/(1024*1024)+1, free/(1024*1024))
		}
	}

	flags := os.O_CREATE | os.O_WRONLY
	if resumed {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(tempPath, flags, 0o644)
	if err != nil {
		return false, fmt.Errorf("ошибка открытия временного файла: %w", err)
	}

	downloaded := existingBytes
	m.mu.Lock()
	te.rec.TotalBytes = totalBytes
	te.rec.DownloadedBytes = downloaded
	te.rec.SupportsRange = supportsRange
	te.rec.UpdatedAt = time.Now().Unix()
	m.persistLocked(te)
	m.mu.Unlock()

	buf := make([]byte, 32*1024)
	winStart := time.Now()
	var winBytes int64
	var emaSpeed float64

	for {
		if ctx.Err() != nil {
			_ = f.Sync()
			_ = f.Close()
			return false, ctx.Err()
		}
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			downloaded += int64(n)
			if downloaded > maxBytes {
				_ = f.Close()
				_ = os.Remove(tempPath)
				return false, fmt.Errorf("превышен максимальный размер файла (%d МБ)", maxBytes/(1024*1024))
			}
			nw, werr := f.Write(buf[:n])
			if werr != nil || nw != n {
				_ = f.Close()
				if werr == nil {
					werr = io.ErrShortWrite
				}
				return false, fmt.Errorf("ошибка записи на диск: %w", werr)
			}
			winBytes += int64(n)

			now := time.Now()
			dt := now.Sub(winStart).Seconds()
			if dt >= 0.14 || rerr == io.EOF {
				if dt > 0 {
					instSpeed := float64(winBytes) / dt
					if emaSpeed == 0 {
						emaSpeed = instSpeed
					} else {
						emaSpeed = 0.35*instSpeed + 0.65*emaSpeed
					}
				}
				winStart = now
				winBytes = 0

				var eta int64 = -1
				if totalBytes > downloaded && emaSpeed >= 64 {
					eta = int64(float64(totalBytes-downloaded) / emaSpeed)
				}

				m.mu.Lock()
				te.rec.DownloadedBytes = downloaded
				te.speedBps = emaSpeed
				te.etaSecs = eta
				shouldEmit := now.Sub(te.lastEmit) >= progressEmitInterval
				var snap TaskView
				var sum QueueSummary
				shouldSum := false
				if shouldEmit {
					te.lastEmit = now
					snap = te.snapshot()
					if now.Sub(m.lastSummaryEmit) >= progressEmitInterval {
						m.lastSummaryEmit = now
						sum = m.buildSummaryLocked()
						shouldSum = true
					}
				}
				m.mu.Unlock()

				if shouldEmit {
					m.emitEvent(events.EventDownloadProgress, snap)
					if shouldSum {
						m.emitEvent(events.EventDownloadSummary, sum)
					}
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = f.Sync()
			_ = f.Close()
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			// Persist current progress before transient retry so Range resume picks up cleanly
			m.mu.Lock()
			te.rec.DownloadedBytes = downloaded
			te.rec.UpdatedAt = time.Now().Unix()
			m.persistLocked(te)
			m.mu.Unlock()
			return true, fmt.Errorf("обрыв соединения: %w", rerr)
		}
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		return false, fmt.Errorf("ошибка синхронизации файла: %w", err)
	}
	if err := f.Close(); err != nil {
		return false, fmt.Errorf("ошибка закрытия временного файла: %w", err)
	}

	// Verify downloaded size if Content-Length / ExpectedSize was known
	if totalBytes > 0 && downloaded != totalBytes {
		return true, fmt.Errorf("неполная загрузка: получено %d из %d байт", downloaded, totalBytes)
	}

	// Enter StatusVerifying
	m.mu.Lock()
	te.rec.Status = string(StatusVerifying)
	te.rec.DownloadedBytes = downloaded
	if te.rec.TotalBytes == 0 {
		te.rec.TotalBytes = downloaded
	}
	te.rec.UpdatedAt = time.Now().Unix()
	te.speedBps = 0
	te.etaSecs = 0
	m.persistLocked(te)
	verSnap := te.snapshot()
	m.mu.Unlock()
	m.emitEvent(events.EventDownloadStatus, verSnap)

	ok, verifyErr := verifyFileIntegrity(tempPath, totalBytes, checksumAlgo, checksumVal)
	if !ok {
		_ = os.Remove(tempPath)
		return false, verifyErr
	}

	return m.commitVerifiedFile(te, tempPath, destPath, downloaded, checksumAlgo)
}

func (m *Manager) commitVerifiedFile(te *taskEntry, tempPath, destPath string, finalSize int64, algo ChecksumAlgo) (bool, error) {
	// Atomic replacement: existing file is untouched until tempPath is verified & ready.
	if err := replaceFileAtomic(tempPath, destPath); err != nil {
		return false, fmt.Errorf("ошибка сохранения итогового файла: %w", err)
	}

	note := ""
	verified := false
	switch algo {
	case ChecksumSHA1:
		verified = true
		note = "Контрольная сумма SHA-1 подтверждена"
	case ChecksumSHA256:
		verified = true
		note = "Контрольная сумма SHA-256 подтверждена"
	default:
		if te.rec.TotalBytes > 0 {
			note = "Криптографическая проверка не выполнялась (проверен размер файла)"
		} else {
			note = "Криптографическая проверка не выполнялась"
		}
	}

	m.mu.Lock()
	te.rec.Status = string(StatusCompleted)
	te.rec.DownloadedBytes = finalSize
	if te.rec.TotalBytes == 0 {
		te.rec.TotalBytes = finalSize
	}
	te.rec.ErrorMsg = ""
	te.rec.IntegrityNote = note
	te.rec.UpdatedAt = time.Now().Unix()
	te.verified = verified
	te.speedBps = 0
	te.etaSecs = 0
	m.releaseDestLocked(te)
	m.persistLocked(te)
	m.closeDoneLocked(te)
	v := te.snapshot()
	sum := m.buildSummaryLocked()
	m.mu.Unlock()

	m.emitEvent(events.EventDownloadCompleted, v)
	m.emitEvent(events.EventDownloadStatus, v)
	m.emitEvent(events.EventDownloadSummary, sum)
	return false, nil
}

func replaceFileAtomic(srcTemp, dst string) error {
	if err := os.Rename(srcTemp, dst); err == nil {
		return nil
	}
	// On Windows os.Rename fails if dst already exists: safely swap via backup path so dst is never lost if rename fails.
	backupPath := dst + ".mhbak"
	_ = os.Remove(backupPath)
	hadExisting := false
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, backupPath); err != nil {
			return err
		}
		hadExisting = true
	}
	if err := os.Rename(srcTemp, dst); err != nil {
		if hadExisting {
			_ = os.Rename(backupPath, dst)
		}
		return err
	}
	if hadExisting {
		_ = os.Remove(backupPath)
	}
	return nil
}

func verifyFileIntegrity(path string, expectedSize int64, algo ChecksumAlgo, expectedHash string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("временный файл недоступен для проверки: %w", err)
	}
	if expectedSize > 0 && info.Size() != expectedSize {
		return false, fmt.Errorf("несовпадение размера файла: получено %d, ожидалось %d", info.Size(), expectedSize)
	}
	if algo == ChecksumNone {
		return true, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("ошибка чтения файла для проверки контрольной суммы: %w", err)
	}
	defer f.Close()

	var h hash.Hash
	switch algo {
	case ChecksumSHA1:
		h = sha1.New()
	case ChecksumSHA256:
		h = sha256.New()
	default:
		return false, fmt.Errorf("неподдерживаемый алгоритм хэширования: %s", algo)
	}
	if _, err := io.Copy(h, f); err != nil {
		return false, fmt.Errorf("ошибка вычисления контрольной суммы: %w", err)
	}
	actual := strings.ToLower(hex.EncodeToString(h.Sum(nil)))
	want := strings.ToLower(strings.TrimSpace(expectedHash))
	if actual != want {
		return false, fmt.Errorf("несовпадение контрольной суммы %s (получено %s, ожидалось %s)", strings.ToUpper(string(algo)), actual, want)
	}
	return true, nil
}

func (te *taskEntry) snapshot() TaskView {
	var pct float64
	if te.rec.Status == string(StatusCompleted) {
		pct = 100
	} else if te.rec.TotalBytes > 0 {
		pct = float64(te.rec.DownloadedBytes) * 100 / float64(te.rec.TotalBytes)
		if pct > 100 {
			pct = 100
		}
	}
	return TaskView{
		ID:               te.rec.ID,
		Name:             te.rec.Name,
		Category:         te.rec.Category,
		URL:              te.rec.URL,
		DestPath:         te.rec.DestPath,
		TotalBytes:       te.rec.TotalBytes,
		DownloadedBytes:  te.rec.DownloadedBytes,
		SpeedBytesPerSec: te.speedBps,
		ETASeconds:       te.etaSecs,
		Progress:         pct,
		Status:           Status(te.rec.Status),
		Error:            te.rec.ErrorMsg,
		ChecksumAlgo:     te.rec.ChecksumAlgo,
		ChecksumValue:    te.rec.ChecksumValue,
		IntegrityNote:    te.rec.IntegrityNote,
		Verified:         te.verified,
		SupportsRange:    te.rec.SupportsRange,
		Attempts:         te.rec.Attempts,
		MaxRetries:       te.rec.MaxRetries,
		GroupID:          te.rec.GroupID,
		CreatedAt:        te.rec.CreatedAt,
		UpdatedAt:        te.rec.UpdatedAt,
	}
}

func (m *Manager) buildSummaryLocked() QueueSummary {
	sum := QueueSummary{
		TotalCount:    len(m.tasks),
		MaxConcurrent: m.maxConcurrent,
	}
	var activeTotalBytes, activeDownloadedBytes int64
	for _, te := range m.tasks {
		switch Status(te.rec.Status) {
		case StatusDownloading, StatusVerifying:
			sum.ActiveCount++
			sum.TotalSpeedBytesPerSec += te.speedBps
			activeTotalBytes += te.rec.TotalBytes
			activeDownloadedBytes += te.rec.DownloadedBytes
		case StatusQueued, StatusPending:
			sum.QueuedCount++
			activeTotalBytes += te.rec.TotalBytes
			activeDownloadedBytes += te.rec.DownloadedBytes
		case StatusPaused:
			sum.PausedCount++
			activeTotalBytes += te.rec.TotalBytes
			activeDownloadedBytes += te.rec.DownloadedBytes
		case StatusCompleted:
			sum.CompletedCount++
		case StatusError:
			sum.ErrorCount++
		case StatusCancelled:
			sum.CancelledCount++
		}
	}
	sum.TotalBytes = activeTotalBytes
	sum.DownloadedBytes = activeDownloadedBytes
	if activeTotalBytes > 0 {
		sum.OverallProgress = float64(activeDownloadedBytes) * 100 / float64(activeTotalBytes)
		if sum.OverallProgress > 100 {
			sum.OverallProgress = 100
		}
	} else if sum.ActiveCount == 0 && sum.QueuedCount == 0 && sum.CompletedCount > 0 {
		sum.OverallProgress = 100
	}
	return sum
}

func (m *Manager) persistLocked(te *taskEntry) {
	if m.store != nil {
		_ = m.store.UpsertDownloadTask(te.rec)
	}
}

func (m *Manager) emitSnapshot(te *taskEntry, ev events.EventName) {
	m.mu.Lock()
	v := te.snapshot()
	m.mu.Unlock()
	m.emitEvent(ev, v)
}

func (m *Manager) emitEvent(name events.EventName, payload any) {
	if m.bus != nil {
		m.bus.Publish(name, payload)
	}
}
