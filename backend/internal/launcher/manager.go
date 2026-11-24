package launcher

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"modhub/internal/accounts"
	"modhub/internal/domain"
	"modhub/internal/events"
	"modhub/internal/instances"
	"modhub/internal/mcversion"
	"modhub/internal/modloader"
	javart "modhub/internal/runtime"
	"modhub/internal/security"
)

var (
	ErrAlreadyRunning    = errors.New("этот игровой профиль уже запущен или запускается")
	ErrSessionNotFound   = errors.New("активная игровая сессия не найдена")
	ErrPreflightFailed   = errors.New("проверка перед запуском не пройдена")
	ErrJavaIncompatible  = errors.New("выбранная версия Java несовместима с этой версией Minecraft")
)

type runningEntry struct {
	session    GameSession
	cmd        *exec.Cmd
	logger     *SessionLogger
	javaRTID   string
	stopManual bool
}

// Manager coordinates preflight verification, native extraction, launch command building,
// real Java process execution, live log streaming, stop/force-kill, and session history.
type Manager struct {
	sharedDir string
	instMgr   *instances.Manager
	mcMgr     *mcversion.Manager
	mlMgr     *modloader.Manager
	javaMgr   *javart.Manager
	acctMgr   *accounts.Manager
	store     Store
	bus       *events.Bus

	mu              sync.Mutex
	running         map[string]*runningEntry // instanceID -> active/running session
	lastSessions    map[string]GameSession   // instanceID -> most recent session (even after exit)
	loggersByInst   map[string]*SessionLogger
	javaProbeFn     javart.ProbeFunc
	gracefulWait    time.Duration
	bgCtx           context.Context
	bgCancel        context.CancelFunc
	launchWg        sync.WaitGroup
	closeOnce       sync.Once
}

func NewManager(
	sharedDir string,
	instMgr *instances.Manager,
	mcMgr *mcversion.Manager,
	mlMgr *modloader.Manager,
	javaMgr *javart.Manager,
	acctMgr *accounts.Manager,
	store Store,
	bus *events.Bus,
) *Manager {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	m := &Manager{
		sharedDir:     filepath.Clean(sharedDir),
		instMgr:       instMgr,
		mcMgr:         mcMgr,
		mlMgr:         mlMgr,
		javaMgr:       javaMgr,
		acctMgr:       acctMgr,
		store:         store,
		bus:           bus,
		running:       make(map[string]*runningEntry),
		lastSessions:  make(map[string]GameSession),
		loggersByInst: make(map[string]*SessionLogger),
		javaProbeFn:   javart.ProbeJavaExecutable,
		gracefulWait:  6 * time.Second,
		bgCtx:         bgCtx,
		bgCancel:      bgCancel,
	}
	m.reconcileStoredSessions()
	return m
}

func (m *Manager) SetJavaProbeFunc(fn javart.ProbeFunc) {
	if fn != nil {
		m.mu.Lock()
		m.javaProbeFn = fn
		m.mu.Unlock()
	}
}

func (m *Manager) SetGracefulWait(d time.Duration) {
	if d > 0 {
		m.mu.Lock()
		m.gracefulWait = d
		m.mu.Unlock()
	}
}

// Close detaches cleanly on ModHub exit WITHOUT automatically terminating running Minecraft games.
// Running PIDs remain stored in SQLite so the next ModHub launch can inspect whether they are still alive.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		if m.bgCancel != nil {
			m.bgCancel()
		}
		m.launchWg.Wait()
		m.mu.Lock()
		for _, re := range m.running {
			if re.logger != nil {
				re.logger.Append("launcher", "ModHub закрывается; игровой процесс Minecraft продолжает работу независимо.")
			}
			m.persistSessionLocked(re.session)
		}
		m.mu.Unlock()
	})
}

func (m *Manager) HasRunningGames() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, re := range m.running {
		if re.session.State.IsActive() {
			return true
		}
	}
	return false
}

func (m *Manager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, re := range m.running {
		if re.session.State.IsActive() {
			n++
		}
	}
	return n
}

func (m *Manager) reconcileStoredSessions() {
	if m.store == nil {
		return
	}
	recs, err := m.store.ListGameSessions("", 100)
	if err != nil {
		return
	}
	now := time.Now().Unix()
	for _, rec := range recs {
		sess := recordToSession(rec)
		if sess.State.IsActive() {
			if sess.PID > 0 && isProcessAlive(sess.PID) {
				// Process survived a ModHub restart -> keep visible as detached running session
				sess.State = StateRunning
				sess.StageMessage = fmt.Sprintf("Процесс продолжает работу после перезапуска ModHub (PID %d)", sess.PID)
				m.running[sess.InstanceID] = &runningEntry{session: sess}
				m.lastSessions[sess.InstanceID] = sess
				go m.monitorDetachedPID(sess.InstanceID, sess.ID, sess.PID)
				continue
			}
			// Process ended while ModHub was closed
			sess.State = StateExited
			sess.EndedAt = now
			if sess.ExitReason == "" {
				sess.ExitReason = "Игровой процесс завершился, пока ModHub был закрыт"
			}
			_ = m.store.UpsertGameSession(sessionToRecord(sess))
		}
		if _, exists := m.lastSessions[sess.InstanceID]; !exists {
			m.lastSessions[sess.InstanceID] = sess
		}
	}
}

func (m *Manager) monitorDetachedPID(instanceID, sessionID string, pid int) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.bgCtx.Done():
			return
		case <-ticker.C:
			if !isProcessAlive(pid) {
				// Защита от ложного срабатывания при выходе ОС Windows из спящего режима (Sleep/Hibernate)
				time.Sleep(500 * time.Millisecond)
				if isProcessAlive(pid) {
					continue
				}
				m.mu.Lock()
				re, ok := m.running[instanceID]
				if ok && re.session.ID == sessionID {
					re.session.State = StateExited
					re.session.EndedAt = time.Now().Unix()
					re.session.ExitReason = "Отсоединённый процесс Minecraft завершился"
					snap := re.session
					delete(m.running, instanceID)
					m.lastSessions[instanceID] = snap
					m.persistSessionLocked(snap)
					m.mu.Unlock()
					m.emitStatus(snap, events.EventGameExited)
					return
				}
				m.mu.Unlock()
				return
			}
		}
	}
}

func generateSessionID() string {
	var b [5]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("sess_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(b[:]))
}

func (m *Manager) GetSnapshot() LauncherSnapshot {
	m.mu.Lock()
	seen := map[string]bool{}
	sessions := make([]GameSession, 0, len(m.running)+len(m.lastSessions))
	for instID, re := range m.running {
		sessions = append(sessions, re.session)
		seen[instID] = true
	}
	for instID, sess := range m.lastSessions {
		if !seen[instID] {
			sessions = append(sessions, sess)
		}
	}
	m.mu.Unlock()

	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].State.IsActive() != sessions[j].State.IsActive() {
			return sessions[i].State.IsActive()
		}
		return sessions[i].StartedAt > sessions[j].StartedAt
	})
	return LauncherSnapshot{
		Sessions: sessions,
		System:   QuerySystemMemory(),
	}
}

func (m *Manager) GetInstanceSession(instanceID string) *GameSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	if re, ok := m.running[instanceID]; ok {
		cp := re.session
		return &cp
	}
	if sess, ok := m.lastSessions[instanceID]; ok {
		cp := sess
		return &cp
	}
	return nil
}

func (m *Manager) GetSessionHistory(instanceID string, limit int) []GameSession {
	if m.store == nil {
		return []GameSession{}
	}
	recs, err := m.store.ListGameSessions(instanceID, limit)
	if err != nil {
		return []GameSession{}
	}
	out := make([]GameSession, 0, len(recs))
	for _, r := range recs {
		out = append(out, recordToSession(r))
	}
	return out
}

func (m *Manager) GetInstanceLogs(instanceID string, limit int) []LogLine {
	m.mu.Lock()
	logger := m.loggersByInst[instanceID]
	m.mu.Unlock()
	if logger == nil {
		return []LogLine{}
	}
	return logger.SnapshotLines(limit)
}

func (m *Manager) ListCrashReports(instanceID string) ([]CrashReportInfo, error) {
	inst, err := m.instMgr.Get(instanceID)
	if err != nil {
		return nil, err
	}
	return ScanCrashReports(inst.GameDir, 0), nil
}

func (m *Manager) AnalyzeInstanceCrash(instanceID string) (*CrashDiagnosis, error) {
	inst, err := m.instMgr.Get(instanceID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	sess, hasSess := m.lastSessions[instanceID]
	logger := m.loggersByInst[instanceID]
	m.mu.Unlock()

	if hasSess && sess.CrashDiagnosis != nil {
		return sess.CrashDiagnosis, nil
	}
	var lines []LogLine
	if logger != nil {
		lines = logger.SnapshotLines(300)
	}
	crashes := ScanCrashReports(inst.GameDir, 0)
	exitCode := 0
	if hasSess {
		exitCode = sess.ExitCode
	} else if len(crashes) > 0 {
		exitCode = 1
	}
	return AnalyzeCrash(exitCode, inst.GameDir, lines, crashes), nil
}

// Preflight runs non-destructive validation of profile, installed version, libraries, assets, Java,
// Microsoft account + Java Edition license, RAM, and free disk space.
func (m *Manager) Preflight(ctx context.Context, req LaunchRequest) PreflightReport {
	sysMem := QuerySystemMemory()
	rep := PreflightReport{
		InstanceID:       req.InstanceID,
		SystemRAMMB:      sysMem.TotalRAMMB,
		RecommendedMaxMB: sysMem.RecommendedMaxMB,
	}
	if m.instMgr == nil {
		rep.Errors = append(rep.Errors, "Instance Manager не инициализирован")
		return rep
	}
	inst, err := m.instMgr.Get(req.InstanceID)
	if err != nil {
		rep.Errors = append(rep.Errors, "Игровой профиль не найден: "+err.Error())
		return rep
	}
	if freeMB, ok := availableDiskMB(inst.GameDir); ok {
		rep.FreeDiskMB = freeMB
		if freeMB < minRecommendedDiskMB {
			rep.Errors = append(rep.Errors, fmt.Sprintf("Недостаточно свободного места на диске (%d МБ свободно, требуется минимум %d МБ)", freeMB, minRecommendedDiskMB))
		}
	}
	if sysMem.TotalRAMMB > 0 && inst.MemoryMaxMB > int(sysMem.TotalRAMMB-512) {
		rep.Errors = append(rep.Errors, fmt.Sprintf("Выделено %d МБ ОЗУ при доступных %d МБ на компьютере", inst.MemoryMaxMB, sysMem.TotalRAMMB))
	} else if sysMem.RecommendedMaxMB > 0 && inst.MemoryMaxMB > sysMem.RecommendedMaxMB {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("Выделенная память (%d МБ) выше рекомендуемой (%d МБ)", inst.MemoryMaxMB, sysMem.RecommendedMaxMB))
	}

	verID, mcVer, loader, _, err := m.resolveTargetVersion(inst, req.InstalledVersionID)
	if err != nil {
		rep.Errors = append(rep.Errors, err.Error())
	} else {
		rep.VersionID = verID
		rep.MCVersion = mcVer
		rep.Loader = loader
		if m.mcMgr != nil {
			vr := m.mcMgr.VerifyVanilla(mcVer)
			if !vr.Valid {
				rep.Errors = append(rep.Errors, "Файлы Minecraft "+mcVer+" неполные: "+strings.Join(vr.Errors, "; "))
			}
		}
		if loader != "" && loader != "vanilla" && m.mlMgr != nil {
			if prov, ok := m.mlMgr.Provider(modloader.LoaderType(loader)); ok {
				lr := prov.Verify(ctx, verID)
				if !lr.Valid {
					rep.Errors = append(rep.Errors, "Файлы загрузчика "+loader+" неполные: "+strings.Join(lr.Errors, "; "))
				}
			}
		}
	}

	// Java check
	mergedMeta, _, metaErr := ResolveEffectiveMetadata(m.mcMgr, verID)
	if metaErr == nil && mergedMeta != nil {
		_, javaSummary, _, jErr := m.resolveAndVerifyJava(ctx, inst, mergedMeta, loader)
		rep.JavaSummary = javaSummary
		if jErr != nil {
			rep.Errors = append(rep.Errors, jErr.Error())
		}
	}

	// Account check (token-free)
	acctID := strings.TrimSpace(req.AccountID)
	if acctID == "" {
		acctID = strings.TrimSpace(inst.LaunchSettings.AccountID)
	}
	if m.acctMgr == nil {
		rep.Errors = append(rep.Errors, "Microsoft Account Manager не инициализирован")
	} else {
		snap := m.acctMgr.GetSnapshot()
		var chosen *accounts.AccountView
		for i := range snap.Accounts {
			if (acctID != "" && snap.Accounts[i].ID == acctID) || (acctID == "" && snap.Accounts[i].Active) {
				cp := snap.Accounts[i]
				chosen = &cp
				break
			}
		}
		if chosen == nil {
			rep.Errors = append(rep.Errors, "Не выбран аккаунт — выберите или добавьте профиль в разделе «Аккаунты»")
		} else {
			rep.AccountSummary = chosen.MinecraftUsername
			if rep.AccountSummary == "" {
				rep.AccountSummary = chosen.XboxGamertag
			}
			if chosen.AccountType == accounts.AccountTypeOffline {
				rep.AccountSummary += " (Офлайн)"
			} else {
				if chosen.AuthStatus == accounts.AuthUnauthorized || chosen.AuthStatus == accounts.AuthReauthRequired {
					rep.Errors = append(rep.Errors, "Выбранный аккаунт Microsoft требует повторного входа")
				} else if chosen.LicenseStatus != accounts.LicenseOwned {
					rep.Errors = append(rep.Errors, "Для выбранного аккаунта не подтверждена лицензия Minecraft Java Edition")
				}
			}
		}
	}

	rep.OK = len(rep.Errors) == 0
	return rep
}

// LaunchAsync starts the full preflight + native preparation + real Java Minecraft launch in background.
func (m *Manager) LaunchAsync(req LaunchRequest) (GameSession, error) {
	if strings.TrimSpace(req.InstanceID) == "" {
		return GameSession{}, errors.New("не указан идентификатор профиля")
	}
	inst, err := m.instMgr.Get(req.InstanceID)
	if err != nil {
		return GameSession{}, err
	}

	m.mu.Lock()
	if existing, busy := m.running[req.InstanceID]; busy && existing.session.State.IsActive() {
		m.mu.Unlock()
		return GameSession{}, ErrAlreadyRunning
	}
	now := time.Now().Unix()
	sess := GameSession{
		ID:           generateSessionID(),
		InstanceID:   inst.ID,
		InstanceName: inst.Name,
		MCVersion:    inst.MCVersion,
		Loader:       string(inst.InstanceType),
		LoaderVersion: inst.LoaderVersion,
		State:        StateCheckingProfile,
		StageMessage: "Проверка профиля и установленных файлов...",
		StartedAt:    now,
	}
	entry := &runningEntry{session: sess}
	m.running[inst.ID] = entry
	m.lastSessions[inst.ID] = sess
	m.persistSessionLocked(sess)
	m.mu.Unlock()

	m.emitStatus(sess, events.EventGameStatus)

	m.launchWg.Add(1)
	go func() {
		defer m.launchWg.Done()
		m.executeLaunchLifecycle(entry, inst, req)
	}()
	return sess, nil
}

// LaunchTestProcessSync starts a real OS subprocess (used for automated process lifecycle tests
// without requiring a full Mojang client JAR or Microsoft OAuth account).
func (m *Manager) LaunchTestProcessSync(instanceID string, exe string, args []string, workDir string, redactTokens []string) (GameSession, error) {
	m.mu.Lock()
	if existing, busy := m.running[instanceID]; busy && existing.session.State.IsActive() {
		m.mu.Unlock()
		return GameSession{}, ErrAlreadyRunning
	}
	now := time.Now().Unix()
	sess := GameSession{
		ID:           generateSessionID(),
		InstanceID:   instanceID,
		InstanceName: instanceID,
		VersionID:    "test-process",
		MCVersion:    "test",
		Loader:       "vanilla",
		JavaPath:     exe,
		State:        StateStarting,
		StageMessage: "Запуск тестового процесса...",
		StartedAt:    now,
	}
	entry := &runningEntry{session: sess}
	m.running[instanceID] = entry
	m.lastSessions[instanceID] = sess
	m.mu.Unlock()

	logger, err := NewSessionLogger(sess.ID, instanceID, workDir, redactTokens, m.bus, func() {
		m.markGameLoaded(instanceID, sess.ID)
	})
	if err != nil {
		m.failLaunch(entry, "Не удалось создать лог-файл", err)
		return entry.session, err
	}
	entry.logger = logger
	m.mu.Lock()
	entry.session.LogFilePath = logger.LogFilePath()
	m.loggersByInst[instanceID] = logger
	m.mu.Unlock()

	if err := m.spawnAndMonitor(entry, exe, args, workDir); err != nil {
		return entry.session, err
	}
	m.mu.Lock()
	out := entry.session
	m.mu.Unlock()
	return out, nil
}

func (m *Manager) executeLaunchLifecycle(entry *runningEntry, inst instances.Instance, req LaunchRequest) {
	ctx := m.bgCtx
	if ctx == nil {
		ctx = context.Background()
	}

	setStage := func(st ProcessState, msg string) {
		m.mu.Lock()
		entry.session.State = st
		entry.session.StageMessage = msg
		snap := entry.session
		m.lastSessions[inst.ID] = snap
		m.persistSessionLocked(snap)
		m.mu.Unlock()
		m.emitStatus(snap, events.EventGameStatus)
	}

	// 1. Profile & installed version check
	setStage(StateCheckingProfile, "Проверка профиля, версии Minecraft и загрузчика...")
	if freeMB, ok := availableDiskMB(inst.GameDir); ok && freeMB < minRecommendedDiskMB {
		m.failLaunch(entry, fmt.Sprintf("Недостаточно места на диске (%d МБ свободно, нужно минимум %d МБ)", freeMB, minRecommendedDiskMB), nil)
		return
	}

	verID, mcVer, loader, loaderVer, err := m.resolveTargetVersion(inst, req.InstalledVersionID)
	if err != nil {
		m.failLaunch(entry, "Minecraft не установлен в этом профиле", err)
		return
	}
	m.mu.Lock()
	entry.session.VersionID = verID
	entry.session.MCVersion = mcVer
	entry.session.Loader = loader
	entry.session.LoaderVersion = loaderVer
	m.mu.Unlock()

	// Verify Vanilla & Modloader integrity (never duplicate downloader; direct user to Version Manager if files missing)
	vRep := m.mcMgr.VerifyVanilla(mcVer)
	if !vRep.Valid {
		m.failLaunch(entry, "Отсутствуют или повреждены файлы Minecraft "+mcVer+" (переустановите версию во вкладке «Версия и загрузчик»)", errors.New(strings.Join(vRep.Errors, "; ")))
		return
	}
	if loader != "" && loader != "vanilla" && m.mlMgr != nil {
		if prov, ok := m.mlMgr.Provider(modloader.LoaderType(loader)); ok {
			lRep := prov.Verify(ctx, verID)
			if !lRep.Valid {
				m.failLaunch(entry, "Отсутствуют или повреждены файлы загрузчика "+loader, errors.New(strings.Join(lRep.Errors, "; ")))
				return
			}
		}
	}

	mergedMeta, baseMCVer, err := ResolveEffectiveMetadata(m.mcMgr, verID)
	if err != nil {
		m.failLaunch(entry, "Ошибка чтения метаданных установленной версии", err)
		return
	}

	// 2. Authentication & Java Edition License check (strictly Go-side, zero tokens in UI/logs)
	setStage(StateCheckingAuth, "Проверка авторизации и подготовка профиля игрока...")
	preferredAcctID := strings.TrimSpace(req.AccountID)
	if preferredAcctID == "" {
		preferredAcctID = strings.TrimSpace(inst.LaunchSettings.AccountID)
	}
	creds, err := m.acctMgr.ResolveLaunchCredentials(ctx, preferredAcctID)
	if err != nil {
		userMsg := "Ошибка авторизации"
		switch {
		case errors.Is(err, accounts.ErrNoActiveAccount):
			userMsg = "Не выбран аккаунт — выберите или создайте профиль в разделе «Аккаунты»"
		case errors.Is(err, accounts.ErrLicenseNotConfirmed):
			userMsg = "Нет подтверждённой лицензии Minecraft Java Edition на выбранном аккаунте"
		case errors.Is(err, accounts.ErrReauthRequired):
			userMsg = "Сессия Microsoft истекла — выполните повторный вход в разделе «Аккаунты»"
		case errors.Is(err, accounts.ErrMCProfileNotCreated):
			userMsg = "Профиль Minecraft Java Edition не создан для выбранного аккаунта"
		}
		m.failLaunch(entry, userMsg, err)
		return
	}
	// Clear token copy from local struct when launch preparation finishes
	defer func() { creds.MinecraftAccessToken = "" }()

	m.mu.Lock()
	entry.session.AccountID = creds.AccountID
	entry.session.Username = creds.MinecraftUsername
	m.mu.Unlock()

	// 3. Java Runtime resolution & strict compatibility/architecture verification
	setStage(StatePreparingFiles, "Проверка совместимости Java Runtime и подготовка нативных библиотек...")
	javaExe, javaVerStr, javaRTID, err := m.resolveAndVerifyJava(ctx, inst, mergedMeta, loader)
	if err != nil {
		m.failLaunch(entry, "Проблема с Java Runtime", err)
		return
	}
	m.mu.Lock()
	entry.session.JavaPath = javaExe
	entry.session.JavaVersion = javaVerStr
	entry.javaRTID = javaRTID
	m.mu.Unlock()
	if javaRTID != "" && m.javaMgr != nil {
		m.javaMgr.AcquireInUse(javaRTID)
	}

	// 4. Prepare platform native libraries safely
	plat := m.mcMgr.Platform()
	filteredLibs := mcversion.FilterLibrariesForPlatform(mergedMeta, plat)
	nativesDir, _, _, err := PrepareNatives(m.sharedDir, mergedMeta.ID, filteredLibs, plat)
	if err != nil {
		m.releaseJavaLock(entry)
		m.failLaunch(entry, "Не удалось подготовить нативные библиотеки (natives)", err)
		return
	}

	// 5. Build launch specification (strictly from real metadata, shell-free argv array)
	setStage(StateBuildingCommand, "Формирование classpath и аргументов запуска Minecraft...")
	sysMem := QuerySystemMemory()
	spec, err := BuildLaunchSpec(m.sharedDir, inst, mergedMeta, baseMCVer, nativesDir, javaExe, creds, plat, sysMem)
	if err != nil {
		m.releaseJavaLock(entry)
		m.failLaunch(entry, "Ошибка формирования аргументов запуска", err)
		return
	}

	// 6. Initialize SessionLogger (explicitly redacts active MC token if it ever appears in stdout/stderr!)
	logger, err := NewSessionLogger(entry.session.ID, inst.ID, inst.GameDir, []string{creds.MinecraftAccessToken}, m.bus, func() {
		m.markGameLoaded(inst.ID, entry.session.ID)
	})
	if err != nil {
		m.releaseJavaLock(entry)
		m.failLaunch(entry, "Не удалось инициализировать файл игровых логов", err)
		return
	}
	entry.logger = logger
	m.mu.Lock()
	entry.session.LogFilePath = logger.LogFilePath()
	m.loggersByInst[inst.ID] = logger
	m.mu.Unlock()

	logger.Append("launcher", fmt.Sprintf("Профиль: %s | Версия: %s (MC %s, %s) | Аккаунт: %s", inst.Name, spec.VersionID, spec.MCVersion, spec.Loader, creds.MinecraftUsername))
	logger.Append("launcher", fmt.Sprintf("Java: %s (%s) | Память: -Xms%dM -Xmx%dM | Classpath: %d JAR", spec.JavaExe, javaVerStr, spec.MemoryMinMB, spec.MemoryMaxMB, spec.ClasspathCount))
	logger.Append("launcher", fmt.Sprintf("MainClass: %s | Natives: %s", spec.MainClass, spec.NativesDir))
	for _, w := range spec.Warnings {
		logger.Append("launcher", "WARN: "+w)
	}

	// 7. Spawn real Java process (direct os/exec, NO cmd.exe!)
	setStage(StateStarting, "Запуск процесса Java / Minecraft...")
	if err := m.spawnAndMonitor(entry, spec.JavaExe, spec.RawArgs, spec.WorkDir); err != nil {
		// Wipe raw args immediately
		for i := range spec.RawArgs {
			spec.RawArgs[i] = ""
		}
		m.releaseJavaLock(entry)
		return
	}
	for i := range spec.RawArgs {
		spec.RawArgs[i] = ""
	}

	// Mark profile last_played_at only after real process started!
	if m.instMgr != nil {
		_ = m.instMgr.MarkLastPlayed(inst.ID, time.Now().Unix())
	}
}

func (m *Manager) spawnAndMonitor(entry *runningEntry, exe string, rawArgs []string, workDir string) error {
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		m.failLaunch(entry, "Рабочая директория профиля недоступна", err)
		return err
	}
	cmd := exec.Command(exe, rawArgs...)
	cmd.Dir = workDir
	configureGameCmd(cmd)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		m.failLaunch(entry, "Не удалось подключить stdout процесса", err)
		return err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		m.failLaunch(entry, "Не удалось подключить stderr процесса", err)
		return err
	}

	startTime := time.Now().Unix()
	if err := cmd.Start(); err != nil {
		m.failLaunch(entry, "Не удалось запустить процесс Java", err)
		return err
	}

	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}

	m.mu.Lock()
	entry.cmd = cmd
	entry.session.PID = pid
	entry.session.State = StateRunning
	entry.session.StageMessage = fmt.Sprintf("Процесс запущен (PID %d)", pid)
	entry.session.StartedAt = startTime
	snap := entry.session
	m.lastSessions[entry.session.InstanceID] = snap
	m.persistSessionLocked(snap)
	m.mu.Unlock()

	if entry.logger != nil {
		entry.logger.Append("launcher", fmt.Sprintf("Процесс запущен (PID %d).", pid))
	}
	m.emitStatus(snap, events.EventGameStatus)

	// Stream stdout & stderr concurrently and wait for process exit
	var pipeWg sync.WaitGroup
	pipeWg.Add(2)
	go func() {
		defer pipeWg.Done()
		if entry.logger != nil {
			entry.logger.PumpReader("stdout", stdoutPipe)
		}
	}()
	go func() {
		defer pipeWg.Done()
		if entry.logger != nil {
			entry.logger.PumpReader("stderr", stderrPipe)
		}
	}()

	go func() {
		pipeWg.Wait()
		waitErr := cmd.Wait()
		exitCode := 0
		if waitErr != nil {
			var ee *exec.ExitError
			if errors.As(waitErr, &ee) {
				exitCode = ee.ExitCode()
			} else {
				exitCode = -1
			}
		}

		endedAt := time.Now().Unix()
		newCrashes := ScanCrashReports(workDir, startTime-2)

		var logSnapshot []LogLine
		if entry.logger != nil {
			logSnapshot = entry.logger.SnapshotLines(300)
		}

		m.mu.Lock()
		manualStop := entry.stopManual
		entry.session.ExitCode = exitCode
		entry.session.EndedAt = endedAt
		entry.session.CrashReports = newCrashes

		switch {
		case manualStop:
			entry.session.State = StateExited
			entry.session.ExitReason = "Остановлено пользователем из ModHub"
			entry.session.StageMessage = entry.session.ExitReason
		case exitCode == 0 && len(newCrashes) == 0:
			entry.session.State = StateExited
			entry.session.ExitReason = "Игра завершилась штатно (код 0)"
			entry.session.StageMessage = entry.session.ExitReason
		default:
			entry.session.State = StateError
			diag := AnalyzeCrash(exitCode, workDir, logSnapshot, newCrashes)
			entry.session.CrashDiagnosis = diag
			if len(newCrashes) > 0 {
				entry.session.ExitReason = fmt.Sprintf("Minecraft аварийно завершился (код %d) — %s", exitCode, newCrashes[0].Filename)
				if newCrashes[0].Summary != "" {
					entry.session.Error = newCrashes[0].Summary
				}
			} else if diag != nil && diag.Title != "" {
				entry.session.ExitReason = diag.Title
				entry.session.Error = diag.Cause
			} else {
				entry.session.ExitReason = fmt.Sprintf("Minecraft аварийно завершился с кодом %d", exitCode)
			}
			if entry.session.Error == "" {
				entry.session.Error = entry.session.ExitReason
			}
			entry.session.StageMessage = entry.session.ExitReason
		}

		if entry.logger != nil {
			entry.logger.Append("launcher", fmt.Sprintf("Процесс завершён (код %d): %s", exitCode, entry.session.ExitReason))
			entry.logger.Close()
		}

		finalSnap := entry.session
		delete(m.running, entry.session.InstanceID)
		m.lastSessions[entry.session.InstanceID] = finalSnap
		m.persistSessionLocked(finalSnap)
		m.mu.Unlock()

		m.releaseJavaLock(entry)
		m.emitStatus(finalSnap, events.EventGameExited)
		m.emitStatus(finalSnap, events.EventGameStatus)
		if len(newCrashes) > 0 && m.bus != nil {
			m.bus.Publish(events.EventGameCrashDetected, map[string]any{
				"instance_id":   finalSnap.InstanceID,
				"session_id":    finalSnap.ID,
				"crash_reports": newCrashes,
			})
		}
	}()

	return nil
}

// Stop requests graceful shutdown of a running game process, escalating to force kill if needed.
func (m *Manager) Stop(instanceID string, force bool) error {
	m.mu.Lock()
	re, ok := m.running[instanceID]
	if !ok || !re.session.State.IsActive() {
		m.mu.Unlock()
		return ErrSessionNotFound
	}
	re.stopManual = true
	re.session.State = StateStopping
	re.session.StageMessage = "Завершение игрового процесса..."
	if force {
		re.session.StageMessage = "Принудительное завершение игрового процесса..."
	}
	snap := re.session
	var proc *os.Process
	if re.cmd != nil {
		proc = re.cmd.Process
	}
	pid := re.session.PID
	logger := re.logger
	waitDur := m.gracefulWait
	m.mu.Unlock()

	m.emitStatus(snap, events.EventGameStatus)
	if logger != nil {
		if force {
			logger.Append("launcher", "Запрошено принудительное завершение игрового процесса...")
		} else {
			logger.Append("launcher", "Запрошено корректное завершение игрового процесса...")
		}
	}

	if proc == nil && pid > 0 {
		proc, _ = os.FindProcess(pid)
	}
	if proc == nil {
		return ErrSessionNotFound
	}
	if force {
		return forceKillProcess(proc)
	}
	if err := requestGracefulStop(proc); err != nil {
		return forceKillProcess(proc)
	}

	// Auto-escalate to force kill if process ignores graceful close after gracefulWait
	go func(sessionID string, targetPID int, p *os.Process) {
		time.Sleep(waitDur)
		m.mu.Lock()
		cur, stillRunning := m.running[instanceID]
		matches := stillRunning && cur.session.ID == sessionID && cur.session.State == StateStopping
		m.mu.Unlock()
		if matches && isProcessAlive(targetPID) {
			if logger != nil {
				logger.Append("launcher", "Процесс не завершился вовремя — выполняется принудительная остановка.")
			}
			_ = forceKillProcess(p)
		}
	}(snap.ID, pid, proc)
	return nil
}

func (m *Manager) markGameLoaded(instanceID, sessionID string) {
	m.mu.Lock()
	re, ok := m.running[instanceID]
	if !ok || re.session.ID != sessionID || re.session.GameLoaded {
		m.mu.Unlock()
		return
	}
	re.session.GameLoaded = true
	re.session.StageMessage = fmt.Sprintf("Игра работает (PID %d)", re.session.PID)
	snap := re.session
	m.lastSessions[instanceID] = snap
	m.mu.Unlock()
	m.emitStatus(snap, events.EventGameStatus)
}

func (m *Manager) failLaunch(entry *runningEntry, userMsg string, cause error) {
	tech := ""
	if cause != nil {
		tech = security.SanitizeLogText(cause.Error())
	}
	fullMsg := security.SanitizeLogText(userMsg)
	if tech != "" && !strings.Contains(fullMsg, tech) {
		fullMsg = fullMsg + ": " + tech
	}
	m.mu.Lock()
	entry.session.State = StateError
	entry.session.Error = fullMsg
	entry.session.TechnicalDetails = tech
	entry.session.ExitReason = userMsg
	entry.session.StageMessage = userMsg
	entry.session.EndedAt = time.Now().Unix()
	if entry.logger != nil {
		entry.logger.Append("launcher", "ERROR: "+fullMsg)
		entry.logger.Close()
	}
	snap := entry.session
	delete(m.running, entry.session.InstanceID)
	m.lastSessions[entry.session.InstanceID] = snap
	m.persistSessionLocked(snap)
	m.mu.Unlock()
	m.emitStatus(snap, events.EventGameStatus)
}

func (m *Manager) releaseJavaLock(entry *runningEntry) {
	m.mu.Lock()
	rtid := entry.javaRTID
	entry.javaRTID = ""
	m.mu.Unlock()
	if rtid != "" && m.javaMgr != nil {
		m.javaMgr.ReleaseInUse(rtid)
	}
}

func (m *Manager) resolveTargetVersion(inst instances.Instance, overrideInstalledID string) (string, string, string, string, error) {
	if m.mlMgr != nil {
		installed, err := m.mlMgr.ListInstalledVersions(inst.ID)
		if err == nil && len(installed) > 0 {
			var chosen *modloader.InstalledVersion
			if strings.TrimSpace(overrideInstalledID) != "" {
				for i := range installed {
					if installed[i].ID == overrideInstalledID {
						cp := installed[i]
						chosen = &cp
						break
					}
				}
			}
			if chosen == nil {
				for i := range installed {
					if installed[i].Active {
						cp := installed[i]
						chosen = &cp
						break
					}
				}
			}
			if chosen == nil {
				cp := installed[0]
				chosen = &cp
			}
			return chosen.VersionID, chosen.MCVersion, string(chosen.InstallType), chosen.LoaderVersion, nil
		}
	}
	// Fallback to instance's configured active_version_id / mc_version if metadata exists on disk
	verID := strings.TrimSpace(inst.LaunchSettings.ActiveVersionID)
	if verID == "" {
		verID = strings.TrimSpace(inst.MCVersion)
	}
	if verID == "" {
		return "", "", "", "", errors.New("в профиле не установлена ни одна версия Minecraft — установите её во вкладке «Версия и загрузчик»")
	}
	return verID, inst.MCVersion, string(inst.InstanceType), inst.LoaderVersion, nil
}

func (m *Manager) resolveAndVerifyJava(
	ctx context.Context,
	inst instances.Instance,
	meta *mcversion.VersionMetadata,
	loader string,
) (string, string, string, error) {
	reqIn := javart.RequirementInput{
		MCVersion:     inst.MCVersion,
		Loader:        loader,
		LoaderVersion: inst.LoaderVersion,
	}
	if meta != nil && meta.JavaVersion != nil && meta.JavaVersion.MajorVersion > 0 {
		reqIn.ManifestJavaMajor = meta.JavaVersion.MajorVersion
	}
	req := javart.ResolveRequirement(reqIn)

	m.mu.Lock()
	probe := m.javaProbeFn
	m.mu.Unlock()

	var exePath, rtID string
	switch inst.JavaMode {
	case instances.JavaModeCustom:
		if strings.TrimSpace(inst.JavaPath) == "" {
			return "", "", "", errors.New("выбран ручной путь Java, но путь к java.exe не указан")
		}
		exePath = inst.JavaPath
	case instances.JavaModeManaged:
		if m.javaMgr == nil {
			return "", "", "", errors.New("Java Runtime Manager недоступен")
		}
		snap := m.javaMgr.GetSnapshot(ctx, false)
		for _, rt := range snap.Runtimes {
			if rt.ID == inst.JavaRuntimeID {
				exePath = rt.ExecutablePath
				rtID = rt.ID
				break
			}
		}
		if exePath == "" {
			return "", "", "", errors.New("выбранная версия Java не найдена — проверьте настройки Java в профиле")
		}
	default:
		if m.javaMgr == nil {
			return "", "", "", fmt.Errorf("не найдена Java %d (менеджер Java недоступен)", req.RecommendedMajor)
		}
		_, rec := m.javaMgr.RecommendForMC(reqIn)
		if rec == nil {
			return "", "", "", fmt.Errorf("не найдена совместимая Java %d (%s) — установите её в Настройках → Java Runtime", req.RecommendedMajor, req.Reason)
		}
		exePath = rec.ExecutablePath
		rtID = rec.ID
	}

	// Real probe before launch to verify binary still exists, runs, and matches version & architecture!
	info, err := probe(ctx, exePath)
	if err != nil || !info.Runnable {
		return "", "", "", fmt.Errorf("не удалось проверить исполняемый файл Java (%s): %v", exePath, err)
	}
	if info.MajorVersion < req.MinMajor || (req.MaxMajor > 0 && info.MajorVersion > req.MaxMajor) {
		return "", "", "", fmt.Errorf("%w: найдена Java %d (%s), но для Minecraft %s требуется Java %d", ErrJavaIncompatible, info.MajorVersion, info.Version, inst.MCVersion, req.RecommendedMajor)
	}
	plat := m.mcMgr.Platform()
	if plat.Bitness == "64" && info.Bitness == 32 && inst.MemoryMaxMB > 1536 {
		return "", "", "", fmt.Errorf("%w: выбрана 32-битная Java (%s), которая не поддерживает выделение %d МБ памяти (используйте 64-битную Java)", ErrJavaIncompatible, info.Arch, inst.MemoryMaxMB)
	}
	return exePath, fmt.Sprintf("Java %d (%s, %s)", info.MajorVersion, info.Version, info.Arch), rtID, nil
}

func (m *Manager) persistSessionLocked(sess GameSession) {
	if m.store != nil {
		_ = m.store.UpsertGameSession(sessionToRecord(sess))
	}
}

func (m *Manager) emitStatus(sess GameSession, ev events.EventName) {
	if m.bus != nil {
		m.bus.Publish(ev, sess)
	}
}

func sessionToRecord(s GameSession) domain.GameSessionRecord {
	return domain.GameSessionRecord{
		ID:            s.ID,
		InstanceID:    s.InstanceID,
		InstanceName:  s.InstanceName,
		VersionID:     s.VersionID,
		MCVersion:     s.MCVersion,
		Loader:        s.Loader,
		LoaderVersion: s.LoaderVersion,
		AccountID:     s.AccountID,
		Username:      s.Username,
		JavaPath:      s.JavaPath,
		JavaVersion:   s.JavaVersion,
		PID:           s.PID,
		State:         string(s.State),
		ExitCode:      s.ExitCode,
		ExitReason:    s.ExitReason,
		ErrorMsg:      s.Error,
		LogFilePath:   s.LogFilePath,
		StartedAt:     s.StartedAt,
		EndedAt:       s.EndedAt,
	}
}

func recordToSession(r domain.GameSessionRecord) GameSession {
	return GameSession{
		ID:            r.ID,
		InstanceID:    r.InstanceID,
		InstanceName:  r.InstanceName,
		VersionID:     r.VersionID,
		MCVersion:     r.MCVersion,
		Loader:        r.Loader,
		LoaderVersion: r.LoaderVersion,
		AccountID:     r.AccountID,
		Username:      r.Username,
		JavaPath:      r.JavaPath,
		JavaVersion:   r.JavaVersion,
		PID:           r.PID,
		State:         ProcessState(r.State),
		ExitCode:      r.ExitCode,
		ExitReason:    r.ExitReason,
		Error:         r.ErrorMsg,
		LogFilePath:   r.LogFilePath,
		StartedAt:     r.StartedAt,
		EndedAt:       r.EndedAt,
	}
}
