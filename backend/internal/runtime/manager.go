package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"modhub/internal/domain"
	"modhub/internal/downloads"
	"modhub/internal/events"
	"modhub/internal/netx"
)

var (
	ErrRuntimeNotFound     = errors.New("указанная версия Java не найдена")
	ErrNotManagedRuntime   = errors.New("можно удалять только Java, установленную через ModHub")
	ErrRuntimeInUse        = errors.New("нельзя удалить Java, пока она используется другой операцией")
	ErrAlreadyInstalling   = errors.New("установка этой версии Java уже выполняется")
	ErrComponentNotFound   = errors.New("официальный компонент Java не найден в метаданных Mojang")
)

type ProbeFunc func(ctx context.Context, exePath string) (ProbeInfo, error)

// Manager handles discovery, compatibility selection, official Mojang installation (via Download Manager),
// verification, and safe removal of Java Runtime Environments.
type Manager struct {
	runtimesDir string
	httpClient  *netx.HTTPClient
	dlManager   *downloads.Manager
	store       Store
	bus         *events.Bus

	mu              sync.Mutex
	manifestURL     string
	platformKey     string
	platformArch    string
	probeFn         ProbeFunc
	runtimes        map[string]JavaRuntime
	available       []AvailableRuntime
	lastMetaErr     string
	installing      map[string]context.CancelFunc // component -> cancel func
	inUse           map[string]int                // runtime ID -> active usage count
	closeOnce       sync.Once
	wg              sync.WaitGroup
}

func NewManager(runtimesDir string, httpClient *netx.HTTPClient, dlManager *downloads.Manager, store Store, bus *events.Bus) *Manager {
	platKey, arch := currentMojangPlatform()
	m := &Manager{
		runtimesDir:  filepath.Clean(runtimesDir),
		httpClient:   httpClient,
		dlManager:    dlManager,
		store:        store,
		bus:          bus,
		manifestURL:  OfficialMojangJavaManifestURL,
		platformKey:  platKey,
		platformArch: arch,
		probeFn:      ProbeJavaExecutable,
		runtimes:     make(map[string]JavaRuntime),
		installing:   make(map[string]context.CancelFunc),
		inUse:        make(map[string]int),
	}
	m.cleanupStaleStagingDirs()
	m.loadStoredRuntimes()
	return m
}

func (m *Manager) SetManifestURL(u string) {
	m.mu.Lock()
	m.manifestURL = strings.TrimSpace(u)
	m.mu.Unlock()
}

func (m *Manager) SetPlatform(platKey, arch string) {
	m.mu.Lock()
	m.platformKey = platKey
	m.platformArch = arch
	m.mu.Unlock()
}

func (m *Manager) SetProbeFunc(fn ProbeFunc) {
	if fn != nil {
		m.mu.Lock()
		m.probeFn = fn
		m.mu.Unlock()
	}
}

func (m *Manager) RuntimesDir() string {
	return m.runtimesDir
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		m.mu.Lock()
		for comp, cancel := range m.installing {
			cancel()
			if m.dlManager != nil {
				m.dlManager.CancelGroup("java:" + comp)
			}
		}
		m.mu.Unlock()
		m.wg.Wait()
	})
}

func (m *Manager) cleanupStaleStagingDirs() {
	entries, err := os.ReadDir(m.runtimesDir)
	if err != nil {
		return
	}
	for _, ent := range entries {
		if ent.IsDir() && strings.HasPrefix(ent.Name(), ".staging-") {
			_ = os.RemoveAll(filepath.Join(m.runtimesDir, ent.Name()))
		}
	}
}

func (m *Manager) loadStoredRuntimes() {
	if m.store == nil {
		return
	}
	recs, err := m.store.ListJavaRuntimes()
	if err != nil {
		return
	}
	for _, r := range recs {
		_, statErr := os.Stat(r.ExecutablePath)
		runnable := statErr == nil && r.Verified
		errMsg := ""
		if statErr != nil {
			errMsg = "Файл Java не найден на диске"
		}
		m.runtimes[r.ID] = JavaRuntime{
			ID:             r.ID,
			Component:      r.Component,
			Name:           r.Name,
			ExecutablePath: r.ExecutablePath,
			RootDir:        r.RootDir,
			Version:        r.Version,
			MajorVersion:   r.MajorVersion,
			Arch:           r.Arch,
			Bitness:        r.Bitness,
			Vendor:         r.Vendor,
			Source:         r.Source,
			Managed:        r.Managed,
			Verified:       r.Verified && statErr == nil,
			Runnable:       runnable,
			CompatibleMC:   CompatibleMCRangesForMajor(r.MajorVersion),
			Error:          errMsg,
			InstalledAt:    r.InstalledAt,
			UpdatedAt:      r.UpdatedAt,
		}
	}
}

// AcquireInUse marks a runtime as currently used (e.g. by a running game or operation) so it cannot be deleted.
func (m *Manager) AcquireInUse(id string) {
	m.mu.Lock()
	m.inUse[id]++
	m.mu.Unlock()
}

func (m *Manager) ReleaseInUse(id string) {
	m.mu.Lock()
	if m.inUse[id] > 1 {
		m.inUse[id]--
	} else {
		delete(m.inUse, id)
	}
	m.mu.Unlock()
}

func (m *Manager) GetSnapshot(ctx context.Context, refreshRemote bool) StateSnapshot {
	if refreshRemote {
		m.RefreshAvailable(ctx)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buildSnapshotLocked()
}

func (m *Manager) RefreshAvailable(ctx context.Context) ([]AvailableRuntime, error) {
	m.mu.Lock()
	manifestURL := m.manifestURL
	platKey := m.platformKey
	arch := m.platformArch
	m.mu.Unlock()

	av, err := FetchMojangAvailableRuntimes(ctx, m.httpClient, manifestURL, platKey, arch)
	m.mu.Lock()
	if err != nil {
		m.lastMetaErr = err.Error()
	} else {
		m.available = av
		m.lastMetaErr = ""
	}
	out := append([]AvailableRuntime(nil), m.available...)
	m.mu.Unlock()
	return out, err
}

func (m *Manager) ScanInstalled(ctx context.Context) StateSnapshot {
	m.mu.Lock()
	probe := m.probeFn
	stored := make([]JavaRuntime, 0, len(m.runtimes))
	for _, rt := range m.runtimes {
		stored = append(stored, rt)
	}
	m.mu.Unlock()

	cands := discoverSystemCandidates(m.runtimesDir)
	// Merge stored custom & managed entries into scan candidates
	byNormPath := map[string]discoveredCandidate{}
	for _, c := range cands {
		byNormPath[strings.ToLower(filepath.Clean(c.exePath))] = c
	}
	managedMetaByPath := map[string]JavaRuntime{}
	for _, st := range stored {
		key := strings.ToLower(filepath.Clean(st.ExecutablePath))
		managedMetaByPath[key] = st
		if st.Managed || st.Source == "custom" {
			if _, ok := byNormPath[key]; !ok {
				byNormPath[key] = discoveredCandidate{exePath: st.ExecutablePath, source: st.Source}
			}
		}
	}

	now := time.Now().Unix()
	nextMap := map[string]JavaRuntime{}
	for normKey, cand := range byNormPath {
		existing, hadExisting := managedMetaByPath[normKey]
		isManaged := cand.source == "managed" || (hadExisting && existing.Managed)
		source := cand.source
		if isManaged {
			source = "managed"
		}

		probeInfo, err := probe(ctx, cand.exePath)
		rootDir := inferJavaRootDir(cand.exePath)
		id := makeRuntimeID(source, cand.exePath)
		if hadExisting && existing.ID != "" {
			id = existing.ID
		}
		comp := existing.Component
		if comp == "" && isManaged {
			comp = inferComponentFromDir(filepath.Base(rootDir))
		}

		rt := JavaRuntime{
			ID:             id,
			Component:      comp,
			ExecutablePath: filepath.Clean(cand.exePath),
			RootDir:        rootDir,
			Source:         source,
			Managed:        isManaged,
			InstalledAt:    existing.InstalledAt,
			UpdatedAt:      now,
		}
		if rt.InstalledAt == 0 {
			rt.InstalledAt = now
		}
		if err != nil {
			// Only keep broken entries if they were explicitly managed or custom so user can reinstall/remove them
			if !isManaged && source != "custom" {
				continue
			}
			rt.Version = existing.Version
			rt.MajorVersion = existing.MajorVersion
			rt.Arch = existing.Arch
			rt.Bitness = existing.Bitness
			rt.Vendor = existing.Vendor
			rt.Name = existing.Name
			if rt.Name == "" {
				rt.Name = filepath.Base(rootDir)
			}
			rt.Verified = false
			rt.Runnable = false
			rt.Error = err.Error()
		} else {
			rt.Version = probeInfo.Version
			rt.MajorVersion = probeInfo.MajorVersion
			rt.Arch = probeInfo.Arch
			rt.Bitness = probeInfo.Bitness
			rt.Vendor = probeInfo.Vendor
			rt.Verified = true
			rt.Runnable = probeInfo.Runnable
			rt.CompatibleMC = CompatibleMCRangesForMajor(probeInfo.MajorVersion)
			if isManaged && comp != "" {
				rt.Name = DisplayNameForComponent(comp)
			} else if probeInfo.Vendor != "" {
				rt.Name = fmt.Sprintf("Java %d (%s)", probeInfo.MajorVersion, probeInfo.Vendor)
			} else {
				rt.Name = fmt.Sprintf("Java %d", probeInfo.MajorVersion)
			}
		}
		nextMap[rt.ID] = rt
		if (isManaged || source == "custom") && m.store != nil {
			_ = m.store.UpsertJavaRuntime(toRecord(rt))
		}
	}

	m.mu.Lock()
	m.runtimes = nextMap
	snap := m.buildSnapshotLocked()
	m.mu.Unlock()
	m.emitUpdated(snap)
	return snap
}

func (m *Manager) AddCustomPath(ctx context.Context, rawPath string) (JavaRuntime, error) {
	exePath, err := ResolveJavaExecutable(rawPath)
	if err != nil {
		return JavaRuntime{}, err
	}
	m.mu.Lock()
	probe := m.probeFn
	m.mu.Unlock()

	info, err := probe(ctx, exePath)
	if err != nil {
		return JavaRuntime{}, err
	}
	now := time.Now().Unix()
	rootDir := inferJavaRootDir(exePath)
	id := makeRuntimeID("custom", exePath)
	name := fmt.Sprintf("Java %d", info.MajorVersion)
	if info.Vendor != "" {
		name = fmt.Sprintf("Java %d (%s)", info.MajorVersion, info.Vendor)
	}
	rt := JavaRuntime{
		ID:             id,
		Name:           name,
		ExecutablePath: exePath,
		RootDir:        rootDir,
		Version:        info.Version,
		MajorVersion:   info.MajorVersion,
		Arch:           info.Arch,
		Bitness:        info.Bitness,
		Vendor:         info.Vendor,
		Source:         "custom",
		Managed:        false,
		Verified:       true,
		Runnable:       info.Runnable,
		CompatibleMC:   CompatibleMCRangesForMajor(info.MajorVersion),
		InstalledAt:    now,
		UpdatedAt:      now,
	}

	m.mu.Lock()
	m.runtimes[rt.ID] = rt
	if m.store != nil {
		_ = m.store.UpsertJavaRuntime(toRecord(rt))
	}
	snap := m.buildSnapshotLocked()
	m.mu.Unlock()
	m.emitUpdated(snap)
	return rt, nil
}

func (m *Manager) VerifyRuntime(ctx context.Context, id string) (JavaRuntime, error) {
	m.mu.Lock()
	rt, ok := m.runtimes[id]
	probe := m.probeFn
	m.mu.Unlock()
	if !ok {
		return JavaRuntime{}, ErrRuntimeNotFound
	}

	info, err := probe(ctx, rt.ExecutablePath)
	now := time.Now().Unix()
	rt.UpdatedAt = now
	if err != nil {
		rt.Verified = false
		rt.Runnable = false
		rt.Error = err.Error()
	} else {
		rt.Version = info.Version
		rt.MajorVersion = info.MajorVersion
		rt.Arch = info.Arch
		rt.Bitness = info.Bitness
		rt.Vendor = info.Vendor
		rt.Verified = true
		rt.Runnable = info.Runnable
		rt.CompatibleMC = CompatibleMCRangesForMajor(info.MajorVersion)
		rt.Error = ""
	}

	m.mu.Lock()
	m.runtimes[rt.ID] = rt
	if (rt.Managed || rt.Source == "custom") && m.store != nil {
		_ = m.store.UpsertJavaRuntime(toRecord(rt))
	}
	snap := m.buildSnapshotLocked()
	m.mu.Unlock()
	m.emitUpdated(snap)
	return rt, err
}

// InstallRuntimeAsync starts installation of an official Mojang runtime component in the background,
// downloading all files through the centralized Download Manager.
func (m *Manager) InstallRuntimeAsync(component string) error {
	comp := strings.ToLower(strings.TrimSpace(component))
	if comp == "" {
		return ErrComponentNotFound
	}
	m.mu.Lock()
	if _, running := m.installing[comp]; running {
		m.mu.Unlock()
		return ErrAlreadyInstalling
	}
	instCtx, cancel := context.WithCancel(context.Background())
	m.installing[comp] = cancel
	snap := m.buildSnapshotLocked()
	m.mu.Unlock()
	m.emitUpdated(snap)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		_, _ = m.runInstall(instCtx, comp)
	}()
	return nil
}

// InstallRuntimeSync synchronously installs an official Mojang runtime component via Download Manager.
func (m *Manager) InstallRuntimeSync(ctx context.Context, component string) (JavaRuntime, error) {
	comp := strings.ToLower(strings.TrimSpace(component))
	if comp == "" {
		return JavaRuntime{}, ErrComponentNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if _, running := m.installing[comp]; running {
		m.mu.Unlock()
		return JavaRuntime{}, ErrAlreadyInstalling
	}
	instCtx, cancel := context.WithCancel(ctx)
	m.installing[comp] = cancel
	snap := m.buildSnapshotLocked()
	m.mu.Unlock()
	m.emitUpdated(snap)

	return m.runInstall(instCtx, comp)
}

func (m *Manager) CancelInstall(component string) {
	comp := strings.ToLower(strings.TrimSpace(component))
	m.mu.Lock()
	cancel, ok := m.installing[comp]
	m.mu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
	if m.dlManager != nil {
		m.dlManager.CancelGroup("java:" + comp)
	}
}

func (m *Manager) runInstall(ctx context.Context, component string) (JavaRuntime, error) {
	groupID := "java:" + component
	defer func() {
		m.mu.Lock()
		if cancel, ok := m.installing[component]; ok {
			cancel()
			delete(m.installing, component)
		}
		snap := m.buildSnapshotLocked()
		m.mu.Unlock()
		m.emitUpdated(snap)
	}()

	if m.dlManager == nil {
		return JavaRuntime{}, fmt.Errorf("менеджер загрузок не инициализирован")
	}

	avList, err := m.RefreshAvailable(ctx)
	if err != nil && len(avList) == 0 {
		return JavaRuntime{}, fmt.Errorf("не удалось получить метаданные Mojang Java: %w", err)
	}
	var chosen *AvailableRuntime
	for i := range avList {
		if strings.EqualFold(avList[i].Component, component) {
			cp := avList[i]
			chosen = &cp
			break
		}
	}
	if chosen == nil || chosen.ManifestURL == "" {
		return JavaRuntime{}, fmt.Errorf("%w: %s", ErrComponentNotFound, component)
	}

	if _, err := m.httpClient.ValidateLauncherURL(chosen.ManifestURL); err != nil {
		return JavaRuntime{}, fmt.Errorf("недопустимый URL манифеста компонента: %w", err)
	}

	var fileManifest MojangRuntimeManifest
	if err := m.httpClient.WithContext(ctx).GetJSON(chosen.ManifestURL, nil, 600, &fileManifest); err != nil {
		return JavaRuntime{}, fmt.Errorf("не удалось загрузить манифест файлов %s: %w", component, err)
	}
	if len(fileManifest.Files) == 0 {
		return JavaRuntime{}, fmt.Errorf("манифест файлов %s пуст", component)
	}

	if err := os.MkdirAll(m.runtimesDir, 0o755); err != nil {
		return JavaRuntime{}, fmt.Errorf("не удалось создать папку runtimes: %w", err)
	}
	stagingDir := filepath.Join(m.runtimesDir, fmt.Sprintf(".staging-%s-%s-%d", component, chosen.Arch, time.Now().UnixNano()))
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return JavaRuntime{}, fmt.Errorf("не удалось создать временную директорию установки: %w", err)
	}
	promoted := false
	defer func() {
		if !promoted {
			m.dlManager.CancelGroup(groupID)
			_ = os.RemoveAll(stagingDir)
		}
	}()

	// Sort file paths deterministically: create directories first, enqueue files through Download Manager, links last
	keys := make([]string, 0, len(fileManifest.Files))
	for k := range fileManifest.Files {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	type pendingFile struct {
		relPath    string
		targetPath string
		executable bool
		taskID     string
	}
	type pendingLink struct {
		relPath string
		target  string
	}
	var filesToWait []pendingFile
	var linksToCreate []pendingLink

	for _, rawRel := range keys {
		if ctx.Err() != nil {
			return JavaRuntime{}, ctx.Err()
		}
		cleanRel, err := sanitizeRuntimeRelPath(rawRel)
		if err != nil {
			return JavaRuntime{}, err
		}
		destFull := filepath.Join(stagingDir, filepath.FromSlash(cleanRel))
		meta := fileManifest.Files[rawRel]

		switch strings.ToLower(strings.TrimSpace(meta.Type)) {
		case "directory":
			if err := os.MkdirAll(destFull, 0o755); err != nil {
				return JavaRuntime{}, err
			}
		case "link":
			linksToCreate = append(linksToCreate, pendingLink{relPath: cleanRel, target: meta.Target})
		case "file":
			if meta.Downloads.Raw.URL == "" {
				continue
			}
			algo := downloads.ChecksumNone
			val := strings.TrimSpace(meta.Downloads.Raw.SHA1)
			if val != "" {
				algo = downloads.ChecksumSHA1
			}
			taskView, err := m.dlManager.Enqueue(downloads.DownloadRequest{
				Name:          fmt.Sprintf("%s / %s", chosen.DisplayName, filepath.Base(cleanRel)),
				Category:      "java",
				URL:           meta.Downloads.Raw.URL,
				DestPath:      destFull,
				ExpectedSize:  meta.Downloads.Raw.Size,
				ChecksumAlgo:  algo,
				ChecksumValue: val,
				GroupID:       groupID,
			})
			if err != nil {
				return JavaRuntime{}, fmt.Errorf("ошибка постановки файла %s в очередь загрузок: %w", cleanRel, err)
			}
			filesToWait = append(filesToWait, pendingFile{
				relPath:    cleanRel,
				targetPath: destFull,
				executable: meta.Executable,
				taskID:     taskView.ID,
			})
		}
	}

	if len(filesToWait) == 0 {
		return JavaRuntime{}, fmt.Errorf("манифест %s не содержит файлов для загрузки", component)
	}

	// Await all queued Download Manager tasks
	for _, pf := range filesToWait {
		fin, err := m.dlManager.Wait(ctx, pf.taskID)
		if err != nil {
			return JavaRuntime{}, err
		}
		if fin.Status != downloads.StatusCompleted {
			return JavaRuntime{}, fmt.Errorf("не удалось загрузить %s (%s): %s", pf.relPath, fin.Status, fin.Error)
		}
		if pf.executable && goruntime.GOOS != "windows" {
			_ = os.Chmod(pf.targetPath, 0o755)
		}
	}

	for _, lnk := range linksToCreate {
		linkPath := filepath.Join(stagingDir, filepath.FromSlash(lnk.relPath))
		_ = os.MkdirAll(filepath.Dir(linkPath), 0o755)
		_ = os.Symlink(lnk.target, linkPath)
	}

	// Verify runtime structure & real java executable before promoting stagingDir!
	stagingExe, err := ResolveJavaExecutable(stagingDir)
	if err != nil {
		return JavaRuntime{}, fmt.Errorf("структура установленного runtime %s некорректна: %w", component, err)
	}
	m.mu.Lock()
	probe := m.probeFn
	m.mu.Unlock()
	probeInfo, err := probe(ctx, stagingExe)
	if err != nil || !probeInfo.Runnable {
		return JavaRuntime{}, fmt.Errorf("проверка запуска установленной Java завершилась ошибкой: %v", err)
	}

	// Atomically promote stagingDir -> finalDir
	finalDir := filepath.Join(m.runtimesDir, fmt.Sprintf("%s-%s", component, chosen.Arch))
	backupDir := finalDir + ".prev"
	_ = os.RemoveAll(backupDir)
	hadOld := false
	if _, err := os.Stat(finalDir); err == nil {
		if err := os.Rename(finalDir, backupDir); err != nil {
			return JavaRuntime{}, fmt.Errorf("не удалось заменить предыдущую установку %s (возможно, файлы заняты): %w", component, err)
		}
		hadOld = true
	}
	if err := os.Rename(stagingDir, finalDir); err != nil {
		if hadOld {
			_ = os.Rename(backupDir, finalDir)
		}
		return JavaRuntime{}, fmt.Errorf("не удалось завершить установку %s: %w", component, err)
	}
	promoted = true
	if hadOld {
		_ = os.RemoveAll(backupDir)
	}

	finalExe, err := ResolveJavaExecutable(finalDir)
	if err != nil {
		return JavaRuntime{}, err
	}
	now := time.Now().Unix()
	major := probeInfo.MajorVersion
	if major == 0 {
		major = chosen.MajorVersion
	}
	version := probeInfo.Version
	if version == "" {
		version = chosen.Version
	}
	vendor := probeInfo.Vendor
	if vendor == "" {
		vendor = "Mojang / OpenJDK"
	}

	rt := JavaRuntime{
		ID:             makeRuntimeID("managed", finalExe),
		Component:      component,
		Name:           chosen.DisplayName,
		ExecutablePath: finalExe,
		RootDir:        finalDir,
		Version:        version,
		MajorVersion:   major,
		Arch:           probeInfo.Arch,
		Bitness:        probeInfo.Bitness,
		Vendor:         vendor,
		Source:         "managed",
		Managed:        true,
		Verified:       true,
		Runnable:       true,
		CompatibleMC:   CompatibleMCRangesForMajor(major),
		InstalledAt:    now,
		UpdatedAt:      now,
	}

	m.mu.Lock()
	m.runtimes[rt.ID] = rt
	if m.store != nil {
		_ = m.store.UpsertJavaRuntime(toRecord(rt))
	}
	snap := m.buildSnapshotLocked()
	m.mu.Unlock()
	m.emitUpdated(snap)
	return rt, nil
}

func (m *Manager) ReinstallRuntimeAsync(id string) error {
	m.mu.Lock()
	rt, ok := m.runtimes[id]
	inUse := m.inUse[id] > 0
	m.mu.Unlock()
	if !ok {
		return ErrRuntimeNotFound
	}
	if !rt.Managed || rt.Component == "" {
		return ErrNotManagedRuntime
	}
	if inUse {
		return ErrRuntimeInUse
	}
	return m.InstallRuntimeAsync(rt.Component)
}

func (m *Manager) RemoveRuntime(id string) error {
	m.mu.Lock()
	rt, ok := m.runtimes[id]
	if !ok {
		m.mu.Unlock()
		return ErrRuntimeNotFound
	}
	if !rt.Managed {
		m.mu.Unlock()
		return ErrNotManagedRuntime
	}
	if m.inUse[id] > 0 {
		m.mu.Unlock()
		return ErrRuntimeInUse
	}
	// Strict safety guard: only ever delete a directory strictly inside m.runtimesDir!
	absRoot, errR := filepath.Abs(m.runtimesDir)
	absTarget, errT := filepath.Abs(rt.RootDir)
	if errR != nil || errT != nil || !isSubPath(absRoot, absTarget) {
		m.mu.Unlock()
		return fmt.Errorf("отказ в удалении: путь %q находится вне директории runtimes ModHub", rt.RootDir)
	}
	m.mu.Unlock()

	if err := os.RemoveAll(absTarget); err != nil {
		return fmt.Errorf("не удалось удалить директорию runtime: %w", err)
	}

	m.mu.Lock()
	delete(m.runtimes, id)
	if m.store != nil {
		_ = m.store.DeleteJavaRuntime(id)
	}
	snap := m.buildSnapshotLocked()
	m.mu.Unlock()
	m.emitUpdated(snap)
	return nil
}

func (m *Manager) GetRuntimeDir(id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt, ok := m.runtimes[id]
	if !ok {
		return "", ErrRuntimeNotFound
	}
	if rt.RootDir != "" {
		return rt.RootDir, nil
	}
	return filepath.Dir(rt.ExecutablePath), nil
}

func (m *Manager) RecommendForMC(in RequirementInput) (RequirementResult, *JavaRuntime) {
	req := ResolveRequirement(in)
	m.mu.Lock()
	list := make([]JavaRuntime, 0, len(m.runtimes))
	for _, rt := range m.runtimes {
		list = append(list, rt)
	}
	m.mu.Unlock()
	return req, RecommendRuntime(list, req)
}

func (m *Manager) buildSnapshotLocked() StateSnapshot {
	rts := make([]JavaRuntime, 0, len(m.runtimes))
	installedByComp := map[string]string{}
	for _, rt := range m.runtimes {
		rt.InUse = m.inUse[rt.ID] > 0
		rts = append(rts, rt)
		if rt.Managed && rt.Component != "" && rt.Verified {
			installedByComp[rt.Component] = rt.ID
		}
	}
	sort.SliceStable(rts, func(i, j int) bool {
		if rts[i].Managed != rts[j].Managed {
			return rts[i].Managed
		}
		if rts[i].MajorVersion != rts[j].MajorVersion {
			return rts[i].MajorVersion > rts[j].MajorVersion
		}
		return rts[i].ExecutablePath < rts[j].ExecutablePath
	})

	installing := make([]string, 0, len(m.installing))
	for comp := range m.installing {
		installing = append(installing, comp)
	}
	sort.Strings(installing)

	av := make([]AvailableRuntime, len(m.available))
	for i, a := range m.available {
		if id, ok := installedByComp[a.Component]; ok {
			a.Installed = true
			a.InstalledID = id
		}
		if _, running := m.installing[a.Component]; running {
			a.Installing = true
		}
		av[i] = a
	}

	return StateSnapshot{
		Runtimes:             rts,
		Available:            av,
		InstallingComponents: installing,
		RuntimesDir:          m.runtimesDir,
		PlatformKey:          m.platformKey,
		MetadataError:        m.lastMetaErr,
	}
}

func (m *Manager) emitUpdated(snap StateSnapshot) {
	if m.bus != nil {
		m.bus.Publish(events.EventJavaUpdated, snap)
	}
}

func inferJavaRootDir(exePath string) string {
	binDir := filepath.Dir(filepath.Clean(exePath))
	if strings.EqualFold(filepath.Base(binDir), "bin") {
		return filepath.Dir(binDir)
	}
	return binDir
}

func inferComponentFromDir(dirName string) string {
	low := strings.ToLower(dirName)
	for _, comp := range []string{"java-runtime-delta", "java-runtime-gamma", "java-runtime-alpha", "java-runtime-beta", "jre-legacy"} {
		if strings.HasPrefix(low, comp) {
			return comp
		}
	}
	return ""
}

func isSubPath(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func toRecord(rt JavaRuntime) domain.JavaRuntimeRecord {
	return domain.JavaRuntimeRecord{
		ID:             rt.ID,
		Component:      rt.Component,
		Name:           rt.Name,
		ExecutablePath: rt.ExecutablePath,
		RootDir:        rt.RootDir,
		Version:        rt.Version,
		MajorVersion:   rt.MajorVersion,
		Arch:           rt.Arch,
		Bitness:        rt.Bitness,
		Vendor:         rt.Vendor,
		Source:         rt.Source,
		Managed:        rt.Managed,
		Verified:       rt.Verified,
		InstalledAt:    rt.InstalledAt,
		UpdatedAt:      rt.UpdatedAt,
	}
}
