package desktop

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DataDirInfo describes the resolved persistent storage location and any performed migration.
type DataDirInfo struct {
	Dir          string `json:"dir"`
	Mode         string `json:"mode"` // standard | portable | env
	MigratedFrom string `json:"migrated_from,omitempty"`
	BackupPath   string `json:"backup_path,omitempty"`
}

var trackedDataFiles = []string{
	"config.json",
	"overrides.json",
	"packs.json",
	"modhub.db",
	"modhub.db-wal",
	"modhub.db-shm",
}

func defaultStableDataDir() (string, error) {
	if cfgRoot, err := os.UserConfigDir(); err == nil && strings.TrimSpace(cfgRoot) != "" {
		return filepath.Join(cfgRoot, "ModHub"), nil
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".modhub"), nil
	}
	return "", fmt.Errorf("не удалось определить системную директорию данных пользователя")
}

func fileExistsNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// dirHasExistingUserData reports whether dir already holds real user data (SQLite DB or settings files).
func dirHasExistingUserData(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	for _, fn := range []string{"modhub.db", "config.json", "packs.json", "overrides.json"} {
		if fileExistsNonEmpty(filepath.Join(dir, fn)) {
			return true
		}
	}
	return false
}

// dirHasMeaningfulLegacyData distinguishes populated legacy user data from an untouched empty repo template.
func dirHasMeaningfulLegacyData(dir string) bool {
	if fileExistsNonEmpty(filepath.Join(dir, "modhub.db")) {
		return true
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil && len(raw) > 0 {
		var cfg struct {
			Port             int    `json:"port"`
			CurseForgeAPIKey string `json:"curseforge_api_key"`
			MergeThreshold   int    `json:"merge_threshold"`
			ProxyURL         string `json:"proxy_url"`
			AutoMed          bool   `json:"auto_install_medium_deps"`
		}
		if json.Unmarshal(raw, &cfg) == nil {
			if strings.TrimSpace(cfg.CurseForgeAPIKey) != "" ||
				strings.TrimSpace(cfg.ProxyURL) != "" ||
				cfg.AutoMed ||
				(cfg.Port != 0 && cfg.Port != 8080) ||
				(cfg.MergeThreshold != 0 && cfg.MergeThreshold != 70) {
				return true
			}
		}
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "packs.json")); err == nil && len(raw) > 0 {
		var wrap struct {
			Packs []any `json:"packs"`
		}
		if json.Unmarshal(raw, &wrap) == nil && len(wrap.Packs) > 0 {
			return true
		}
		var direct []any
		if json.Unmarshal(raw, &direct) == nil && len(direct) > 0 {
			return true
		}
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "overrides.json")); err == nil && len(raw) > 0 {
		var ov struct {
			Merges [][]string `json:"merges"`
			Splits [][]string `json:"splits"`
		}
		if json.Unmarshal(raw, &ov) == nil && (len(ov.Merges) > 0 || len(ov.Splits) > 0) {
			return true
		}
	}
	return false
}

func sameCleanPath(a, b string) bool {
	ca, errA := filepath.Abs(a)
	cb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return strings.EqualFold(filepath.Clean(ca), filepath.Clean(cb))
}

func copyFileSafe(src, dst string, perm os.FileMode) error {
	if _, err := os.Stat(dst); err == nil {
		// Never overwrite an existing user file in destination
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	_ = os.Chmod(tmpPath, perm)
	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func copyDirFilesSafe(srcDir, dstDir string) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return
	}
	_ = os.MkdirAll(dstDir, 0o700)
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		_ = copyFileSafe(filepath.Join(srcDir, ent.Name()), filepath.Join(dstDir, ent.Name()), 0o600)
	}
}

func migrateLegacyDataDir(srcDir, dstDir string) (string, error) {
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		return "", err
	}
	backupDir := filepath.Join(dstDir, "migration_backups", "legacy_"+time.Now().UTC().Format("20060102_150405"))
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", err
	}

	for _, fn := range trackedDataFiles {
		src := filepath.Join(srcDir, fn)
		if !fileExistsNonEmpty(src) {
			continue
		}
		perm := os.FileMode(0o600)
		if err := copyFileSafe(src, filepath.Join(backupDir, fn), perm); err != nil {
			return "", fmt.Errorf("резервное копирование %s: %w", fn, err)
		}
		if err := copyFileSafe(src, filepath.Join(dstDir, fn), perm); err != nil {
			return "", fmt.Errorf("перенос %s: %w", fn, err)
		}
	}

	mediaSrc := filepath.Join(srcDir, "media_cache")
	if info, err := os.Stat(mediaSrc); err == nil && info.IsDir() {
		copyDirFilesSafe(mediaSrc, filepath.Join(dstDir, "media_cache"))
	}
	return backupDir, nil
}

// ResolveDataDir resolves a stable persistent data directory for ModHub Desktop
// without tying user data to temporary build directories or source folders.
//
// Resolution priority:
//  1. MODHUB_DATA_DIR environment variable (explicit override)
//  2. Portable mode: if ".portable" marker exists next to the executable -> <exeDir>/data
//  3. Stable OS config directory (%APPDATA%\ModHub on Windows, ~/.config/ModHub on Linux, ~/Library/Application Support/ModHub on macOS)
//     - If stable dir already contains user data, use it directly without overwriting anything.
//     - Otherwise, if legacy data exists in <exeDir>/data or <wd>/data, safely migrate with a backup copy.
//     - Otherwise, create the stable dir cleanly.
func ResolveDataDir(exeDir, wd string) (DataDirInfo, error) {
	if envDir := strings.TrimSpace(os.Getenv("MODHUB_DATA_DIR")); envDir != "" {
		if err := os.MkdirAll(envDir, 0o700); err != nil {
			return DataDirInfo{}, err
		}
		return DataDirInfo{Dir: envDir, Mode: "env"}, nil
	}
	stableDir, err := defaultStableDataDir()
	if err != nil {
		fallback := filepath.Join(exeDir, "data")
		_ = os.MkdirAll(fallback, 0o700)
		return DataDirInfo{Dir: fallback, Mode: "portable"}, nil
	}
	return ResolveDataDirWithTarget(stableDir, exeDir, wd)
}

// ResolveDataDirWithTarget performs deterministic data directory resolution and migration against an explicit stable target (also used in tests).
func ResolveDataDirWithTarget(stableDir, exeDir, wd string) (DataDirInfo, error) {
	if exeDir != "" {
		if _, err := os.Stat(filepath.Join(exeDir, ".portable")); err == nil {
			pDir := filepath.Join(exeDir, "data")
			if err := os.MkdirAll(pDir, 0o700); err != nil {
				return DataDirInfo{}, err
			}
			return DataDirInfo{Dir: pDir, Mode: "portable"}, nil
		}
	}

	if dirHasExistingUserData(stableDir) {
		return DataDirInfo{Dir: stableDir, Mode: "standard"}, nil
	}

	// Check candidate legacy locations (old portable exeDir/data or repo data/)
	legacyCandidates := []string{}
	addCand := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || sameCleanPath(p, stableDir) {
			return
		}
		for _, ex := range legacyCandidates {
			if sameCleanPath(ex, p) {
				return
			}
		}
		legacyCandidates = append(legacyCandidates, p)
	}
	if exeDir != "" {
		addCand(filepath.Join(exeDir, "data"))
	}
	if wd != "" {
		addCand(filepath.Join(wd, "data"))
		addCand(filepath.Join(filepath.Dir(wd), "data"))
	}

	for _, cand := range legacyCandidates {
		if dirHasMeaningfulLegacyData(cand) {
			backupDir, err := migrateLegacyDataDir(cand, stableDir)
			if err != nil {
				return DataDirInfo{}, err
			}
			return DataDirInfo{
				Dir:          stableDir,
				Mode:         "standard",
				MigratedFrom: cand,
				BackupPath:   backupDir,
			}, nil
		}
	}

	if err := os.MkdirAll(stableDir, 0o700); err != nil {
		return DataDirInfo{}, err
	}
	return DataDirInfo{Dir: stableDir, Mode: "standard"}, nil
}
