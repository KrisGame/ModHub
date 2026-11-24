package launcher

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"modhub/internal/mcversion"
	"modhub/internal/storage"
)

const nativesManifestFile = ".natives-manifest.json"

type nativesMarker struct {
	VersionID string            `json:"version_id"`
	OS        string            `json:"os"`
	Arch      string            `json:"arch"`
	Sources   map[string]string `json:"sources"` // relPath -> sha1 or size:mtime
}

// PrepareNatives extracts platform-matching native libraries for versionID into an isolated
// per-version-and-arch directory, reusing already extracted files safely if source JAR signatures match.
// If the destination directory is locked by a running Minecraft process on Windows, it transparently
// verifies existing binaries or falls back to an isolated directory to avoid ERROR_ACCESS_DENIED.
// Returns (nativesDir, extractedCount, reused, error).
func PrepareNatives(
	sharedDir string,
	versionID string,
	libs []mcversion.ResolvedLibraryFile,
	plat mcversion.PlatformInfo,
) (string, int, bool, error) {
	if err := mcversion.ValidateVersionID(versionID); err != nil {
		return "", 0, false, err
	}
	nativesDir := filepath.Join(sharedDir, "versions", versionID, fmt.Sprintf("natives-%s-%s", plat.OS, plat.Arch))
	librariesDir := filepath.Join(sharedDir, "libraries")

	var nativeLibs []mcversion.ResolvedLibraryFile
	currentSources := map[string]string{}
	for _, lib := range libs {
		if !lib.IsNative {
			continue
		}
		jarPath := filepath.Join(librariesDir, filepath.FromSlash(lib.RelPath))
		fi, err := os.Stat(jarPath)
		if err != nil || fi.IsDir() || fi.Size() == 0 {
			return "", 0, false, fmt.Errorf("отсутствует нативная библиотека %s", lib.RelPath)
		}
		sig := strings.ToLower(strings.TrimSpace(lib.SHA1))
		if sig == "" {
			sig = fmt.Sprintf("%d:%d", fi.Size(), fi.ModTime().Unix())
		}
		currentSources[lib.RelPath] = sig
		nativeLibs = append(nativeLibs, lib)
	}

	if err := os.MkdirAll(nativesDir, 0o755); err != nil {
		return "", 0, false, fmt.Errorf("не удалось создать директорию natives: %w", err)
	}

	if len(nativeLibs) == 0 {
		// Modern LWJGL 3.3+ or versions without separate native classifier JARs still expect an existing natives dir
		return nativesDir, 0, true, nil
	}

	// Check if existing natives directory matches exact source signatures (safe reuse)
	if raw, err := os.ReadFile(filepath.Join(nativesDir, nativesManifestFile)); err == nil {
		var prev nativesMarker
		if json.Unmarshal(raw, &prev) == nil &&
			prev.VersionID == versionID &&
			prev.OS == plat.OS &&
			prev.Arch == plat.Arch &&
			mapsEqualString(prev.Sources, currentSources) {
			if entries, rerr := os.ReadDir(nativesDir); rerr == nil && len(entries) > 1 {
				return nativesDir, len(nativeLibs), true, nil
			}
		}
	}

	// Extract cleanly into a staging directory first so we never corrupt an existing working natives folder on Zip Slip or error
	stagingDir := fmt.Sprintf("%s.staging-%d", nativesDir, time.Now().UnixNano())
	_ = os.RemoveAll(stagingDir)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return "", 0, false, fmt.Errorf("не удалось создать временную папку natives: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	sort.Slice(nativeLibs, func(i, j int) bool { return nativeLibs[i].RelPath < nativeLibs[j].RelPath })
	for _, lib := range nativeLibs {
		jarPath := filepath.Join(librariesDir, filepath.FromSlash(lib.RelPath))
		if err := mcversion.ExtractNativeJAR(jarPath, stagingDir, lib.Extract); err != nil {
			return "", 0, false, fmt.Errorf("ошибка извлечения нативной библиотеки %s: %w", filepath.Base(lib.RelPath), err)
		}
	}

	marker := nativesMarker{
		VersionID: versionID,
		OS:        plat.OS,
		Arch:      plat.Arch,
		Sources:   currentSources,
	}
	if err := storage.WriteJSONFileAtomic(stagingDir, nativesManifestFile, marker, 0o644); err != nil {
		return "", 0, false, err
	}

	// Check if nativesDir is empty or new
	existingEntries, statErr := os.ReadDir(nativesDir)
	if statErr == nil && len(existingEntries) == 0 {
		// Target directory is empty, we can move or copy staging into it directly
		_ = os.Remove(nativesDir)
		if err := os.Rename(stagingDir, nativesDir); err == nil {
			success = true
			return nativesDir, len(nativeLibs), false, nil
		}
	}

	// Try atomic directory swap with backup
	backupDir := fmt.Sprintf("%s.old-%d", nativesDir, time.Now().UnixNano())
	hadPrev := false
	var swapErr error

	if _, err := os.Stat(nativesDir); err == nil {
		if err := os.Rename(nativesDir, backupDir); err == nil {
			hadPrev = true
			if err := os.Rename(stagingDir, nativesDir); err == nil {
				success = true
				_ = os.RemoveAll(backupDir)
				return nativesDir, len(nativeLibs), false, nil
			}
			// Rollback if staging move failed
			_ = os.Rename(backupDir, nativesDir)
		} else {
			swapErr = err
		}
	} else {
		if err := os.Rename(stagingDir, nativesDir); err == nil {
			success = true
			return nativesDir, len(nativeLibs), false, nil
		}
	}

	// If atomic directory swap failed (e.g. Windows ERROR_ACCESS_DENIED / locked DLL by running Minecraft),
	// attempt in-place file synchronization or isolate into an active instance directory
	if swapErr != nil || hadPrev {
		inPlaceOk := syncDirectoryInPlace(stagingDir, nativesDir)
		if inPlaceOk {
			_ = storage.WriteJSONFileAtomic(nativesDir, nativesManifestFile, marker, 0o644)
			success = true
			_ = os.RemoveAll(stagingDir)
			return nativesDir, len(nativeLibs), false, nil
		}

		// Fallback: If existing directory is hard-locked by another running instance, allocate an isolated fallback directory
		fallbackDir := fmt.Sprintf("%s-inst-%d", nativesDir, time.Now().UnixNano())
		if err := os.Rename(stagingDir, fallbackDir); err == nil {
			success = true
			return fallbackDir, len(nativeLibs), false, nil
		}
	}

	return "", 0, false, fmt.Errorf("не удалось обновить директорию natives: %w", swapErr)
}

// syncDirectoryInPlace copies non-conflicting files from src to dst.
// If a destination file is locked and cannot be rewritten, it verifies whether the existing file matches size.
func syncDirectoryInPlace(src, dst string) bool {
	entries, err := os.ReadDir(src)
	if err != nil {
		return false
	}
	_ = os.MkdirAll(dst, 0o755)

	allOk := true
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if !syncDirectoryInPlace(srcPath, dstPath) {
				allOk = false
			}
			continue
		}

		// Check if file already exists with matching size
		srcInfo, err := entry.Info()
		if err != nil {
			allOk = false
			continue
		}
		if dstInfo, err := os.Stat(dstPath); err == nil {
			if dstInfo.Size() == srcInfo.Size() {
				// File is identical in size, skip overwrite if locked
				continue
			}
		}

		if err := copyFileSafe(srcPath, dstPath); err != nil {
			// If file is locked, but already exists with non-zero size, don't abort Minecraft startup
			if dstFi, sErr := os.Stat(dstPath); sErr == nil && dstFi.Size() > 0 {
				continue
			}
			allOk = false
		}
	}
	return allOk
}

func copyFileSafe(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func mapsEqualString(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}
