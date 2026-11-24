package mcversion

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"modhub/internal/storage"
)

const (
	manifestCacheFile   = "version_manifest_v2.cache.json"
	manifestFreshWindow = 15 * time.Minute
)

var reSafeVersionID = regexp.MustCompile(`^[a-zA-Z0-9._+-]{1,96}$`)

type cachedManifestEnvelope struct {
	FetchedAt int64             `json:"fetched_at"`
	Manifest  VersionManifestV2 `json:"manifest"`
}

func ValidateVersionID(id string) error {
	s := strings.TrimSpace(id)
	if !reSafeVersionID.MatchString(s) || strings.Contains(s, "..") || strings.ContainsAny(s, `/\:`) {
		return fmt.Errorf("некорректный идентификатор версии Minecraft %q", id)
	}
	return nil
}

func validateManifestV2(m *VersionManifestV2) error {
	if m == nil || len(m.Versions) == 0 {
		return fmt.Errorf("официальный манифест версий Minecraft пуст или повреждён")
	}
	for _, v := range m.Versions {
		if strings.TrimSpace(v.ID) != "" && strings.TrimSpace(v.URL) != "" {
			return nil
		}
	}
	return fmt.Errorf("манифест версий Minecraft не содержит валидных записей")
}

func (m *Manager) loadDiskManifestCache() (*cachedManifestEnvelope, error) {
	raw, err := os.ReadFile(filepath.Join(m.versionsDir(), manifestCacheFile))
	if err != nil {
		return nil, err
	}
	var env cachedManifestEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if err := validateManifestV2(&env.Manifest); err != nil {
		return nil, err
	}
	return &env, nil
}

func (m *Manager) saveDiskManifestCache(manifest VersionManifestV2, fetchedAt int64) {
	_ = storage.WriteJSONFileAtomic(m.versionsDir(), manifestCacheFile, cachedManifestEnvelope{FetchedAt: fetchedAt, Manifest: manifest}, 0o644)
}

func (m *Manager) buildManifestResult(manifest VersionManifestV2, fromCache, stale bool, fetchedAt int64, warning string) VersionManifestResult {
	vers := make([]VersionSummary, 0, len(manifest.Versions))
	for _, v := range manifest.Versions {
		if strings.TrimSpace(v.ID) == "" {
			continue
		}
		v.Installable = strings.TrimSpace(v.URL) != ""
		v.Installed = fileExistsNonEmpty(filepath.Join(m.versionsDir(), v.ID, v.ID+".jar")) && fileExistsNonEmpty(filepath.Join(m.versionsDir(), v.ID, v.ID+".json"))
		vers = append(vers, v)
	}
	return VersionManifestResult{
		LatestRelease: manifest.Latest.Release, LatestSnapshot: manifest.Latest.Snapshot,
		Versions: vers, FromCache: fromCache, Stale: stale, FetchedAt: fetchedAt, Warning: warning,
	}
}

func (m *Manager) FetchManifest(ctx context.Context, forceRefresh bool) (VersionManifestResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().Unix()
	cachedEnv, cacheErr := m.loadDiskManifestCache()
	if !forceRefresh && cacheErr == nil && cachedEnv != nil {
		if age := time.Duration(now-cachedEnv.FetchedAt) * time.Second; age >= 0 && age < manifestFreshWindow {
			return m.buildManifestResult(cachedEnv.Manifest, true, false, cachedEnv.FetchedAt, ""), nil
		}
	}
	m.mu.Lock()
	manifestURL := m.manifestURL
	m.mu.Unlock()
	if _, err := m.httpClient.ValidateLauncherURL(manifestURL); err != nil {
		if cachedEnv != nil {
			return m.buildManifestResult(cachedEnv.Manifest, true, true, cachedEnv.FetchedAt, "Показан устаревший кэш: "+err.Error()), nil
		}
		return VersionManifestResult{}, fmt.Errorf("недопустимый URL манифеста Minecraft: %w", err)
	}
	var live VersionManifestV2
	err := m.httpClient.WithContext(ctx).GetJSON(manifestURL, nil, 0, &live)
	if err == nil {
		err = validateManifestV2(&live)
	}
	if err != nil {
		if cachedEnv != nil {
			warn := fmt.Sprintf("Нет связи с Mojang (%v). Показана сохранённая копия списка версий (может быть устаревшей).", err)
			return m.buildManifestResult(cachedEnv.Manifest, true, true, cachedEnv.FetchedAt, warn), nil
		}
		return VersionManifestResult{}, fmt.Errorf("не удалось загрузить официальный манифест Minecraft: %w", err)
	}
	m.saveDiskManifestCache(live, now)
	return m.buildManifestResult(live, false, false, now, ""), nil
}

func (m *Manager) FetchVersionMetadata(ctx context.Context, versionID string, forceRefresh bool) (*VersionMetadata, error) {
	if err := ValidateVersionID(versionID); err != nil {
		return nil, err
	}
	verDir := filepath.Join(m.versionsDir(), versionID)
	localJSONPath := filepath.Join(verDir, versionID+".json")
	if !forceRefresh {
		if meta, err := ReadVersionMetadataFile(localJSONPath); err == nil && meta.ID != "" {
			return meta, nil
		}
	}
	manifest, err := m.FetchManifest(ctx, false)
	if err != nil {
		if meta, rerr := ReadVersionMetadataFile(localJSONPath); rerr == nil && meta.ID != "" {
			return meta, nil
		}
		return nil, err
	}
	var summary *VersionSummary
	for i := range manifest.Versions {
		if manifest.Versions[i].ID == versionID {
			cp := manifest.Versions[i]
			summary = &cp
			break
		}
	}
	if summary == nil || strings.TrimSpace(summary.URL) == "" {
		if meta, rerr := ReadVersionMetadataFile(localJSONPath); rerr == nil && meta.ID != "" {
			return meta, nil
		}
		return nil, fmt.Errorf("версия Minecraft %q не найдена в официальном манифесте", versionID)
	}
	if _, err := m.httpClient.ValidateLauncherURL(summary.URL); err != nil {
		return nil, fmt.Errorf("недопустимый URL метаданных версии %s: %w", versionID, err)
	}
	var meta VersionMetadata
	if err := m.httpClient.WithContext(ctx).GetJSON(summary.URL, nil, 0, &meta); err != nil {
		if localMeta, rerr := ReadVersionMetadataFile(localJSONPath); rerr == nil && localMeta.ID != "" {
			return localMeta, nil
		}
		return nil, fmt.Errorf("не удалось загрузить метаданные версии %s: %w", versionID, err)
	}
	if strings.TrimSpace(meta.ID) == "" || strings.TrimSpace(meta.MainClass) == "" {
		return nil, fmt.Errorf("метаданные версии %s некорректны (отсутствует id или mainClass)", versionID)
	}
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		return nil, err
	}
	if err := storage.WriteJSONFileAtomic(verDir, versionID+".json", meta, 0o644); err != nil {
		return nil, fmt.Errorf("не удалось сохранить %s.json: %w", versionID, err)
	}
	return &meta, nil
}

func ReadVersionMetadataFile(path string) (*VersionMetadata, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta VersionMetadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, err
	}
	if strings.TrimSpace(meta.ID) == "" {
		return nil, fmt.Errorf("пустой id в %s", path)
	}
	return &meta, nil
}

func FileMatchesSHA1(path, expectedSHA1 string, expectedSize int64) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if expectedSize > 0 && info.Size() != expectedSize {
		return false
	}
	want := strings.ToLower(strings.TrimSpace(expectedSHA1))
	if want == "" {
		return info.Size() > 0
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want)
}

func fileExistsNonEmpty(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Size() > 0
}
