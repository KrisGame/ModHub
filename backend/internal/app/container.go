package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"modhub/internal/accounts"
	"modhub/internal/catalog"
	"modhub/internal/domain"
	"modhub/internal/downloads"
	"modhub/internal/events"
	"modhub/internal/instances"
	"modhub/internal/launcher"
	"modhub/internal/mcversion"
	"modhub/internal/modloader"
	"modhub/internal/modpacks"
	"modhub/internal/netx"
	javart "modhub/internal/runtime"
	"modhub/internal/security"
	"modhub/internal/settings"
	"modhub/internal/storage"
)

var ErrValidation = errors.New("validation error")
var ErrNotFound = errors.New("not found")

type ConfigUpdatePatch struct {
	CurseForgeAPIKey       *string
	MergeThreshold         *int
	ProxyURL               *string
	AutoInstallMediumDeps  *bool
	MaxConcurrentDownloads *int
	MicrosoftClientID      *string
}

type Container struct {
	DataDir      string
	MediaDir     string
	RuntimesDir  string
	SharedDir    string
	InstancesDir string
	VaultDir     string

	DB         *storage.Database
	HTTP       *netx.HTTPClient
	Events     *events.Bus
	Icons      *catalog.IconCache
	Downloads  *downloads.Manager
	Java       *javart.Manager
	Instances  *instances.Manager
	Mods       *instances.Mods
	Content    *instances.Content
	MCVersions *mcversion.Manager
	Modloaders *modloader.Manager
	Accounts   *accounts.Manager
	Launcher   *launcher.Manager
	Modpacks   *modpacks.Manager

	SearchCache     *storage.LRUCache[[]byte]
	DetailsCache    *storage.LRUCache[[]byte]
	DepTreeCache    *storage.LRUCache[[]byte]
	DependentsCache *storage.LRUCache[[]byte]

	cfgMu sync.RWMutex
	cfg   domain.AppConfig

	packsMu sync.RWMutex
	packs   []domain.SavedPack

	ovMu      sync.RWMutex
	overrides domain.OverridesData

	syncMu        sync.Mutex
	syncing       atomic.Bool
	syncPhase     string
	syncProgress  int
	syncTotal     int
	lastCompleted float64
	indexVersion  atomic.Int64

	bgCtx     context.Context
	bgCancel  context.CancelFunc
	bgWg      sync.WaitGroup
	closeOnce sync.Once
}

func NewContainer(dataDir string, db *storage.Database) *Container {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	mediaDir := filepath.Join(dataDir, "media_cache")
	runtimesDir := filepath.Join(dataDir, "runtimes")
	sharedDir := filepath.Join(dataDir, "shared")
	instancesDir := filepath.Join(dataDir, "instances")
	vaultDir := filepath.Join(dataDir, "vault")
	cfg := settings.LoadAppConfig(dataDir)
	httpClient := netx.NewHTTPClient(db)
	bus := events.NewBus()
	dlMgr := downloads.NewManager(httpClient, db, bus, cfg.MaxConcurrentDownloads)
	javaMgr := javart.NewManager(runtimesDir, httpClient, dlMgr, db, bus)
	c := &Container{
		DataDir:         dataDir,
		MediaDir:        mediaDir,
		RuntimesDir:     runtimesDir,
		SharedDir:       sharedDir,
		InstancesDir:    instancesDir,
		VaultDir:        vaultDir,
		DB:              db,
		HTTP:            httpClient,
		Events:          bus,
		Icons:           catalog.NewIconCache(mediaDir),
		Downloads:       dlMgr,
		Java:            javaMgr,
		SearchCache:     storage.NewLRUCache[[]byte](128),
		DetailsCache:    storage.NewLRUCache[[]byte](128),
		DepTreeCache:    storage.NewLRUCache[[]byte](192),
		DependentsCache: storage.NewLRUCache[[]byte](128),
		cfg:             cfg,
		packs:           settings.LoadPacksCompat(dataDir),
		overrides:       settings.LoadOverrides(dataDir),
		bgCtx:           bgCtx,
		bgCancel:        bgCancel,
	}
	c.Instances = instances.NewManager(dataDir, instancesDir, sharedDir, runtimesDir, mediaDir, db, javaMgr, bus, c.GetPacks)
	c.Mods = instances.NewMods(c.Instances, dlMgr, bus, httpClient)
	c.Content = instances.NewContent(c.Instances, dlMgr, bus)
	c.MCVersions = mcversion.NewManager(sharedDir, httpClient, dlMgr, javaMgr, bus)
	c.Modloaders = modloader.NewManager(httpClient, c.MCVersions, dlMgr, javaMgr, c.Instances, db, bus)
	c.Accounts = accounts.NewManager(vaultDir, db, bus, func() string { return c.GetConfig().MicrosoftClientID }, nil)
	c.Launcher = launcher.NewManager(sharedDir, c.Instances, c.MCVersions, c.Modloaders, javaMgr, c.Accounts, db, bus)
	c.Modpacks = modpacks.NewManager(filepath.Join(dataDir, "modpacks_cache"), httpClient, dlMgr, c.Instances, c.Modloaders, db, bus, func() string { return c.GetConfig().CurseForgeAPIKey }, c.GetPacks)
	// Local-only discovery runs once for desktop and web, without delaying startup.
	c.bgWg.Add(1)
	go func() {
		defer c.bgWg.Done()
		c.Java.ScanInstalled(c.bgCtx)
	}()
	netx.SetManualProxyURL(c.cfg.ProxyURL)
	c.indexVersion.Store(1)
	return c
}

// CheckAndRepairInstance verifies then restores safe missing profile structure and
// known managed modpack files. Unrepairable components remain visible in the report.
func (c *Container) CheckAndRepairInstance(id string) (instances.IntegrityReport, error) {
	rep, err := c.Instances.Verify(id)
	if err != nil {
		return rep, err
	}
	if rep.Status == instances.StatusCorrupted {
		rep, err = c.Instances.RepairMissingStructure(id)
		if err != nil {
			return rep, err
		}
	}
	if err := c.Instances.AcquireBusyExclusive(id); err != nil {
		return rep, err
	}
	defer c.Instances.ReleaseBusy(id)
	issues := c.Modpacks.RepairMissing(c.bgCtx, id)
	rep, err = c.Instances.Verify(id)
	if err != nil {
		return rep, err
	}
	rep.Issues = append(rep.Issues, issues...)
	if len(issues) > 0 && rep.Status == instances.StatusReady {
		rep.Status = instances.StatusNeedsSetup
	}
	return rep, nil
}

func (c *Container) Close() {
	if c == nil {
		return
	}
	c.closeOnce.Do(func() {
		if c.bgCancel != nil {
			c.bgCancel()
		}
		c.bgWg.Wait()
		if c.Modpacks != nil {
			c.Modpacks.Close()
		}
		if c.Launcher != nil {
			c.Launcher.Close()
		}
		if c.Accounts != nil {
			c.Accounts.Close()
		}
		if c.Modloaders != nil {
			c.Modloaders.Close()
		}
		if c.Instances != nil {
			c.Instances.Close()
		}
		if c.Java != nil {
			c.Java.Close()
		}
		if c.Downloads != nil {
			c.Downloads.Close()
		}
		if c.Icons != nil {
			c.Icons.Close()
		}
		if c.HTTP != nil {
			c.HTTP.ResetConnections()
		}
		if c.Events != nil {
			c.Events.Close()
		}
	})
}

// CreateInstanceAndPrepare creates the isolated profile and immediately queues background downloads
// for all required Minecraft client files, libraries, assets, and selected modloader (without Java).
func (c *Container) CreateInstanceAndPrepare(req instances.CreateInstanceRequest) (instances.Instance, error) {
	if c == nil || c.Instances == nil {
		return instances.Instance{}, fmt.Errorf("менеджер профилей не инициализирован")
	}
	inst, err := c.Instances.Create(req)
	if err != nil {
		return instances.Instance{}, err
	}
	if c.Modloaders != nil && strings.TrimSpace(inst.MCVersion) != "" && inst.InstanceType != instances.TypeEmpty {
		_ = c.Modloaders.InstallForInstanceAsync(modloader.InstallProfileVersionRequest{
			InstanceID:    inst.ID,
			MCVersion:     inst.MCVersion,
			Loader:        modloader.LoaderType(inst.InstanceType),
			LoaderVersion: inst.LoaderVersion,
			CreateBackup:  false,
		})
	}
	return inst, nil
}

func (c *Container) GetConfig() domain.AppConfig {
	c.cfgMu.RLock()
	defer c.cfgMu.RUnlock()
	return c.cfg
}

func (c *Container) SetConfigForTest(cfg domain.AppConfig) {
	c.cfgMu.Lock()
	c.cfg = cfg
	c.cfgMu.Unlock()
}

func (c *Container) GetOverrides() domain.OverridesData {
	c.ovMu.RLock()
	defer c.ovMu.RUnlock()
	return domain.OverridesData{
		Merges: append([][]string{}, c.overrides.Merges...),
		Splits: append([][]string{}, c.overrides.Splits...),
	}
}

func (c *Container) GetPacks() []domain.SavedPack {
	c.packsMu.RLock()
	defer c.packsMu.RUnlock()
	return append([]domain.SavedPack{}, c.packs...)
}

func (c *Container) ClearResponseCaches() {
	c.SearchCache.Clear()
	c.DetailsCache.Clear()
	c.DepTreeCache.Clear()
	c.DependentsCache.Clear()
}

func (c *Container) ClearAllCaches() {
	if c.DB != nil {
		c.DB.CacheClear()
	}
	c.ClearResponseCaches()
	if c.Icons != nil {
		c.Icons.Clear()
	}
	c.Events.Publish(events.EventCacheCleared, nil)
}

func (c *Container) PublicConfigPayload() map[string]any {
	cfg := c.GetConfig()
	key := security.SanitizeCFKey(cfg.CurseForgeAPIKey)
	return map[string]any{
		"ok":                       true,
		"curseforge_configured":    key != "",
		"curseforge_key_masked":    security.MaskCFKey(key),
		"merge_threshold":          cfg.MergeThreshold,
		"proxy_url":                strings.TrimSpace(cfg.ProxyURL),
		"proxy_detected":           netx.GetProxyStatus(),
		"auto_install_medium_deps": cfg.AutoInstallMediumDeps,
		"max_concurrent_downloads": cfg.MaxConcurrentDownloads,
		"microsoft_configured":     c.Accounts != nil && c.Accounts.GetConfigStatus().Configured,
		"microsoft_client_id":      strings.TrimSpace(cfg.MicrosoftClientID),
		"port":                     cfg.Port,
	}
}

func (c *Container) UpdateConfig(patch ConfigUpdatePatch) (map[string]any, error) {
	proxyChanged := false
	c.cfgMu.Lock()
	nextCfg := c.cfg
	if patch.CurseForgeAPIKey != nil {
		nextCfg.CurseForgeAPIKey = security.SanitizeCFKey(*patch.CurseForgeAPIKey)
	}
	if patch.MergeThreshold != nil {
		t := *patch.MergeThreshold
		if t < 40 {
			t = 40
		} else if t > 95 {
			t = 95
		}
		nextCfg.MergeThreshold = t
	}
	if patch.ProxyURL != nil {
		pv := strings.TrimSpace(*patch.ProxyURL)
		if pv != "" {
			low := strings.ToLower(pv)
			if low != "direct" && low != "none" && low != "off" {
				if u, err := netx.NormalizeProxyURL(pv, "http"); err != nil || u == nil || u.Hostname() == "" {
					c.cfgMu.Unlock()
					return nil, fmt.Errorf("%w: Некорректный адрес прокси (ожидается http://, socks5:// или direct)", ErrValidation)
				}
			}
		}
		nextCfg.ProxyURL = pv
		proxyChanged = true
	}
	if patch.AutoInstallMediumDeps != nil {
		nextCfg.AutoInstallMediumDeps = *patch.AutoInstallMediumDeps
	}
	if patch.MaxConcurrentDownloads != nil {
		mc := *patch.MaxConcurrentDownloads
		if mc < 1 {
			mc = 1
		} else if mc > 32 {
			mc = 32
		}
		nextCfg.MaxConcurrentDownloads = mc
	}
	if patch.MicrosoftClientID != nil {
		nextCfg.MicrosoftClientID = strings.TrimSpace(*patch.MicrosoftClientID)
	}
	if err := settings.SaveAppConfig(c.DataDir, nextCfg); err != nil {
		c.cfgMu.Unlock()
		return nil, fmt.Errorf("Не удалось сохранить конфигурацию на диск: %w", err)
	}
	c.cfg = nextCfg
	cfgCopy := c.cfg
	c.cfgMu.Unlock()
	if patch.MaxConcurrentDownloads != nil && c.Downloads != nil {
		c.Downloads.SetMaxConcurrent(cfgCopy.MaxConcurrentDownloads)
	}

	c.ClearResponseCaches()
	if proxyChanged {
		netx.SetManualProxyURL(cfgCopy.ProxyURL)
		c.HTTP.ResetConnections()
		if c.Icons != nil {
			c.Icons.ResetConnections()
		}
		if c.Accounts != nil {
			c.Accounts.ResetConnections()
		}
	}
	payload := c.PublicConfigPayload()
	c.Events.Publish(events.EventConfigUpdated, payload)
	return payload, nil
}

func (c *Container) CheckNetwork() domain.NetworkCheckResult {
	return c.HTTP.CheckAPIConnectivity(c.GetConfig().CurseForgeAPIKey)
}

func (c *Container) TestCurseForgeKey(candidateKey string) error {
	return catalog.TestCurseForgeKey(c.HTTP, candidateKey)
}

func (c *Container) AddMergeOverride(sourceKeys []string) (domain.OverridesData, error) {
	if len(sourceKeys) < 2 || len(sourceKeys) > 12 {
		return domain.OverridesData{}, fmt.Errorf("%w: Укажите от 2 до 12 source_keys для объединения", ErrValidation)
	}
	c.ovMu.Lock()
	next := domain.OverridesData{
		Merges: append(append([][]string{}, c.overrides.Merges...), append([]string{}, sourceKeys...)),
		Splits: append([][]string{}, c.overrides.Splits...),
	}
	if err := settings.SaveOverrides(c.DataDir, next); err != nil {
		c.ovMu.Unlock()
		return domain.OverridesData{}, fmt.Errorf("Не удалось сохранить overrides.json: %w", err)
	}
	c.overrides = next
	ov := c.overrides
	c.ovMu.Unlock()
	c.ClearResponseCaches()
	c.Events.Publish(events.EventOverridesUpdated, ov)
	return ov, nil
}

func (c *Container) AddSplitOverride(sourceKeys []string) (domain.OverridesData, error) {
	if len(sourceKeys) < 2 || len(sourceKeys) > 12 {
		return domain.OverridesData{}, fmt.Errorf("%w: Укажите от 2 до 12 source_keys для разделения", ErrValidation)
	}
	c.ovMu.Lock()
	next := domain.OverridesData{
		Merges: append([][]string{}, c.overrides.Merges...),
		Splits: append(append([][]string{}, c.overrides.Splits...), append([]string{}, sourceKeys...)),
	}
	if err := settings.SaveOverrides(c.DataDir, next); err != nil {
		c.ovMu.Unlock()
		return domain.OverridesData{}, fmt.Errorf("Не удалось сохранить overrides.json: %w", err)
	}
	c.overrides = next
	ov := c.overrides
	c.ovMu.Unlock()
	c.ClearResponseCaches()
	c.Events.Publish(events.EventOverridesUpdated, ov)
	return ov, nil
}

func (c *Container) DeleteOverride(kind string, index int) (domain.OverridesData, error) {
	if kind != "merges" && kind != "splits" {
		return domain.OverridesData{}, fmt.Errorf("%w: Поле kind должно быть 'merges' или 'splits'", ErrValidation)
	}
	c.ovMu.Lock()
	next := domain.OverridesData{
		Merges: append([][]string{}, c.overrides.Merges...),
		Splits: append([][]string{}, c.overrides.Splits...),
	}
	if kind == "merges" {
		if index < 0 || index >= len(next.Merges) {
			c.ovMu.Unlock()
			return domain.OverridesData{}, fmt.Errorf("%w: Некорректный индекс правила", ErrValidation)
		}
		next.Merges = append(next.Merges[:index], next.Merges[index+1:]...)
	} else {
		if index < 0 || index >= len(next.Splits) {
			c.ovMu.Unlock()
			return domain.OverridesData{}, fmt.Errorf("%w: Некорректный индекс правила", ErrValidation)
		}
		next.Splits = append(next.Splits[:index], next.Splits[index+1:]...)
	}
	if err := settings.SaveOverrides(c.DataDir, next); err != nil {
		c.ovMu.Unlock()
		return domain.OverridesData{}, fmt.Errorf("Не удалось сохранить overrides.json: %w", err)
	}
	c.overrides = next
	ov := c.overrides
	c.ovMu.Unlock()
	c.ClearResponseCaches()
	c.Events.Publish(events.EventOverridesUpdated, ov)
	return ov, nil
}

func (c *Container) ResetOverrides() (domain.OverridesData, error) {
	c.ovMu.Lock()
	next := domain.OverridesData{Merges: [][]string{}, Splits: [][]string{}}
	if err := settings.SaveOverrides(c.DataDir, next); err != nil {
		c.ovMu.Unlock()
		return domain.OverridesData{}, fmt.Errorf("Не удалось сбросить overrides.json: %w", err)
	}
	c.overrides = next
	ov := c.overrides
	c.ovMu.Unlock()
	c.ClearResponseCaches()
	c.Events.Publish(events.EventOverridesUpdated, ov)
	return ov, nil
}

func (c *Container) SavePack(pack domain.SavedPack) ([]domain.SavedPack, error) {
	pack.Name = strings.TrimSpace(pack.Name)
	if pack.Name == "" {
		pack.Name = "My-Modpack"
	}
	if len(pack.Items) == 0 {
		return nil, fmt.Errorf("%w: Нельзя сохранить пустую сборку", ErrValidation)
	}
	if len(pack.Items) > 500 {
		return nil, fmt.Errorf("%w: Слишком много элементов в сборке (максимум 500)", ErrValidation)
	}
	if pack.ID == "" {
		pack.ID = fmt.Sprintf("pack_%d", time.Now().UnixMilli())
	}
	if pack.CreatedAt == "" {
		pack.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	c.packsMu.Lock()
	next := append([]domain.SavedPack{}, c.packs...)
	replaced := false
	for i := range next {
		if next[i].ID == pack.ID {
			next[i] = pack
			replaced = true
			break
		}
	}
	if !replaced {
		next = append([]domain.SavedPack{pack}, next...)
	}
	if err := settings.SavePacksCompat(c.DataDir, next); err != nil {
		c.packsMu.Unlock()
		return nil, fmt.Errorf("Не удалось сохранить сборку на диск: %w", err)
	}
	c.packs = next
	out := append([]domain.SavedPack{}, c.packs...)
	c.packsMu.Unlock()
	c.Events.Publish(events.EventPacksUpdated, len(out))
	return out, nil
}

func (c *Container) DeletePack(id string) ([]domain.SavedPack, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: Не указан идентификатор сборки (id)", ErrValidation)
	}
	c.packsMu.Lock()
	next := make([]domain.SavedPack, 0, len(c.packs))
	for _, p := range c.packs {
		if p.ID != id {
			next = append(next, p)
		}
	}
	if err := settings.SavePacksCompat(c.DataDir, next); err != nil {
		c.packsMu.Unlock()
		return nil, fmt.Errorf("Не удалось обновить packs.json на диск: %w", err)
	}
	c.packs = next
	out := append([]domain.SavedPack{}, c.packs...)
	c.packsMu.Unlock()
	c.Events.Publish(events.EventPacksUpdated, len(out))
	return out, nil
}

func (c *Container) IndexStatusPayload() map[string]any {
	rawCounts := c.DB.CountsByType()
	platCounts := c.DB.PlatformCountsByType()
	c.syncMu.Lock()
	phase := c.syncPhase
	prog := c.syncProgress
	tot := c.syncTotal
	lastDone := c.lastCompleted
	c.syncMu.Unlock()
	return map[string]any{
		"ok":              true,
		"running":         c.syncing.Load(),
		"phase":           phase,
		"progress":        prog,
		"total_steps":     tot,
		"last_completed":  lastDone,
		"last_error":      "",
		"index_version":   c.indexVersion.Load(),
		"schema_version":  c.DB.SchemaVersion(),
		"fts5":            c.DB.HasFTS5(),
		"raw_counts":      rawCounts,
		"merged_counts":   rawCounts,
		"platform_counts": platCounts,
		"media_cache":     c.Icons.Stats(),
	}
}

func (c *Container) IndexSnapshot() map[string]any {
	return catalog.BuildBrowserSnapshot(c.DB, c.indexVersion.Load())
}

func (c *Container) TriggerBackgroundSync(full bool) bool {
	if c.bgCtx != nil && c.bgCtx.Err() != nil {
		return false
	}
	if !c.syncing.CompareAndSwap(false, true) {
		return false
	}
	c.bgWg.Add(1)
	go func() {
		defer c.bgWg.Done()
		defer c.syncing.Store(false)
		bgCtx := c.bgCtx
		if bgCtx == nil {
			bgCtx = context.Background()
		}
		hc := c.HTTP.WithContext(bgCtx)
		cfKey := security.SanitizeCFKey(c.GetConfig().CurseForgeAPIKey)
		types := []string{"mod", "resourcepack", "shader", "datapack"}

		c.syncMu.Lock()
		c.syncTotal = len(types) + 1
		c.syncProgress = 0
		c.syncMu.Unlock()
		c.Events.Publish(events.EventIndexSyncStarted, map[string]any{"full": full})

		for _, ct := range types {
			if bgCtx.Err() != nil {
				return
			}
			c.syncMu.Lock()
			c.syncPhase = "sync:" + ct
			c.syncMu.Unlock()

			pages := 2
			if ct == "mod" {
				if full {
					pages = 4
				} else {
					pages = 3
				}
			}
			for pg := 0; pg < pages; pg++ {
				if bgCtx.Err() != nil {
					return
				}
				if items, _, err := catalog.SearchModrinth(hc, "", ct, "", nil, nil, nil, nil, "and", "", "downloads", 50, pg*50); err == nil && len(items) > 0 {
					c.DB.UpsertCatalogItems(items)
				}
				if cfKey != "" && bgCtx.Err() == nil {
					if items, _, err := catalog.SearchCurseForge(hc, cfKey, "", ct, "", "", "downloads", 50, pg*50); err == nil && len(items) > 0 {
						c.DB.UpsertCatalogItems(items)
					}
				}
			}
			c.syncMu.Lock()
			c.syncProgress++
			prog := c.syncProgress
			tot := c.syncTotal
			c.syncMu.Unlock()
			c.Events.Publish(events.EventIndexSyncProgress, map[string]any{"phase": "sync:" + ct, "progress": prog, "total": tot})
		}

		if bgCtx.Err() != nil {
			return
		}
		c.syncMu.Lock()
		c.syncPhase = "rdeps:mod"
		c.syncMu.Unlock()

		topMods := c.DB.GetCatalogItemsByType("mod", 200)
		var keys []string
		for _, m := range topMods {
			if m.Platform == "modrinth" && m.ID != "" {
				keys = append(keys, "rdep:modrinth:"+m.ID)
			}
		}
		needed := map[string]bool{}
		for _, k := range c.DB.FilterUnindexedLookups(keys, 86400) {
			needed[k] = true
		}
		var targets []domain.RawProjectCard
		for _, m := range topMods {
			if m.Platform == "modrinth" && needed["rdep:modrinth:"+m.ID] {
				targets = append(targets, m)
				if len(targets) >= 50 {
					break
				}
			}
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 6)
		var edges []domain.RDepEdge
		var doneKeys []string
		for _, t := range targets {
			if bgCtx.Err() != nil {
				break
			}
			wg.Add(1)
			go func(it domain.RawProjectCard) {
				defer wg.Done()
				if !netx.AcquireSemCtx(bgCtx, sem) {
					return
				}
				defer func() { <-sem }()
				rm := catalog.InspectMRDepRels(hc, it.ID, it.ContentType, "", "")
				if bgCtx.Err() != nil {
					return
				}
				mu.Lock()
				for dpid, dtSet := range rm {
					for dt := range dtSet {
						edges = append(edges, domain.RDepEdge{DepKey: "modrinth:" + dpid, DependentKey: it.SourceKey, DepType: dt})
					}
				}
				doneKeys = append(doneKeys, "rdep:modrinth:"+it.ID)
				mu.Unlock()
			}(t)
		}
		wg.Wait()
		if bgCtx.Err() == nil {
			c.DB.UpsertReverseDeps(edges)
			c.DB.MarkLookups(doneKeys)
			c.indexVersion.Add(1)
			c.syncMu.Lock()
			c.syncProgress = c.syncTotal
			c.syncPhase = "idle"
			c.lastCompleted = float64(time.Now().Unix())
			c.syncMu.Unlock()
			c.Events.Publish(events.EventIndexSyncCompleted, map[string]any{"index_version": c.indexVersion.Load()})
		}
	}()
	return true
}
