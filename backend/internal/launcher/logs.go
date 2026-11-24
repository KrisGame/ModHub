package launcher

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"modhub/internal/events"
	"modhub/internal/security"
)

const (
	maxRingLogLines   = 3000
	maxLogFileBytes   = 20 * 1024 * 1024 // 20 MB cap per session log file
	maxRetainedLogs   = 15               // keep latest 15 ModHub session logs per profile
	logBatchFlushMs   = 80 * time.Millisecond
)

// SessionLogger captures stdout/stderr, redacts secrets, writes bounded rotating log files inside
// <instance>/minecraft/logs/, maintains a memory-bounded ring buffer, and streams batches to EventBus.
type SessionLogger struct {
	sessionID  string
	instanceID string
	logPath    string
	bus        *events.Bus
	redactList []string
	onLoaded   func()

	mu            sync.Mutex
	file          *os.File
	writtenBytes  int64
	truncatedNote bool
	ring          []LogLine
	pendingEmit   []LogLine
	seq           atomic.Int64
	loadedFired   atomic.Bool
	closed        bool
	stopCh        chan struct{}
	flushDone     chan struct{}
}

func NewSessionLogger(sessionID, instanceID, gameDir string, redactTokens []string, bus *events.Bus, onLoaded func()) (*SessionLogger, error) {
	logsDir := filepath.Join(gameDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, fmt.Errorf("не удалось создать папку логов: %w", err)
	}
	rotateOldSessionLogs(logsDir, maxRetainedLogs)

	fname := fmt.Sprintf("modhub-%s-%s.log", time.Now().Format("20060102-150405"), shortSessionSuffix(sessionID))
	logPath := filepath.Join(logsDir, fname)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать файл лога: %w", err)
	}

	cleanRedact := make([]string, 0, len(redactTokens))
	for _, tok := range redactTokens {
		t := strings.TrimSpace(tok)
		if len(t) >= 8 {
			cleanRedact = append(cleanRedact, t)
		}
	}

	sl := &SessionLogger{
		sessionID:  sessionID,
		instanceID: instanceID,
		logPath:    logPath,
		bus:        bus,
		redactList: cleanRedact,
		onLoaded:   onLoaded,
		file:       f,
		ring:       make([]LogLine, 0, 256),
		pendingEmit: make([]LogLine, 0, 32),
		stopCh:     make(chan struct{}),
		flushDone:  make(chan struct{}),
	}
	go sl.runBatchEmitter()
	return sl, nil
}

func shortSessionSuffix(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[len(id)-8:]
}

func rotateOldSessionLogs(logsDir string, keep int) {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return
	}
	type fInfo struct {
		path    string
		modTime int64
	}
	var logs []fInfo
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		n := ent.Name()
		if strings.HasPrefix(n, "modhub-") && strings.HasSuffix(n, ".log") {
			var mt int64
			if fi, err := ent.Info(); err == nil {
				mt = fi.ModTime().UnixNano()
			}
			logs = append(logs, fInfo{path: filepath.Join(logsDir, n), modTime: mt})
		}
	}
	if len(logs) < keep {
		return
	}
	sort.Slice(logs, func(i, j int) bool { return logs[i].modTime > logs[j].modTime })
	for i := keep - 1; i < len(logs); i++ {
		_ = os.Remove(logs[i].path)
	}
}

func (l *SessionLogger) LogFilePath() string { return l.logPath }

func (l *SessionLogger) Sanitize(text string) string {
	s := security.SanitizeLogText(text)
	for _, tok := range l.redactList {
		if strings.Contains(s, tok) {
			s = strings.ReplaceAll(s, tok, "[REDACTED_TOKEN]")
		}
	}
	return s
}

func classifyLogLevel(stream, line string) string {
	u := strings.ToUpper(line)
	switch {
	case strings.Contains(u, "/FATAL]") || strings.Contains(u, "[FATAL]") || strings.Contains(u, "OUTOFMEMORYERROR") || strings.Contains(u, "CRASH REPORT"):
		return "FATAL"
	case strings.Contains(u, "/ERROR]") || strings.Contains(u, "[ERROR]") || strings.Contains(u, "EXCEPTION IN THREAD") || strings.Contains(u, "CAUSED BY:") || strings.Contains(u, "STDERR]"):
		return "ERROR"
	case strings.Contains(u, "/WARN]") || strings.Contains(u, "[WARN]"):
		return "WARN"
	case strings.Contains(u, "/DEBUG]") || strings.Contains(u, "[DEBUG]"):
		return "DEBUG"
	case stream == "stderr":
		return "ERROR"
	default:
		return "INFO"
	}
}

func lineSignalsGameLoaded(line string) bool {
	low := strings.ToLower(line)
	return strings.Contains(low, "sound engine started") ||
		strings.Contains(low, "openal initialized") ||
		(strings.Contains(low, "created:") && strings.Contains(low, "atlas"))
}

func (l *SessionLogger) Append(stream, rawLine string) {
	text := strings.TrimRight(l.Sanitize(rawLine), "\r\n")
	if text == "" {
		return
	}
	if len(text) > 4096 {
		text = text[:4096] + " …[обрезано]"
	}
	nowStr := time.Now().Format("15:04:05")
	level := classifyLogLevel(stream, text)
	entry := LogLine{
		SessionID:  l.sessionID,
		InstanceID: l.instanceID,
		Seq:        l.seq.Add(1),
		Timestamp:  nowStr,
		Stream:     stream,
		Level:      level,
		Text:       text,
	}

	if !l.loadedFired.Load() && lineSignalsGameLoaded(text) {
		if l.loadedFired.CompareAndSwap(false, true) && l.onLoaded != nil {
			go l.onLoaded()
		}
	}

	l.mu.Lock()
	if len(l.ring) >= maxRingLogLines {
		// Drop oldest 10% to keep append amortized O(1) and memory strictly bounded
		drop := maxRingLogLines / 10
		copy(l.ring, l.ring[drop:])
		l.ring = l.ring[:len(l.ring)-drop]
	}
	l.ring = append(l.ring, entry)
	l.pendingEmit = append(l.pendingEmit, entry)

	if l.file != nil && !l.closed {
		diskLine := fmt.Sprintf("[%s] [%s/%s] %s\n", nowStr, strings.ToUpper(stream), level, text)
		if l.writtenBytes+int64(len(diskLine)) <= maxLogFileBytes {
			n, _ := l.file.WriteString(diskLine)
			l.writtenBytes += int64(n)
		} else if !l.truncatedNote {
			l.truncatedNote = true
			_, _ = l.file.WriteString(fmt.Sprintf("[%s] [LAUNCHER/WARN] Достигнут лимит размера лог-файла (%d МБ); запись в файл приостановлена.\n", nowStr, maxLogFileBytes/(1024*1024)))
		}
	}
	l.mu.Unlock()
}

func (l *SessionLogger) PumpReader(stream string, r io.Reader) {
	br := bufio.NewReaderSize(r, 32*1024)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			l.Append(stream, line)
		}
		if err != nil {
			return
		}
	}
}

func (l *SessionLogger) runBatchEmitter() {
	defer close(l.flushDone)
	ticker := time.NewTicker(logBatchFlushMs)
	defer ticker.Stop()
	flush := func() {
		l.mu.Lock()
		if len(l.pendingEmit) == 0 {
			l.mu.Unlock()
			return
		}
		batch := l.pendingEmit
		l.pendingEmit = make([]LogLine, 0, 32)
		l.mu.Unlock()
		if l.bus != nil {
			l.bus.Publish(events.EventGameLog, map[string]any{
				"session_id":  l.sessionID,
				"instance_id": l.instanceID,
				"lines":       batch,
			})
		}
	}
	for {
		select {
		case <-ticker.C:
			flush()
		case <-l.stopCh:
			flush()
			return
		}
	}
}

func (l *SessionLogger) SnapshotLines(limit int) []LogLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit <= 0 || limit >= len(l.ring) {
		return append([]LogLine(nil), l.ring...)
	}
	return append([]LogLine(nil), l.ring[len(l.ring)-limit:]...)
}

func (l *SessionLogger) Close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	close(l.stopCh)
	if l.file != nil {
		_ = l.file.Sync()
		_ = l.file.Close()
		l.file = nil
	}
	l.mu.Unlock()
	<-l.flushDone
}

// ScanCrashReports lists crash reports in <gameDir>/crash-reports/ newest first.
// If sinceUnix > 0, only returns reports modified at or after sinceUnix.
func ScanCrashReports(gameDir string, sinceUnix int64) []CrashReportInfo {
	dir := filepath.Join(gameDir, "crash-reports")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []CrashReportInfo{}
	}
	out := make([]CrashReportInfo, 0, 8)
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		n := ent.Name()
		if !strings.HasSuffix(strings.ToLower(n), ".txt") {
			continue
		}
		fi, err := ent.Info()
		if err != nil {
			continue
		}
		mt := fi.ModTime().Unix()
		if sinceUnix > 0 && mt < sinceUnix {
			continue
		}
		full := filepath.Join(dir, n)
		out = append(out, CrashReportInfo{
			Filename: n,
			FullPath: full,
			ModTime:  mt,
			Size:     fi.Size(),
			Summary:  extractCrashSummary(full),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	if len(out) > 25 {
		out = out[:25]
	}
	return out
}

func extractCrashSummary(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, 8192))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "Description:") {
			return security.SanitizeLogText(strings.TrimSpace(strings.TrimPrefix(line, "Description:")))
		}
	}
	return ""
}
