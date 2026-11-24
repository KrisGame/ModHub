package mcversion

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"modhub/internal/downloads"
	"modhub/internal/events"
	"modhub/internal/netx"
	javart "modhub/internal/runtime"
)

const OfficialAssetsBaseURL = "https://resources.download.minecraft.net"

type Manager struct {
	sharedDir  string
	httpClient *netx.HTTPClient
	dlManager  *downloads.Manager
	javaMgr    *javart.Manager
	bus        *events.Bus

	mu            sync.Mutex
	manifestURL   string
	assetsBaseURL string
	platform      PlatformInfo
	versionLocks  map[string]*sync.Mutex
}

func NewManager(sharedDir string, httpClient *netx.HTTPClient, dlManager *downloads.Manager, javaMgr *javart.Manager, bus *events.Bus) *Manager {
	m := &Manager{
		sharedDir:     filepath.Clean(sharedDir),
		httpClient:    httpClient,
		dlManager:     dlManager,
		javaMgr:       javaMgr,
		bus:           bus,
		manifestURL:   OfficialVersionManifestURL,
		assetsBaseURL: OfficialAssetsBaseURL,
		platform:      CurrentPlatform(),
		versionLocks:  make(map[string]*sync.Mutex),
	}
	_ = os.MkdirAll(m.versionsDir(), 0o755)
	_ = os.MkdirAll(m.librariesDir(), 0o755)
	_ = os.MkdirAll(m.assetsIndexesDir(), 0o755)
	_ = os.MkdirAll(m.assetsObjectsDir(), 0o755)
	_ = os.MkdirAll(m.logConfigsDir(), 0o755)
	return m
}

func (m *Manager) SetManifestURL(u string) { m.mu.Lock(); m.manifestURL = strings.TrimSpace(u); m.mu.Unlock() }
func (m *Manager) SetAssetsBaseURL(u string) {
	m.mu.Lock()
	m.assetsBaseURL = strings.TrimRight(strings.TrimSpace(u), "/")
	m.mu.Unlock()
}
func (m *Manager) SetPlatform(p PlatformInfo) { m.mu.Lock(); m.platform = p; m.mu.Unlock() }
func (m *Manager) Platform() PlatformInfo     { m.mu.Lock(); defer m.mu.Unlock(); return m.platform }

func (m *Manager) SharedDir() string          { return m.sharedDir }
func (m *Manager) versionsDir() string        { return filepath.Join(m.sharedDir, "versions") }
func (m *Manager) librariesDir() string       { return filepath.Join(m.sharedDir, "libraries") }
func (m *Manager) assetsIndexesDir() string   { return filepath.Join(m.sharedDir, "assets", "indexes") }
func (m *Manager) assetsObjectsDir() string   { return filepath.Join(m.sharedDir, "assets", "objects") }
func (m *Manager) logConfigsDir() string      { return filepath.Join(m.sharedDir, "assets", "log_configs") }
func (m *Manager) VersionDir(id string) string { return filepath.Join(m.versionsDir(), id) }
func (m *Manager) VersionJSONPath(id string) string { return filepath.Join(m.versionsDir(), id, id+".json") }
func (m *Manager) ClientJARPath(id string) string   { return filepath.Join(m.versionsDir(), id, id+".jar") }
func (m *Manager) LibrariesDir() string       { return m.librariesDir() }

func (m *Manager) lockVersion(versionID string) func() {
	m.mu.Lock()
	lk, ok := m.versionLocks[versionID]
	if !ok {
		lk = &sync.Mutex{}
		m.versionLocks[versionID] = lk
	}
	m.mu.Unlock()
	lk.Lock()
	return lk.Unlock
}

func (m *Manager) RequiredJavaForMetadata(meta *VersionMetadata) javart.RequirementResult {
	in := javart.RequirementInput{}
	if meta != nil {
		in.MCVersion = meta.ID
		if meta.JavaVersion != nil && meta.JavaVersion.MajorVersion > 0 {
			in.ManifestJavaMajor = meta.JavaVersion.MajorVersion
		}
	}
	return javart.ResolveRequirement(in)
}

type queuedDownload struct {
	name   string
	taskID string
}

func (m *Manager) InstallVanilla(
	ctx context.Context,
	instanceID string,
	versionID string,
	groupID string,
	onProgress ProgressCallback,
) (*VersionMetadata, VerificationReport, error) {
	if err := ValidateVersionID(versionID); err != nil {
		return nil, VerificationReport{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m.dlManager == nil {
		return nil, VerificationReport{}, fmt.Errorf("менеджер загрузок не инициализирован")
	}
	if strings.TrimSpace(groupID) == "" {
		groupID = "mc:" + versionID
	}
	unlock := m.lockVersion(versionID)
	defer unlock()

	emit := func(stage, msg, comp string, done, total int, pct float64) {
		if onProgress == nil {
			return
		}
		onProgress(InstallProgress{
			InstanceID: instanceID, MCVersion: versionID, Loader: "vanilla",
			Stage: stage, Message: msg, Component: comp,
			CompletedFiles: done, TotalFiles: total, Progress: pct, UpdatedAt: time.Now().Unix(),
		})
	}

	emit("metadata", fmt.Sprintf("Получение метаданных Minecraft %s...", versionID), versionID+".json", 0, 0, -1)
	meta, err := m.FetchVersionMetadata(ctx, versionID, false)
	if err != nil {
		return nil, VerificationReport{}, err
	}
	if ctx.Err() != nil {
		return nil, VerificationReport{}, ctx.Err()
	}

	reqJava := m.RequiredJavaForMetadata(meta)
	emit("java", fmt.Sprintf("Проверка совместимой Java %d...", reqJava.RecommendedMajor), "", 0, 0, -1)
	if m.javaMgr != nil {
		_, rec := m.javaMgr.RecommendForMC(javart.RequirementInput{MCVersion: meta.ID, ManifestJavaMajor: reqJava.RecommendedMajor})
		if rec == nil {
			emit("java", fmt.Sprintf("Требуется Java %d (установите в Настройках → Java Runtime; файлы Minecraft будут подготовлены)", reqJava.RecommendedMajor), "", 0, 0, -1)
		}
	}

	plat := m.Platform()
	libs := FilterLibrariesForPlatform(meta, plat)
	clientArt, hasClient := meta.Downloads["client"]
	if !hasClient || strings.TrimSpace(clientArt.URL) == "" {
		return nil, VerificationReport{}, fmt.Errorf("метаданные Minecraft %s не содержат ссылки на client.jar", versionID)
	}

	var assetIndex *AssetIndex
	if meta.AssetIndex != nil && strings.TrimSpace(meta.AssetIndex.URL) != "" {
		idxID := meta.AssetIndex.ID
		if idxID == "" {
			idxID = meta.Assets
		}
		assetIndexPath := filepath.Join(m.assetsIndexesDir(), idxID+".json")
		if !FileMatchesSHA1(assetIndexPath, meta.AssetIndex.SHA1, meta.AssetIndex.Size) {
			emit("assets", fmt.Sprintf("Загрузка индекса ресурсов %s...", idxID), idxID+".json", 0, 0, -1)
			tv, err := m.dlManager.Enqueue(downloads.DownloadRequest{
				Name: fmt.Sprintf("Asset Index %s", idxID), Category: "minecraft",
				URL: meta.AssetIndex.URL, DestPath: assetIndexPath, ExpectedSize: meta.AssetIndex.Size,
				ChecksumAlgo: checksumAlgoFor(meta.AssetIndex.SHA1), ChecksumValue: meta.AssetIndex.SHA1, GroupID: groupID,
			})
			if err != nil {
				return nil, VerificationReport{}, fmt.Errorf("ошибка загрузки asset index: %w", err)
			}
			fin, err := m.dlManager.Wait(ctx, tv.ID)
			if err != nil {
				m.dlManager.CancelGroup(groupID)
				return nil, VerificationReport{}, err
			}
			if fin.Status != downloads.StatusCompleted {
				return nil, VerificationReport{}, fmt.Errorf("не удалось скачать asset index %s: %s", idxID, fin.Error)
			}
		}
		rawIdx, err := os.ReadFile(assetIndexPath)
		if err != nil {
			return nil, VerificationReport{}, fmt.Errorf("не удалось прочитать asset index: %w", err)
		}
		var parsedIdx AssetIndex
		if err := json.Unmarshal(rawIdx, &parsedIdx); err != nil {
			return nil, VerificationReport{}, fmt.Errorf("некорректный asset index JSON: %w", err)
		}
		assetIndex = &parsedIdx
	}

	type dlSpec struct {
		stage string
		req   downloads.DownloadRequest
	}
	var plan []dlSpec
	seenDest := map[string]struct{}{}
	addIfMissing := func(stage string, req downloads.DownloadRequest) {
		destKey := strings.ToLower(filepath.Clean(req.DestPath))
		if _, dup := seenDest[destKey]; dup {
			return
		}
		seenDest[destKey] = struct{}{}
		if FileMatchesSHA1(req.DestPath, req.ChecksumValue, req.ExpectedSize) {
			return
		}
		plan = append(plan, dlSpec{stage: stage, req: req})
	}

	addIfMissing("client", downloads.DownloadRequest{
		Name: fmt.Sprintf("Minecraft %s (client.jar)", versionID), Category: "minecraft",
		URL: clientArt.URL, DestPath: m.ClientJARPath(versionID), ExpectedSize: clientArt.Size,
		ChecksumAlgo: checksumAlgoFor(clientArt.SHA1), ChecksumValue: clientArt.SHA1, GroupID: groupID,
	})
	for _, lib := range libs {
		addIfMissing("libraries", downloads.DownloadRequest{
			Name: filepath.Base(lib.RelPath), Category: "minecraft",
			URL: lib.URL, DestPath: filepath.Join(m.librariesDir(), filepath.FromSlash(lib.RelPath)),
			ExpectedSize: lib.Size, ChecksumAlgo: checksumAlgoFor(lib.SHA1), ChecksumValue: lib.SHA1, GroupID: groupID,
		})
	}
	if meta.Logging != nil && meta.Logging.Client != nil && meta.Logging.Client.File.URL != "" {
		lf := meta.Logging.Client.File
		lfName := lf.ID
		if lfName == "" {
			lfName = filepath.Base(lf.URL)
		}
		addIfMissing("libraries", downloads.DownloadRequest{
			Name: "Logging Config: " + filepath.Base(lfName), Category: "minecraft",
			URL: lf.URL, DestPath: filepath.Join(m.logConfigsDir(), filepath.Base(lfName)),
			ExpectedSize: lf.Size, ChecksumAlgo: checksumAlgoFor(lf.SHA1), ChecksumValue: lf.SHA1, GroupID: groupID,
		})
	}

	m.mu.Lock()
	assetsBase := m.assetsBaseURL
	m.mu.Unlock()
	if assetIndex != nil && len(assetIndex.Objects) > 0 {
		seenHash := map[string]struct{}{}
		names := make([]string, 0, len(assetIndex.Objects))
		for k := range assetIndex.Objects {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, objName := range names {
			obj := assetIndex.Objects[objName]
			h := strings.ToLower(strings.TrimSpace(obj.Hash))
			if len(h) < 4 {
				continue
			}
			if _, dup := seenHash[h]; dup {
				continue
			}
			seenHash[h] = struct{}{}
			prefix := h[:2]
			addIfMissing("assets", downloads.DownloadRequest{
				Name: "Asset " + filepath.Base(objName), Category: "minecraft",
				URL: fmt.Sprintf("%s/%s/%s", assetsBase, prefix, h),
				DestPath: filepath.Join(m.assetsObjectsDir(), prefix, h),
				ExpectedSize: obj.Size, ChecksumAlgo: downloads.ChecksumSHA1, ChecksumValue: h, GroupID: groupID,
			})
		}
	}

	totalToDL := len(plan)
	doneCount := 0
	const batchSize = 32
	for start := 0; start < totalToDL; start += batchSize {
		if ctx.Err() != nil {
			m.dlManager.CancelGroup(groupID)
			return nil, VerificationReport{}, ctx.Err()
		}
		end := start + batchSize
		if end > totalToDL {
			end = totalToDL
		}
		batch := plan[start:end]
		queued := make([]queuedDownload, 0, len(batch))
		for _, item := range batch {
			tv, err := m.dlManager.Enqueue(item.req)
			if err != nil {
				m.dlManager.CancelGroup(groupID)
				return nil, VerificationReport{}, fmt.Errorf("ошибка постановки %s в очередь загрузок: %w", item.req.Name, err)
			}
			queued = append(queued, queuedDownload{name: item.req.Name, taskID: tv.ID})
		}
		for idx, q := range queued {
			fin, err := m.dlManager.Wait(ctx, q.taskID)
			if err != nil {
				m.dlManager.CancelGroup(groupID)
				return nil, VerificationReport{}, err
			}
			if fin.Status != downloads.StatusCompleted {
				m.dlManager.CancelGroup(groupID)
				return nil, VerificationReport{}, fmt.Errorf("ошибка загрузки %s (%s): %s", q.name, fin.Status, fin.Error)
			}
			doneCount++
			emit(batch[idx].stage, fmt.Sprintf("Загружено файлов: %d из %d", doneCount, totalToDL), q.name, doneCount, totalToDL, float64(doneCount)*100/float64(totalToDL))
		}
	}

	nativesDir := filepath.Join(m.VersionDir(versionID), "natives")
	for _, lib := range libs {
		if !lib.IsNative {
			continue
		}
		jarPath := filepath.Join(m.librariesDir(), filepath.FromSlash(lib.RelPath))
		if !fileExistsNonEmpty(jarPath) {
			continue
		}
		emit("natives", "Распаковка нативных библиотек...", filepath.Base(lib.RelPath), doneCount, totalToDL, 96)
		if err := ExtractNativeJAR(jarPath, nativesDir, lib.Extract); err != nil {
			return nil, VerificationReport{}, fmt.Errorf("ошибка распаковки нативной библиотеки %s: %w", filepath.Base(lib.RelPath), err)
		}
	}

	emit("verifying", fmt.Sprintf("Проверка целостности Minecraft %s...", versionID), "", doneCount, totalToDL, 99)
	rep := m.VerifyVanilla(versionID)
	if !rep.Valid {
		return meta, rep, fmt.Errorf("проверка установленной версии Minecraft %s не пройдена: %s", versionID, strings.Join(rep.Errors, "; "))
	}
	return meta, rep, nil
}

func (m *Manager) VerifyVanilla(versionID string) VerificationReport {
	rep := VerificationReport{VersionID: versionID}
	if err := ValidateVersionID(versionID); err != nil {
		rep.Errors = append(rep.Errors, err.Error())
		return rep
	}
	meta, err := ReadVersionMetadataFile(m.VersionJSONPath(versionID))
	if err != nil {
		rep.Errors = append(rep.Errors, "отсутствуют или повреждены метаданные "+versionID+".json")
		return rep
	}
	rep.MetadataOK = true

	clientArt := meta.Downloads["client"]
	if FileMatchesSHA1(m.ClientJARPath(versionID), clientArt.SHA1, clientArt.Size) {
		rep.ClientJarOK = true
	} else {
		rep.MissingFiles = append(rep.MissingFiles, versionID+".jar")
		rep.Errors = append(rep.Errors, "client.jar отсутствует или не совпадает по контрольной сумме")
	}

	libs := FilterLibrariesForPlatform(meta, m.Platform())
	rep.LibrariesTotal = len(libs)
	for _, lib := range libs {
		p := filepath.Join(m.librariesDir(), filepath.FromSlash(lib.RelPath))
		if FileMatchesSHA1(p, lib.SHA1, lib.Size) {
			rep.LibrariesOK++
		} else {
			rep.MissingFiles = append(rep.MissingFiles, lib.RelPath)
		}
	}
	if rep.LibrariesOK < rep.LibrariesTotal {
		rep.Errors = append(rep.Errors, fmt.Sprintf("отсутствуют библиотеки: %d из %d", rep.LibrariesTotal-rep.LibrariesOK, rep.LibrariesTotal))
	}

	if meta.AssetIndex != nil && meta.AssetIndex.ID != "" {
		idxPath := filepath.Join(m.assetsIndexesDir(), meta.AssetIndex.ID+".json")
		if FileMatchesSHA1(idxPath, meta.AssetIndex.SHA1, meta.AssetIndex.Size) {
			rep.AssetIndexOK = true
			if raw, rerr := os.ReadFile(idxPath); rerr == nil {
				var idx AssetIndex
				if json.Unmarshal(raw, &idx) == nil {
					seenHash := map[string]struct{}{}
					for _, obj := range idx.Objects {
						h := strings.ToLower(strings.TrimSpace(obj.Hash))
						if len(h) < 4 {
							continue
						}
						if _, dup := seenHash[h]; dup {
							continue
						}
						seenHash[h] = struct{}{}
						rep.AssetsTotal++
						objPath := filepath.Join(m.assetsObjectsDir(), h[:2], h)
						if fileExistsNonEmpty(objPath) && (obj.Size <= 0 || fileSizeMatches(objPath, obj.Size)) {
							rep.AssetsOK++
						} else if len(rep.MissingFiles) < 20 {
							rep.MissingFiles = append(rep.MissingFiles, "assets/objects/"+h[:2]+"/"+h)
						}
					}
				}
			}
		} else {
			rep.Errors = append(rep.Errors, "отсутствует или повреждён asset index "+meta.AssetIndex.ID+".json")
		}
	} else {
		rep.AssetIndexOK = true
	}
	if rep.AssetsOK < rep.AssetsTotal {
		rep.Errors = append(rep.Errors, fmt.Sprintf("отсутствуют ресурсы (assets): %d из %d", rep.AssetsTotal-rep.AssetsOK, rep.AssetsTotal))
	}

	if meta.Logging != nil && meta.Logging.Client != nil && meta.Logging.Client.File.URL != "" {
		lf := meta.Logging.Client.File
		lfName := lf.ID
		if lfName == "" {
			lfName = filepath.Base(lf.URL)
		}
		rep.LoggingConfigOK = FileMatchesSHA1(filepath.Join(m.logConfigsDir(), filepath.Base(lfName)), lf.SHA1, lf.Size)
		if !rep.LoggingConfigOK {
			rep.Errors = append(rep.Errors, "отсутствует конфигурация логирования "+lfName)
		}
	} else {
		rep.LoggingConfigOK = true
	}

	reqJava := m.RequiredJavaForMetadata(meta)
	rep.JavaRequiredMajor = reqJava.RecommendedMajor
	if m.javaMgr != nil {
		_, rec := m.javaMgr.RecommendForMC(javart.RequirementInput{MCVersion: meta.ID, ManifestJavaMajor: reqJava.RecommendedMajor})
		if rec != nil {
			rep.JavaAvailable = true
			rep.JavaSummary = fmt.Sprintf("%s (%s)", rec.Name, rec.Version)
		} else {
			rep.JavaSummary = fmt.Sprintf("Требуется Java %d (не установлена)", reqJava.RecommendedMajor)
		}
	} else {
		rep.JavaSummary = fmt.Sprintf("Требуется Java %d", reqJava.RecommendedMajor)
	}
	rep.Valid = rep.MetadataOK && rep.ClientJarOK && rep.LibrariesOK == rep.LibrariesTotal && rep.AssetIndexOK && rep.AssetsOK == rep.AssetsTotal && rep.LoggingConfigOK
	return rep
}

func checksumAlgoFor(sha1Val string) downloads.ChecksumAlgo {
	if strings.TrimSpace(sha1Val) != "" {
		return downloads.ChecksumSHA1
	}
	return downloads.ChecksumNone
}

func fileSizeMatches(path string, expected int64) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Size() == expected
}
