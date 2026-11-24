package modloader

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"modhub/internal/domain"
	"modhub/internal/downloads"
	"modhub/internal/events"
	"modhub/internal/instances"
	"modhub/internal/mcversion"
	"modhub/internal/netx"
	javart "modhub/internal/runtime"
)

var (
	ErrUnsupportedLoader = errors.New("неподдерживаемый загрузчик модов")
	ErrInstallInProgress = errors.New("установка для этого профиля уже выполняется")
	ErrInstalledNotFound = errors.New("установленная версия не найдена в профиле")
)

type activeInstallState struct {
	cancel   context.CancelFunc
	groupID  string
	progress mcversion.InstallProgress
}

type Manager struct {
	mcMgr     *mcversion.Manager
	dlMgr     *downloads.Manager
	javaMgr   *javart.Manager
	instMgr   *instances.Manager
	store     Store
	bus       *events.Bus
	providers map[LoaderType]LoaderProvider

	mu        sync.Mutex
	active    map[string]*activeInstallState
	closeOnce sync.Once
	wg        sync.WaitGroup
}

func NewManager(httpClient *netx.HTTPClient, mcMgr *mcversion.Manager, dlMgr *downloads.Manager, javaMgr *javart.Manager, instMgr *instances.Manager, store Store, bus *events.Bus) *Manager {
	return &Manager{
		mcMgr: mcMgr, dlMgr: dlMgr, javaMgr: javaMgr, instMgr: instMgr, store: store, bus: bus,
		providers: map[LoaderType]LoaderProvider{
			LoaderFabric:   NewFabricProvider(httpClient, dlMgr, mcMgr),
			LoaderQuilt:    NewQuiltProvider(httpClient, dlMgr, mcMgr),
			LoaderForge:    NewForgeProvider(httpClient, dlMgr, mcMgr, OSJavaProcessRunner{}),
			LoaderNeoForge: NewNeoForgeProvider(httpClient, dlMgr, mcMgr, OSJavaProcessRunner{}),
		},
		active: make(map[string]*activeInstallState),
	}
}

func (m *Manager) Provider(loader LoaderType) (LoaderProvider, bool) { p, ok := m.providers[loader]; return p, ok }

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		m.mu.Lock()
		for _, st := range m.active {
			if st.cancel != nil {
				st.cancel()
			}
			if m.dlMgr != nil && st.groupID != "" {
				m.dlMgr.CancelGroup(st.groupID)
			}
		}
		m.mu.Unlock()
		m.wg.Wait()
	})
}

func normalizeLoaderType(raw LoaderType) LoaderType {
	switch strings.ToLower(strings.TrimSpace(string(raw))) {
	case "", "vanilla":
		return LoaderVanilla
	case "fabric":
		return LoaderFabric
	case "quilt":
		return LoaderQuilt
	case "forge":
		return LoaderForge
	case "neoforge":
		return LoaderNeoForge
	default:
		return LoaderType(strings.ToLower(strings.TrimSpace(string(raw))))
	}
}

func (m *Manager) ListLoaderVersions(ctx context.Context, loader LoaderType, mcVersion string) (LoaderVersionsResult, error) {
	ld := normalizeLoaderType(loader)
	if ld == LoaderVanilla {
		return LoaderVersionsResult{Loader: LoaderVanilla, MCVersion: mcVersion, Compatible: true, Versions: []LoaderVersionInfo{}}, nil
	}
	prov, ok := m.providers[ld]
	if !ok {
		return LoaderVersionsResult{}, fmt.Errorf("%w: %s", ErrUnsupportedLoader, loader)
	}
	return prov.ListVersions(ctx, mcVersion)
}

func (m *Manager) GetActiveInstall(instanceID string) *mcversion.InstallProgress {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.active[instanceID]
	if !ok {
		return nil
	}
	cp := st.progress
	return &cp
}

func (m *Manager) CancelInstall(instanceID string) bool {
	m.mu.Lock()
	st, ok := m.active[instanceID]
	if !ok {
		m.mu.Unlock()
		return false
	}
	cancel, groupID := st.cancel, st.groupID
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if m.dlMgr != nil && groupID != "" {
		m.dlMgr.CancelGroup(groupID)
	}
	return true
}

func (m *Manager) InstallForInstanceAsync(req InstallProfileVersionRequest) error {
	if err := m.validateInstallReq(req); err != nil {
		return err
	}
	m.mu.Lock()
	if _, running := m.active[req.InstanceID]; running {
		m.mu.Unlock()
		return ErrInstallInProgress
	}
	ctx, cancel := context.WithCancel(context.Background())
	groupID := fmt.Sprintf("install:%s:%d", req.InstanceID, time.Now().UnixMilli())
	m.active[req.InstanceID] = &activeInstallState{
		cancel: cancel, groupID: groupID,
		progress: mcversion.InstallProgress{InstanceID: req.InstanceID, MCVersion: req.MCVersion, Loader: string(normalizeLoaderType(req.Loader)), LoaderVersion: req.LoaderVersion, Stage: "metadata", Message: "Подготовка установки...", Progress: -1, UpdatedAt: time.Now().Unix()},
	}
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		_, _ = m.runInstallLockedEntry(ctx, req, groupID)
	}()
	return nil
}

func (m *Manager) InstallForInstanceSync(ctx context.Context, req InstallProfileVersionRequest) (InstalledVersion, error) {
	if err := m.validateInstallReq(req); err != nil {
		return InstalledVersion{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if _, running := m.active[req.InstanceID]; running {
		m.mu.Unlock()
		return InstalledVersion{}, ErrInstallInProgress
	}
	iCtx, cancel := context.WithCancel(ctx)
	groupID := fmt.Sprintf("install:%s:%d", req.InstanceID, time.Now().UnixMilli())
	m.active[req.InstanceID] = &activeInstallState{cancel: cancel, groupID: groupID}
	m.mu.Unlock()
	return m.runInstallLockedEntry(iCtx, req, groupID)
}

func (m *Manager) validateInstallReq(req InstallProfileVersionRequest) error {
	if strings.TrimSpace(req.InstanceID) == "" {
		return fmt.Errorf("не указан идентификатор профиля")
	}
	if err := mcversion.ValidateVersionID(req.MCVersion); err != nil {
		return err
	}
	ld := normalizeLoaderType(req.Loader)
	if ld != LoaderVanilla {
		if _, ok := m.providers[ld]; !ok {
			return fmt.Errorf("%w: %s", ErrUnsupportedLoader, req.Loader)
		}
	}
	if m.instMgr != nil {
		if _, err := m.instMgr.Get(req.InstanceID); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) runInstallLockedEntry(ctx context.Context, req InstallProfileVersionRequest, groupID string) (InstalledVersion, error) {
	ld := normalizeLoaderType(req.Loader)
	mcVer := strings.TrimSpace(req.MCVersion)
	loaderVer := strings.TrimSpace(req.LoaderVersion)

	defer func() {
		m.mu.Lock()
		if st, ok := m.active[req.InstanceID]; ok && st.cancel != nil {
			st.cancel()
		}
		delete(m.active, req.InstanceID)
		m.mu.Unlock()
	}()

	if m.instMgr != nil {
		if err := m.instMgr.AcquireBusy(req.InstanceID); err != nil {
			return InstalledVersion{}, err
		}
		defer m.instMgr.ReleaseBusy(req.InstanceID)
	}

	emitProgress := func(p mcversion.InstallProgress) {
		p.InstanceID = req.InstanceID
		p.MCVersion = mcVer
		p.Loader = string(ld)
		if p.LoaderVersion == "" {
			p.LoaderVersion = loaderVer
		}
		p.UpdatedAt = time.Now().Unix()
		if m.dlMgr != nil {
			p.SpeedBps = m.dlMgr.GetSnapshot().Summary.TotalSpeedBytesPerSec
		}
		m.mu.Lock()
		if st, ok := m.active[req.InstanceID]; ok {
			st.progress = p
		}
		m.mu.Unlock()
		if m.bus != nil {
			m.bus.Publish(events.EventInstallProgress, p)
		}
	}

	fail := func(err error) (InstalledVersion, error) {
		if m.dlMgr != nil {
			m.dlMgr.CancelGroup(groupID)
		}
		stage, evName := "error", events.EventInstallError
		if errors.Is(err, context.Canceled) {
			stage, evName = "cancelled", events.EventInstallCancelled
		}
		p := mcversion.InstallProgress{InstanceID: req.InstanceID, MCVersion: mcVer, Loader: string(ld), LoaderVersion: loaderVer, Stage: stage, Message: err.Error(), Error: err.Error(), Progress: -1, UpdatedAt: time.Now().Unix()}
		if m.bus != nil {
			m.bus.Publish(evName, p)
			m.bus.Publish(events.EventInstallStatus, p)
		}
		return InstalledVersion{}, err
	}

	if req.CreateBackup && m.instMgr != nil {
		_, _ = m.instMgr.BackupMetadata(req.InstanceID, "pre_version_install")
	}

	meta, vanillaRep, err := m.mcMgr.InstallVanilla(ctx, req.InstanceID, mcVer, groupID+":vanilla", emitProgress)
	if err != nil {
		return fail(err)
	}

	javaReq := m.mcMgr.RequiredJavaForMetadata(meta)
	var javaRTID, javaExe string
	javaMajor := javaReq.RecommendedMajor
	if m.instMgr != nil {
		if inst, ierr := m.instMgr.Get(req.InstanceID); ierr == nil && inst.JavaResolved != nil && inst.JavaResolved.Runnable {
			javaRTID, javaExe = inst.JavaResolved.ID, inst.JavaResolved.ExecutablePath
			if inst.JavaResolved.MajorVersion > 0 {
				javaMajor = inst.JavaResolved.MajorVersion
			}
		}
	}
	if javaExe == "" && m.javaMgr != nil {
		if _, rec := m.javaMgr.RecommendForMC(javart.RequirementInput{MCVersion: mcVer, ManifestJavaMajor: javaReq.RecommendedMajor, Loader: string(ld)}); rec != nil {
			javaRTID, javaExe, javaMajor = rec.ID, rec.ExecutablePath, rec.MajorVersion
		}
	}

	finalVersionID := mcVer
	finalMetadataPath := m.mcMgr.VersionJSONPath(mcVer)
	finalLoaderVer := ""
	if ld != LoaderVanilla {
		prov := m.providers[ld]
		if javaRTID != "" && m.javaMgr != nil {
			m.javaMgr.AcquireInUse(javaRTID)
			defer m.javaMgr.ReleaseInUse(javaRTID)
		}
		lRes, err := prov.Install(ctx, LoaderInstallContext{InstanceID: req.InstanceID, MCVersion: mcVer, LoaderVersion: loaderVer, GroupID: groupID + ":" + string(ld), JavaExePath: javaExe, OnProgress: emitProgress})
		if err != nil {
			return fail(err)
		}
		finalVersionID, finalMetadataPath, finalLoaderVer = lRes.VersionID, lRes.MetadataPath, lRes.LoaderVersion
		if lRes.JavaMajor > 0 {
			javaMajor = lRes.JavaMajor
		}
	}

	emitProgress(mcversion.InstallProgress{Stage: "verifying", Message: fmt.Sprintf("Финальная проверка установки %s...", finalVersionID), Progress: 99})
	fullValid := vanillaRep.Valid
	verifyErr := ""
	if ld != LoaderVanilla {
		if lCheck := m.providers[ld].Verify(ctx, finalVersionID); !lCheck.Valid {
			fullValid = false
			verifyErr = strings.Join(lCheck.Errors, "; ")
		}
	}
	if !fullValid {
		return fail(fmt.Errorf("финальная проверка целостности не пройдена: %s", verifyErr))
	}

	now := time.Now().Unix()
	modloaderField := ""
	if ld != LoaderVanilla {
		modloaderField = string(ld)
	}
	rec := domain.InstalledVersionRecord{
		ID: makeInstalledVersionID(req.InstanceID, finalVersionID), InstanceID: req.InstanceID, VersionID: finalVersionID,
		MCVersion: mcVer, InstallType: string(ld), Modloader: modloaderField, LoaderVersion: finalLoaderVer,
		JavaRuntimeID: javaRTID, JavaPath: javaExe, JavaMajor: javaMajor, MetadataPath: finalMetadataPath,
		InstalledAt: now, Verified: true, VerifiedAt: now, Active: true,
	}
	if m.store != nil {
		if err := m.store.UpsertInstalledVersion(rec); err != nil {
			return fail(fmt.Errorf("не удалось сохранить запись установленной версии: %w", err))
		}
	}
	if m.instMgr != nil {
		if err := m.instMgr.ApplyInstalledVersion(req.InstanceID, mcVer, instances.InstanceType(ld), finalLoaderVer, finalVersionID); err != nil {
			return fail(err)
		}
	}

	out := recordToInstalledVersion(rec)
	completedProg := mcversion.InstallProgress{InstanceID: req.InstanceID, MCVersion: mcVer, Loader: string(ld), LoaderVersion: finalLoaderVer, Stage: "completed", Message: fmt.Sprintf("Установка %s успешно завершена", finalVersionID), Progress: 100, UpdatedAt: now}
	if m.bus != nil {
		m.bus.Publish(events.EventInstallCompleted, completedProg)
		m.bus.Publish(events.EventInstallStatus, completedProg)
		m.bus.Publish(events.EventVersionsUpdated, map[string]any{"instance_id": req.InstanceID})
	}
	return out, nil
}

func (m *Manager) ListInstalledVersions(instanceID string) ([]InstalledVersion, error) {
	if m.store == nil {
		return []InstalledVersion{}, nil
	}
	recs, err := m.store.ListInstalledVersions(instanceID)
	if err != nil {
		return nil, err
	}
	out := make([]InstalledVersion, 0, len(recs))
	for _, r := range recs {
		out = append(out, recordToInstalledVersion(r))
	}
	return out, nil
}

func (m *Manager) ActivateInstalledVersion(instanceID, installedID string, createBackup bool) (InstalledVersion, error) {
	if m.store == nil {
		return InstalledVersion{}, ErrInstalledNotFound
	}
	recs, err := m.store.ListInstalledVersions(instanceID)
	if err != nil {
		return InstalledVersion{}, err
	}
	var target *domain.InstalledVersionRecord
	for i := range recs {
		if recs[i].ID == installedID {
			cp := recs[i]
			target = &cp
			break
		}
	}
	if target == nil {
		return InstalledVersion{}, ErrInstalledNotFound
	}
	if createBackup && m.instMgr != nil {
		_, _ = m.instMgr.BackupMetadata(instanceID, "pre_version_switch")
	}
	if err := m.store.SetActiveInstalledVersion(instanceID, installedID); err != nil {
		return InstalledVersion{}, err
	}
	target.Active = true
	if m.instMgr != nil {
		_ = m.instMgr.ApplyInstalledVersion(instanceID, target.MCVersion, instances.InstanceType(target.InstallType), target.LoaderVersion, target.VersionID)
	}
	if m.bus != nil {
		m.bus.Publish(events.EventVersionsUpdated, map[string]any{"instance_id": instanceID})
	}
	return recordToInstalledVersion(*target), nil
}

func (m *Manager) VerifyInstalledVersion(ctx context.Context, instanceID, installedID string) (FullVerificationReport, error) {
	if m.store == nil {
		return FullVerificationReport{}, ErrInstalledNotFound
	}
	recs, err := m.store.ListInstalledVersions(instanceID)
	if err != nil {
		return FullVerificationReport{}, err
	}
	var target *domain.InstalledVersionRecord
	for i := range recs {
		if recs[i].ID == installedID {
			cp := recs[i]
			target = &cp
			break
		}
	}
	if target == nil {
		return FullVerificationReport{}, ErrInstalledNotFound
	}
	vRep := m.mcMgr.VerifyVanilla(target.MCVersion)
	full := FullVerificationReport{InstalledID: target.ID, VersionID: target.VersionID, MCVersion: target.MCVersion, Loader: LoaderType(target.InstallType), LoaderVersion: target.LoaderVersion, Vanilla: vRep, Valid: vRep.Valid, Errors: append([]string(nil), vRep.Errors...)}
	if ld := normalizeLoaderType(LoaderType(target.InstallType)); ld != LoaderVanilla {
		if prov, ok := m.providers[ld]; ok {
			lc := prov.Verify(ctx, target.VersionID)
			full.LoaderCheck = &lc
			if !lc.Valid {
				full.Valid = false
				full.Errors = append(full.Errors, lc.Errors...)
			}
		}
	}
	target.Verified = full.Valid
	target.VerifiedAt = time.Now().Unix()
	target.VerifyError = strings.Join(full.Errors, "; ")
	_ = m.store.UpsertInstalledVersion(*target)
	if m.instMgr != nil && target.Active {
		_, _ = m.instMgr.Verify(instanceID)
	}
	if m.bus != nil {
		m.bus.Publish(events.EventVersionsUpdated, map[string]any{"instance_id": instanceID})
	}
	return full, nil
}

func (m *Manager) DeleteInstalledVersion(instanceID, installedID string) error {
	if m.store == nil {
		return ErrInstalledNotFound
	}
	recs, err := m.store.ListInstalledVersions(instanceID)
	if err != nil {
		return err
	}
	var target *domain.InstalledVersionRecord
	for i := range recs {
		if recs[i].ID == installedID {
			cp := recs[i]
			target = &cp
			break
		}
	}
	if target == nil {
		return ErrInstalledNotFound
	}
	if err := m.store.DeleteInstalledVersion(instanceID, installedID); err != nil {
		return err
	}
	if target.InstallType != string(LoaderVanilla) && m.store.CountVersionIDReferences(target.VersionID) == 0 {
		_ = os.Remove(m.mcMgr.VersionJSONPath(target.VersionID))
	}
	if m.instMgr != nil && target.Active {
		_, _ = m.instMgr.Verify(instanceID)
	}
	if m.bus != nil {
		m.bus.Publish(events.EventVersionsUpdated, map[string]any{"instance_id": instanceID})
	}
	return nil
}

func makeInstalledVersionID(instanceID, versionID string) string {
	h := sha1.Sum([]byte(instanceID + "|" + versionID))
	return "iv_" + hex.EncodeToString(h[:])[:14]
}

func recordToInstalledVersion(r domain.InstalledVersionRecord) InstalledVersion {
	return InstalledVersion{
		ID: r.ID, InstanceID: r.InstanceID, VersionID: r.VersionID, MCVersion: r.MCVersion,
		InstallType: LoaderType(r.InstallType), Modloader: LoaderType(r.Modloader), LoaderVersion: r.LoaderVersion,
		JavaRuntimeID: r.JavaRuntimeID, JavaPath: r.JavaPath, JavaMajor: r.JavaMajor, MetadataPath: r.MetadataPath,
		InstalledAt: r.InstalledAt, Verified: r.Verified, VerifiedAt: r.VerifiedAt, VerifyError: r.VerifyError, Active: r.Active,
	}
}
