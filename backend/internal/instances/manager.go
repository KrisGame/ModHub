package instances

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"modhub/internal/domain"
	"modhub/internal/events"
	javart "modhub/internal/runtime"
	"modhub/internal/storage"
)

const (
	manifestSchemaVersion = 1
	defaultMemoryMinMB    = 512
	defaultMemoryMaxMB    = 4096
	minMemoryMB           = 256
	maxMemoryMB           = 65536
)

var (
	ErrInstanceNotFound = errors.New("игровой профиль не найден")
	ErrInvalidInput     = errors.New("некорректные параметры профиля")
	ErrInstanceBusy     = errors.New("профиль сейчас занят активной операцией")
	ErrPackNotFound     = errors.New("указанная сборка ModHub не найдена")
)

// Manager owns the lifecycle, directory isolation, integrity verification, duplication,
// and SQLite/instance.json persistence of Minecraft game profiles (Instances).
type Manager struct {
	dataDir      string
	instancesDir string
	sharedDir    string
	runtimesDir  string
	mediaDir     string

	store      Store
	javaMgr    *javart.Manager
	bus        *events.Bus
	packLookup PackLookup

	mu        sync.Mutex
	items     map[string]*Instance
	busy      map[string]int
	closeOnce sync.Once
}

func NewManager(
	dataDir, instancesDir, sharedDir, runtimesDir, mediaDir string,
	store Store,
	javaMgr *javart.Manager,
	bus *events.Bus,
	packLookup PackLookup,
) *Manager {
	m := &Manager{
		dataDir:      filepath.Clean(dataDir),
		instancesDir: filepath.Clean(instancesDir),
		sharedDir:    filepath.Clean(sharedDir),
		runtimesDir:  filepath.Clean(runtimesDir),
		mediaDir:     filepath.Clean(mediaDir),
		store:        store,
		javaMgr:      javaMgr,
		bus:          bus,
		packLookup:   packLookup,
		items:        make(map[string]*Instance),
		busy:         make(map[string]int),
	}
	m.ensureBaseDirs()
	m.restoreAndReconcile()
	return m
}

func (m *Manager) ensureBaseDirs() {
	_ = os.MkdirAll(m.instancesDir, 0o755)
	_ = os.MkdirAll(filepath.Join(m.sharedDir, "assets"), 0o755)
	_ = os.MkdirAll(filepath.Join(m.sharedDir, "libraries"), 0o755)
	_ = os.MkdirAll(filepath.Join(m.sharedDir, "versions"), 0o755)
	_ = os.MkdirAll(m.runtimesDir, 0o755)
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {})
}

func (m *Manager) SharedPaths() SharedStoragePaths {
	return SharedStoragePaths{
		SharedDir:    m.sharedDir,
		AssetsDir:    filepath.Join(m.sharedDir, "assets"),
		LibrariesDir: filepath.Join(m.sharedDir, "libraries"),
		VersionsDir:  filepath.Join(m.sharedDir, "versions"),
		RuntimesDir:  m.runtimesDir,
		InstancesDir: m.instancesDir,
	}
}

func (m *Manager) protectedDirs() []string {
	return []string{
		m.sharedDir,
		m.runtimesDir,
		m.mediaDir,
		filepath.Join(m.dataDir, "migration_backups"),
	}
}

func (m *Manager) otherInstanceDirsLocked(excludeID string) []string {
	out := make([]string, 0, len(m.items))
	for id, inst := range m.items {
		if id == excludeID || inst.RootDir == "" {
			continue
		}
		out = append(out, inst.RootDir)
	}
	return out
}

func (m *Manager) AcquireBusy(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[id]; !ok {
		return ErrInstanceNotFound
	}
	m.busy[id]++
	return nil
}

// AcquireBusyExclusive rejects simultaneous repairs, launches, and other profile operations.
func (m *Manager) AcquireBusyExclusive(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[id]; !ok {
		return ErrInstanceNotFound
	}
	if m.busy[id] > 0 {
		return ErrInstanceBusy
	}
	m.busy[id] = 1
	return nil
}

func (m *Manager) ReleaseBusy(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.busy[id] > 1 {
		m.busy[id]--
	} else {
		delete(m.busy, id)
	}
}

func generateInstanceID() string {
	var rnd [4]byte
	_, _ = rand.Read(rnd[:])
	return fmt.Sprintf("inst_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(rnd[:]))
}

func normalizeInstanceType(raw InstanceType, mcVersion string) InstanceType {
	switch strings.ToLower(strings.TrimSpace(string(raw))) {
	case "vanilla":
		return TypeVanilla
	case "neoforge":
		return TypeNeoForge
	case "fabric":
		return TypeFabric
	case "forge":
		return TypeForge
	case "quilt":
		return TypeQuilt
	case "empty":
		return TypeEmpty
	default:
		if strings.TrimSpace(mcVersion) != "" {
			return TypeVanilla
		}
		return TypeEmpty
	}
}

func normalizeJavaMode(raw JavaMode) JavaMode {
	switch strings.ToLower(strings.TrimSpace(string(raw))) {
	case "managed":
		return JavaModeManaged
	case "custom":
		return JavaModeCustom
	default:
		return JavaModeAuto
	}
}

func normalizeMemory(minMB, maxMB int) (int, int) {
	if maxMB <= 0 {
		maxMB = defaultMemoryMaxMB
	}
	if maxMB < minMemoryMB {
		maxMB = minMemoryMB
	}
	if maxMB > maxMemoryMB {
		maxMB = maxMemoryMB
	}
	if minMB <= 0 {
		minMB = defaultMemoryMinMB
	}
	if minMB < minMemoryMB {
		minMB = minMemoryMB
	}
	if minMB > maxMB {
		minMB = maxMB
	}
	return minMB, maxMB
}

func normalizeIcon(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "grass"
	}
	if len(s) > 256 {
		return "grass"
	}
	return s
}

func (m *Manager) findSavedPack(packID string) *domain.SavedPack {
	id := strings.TrimSpace(packID)
	if id == "" || m.packLookup == nil {
		return nil
	}
	for _, p := range m.packLookup() {
		if p.ID == id {
			cp := p
			return &cp
		}
	}
	return nil
}

func (m *Manager) restoreAndReconcile() {
	byRoot := map[string]string{}

	// 1. Load records from SQLite without auto-deleting any user data
	if m.store != nil {
		recs, err := m.store.ListInstances()
		if err == nil {
			for _, rec := range recs {
				if ValidateInstanceID(rec.ID) != nil || strings.TrimSpace(rec.RootDir) == "" {
					continue
				}
				inst := m.recordToInstance(rec)
				interruptedStatus := Status(rec.Status)
				rep := m.evaluateIntegrity(inst, interruptedStatus)
				inst.Status = rep.Status
				inst.Issues = rep.Issues
				inst.ModsCount = rep.ModsCount
				m.enrichDynamicFields(inst)
				m.items[inst.ID] = inst
				byRoot[strings.ToLower(filepath.Clean(inst.RootDir))] = inst.ID
				if string(inst.Status) != rec.Status {
					_ = m.store.UpsertInstance(m.instanceToRecord(inst))
				}
			}
		}
	}

	// 2. Discover untracked instance folders inside data/instances/ (e.g. restored from backup)
	entries, err := os.ReadDir(m.instancesDir)
	if err != nil {
		return
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		dirPath := filepath.Join(m.instancesDir, ent.Name())
		if _, known := byRoot[strings.ToLower(filepath.Clean(dirPath))]; known {
			continue
		}
		mRaw, err := os.ReadFile(filepath.Join(dirPath, "instance.json"))
		if err != nil {
			continue
		}
		var mf InstanceManifest
		if json.Unmarshal(mRaw, &mf) != nil {
			continue
		}
		if ValidateInstanceID(mf.ID) != nil {
			if ValidateInstanceID(ent.Name()) == nil {
				mf.ID = ent.Name()
			} else {
				continue
			}
		}
		if _, dup := m.items[mf.ID]; dup {
			continue
		}
		gameDir := filepath.Join(dirPath, "minecraft")
		minMB, maxMB := normalizeMemory(mf.MemoryMinMB, mf.MemoryMaxMB)
		now := time.Now().Unix()
		if mf.CreatedAt == 0 {
			mf.CreatedAt = now
		}
		if mf.UpdatedAt == 0 {
			mf.UpdatedAt = now
		}
		inst := &Instance{
			ID:             mf.ID,
			Name:           strings.TrimSpace(mf.Name),
			Description:    mf.Description,
			Icon:           normalizeIcon(mf.Icon),
			MCVersion:      strings.TrimSpace(mf.MCVersion),
			InstanceType:   normalizeInstanceType(mf.InstanceType, mf.MCVersion),
			LoaderVersion:  strings.TrimSpace(mf.LoaderVersion),
			RootDir:        dirPath,
			GameDir:        gameDir,
			JavaMode:       normalizeJavaMode(mf.JavaMode),
			JavaRuntimeID:  mf.JavaRuntimeID,
			JavaPath:       mf.JavaPath,
			MemoryMinMB:    minMB,
			MemoryMaxMB:    maxMB,
			LaunchSettings: mf.LaunchSettings,
			LinkedPackID:   mf.LinkedPackID,
			LinkedPackName: mf.LinkedPackName,
			CreatedAt:      mf.CreatedAt,
			UpdatedAt:      mf.UpdatedAt,
			LastPlayedAt:   mf.LastPlayedAt,
		}
		if inst.Name == "" {
			inst.Name = inst.ID
		}
		rep := m.evaluateIntegrity(inst, "")
		inst.Status = rep.Status
		inst.Issues = rep.Issues
		inst.ModsCount = rep.ModsCount
		m.enrichDynamicFields(inst)
		m.items[inst.ID] = inst
		if m.store != nil {
			_ = m.store.UpsertInstance(m.instanceToRecord(inst))
			if inst.LinkedPackID != "" {
				_ = m.store.SetInstancePackLink(inst.ID, inst.LinkedPackID, inst.LinkedPackName, inst.UpdatedAt)
			}
		}
	}
}

func (m *Manager) List() ListSnapshot {
	m.mu.Lock()
	list := make([]Instance, 0, len(m.items))
	for _, inst := range m.items {
		cp := m.copyInstanceLocked(inst)
		list = append(list, cp)
	}
	m.mu.Unlock()

	for i := range list {
		m.enrichDynamicFields(&list[i])
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].UpdatedAt != list[j].UpdatedAt {
			return list[i].UpdatedAt > list[j].UpdatedAt
		}
		return list[i].CreatedAt > list[j].CreatedAt
	})
	return ListSnapshot{
		Instances: list,
		Paths:     m.SharedPaths(),
	}
}

func (m *Manager) Get(id string) (Instance, error) {
	if err := ValidateInstanceID(id); err != nil {
		return Instance{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	m.mu.Lock()
	inst, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return Instance{}, ErrInstanceNotFound
	}
	cp := m.copyInstanceLocked(inst)
	m.mu.Unlock()

	m.enrichDynamicFields(&cp)
	return cp, nil
}

func (m *Manager) Create(req CreateInstanceRequest) (Instance, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return Instance{}, fmt.Errorf("%w: укажите название профиля", ErrInvalidInput)
	}
	if len([]rune(name)) > 120 {
		return Instance{}, fmt.Errorf("%w: название профиля слишком длинное (максимум 120 символов)", ErrInvalidInput)
	}

	mcVer := strings.TrimSpace(req.MCVersion)
	instType := normalizeInstanceType(req.InstanceType, mcVer)
	loaderVer := strings.TrimSpace(req.LoaderVersion)
	linkedPackID := strings.TrimSpace(req.LinkedPackID)
	linkedPackName := ""
	if linkedPackID != "" {
		pk := m.findSavedPack(linkedPackID)
		if pk == nil {
			return Instance{}, ErrPackNotFound
		}
		linkedPackName = pk.Name
		if mcVer == "" && pk.MCVersion != "" {
			mcVer = strings.TrimSpace(pk.MCVersion)
		}
		if req.InstanceType == "" && pk.Loader != "" {
			instType = normalizeInstanceType(InstanceType(pk.Loader), mcVer)
		}
	}
	if instType == TypeEmpty || instType == TypeVanilla {
		loaderVer = ""
	}

	minMB, maxMB := normalizeMemory(req.MemoryMinMB, req.MemoryMaxMB)
	javaMode := normalizeJavaMode(req.JavaMode)
	id := generateInstanceID()

	m.mu.Lock()
	otherDirs := m.otherInstanceDirsLocked("")
	var rootDir string
	var err error
	if strings.TrimSpace(req.CustomDir) != "" {
		baseCustom, cleanErr := cleanAbsPath(req.CustomDir)
		if cleanErr != nil {
			m.mu.Unlock()
			return Instance{}, fmt.Errorf("%w: %v", ErrInvalidInput, cleanErr)
		}
		if isDangerousSystemPath(baseCustom) {
			m.mu.Unlock()
			return Instance{}, fmt.Errorf("%w: путь %q является системной или корневой директорией", ErrInvalidInput, baseCustom)
		}
		// Always place the profile in its own <id> subfolder if user selected an existing non-empty folder,
		// or validate directly if it's a brand-new dedicated folder path.
		candDir := baseCustom
		if info, statErr := os.Stat(baseCustom); statErr == nil && info.IsDir() {
			if ents, _ := os.ReadDir(baseCustom); len(ents) > 0 {
				candDir = filepath.Join(baseCustom, id)
			}
		}
		rootDir, err = ValidateSafeInstanceRoot(candDir, m.protectedDirs(), otherDirs)
	} else {
		rootDir, err = ValidateSafeInstanceRoot(filepath.Join(m.instancesDir, id), m.protectedDirs(), otherDirs)
	}
	if err != nil {
		m.mu.Unlock()
		return Instance{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	now := time.Now().Unix()
	gameDir := filepath.Join(rootDir, "minecraft")
	inst := &Instance{
		ID:             id,
		Name:           name,
		Description:    strings.TrimSpace(req.Description),
		Icon:           normalizeIcon(req.Icon),
		MCVersion:      mcVer,
		InstanceType:   instType,
		LoaderVersion:  loaderVer,
		RootDir:        rootDir,
		GameDir:        gameDir,
		JavaMode:       javaMode,
		JavaRuntimeID:  strings.TrimSpace(req.JavaRuntimeID),
		JavaPath:       strings.TrimSpace(req.JavaPath),
		MemoryMinMB:    minMB,
		MemoryMaxMB:    maxMB,
		LaunchSettings: req.LaunchSettings,
		LinkedPackID:   linkedPackID,
		LinkedPackName: linkedPackName,
		Status:         StatusCreating,
		CreatedAt:      now,
		UpdatedAt:      now,
		Issues:         []IntegrityIssue{},
		InstalledMods:  []InstalledModRef{},
	}
	m.items[id] = inst
	m.busy[id] = 1
	if m.store != nil {
		if err := m.store.UpsertInstance(m.instanceToRecord(inst)); err != nil {
			delete(m.items, id)
			delete(m.busy, id)
			m.mu.Unlock()
			return Instance{}, fmt.Errorf("не удалось сохранить запись профиля: %w", err)
		}
	}
	m.mu.Unlock()

	defer m.ReleaseBusy(id)

	// Create isolated profile directories
	if err := m.createProfileDirectoryStructure(rootDir, gameDir); err != nil {
		m.mu.Lock()
		inst.Status = StatusCorrupted
		inst.Issues = []IntegrityIssue{{Code: "dir_create_failed", Severity: "corruption", Message: err.Error()}}
		inst.UpdatedAt = time.Now().Unix()
		if m.store != nil {
			_ = m.store.UpsertInstance(m.instanceToRecord(inst))
		}
		m.mu.Unlock()
		return Instance{}, fmt.Errorf("не удалось создать структуру директорий профиля: %w", err)
	}

	if err := m.writeInstanceManifest(inst); err != nil {
		m.mu.Lock()
		inst.Status = StatusCorrupted
		inst.Issues = []IntegrityIssue{{Code: "manifest_write_failed", Severity: "corruption", Message: err.Error()}}
		inst.UpdatedAt = time.Now().Unix()
		if m.store != nil {
			_ = m.store.UpsertInstance(m.instanceToRecord(inst))
		}
		m.mu.Unlock()
		return Instance{}, fmt.Errorf("не удалось записать instance.json: %w", err)
	}

	rep := m.evaluateIntegrity(inst, "")
	m.mu.Lock()
	inst.Status = rep.Status
	inst.Issues = rep.Issues
	inst.ModsCount = rep.ModsCount
	inst.UpdatedAt = time.Now().Unix()
	if m.store != nil {
		_ = m.store.UpsertInstance(m.instanceToRecord(inst))
		_ = m.store.SetInstancePackLink(inst.ID, inst.LinkedPackID, inst.LinkedPackName, inst.UpdatedAt)
	}
	result := m.copyInstanceLocked(inst)
	m.mu.Unlock()

	m.enrichDynamicFields(&result)
	m.emitEvent(events.EventInstanceCreated, result)
	m.emitEvent(events.EventInstanceStatus, result)
	return result, nil
}

func (m *Manager) Update(req UpdateInstanceRequest) (Instance, error) {
	if err := ValidateInstanceID(req.ID); err != nil {
		return Instance{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	m.mu.Lock()
	inst, ok := m.items[req.ID]
	if !ok {
		m.mu.Unlock()
		return Instance{}, ErrInstanceNotFound
	}
	if m.busy[req.ID] > 0 {
		m.mu.Unlock()
		return Instance{}, ErrInstanceBusy
	}
	m.busy[req.ID]++

	next := *inst
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" {
			m.busy[req.ID]--
			if m.busy[req.ID] <= 0 {
				delete(m.busy, req.ID)
			}
			m.mu.Unlock()
			return Instance{}, fmt.Errorf("%w: название профиля не может быть пустым", ErrInvalidInput)
		}
		next.Name = n
	}
	if req.Description != nil {
		next.Description = strings.TrimSpace(*req.Description)
	}
	if req.Icon != nil {
		next.Icon = normalizeIcon(*req.Icon)
	}
	if req.MCVersion != nil {
		next.MCVersion = strings.TrimSpace(*req.MCVersion)
	}
	if req.InstanceType != nil {
		next.InstanceType = normalizeInstanceType(*req.InstanceType, next.MCVersion)
	}
	if req.LoaderVersion != nil {
		next.LoaderVersion = strings.TrimSpace(*req.LoaderVersion)
	}
	if next.InstanceType == TypeEmpty || next.InstanceType == TypeVanilla {
		next.LoaderVersion = ""
	}
	if req.JavaMode != nil {
		next.JavaMode = normalizeJavaMode(*req.JavaMode)
	}
	if req.JavaRuntimeID != nil {
		next.JavaRuntimeID = strings.TrimSpace(*req.JavaRuntimeID)
	}
	if req.JavaPath != nil {
		next.JavaPath = strings.TrimSpace(*req.JavaPath)
	}
	minMB, maxMB := next.MemoryMinMB, next.MemoryMaxMB
	if req.MemoryMinMB != nil {
		minMB = *req.MemoryMinMB
	}
	if req.MemoryMaxMB != nil {
		maxMB = *req.MemoryMaxMB
	}
	next.MemoryMinMB, next.MemoryMaxMB = normalizeMemory(minMB, maxMB)
	if req.LaunchSettings != nil {
		next.LaunchSettings = *req.LaunchSettings
	}
	if req.LinkedPackID != nil {
		pid := strings.TrimSpace(*req.LinkedPackID)
		if pid == "" {
			next.LinkedPackID = ""
			next.LinkedPackName = ""
		} else {
			pk := m.findSavedPack(pid)
			if pk == nil {
				m.busy[req.ID]--
				if m.busy[req.ID] <= 0 {
					delete(m.busy, req.ID)
				}
				m.mu.Unlock()
				return Instance{}, ErrPackNotFound
			}
			next.LinkedPackID = pk.ID
			next.LinkedPackName = pk.Name
		}
	}
	next.UpdatedAt = time.Now().Unix()
	m.mu.Unlock()

	defer m.ReleaseBusy(req.ID)

	if err := m.writeInstanceManifest(&next); err != nil {
		return Instance{}, fmt.Errorf("не удалось обновить instance.json: %w", err)
	}
	rep := m.evaluateIntegrity(&next, "")
	next.Status = rep.Status
	next.Issues = rep.Issues
	next.ModsCount = rep.ModsCount

	m.mu.Lock()
	*inst = next
	if m.store != nil {
		_ = m.store.UpsertInstance(m.instanceToRecord(inst))
		_ = m.store.SetInstancePackLink(inst.ID, inst.LinkedPackID, inst.LinkedPackName, inst.UpdatedAt)
	}
	result := m.copyInstanceLocked(inst)
	m.mu.Unlock()

	m.enrichDynamicFields(&result)
	m.emitEvent(events.EventInstanceUpdated, result)
	m.emitEvent(events.EventInstanceStatus, result)
	return result, nil
}

func (m *Manager) Duplicate(req DuplicateInstanceRequest) (Instance, error) {
	if err := ValidateInstanceID(req.SourceID); err != nil {
		return Instance{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	m.mu.Lock()
	src, ok := m.items[req.SourceID]
	if !ok {
		m.mu.Unlock()
		return Instance{}, ErrInstanceNotFound
	}
	if m.busy[req.SourceID] > 0 {
		m.mu.Unlock()
		return Instance{}, ErrInstanceBusy
	}
	m.busy[req.SourceID]++
	srcCopy := *src
	m.mu.Unlock()

	defer m.ReleaseBusy(req.SourceID)

	newName := strings.TrimSpace(req.NewName)
	if newName == "" {
		newName = srcCopy.Name + " (Копия)"
	}
	desc := strings.TrimSpace(req.Description)
	if desc == "" {
		desc = srcCopy.Description
	}

	createReq := CreateInstanceRequest{
		Name:          newName,
		Description:   desc,
		Icon:          srcCopy.Icon,
		MCVersion:     srcCopy.MCVersion,
		InstanceType:  srcCopy.InstanceType,
		LoaderVersion: srcCopy.LoaderVersion,
		LinkedPackID:  srcCopy.LinkedPackID,
	}
	if req.CopySettings {
		createReq.JavaMode = srcCopy.JavaMode
		createReq.JavaRuntimeID = srcCopy.JavaRuntimeID
		createReq.JavaPath = srcCopy.JavaPath
		createReq.MemoryMinMB = srcCopy.MemoryMinMB
		createReq.MemoryMaxMB = srcCopy.MemoryMaxMB
		createReq.LaunchSettings = srcCopy.LaunchSettings
	}

	dupInst, err := m.Create(createReq)
	if err != nil {
		return Instance{}, err
	}

	// Lock duplicate while selectively copying requested physical files (strict independence, no symlinks)
	_ = m.AcquireBusy(dupInst.ID)
	defer m.ReleaseBusy(dupInst.ID)

	copyTargets := []struct {
		enabled bool
		subdir  string
	}{
		{req.CopyMods, "mods"},
		{req.CopyConfigs, "config"},
		{req.CopyResourcePacks, "resourcepacks"},
		{req.CopyShaderPacks, "shaderpacks"},
		{req.CopySaves, "saves"},
	}
	for _, ct := range copyTargets {
		if !ct.enabled {
			continue
		}
		if err := copyDirIndependent(filepath.Join(srcCopy.GameDir, ct.subdir), filepath.Join(dupInst.GameDir, ct.subdir)); err != nil {
			return Instance{}, fmt.Errorf("ошибка копирования папки %s: %w", ct.subdir, err)
		}
	}
	if req.CopySettings {
		// Also copy top-level Minecraft settings files like options.txt & servers.dat if present
		for _, fn := range []string{"options.txt", "optionsof.txt", "servers.dat"} {
			srcF := filepath.Join(srcCopy.GameDir, fn)
			if info, err := os.Stat(srcF); err == nil && !info.IsDir() {
				_ = copyRegularFile(srcF, filepath.Join(dupInst.GameDir, fn))
			}
		}
	}

	_, _ = m.Verify(dupInst.ID)
	return m.Get(dupInst.ID)
}

func (m *Manager) Delete(req DeleteInstanceRequest) error {
	if err := ValidateInstanceID(req.ID); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	m.mu.Lock()
	inst, ok := m.items[req.ID]
	if !ok {
		m.mu.Unlock()
		return ErrInstanceNotFound
	}
	if m.busy[req.ID] > 0 {
		m.mu.Unlock()
		return ErrInstanceBusy
	}
	m.busy[req.ID]++
	inst.Status = StatusDeleting
	inst.UpdatedAt = time.Now().Unix()
	rootDir := inst.RootDir
	otherDirs := m.otherInstanceDirsLocked(req.ID)
	if m.store != nil {
		_ = m.store.UpsertInstance(m.instanceToRecord(inst))
	}
	snapDeleting := m.copyInstanceLocked(inst)
	m.mu.Unlock()

	m.emitEvent(events.EventInstanceStatus, snapDeleting)

	if req.DeleteFiles {
		safeDir, err := ValidateSafeDeleteRoot(rootDir, m.instancesDir, m.protectedDirs(), otherDirs)
		if err != nil {
			m.mu.Lock()
			inst.Status = StatusCorrupted
			m.busy[req.ID]--
			if m.busy[req.ID] <= 0 {
				delete(m.busy, req.ID)
			}
			if m.store != nil {
				_ = m.store.UpsertInstance(m.instanceToRecord(inst))
			}
			m.mu.Unlock()
			return err
		}
		if _, statErr := os.Stat(safeDir); statErr == nil {
			if err := os.RemoveAll(safeDir); err != nil {
				m.mu.Lock()
				inst.Status = StatusCorrupted
				inst.Issues = append(inst.Issues, IntegrityIssue{
					Code:     "delete_locked",
					Severity: "corruption",
					Message:  "Не удалось удалить часть файлов (возможно, заблокированы другим процессом): " + err.Error(),
				})
				m.busy[req.ID]--
				if m.busy[req.ID] <= 0 {
					delete(m.busy, req.ID)
				}
				if m.store != nil {
					_ = m.store.UpsertInstance(m.instanceToRecord(inst))
				}
				m.mu.Unlock()
				return fmt.Errorf("не удалось удалить файлы профиля: %w", err)
			}
		}
	}

	m.mu.Lock()
	delete(m.items, req.ID)
	delete(m.busy, req.ID)
	if m.store != nil {
		_ = m.store.DeleteInstance(req.ID)
	}
	m.mu.Unlock()

	m.emitEvent(events.EventInstanceDeleted, map[string]any{
		"id":           req.ID,
		"delete_files": req.DeleteFiles,
	})
	return nil
}

func (m *Manager) Verify(id string) (IntegrityReport, error) {
	if err := ValidateInstanceID(id); err != nil {
		return IntegrityReport{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	m.mu.Lock()
	inst, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return IntegrityReport{}, ErrInstanceNotFound
	}
	cp := *inst
	m.mu.Unlock()

	rep := m.evaluateIntegrity(&cp, "")

	m.mu.Lock()
	if cur, exists := m.items[id]; exists {
		cur.Status = rep.Status
		cur.Issues = rep.Issues
		cur.ModsCount = rep.ModsCount
		cur.UpdatedAt = time.Now().Unix()
		if m.store != nil {
			_ = m.store.UpsertInstance(m.instanceToRecord(cur))
		}
		snap := m.copyInstanceLocked(cur)
		m.mu.Unlock()
		m.enrichDynamicFields(&snap)
		m.emitEvent(events.EventInstanceStatus, snap)
		return rep, nil
	}
	m.mu.Unlock()
	return rep, nil
}

// RefreshMods rescans a profile's mods directory and persists the updated count.
func (m *Manager) RefreshMods(id string) error {
	if err := ValidateInstanceID(id); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	m.mu.Lock()
	inst, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return ErrInstanceNotFound
	}
	mods := scanInstalledModFiles(filepath.Join(inst.GameDir, "mods"))
	inst.InstalledMods = mods
	inst.ModsCount = len(mods)
	inst.UpdatedAt = time.Now().Unix()
	if m.store != nil {
		_ = m.store.UpsertInstance(m.instanceToRecord(inst))
	}
	snap := m.copyInstanceLocked(inst)
	m.mu.Unlock()
	m.enrichDynamicFields(&snap)
	m.emitEvent(events.EventInstanceUpdated, snap)
	return nil
}

// Repair non-destructively recreates missing expected directories and rewrites instance.json if damaged.
func (m *Manager) Repair(id string) (IntegrityReport, error) {
	if err := ValidateInstanceID(id); err != nil {
		return IntegrityReport{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	m.mu.Lock()
	inst, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return IntegrityReport{}, ErrInstanceNotFound
	}
	if m.busy[id] > 0 {
		m.mu.Unlock()
		return IntegrityReport{}, ErrInstanceBusy
	}
	m.busy[id]++
	cp := *inst
	m.mu.Unlock()

	defer m.ReleaseBusy(id)

	m.ensureBaseDirs()
	if err := m.createProfileDirectoryStructure(cp.RootDir, cp.GameDir); err != nil {
		return IntegrityReport{}, err
	}
	if err := m.writeInstanceManifest(&cp); err != nil {
		return IntegrityReport{}, err
	}
	return m.Verify(id)
}

// RepairMissingStructure only creates missing directories and metadata; it never
// replaces an existing (possibly user-edited) manifest or file.
func (m *Manager) RepairMissingStructure(id string) (IntegrityReport, error) {
	if err := m.AcquireBusyExclusive(id); err != nil {
		return IntegrityReport{}, err
	}
	defer m.ReleaseBusy(id)
	inst, err := m.Get(id)
	if err != nil {
		return IntegrityReport{}, err
	}
	// Refuse to traverse substituted profile directories.
	dirs := []string{inst.RootDir, inst.GameDir}
	for _, sub := range ExpectedGameSubdirs {
		dirs = append(dirs, filepath.Join(inst.GameDir, sub))
	}
	for _, dir := range dirs {
		if info, statErr := os.Lstat(dir); statErr == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return IntegrityReport{}, fmt.Errorf("путь профиля содержит ссылку или файл: %s", dir)
			}
		} else if !os.IsNotExist(statErr) {
			return IntegrityReport{}, statErr
		}
	}
	if err := m.createProfileDirectoryStructure(inst.RootDir, inst.GameDir); err != nil {
		return IntegrityReport{}, err
	}
	manifest := filepath.Join(inst.RootDir, "instance.json")
	if _, err := os.Lstat(manifest); os.IsNotExist(err) {
		if err := m.writeInstanceManifest(&inst); err != nil {
			return IntegrityReport{}, err
		}
	} else if err != nil {
		return IntegrityReport{}, err
	}
	return m.Verify(id)
}

func (m *Manager) GetFilesSummary(id string) (InstanceFilesSummary, error) {
	inst, err := m.Get(id)
	if err != nil {
		return InstanceFilesSummary{}, err
	}
	folders := make([]FolderStat, 0, len(ExpectedGameSubdirs))
	var grandTotal int64
	for _, sub := range ExpectedGameSubdirs {
		full := filepath.Join(inst.GameDir, sub)
		exists, cnt, sz := statDirRecursive(full)
		grandTotal += sz
		folders = append(folders, FolderStat{
			Name:       sub,
			RelPath:    "minecraft/" + sub,
			FullPath:   full,
			Exists:     exists,
			FileCount:  cnt,
			TotalBytes: sz,
		})
	}
	return InstanceFilesSummary{
		InstanceID:  inst.ID,
		RootDir:     inst.RootDir,
		GameDir:     inst.GameDir,
		SharedDir:   m.sharedDir,
		RuntimesDir: m.runtimesDir,
		Folders:     folders,
		TotalBytes:  grandTotal,
	}, nil
}

// ResolveOpenDir safely resolves either the profile root, minecraft/, or a direct safe subdirectory inside minecraft/.
func (m *Manager) ResolveOpenDir(id, subdir string) (string, error) {
	inst, err := m.Get(id)
	if err != nil {
		return "", err
	}
	sub := strings.TrimSpace(subdir)
	if sub == "" || sub == "minecraft" {
		if _, err := os.Stat(inst.GameDir); err == nil {
			return inst.GameDir, nil
		}
		return inst.RootDir, nil
	}
	if sub == "root" {
		return inst.RootDir, nil
	}
	if !reSafeSubdir.MatchString(sub) || strings.Contains(sub, "..") {
		return "", fmt.Errorf("%w: недопустимое имя подпапки %q", ErrInvalidInput, sub)
	}
	target := filepath.Join(inst.GameDir, sub)
	if !isStrictSubPath(inst.GameDir, target) {
		return "", fmt.Errorf("%w: выход за пределы директории профиля запрещён", ErrInvalidInput)
	}
	if _, err := os.Stat(target); err != nil {
		return "", fmt.Errorf("папка %s не существует", sub)
	}
	return target, nil
}

func (m *Manager) evaluateIntegrity(inst *Instance, priorStatus Status) IntegrityReport {
	rep := IntegrityReport{
		InstanceID: inst.ID,
		Issues:     []IntegrityIssue{},
		CheckedAt:  time.Now().Unix(),
	}
	hasCorruption := false
	hasMissingComponent := false

	if priorStatus == StatusCreating {
		hasCorruption = true
		rep.Issues = append(rep.Issues, IntegrityIssue{
			Code:     "interrupted_creation",
			Severity: "corruption",
			Message:  "Создание профиля было прервано до завершения. Запустите восстановление структуры или пересоздайте профиль.",
		})
	} else if priorStatus == StatusDeleting {
		hasCorruption = true
		rep.Issues = append(rep.Issues, IntegrityIssue{
			Code:     "interrupted_deletion",
			Severity: "corruption",
			Message:  "Предыдущее удаление профиля было прервано.",
		})
	}

	// 1. Profile root directory
	if info, err := os.Stat(inst.RootDir); err == nil && info.IsDir() {
		rep.RootExists = true
	} else {
		hasCorruption = true
		rep.Issues = append(rep.Issues, IntegrityIssue{
			Code:     "missing_root_dir",
			Severity: "corruption",
			Message:  "Директория профиля отсутствует на диске: " + inst.RootDir,
		})
	}

	// 2. instance.json validity
	if raw, err := os.ReadFile(filepath.Join(inst.RootDir, "instance.json")); err == nil && len(raw) > 0 {
		var mf InstanceManifest
		if json.Unmarshal(raw, &mf) == nil && mf.ID != "" && mf.Name != "" {
			rep.ManifestValid = true
		} else {
			hasCorruption = true
			rep.Issues = append(rep.Issues, IntegrityIssue{
				Code:     "corrupt_manifest",
				Severity: "corruption",
				Message:  "Файл instance.json повреждён или содержит некорректный JSON.",
			})
		}
	} else {
		hasCorruption = true
		rep.Issues = append(rep.Issues, IntegrityIssue{
			Code:     "missing_manifest",
			Severity: "corruption",
			Message:  "Отсутствует файл метаданных instance.json.",
		})
	}

	// 3. minecraft/ and expected subdirectories
	if info, err := os.Stat(inst.GameDir); err == nil && info.IsDir() {
		rep.GameDirExists = true
		rep.SubdirsOK = true
		for _, sub := range ExpectedGameSubdirs {
			if si, serr := os.Stat(filepath.Join(inst.GameDir, sub)); serr != nil || !si.IsDir() {
				rep.SubdirsOK = false
				rep.MissingSubdirs = append(rep.MissingSubdirs, sub)
			}
		}
		if !rep.SubdirsOK {
			rep.Issues = append(rep.Issues, IntegrityIssue{
				Code:     "missing_subdirs",
				Severity: "warning",
				Message:  "Отсутствуют поддиректории: " + strings.Join(rep.MissingSubdirs, ", "),
			})
		}
	} else {
		hasCorruption = true
		rep.Issues = append(rep.Issues, IntegrityIssue{
			Code:     "missing_game_dir",
			Severity: "corruption",
			Message:  "Отсутствует игровая директория minecraft/.",
		})
	}

	// 4. Shared resources accessibility
	sharedOK := true
	for _, sdir := range []string{
		filepath.Join(m.sharedDir, "assets"),
		filepath.Join(m.sharedDir, "libraries"),
		filepath.Join(m.sharedDir, "versions"),
	} {
		if si, serr := os.Stat(sdir); serr != nil || !si.IsDir() {
			sharedOK = false
		}
	}
	rep.SharedResourcesOK = sharedOK
	if !sharedOK {
		rep.Issues = append(rep.Issues, IntegrityIssue{
			Code:     "missing_shared_dirs",
			Severity: "warning",
			Message:  "Общие директории библиотек/ресурсов не инициализированы.",
		})
	}

	// 5. Java runtime availability
	req, resolvedJava, javaOK, javaSummary := m.resolveInstanceJava(inst)
	_ = req
	rep.JavaOK = javaOK
	rep.JavaSummary = javaSummary
	if !javaOK {
		hasMissingComponent = true
		rep.Issues = append(rep.Issues, IntegrityIssue{
			Code:     "missing_java",
			Severity: "missing_component",
			Message:  javaSummary,
		})
	}

	// 6. Minecraft client & modloader installation check (HONEST: never report ready unless real game files exist!)
	if strings.TrimSpace(inst.MCVersion) == "" || inst.InstanceType == TypeEmpty {
		hasMissingComponent = true
		rep.Issues = append(rep.Issues, IntegrityIssue{
			Code:     "mc_version_not_selected",
			Severity: "missing_component",
			Message:  "Версия Minecraft не выбрана — профиль создан как пустая заготовка.",
		})
	} else {
		clientJarShared := filepath.Join(m.sharedDir, "versions", inst.MCVersion, inst.MCVersion+".jar")
		versionJSONShared := filepath.Join(m.sharedDir, "versions", inst.MCVersion, inst.MCVersion+".json")
		clientJarLocal := filepath.Join(inst.GameDir, "versions", inst.MCVersion, inst.MCVersion+".jar")
		hasSharedMC := fileExistsNonEmpty(clientJarShared) && fileExistsNonEmpty(versionJSONShared)
		hasLocalMC := fileExistsNonEmpty(clientJarLocal)
		if hasSharedMC || hasLocalMC {
			rep.GameFilesInstalled = true
		} else {
			hasMissingComponent = true
			rep.Issues = append(rep.Issues, IntegrityIssue{
				Code:     "missing_minecraft_client",
				Severity: "missing_component",
				Message:  fmt.Sprintf("Файлы Minecraft %s не установлены. Откройте вкладку «Версия и загрузчик» для установки.", inst.MCVersion),
			})
		}
		if inst.InstanceType != TypeVanilla && inst.InstanceType != TypeEmpty {
			loaderInstalled := false
			activeVID := strings.TrimSpace(inst.LaunchSettings.ActiveVersionID)
			if activeVID != "" && activeVID != inst.MCVersion {
				loaderJSON := filepath.Join(m.sharedDir, "versions", activeVID, activeVID+".json")
				if fileExistsNonEmpty(loaderJSON) {
					loaderInstalled = true
				}
			}
			if !loaderInstalled {
				hasMissingComponent = true
				rep.Issues = append(rep.Issues, IntegrityIssue{
					Code:     "missing_modloader",
					Severity: "missing_component",
					Message:  fmt.Sprintf("Загрузчик %s не установлен для этого профиля. Установите его во вкладке «Версия и загрузчик».", strings.ToUpper(string(inst.InstanceType))),
				})
			}
		}
	}

	// 7. Count real mods inside minecraft/mods/
	mods := scanInstalledModFiles(filepath.Join(inst.GameDir, "mods"))
	rep.ModsCount = len(mods)
	_ = resolvedJava

	switch {
	case hasCorruption:
		rep.Status = StatusCorrupted
	case hasMissingComponent:
		rep.Status = StatusNeedsSetup
	default:
		rep.Status = StatusReady
	}
	return rep
}

func (m *Manager) resolveInstanceJava(inst *Instance) (javart.RequirementResult, *javart.JavaRuntime, bool, string) {
	req := javart.ResolveRequirement(javart.RequirementInput{
		MCVersion:     inst.MCVersion,
		Loader:        string(inst.InstanceType),
		LoaderVersion: inst.LoaderVersion,
	})
	if m.javaMgr == nil {
		return req, nil, false, fmt.Sprintf("Требуется Java %d (менеджер Java недоступен)", req.RecommendedMajor)
	}

	switch inst.JavaMode {
	case JavaModeCustom:
		if strings.TrimSpace(inst.JavaPath) == "" {
			return req, nil, false, "Выбран ручной режим Java, но путь к java.exe не указан"
		}
		if _, err := javart.ResolveJavaExecutable(inst.JavaPath); err != nil {
			return req, nil, false, "Указанный файл Java недоступен: " + inst.JavaPath
		}
		return req, &javart.JavaRuntime{
			Name:           "Пользовательская Java",
			ExecutablePath: inst.JavaPath,
			Source:         "custom",
			Verified:       true,
			Runnable:       true,
		}, true, "Пользовательская Java: " + inst.JavaPath

	case JavaModeManaged:
		snap := m.javaMgr.GetSnapshot(nil, false)
		for i := range snap.Runtimes {
			if snap.Runtimes[i].ID == inst.JavaRuntimeID {
				rt := snap.Runtimes[i]
				if rt.Verified && rt.Runnable {
					return req, &rt, true, fmt.Sprintf("%s (%s)", rt.Name, rt.Version)
				}
				return req, &rt, false, fmt.Sprintf("Выбранная Java (%s) недоступна или требует проверки", rt.Name)
			}
		}
		return req, nil, false, "Выбранная версия Java больше не найдена в менеджере Java"

	default:
		_, rec := m.javaMgr.RecommendForMC(javart.RequirementInput{
			MCVersion:     inst.MCVersion,
			Loader:        string(inst.InstanceType),
			LoaderVersion: inst.LoaderVersion,
		})
		if rec != nil {
			return req, rec, true, fmt.Sprintf("Авто: %s (%s)", rec.Name, rec.Version)
		}
		return req, nil, false, fmt.Sprintf("Не найдена совместимая Java %d (установите в Настройках → Java Runtime)", req.RecommendedMajor)
	}
}

func (m *Manager) enrichDynamicFields(inst *Instance) {
	req, resolved, _, _ := m.resolveInstanceJava(inst)
	reqCopy := req
	inst.JavaRequirement = &reqCopy
	inst.JavaResolved = resolved
	mods := scanInstalledModFiles(filepath.Join(inst.GameDir, "mods"))
	inst.InstalledMods = mods
	inst.ModsCount = len(mods)
	if inst.LinkedPackID != "" {
		if pk := m.findSavedPack(inst.LinkedPackID); pk != nil {
			inst.LinkedPackName = pk.Name
			inst.LinkedPackItems = len(pk.Items)
		}
	}
}

func scanInstalledModFiles(modsDir string) []InstalledModRef {
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return []InstalledModRef{}
	}
	out := make([]InstalledModRef, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		low := strings.ToLower(ent.Name())
		enabled := false
		if strings.HasSuffix(low, ".jar") {
			enabled = true
		} else if !strings.HasSuffix(low, ".jar.disabled") {
			continue
		}
		var sz int64
		if fi, err := ent.Info(); err == nil {
			sz = fi.Size()
		}
		out = append(out, InstalledModRef{
			Filename: ent.Name(),
			Size:     sz,
			Enabled:  enabled,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Filename) < strings.ToLower(out[j].Filename)
	})
	return out
}

func fileExistsNonEmpty(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Size() > 0
}

func (m *Manager) createProfileDirectoryStructure(rootDir, gameDir string) error {
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		return err
	}
	for _, sub := range ExpectedGameSubdirs {
		if err := os.MkdirAll(filepath.Join(gameDir, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) writeInstanceManifest(inst *Instance) error {
	mf := InstanceManifest{
		SchemaVersion:  manifestSchemaVersion,
		ID:             inst.ID,
		Name:           inst.Name,
		Description:    inst.Description,
		Icon:           inst.Icon,
		MCVersion:      inst.MCVersion,
		InstanceType:   inst.InstanceType,
		LoaderVersion:  inst.LoaderVersion,
		GameDir:        "minecraft",
		JavaMode:       inst.JavaMode,
		JavaRuntimeID:  inst.JavaRuntimeID,
		JavaPath:       inst.JavaPath,
		MemoryMinMB:    inst.MemoryMinMB,
		MemoryMaxMB:    inst.MemoryMaxMB,
		LaunchSettings: inst.LaunchSettings,
		LinkedPackID:   inst.LinkedPackID,
		LinkedPackName: inst.LinkedPackName,
		CreatedAt:      inst.CreatedAt,
		UpdatedAt:      inst.UpdatedAt,
		LastPlayedAt:   inst.LastPlayedAt,
	}
	return storage.WriteJSONFileAtomic(inst.RootDir, "instance.json", mf, 0o644)
}

func (m *Manager) copyInstanceLocked(inst *Instance) Instance {
	cp := *inst
	cp.InUse = m.busy[inst.ID] > 0
	if len(inst.Issues) > 0 {
		cp.Issues = append([]IntegrityIssue(nil), inst.Issues...)
	} else {
		cp.Issues = []IntegrityIssue{}
	}
	if len(inst.InstalledMods) > 0 {
		cp.InstalledMods = append([]InstalledModRef(nil), inst.InstalledMods...)
	} else {
		cp.InstalledMods = []InstalledModRef{}
	}
	return cp
}

func (m *Manager) recordToInstance(rec domain.InstanceRecord) *Instance {
	var ls LaunchSettings
	if strings.TrimSpace(rec.LaunchSettingsJSON) != "" {
		_ = json.Unmarshal([]byte(rec.LaunchSettingsJSON), &ls)
	}
	minMB, maxMB := normalizeMemory(rec.MemoryMinMB, rec.MemoryMaxMB)
	gameDir := rec.GameDir
	if gameDir == "" {
		gameDir = filepath.Join(rec.RootDir, "minecraft")
	}
	return &Instance{
		ID:             rec.ID,
		Name:           rec.Name,
		Description:    rec.Description,
		Icon:           normalizeIcon(rec.Icon),
		MCVersion:      rec.MCVersion,
		InstanceType:   normalizeInstanceType(InstanceType(rec.InstanceType), rec.MCVersion),
		LoaderVersion:  rec.LoaderVersion,
		RootDir:        filepath.Clean(rec.RootDir),
		GameDir:        filepath.Clean(gameDir),
		JavaMode:       normalizeJavaMode(JavaMode(rec.JavaMode)),
		JavaRuntimeID:  rec.JavaRuntimeID,
		JavaPath:       rec.JavaPath,
		MemoryMinMB:    minMB,
		MemoryMaxMB:    maxMB,
		LaunchSettings: ls,
		LinkedPackID:   rec.LinkedPackID,
		LinkedPackName: rec.LinkedPackName,
		Status:         Status(rec.Status),
		CreatedAt:      rec.CreatedAt,
		UpdatedAt:      rec.UpdatedAt,
		LastPlayedAt:   rec.LastPlayedAt,
		Issues:         []IntegrityIssue{},
		InstalledMods:  []InstalledModRef{},
	}
}

func (m *Manager) instanceToRecord(inst *Instance) domain.InstanceRecord {
	lsRaw, _ := json.Marshal(inst.LaunchSettings)
	return domain.InstanceRecord{
		ID:                 inst.ID,
		Name:               inst.Name,
		Description:        inst.Description,
		Icon:               inst.Icon,
		MCVersion:          inst.MCVersion,
		InstanceType:       string(inst.InstanceType),
		LoaderVersion:      inst.LoaderVersion,
		RootDir:            inst.RootDir,
		GameDir:            inst.GameDir,
		JavaMode:           string(inst.JavaMode),
		JavaRuntimeID:      inst.JavaRuntimeID,
		JavaPath:           inst.JavaPath,
		MemoryMinMB:        inst.MemoryMinMB,
		MemoryMaxMB:        inst.MemoryMaxMB,
		LaunchSettingsJSON: string(lsRaw),
		LinkedPackID:       inst.LinkedPackID,
		LinkedPackName:     inst.LinkedPackName,
		Status:             string(inst.Status),
		CreatedAt:          inst.CreatedAt,
		UpdatedAt:          inst.UpdatedAt,
		LastPlayedAt:       inst.LastPlayedAt,
	}
}

func (m *Manager) emitEvent(name events.EventName, payload any) {
	if m.bus != nil {
		m.bus.Publish(name, payload)
	}
}

func (m *Manager) ApplyInstalledVersion(id, mcVersion string, instType InstanceType, loaderVersion, activeVersionID string) error {
	if err := ValidateInstanceID(id); err != nil {
		return err
	}
	m.mu.Lock()
	inst, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return ErrInstanceNotFound
	}
	next := *inst
	next.MCVersion = strings.TrimSpace(mcVersion)
	next.InstanceType = normalizeInstanceType(instType, next.MCVersion)
	next.LoaderVersion = strings.TrimSpace(loaderVersion)
	if next.InstanceType == TypeVanilla || next.InstanceType == TypeEmpty {
		next.LoaderVersion = ""
	}
	next.LaunchSettings.ActiveVersionID = strings.TrimSpace(activeVersionID)
	next.UpdatedAt = time.Now().Unix()
	m.mu.Unlock()

	if err := m.writeInstanceManifest(&next); err != nil {
		return err
	}
	rep := m.evaluateIntegrity(&next, "")
	next.Status = rep.Status
	next.Issues = rep.Issues
	next.ModsCount = rep.ModsCount

	m.mu.Lock()
	if cur, exists := m.items[id]; exists {
		*cur = next
		if m.store != nil {
			_ = m.store.UpsertInstance(m.instanceToRecord(cur))
		}
		snap := m.copyInstanceLocked(cur)
		m.mu.Unlock()
		m.enrichDynamicFields(&snap)
		m.emitEvent(events.EventInstanceUpdated, snap)
		m.emitEvent(events.EventInstanceStatus, snap)
		return nil
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) BackupMetadata(id, reason string) (string, error) {
	inst, err := m.Get(id)
	if err != nil {
		return "", err
	}
	safeReason := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, strings.TrimSpace(reason))
	if safeReason == "" {
		safeReason = "backup"
	}
	backupDir := filepath.Join(m.dataDir, "backups", inst.ID, fmt.Sprintf("%s_%s", time.Now().UTC().Format("20060102_150405"), safeReason))
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", err
	}
	for _, pair := range [][2]string{
		{filepath.Join(inst.RootDir, "instance.json"), filepath.Join(backupDir, "instance.json")},
		{filepath.Join(inst.GameDir, "options.txt"), filepath.Join(backupDir, "options.txt")},
		{filepath.Join(inst.GameDir, "servers.dat"), filepath.Join(backupDir, "servers.dat")},
	} {
		if fi, serr := os.Stat(pair[0]); serr == nil && !fi.IsDir() {
			_ = copyRegularFile(pair[0], pair[1])
		}
	}
	return backupDir, nil
}

// MarkLastPlayed updates last_played_at in SQLite and instance.json when a real Minecraft process starts.
func (m *Manager) MarkLastPlayed(id string, playedAt int64) error {
	if err := ValidateInstanceID(id); err != nil {
		return err
	}
	if playedAt <= 0 {
		playedAt = time.Now().Unix()
	}
	m.mu.Lock()
	inst, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return ErrInstanceNotFound
	}
	next := *inst
	next.LastPlayedAt = playedAt
	next.UpdatedAt = playedAt
	m.mu.Unlock()

	_ = m.writeInstanceManifest(&next)

	m.mu.Lock()
	if cur, exists := m.items[id]; exists {
		cur.LastPlayedAt = playedAt
		cur.UpdatedAt = playedAt
		if m.store != nil {
			_ = m.store.UpsertInstance(m.instanceToRecord(cur))
		}
		snap := m.copyInstanceLocked(cur)
		m.mu.Unlock()
		m.enrichDynamicFields(&snap)
		m.emitEvent(events.EventInstanceUpdated, snap)
		return nil
	}
	m.mu.Unlock()
	return nil
}
