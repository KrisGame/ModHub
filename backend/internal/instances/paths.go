package instances

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reSafeInstanceID = regexp.MustCompile(`^[a-zA-Z0-9_-]{4,64}$`)
	reSafeSubdir     = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,48}$`)
)

// ExpectedGameSubdirs lists the standard isolated subdirectories inside <instance>/minecraft/.
var ExpectedGameSubdirs = []string{
	"mods",
	"config",
	"resourcepacks",
	"shaderpacks",
	"datapacks",
	"saves",
	"screenshots",
	"logs",
	"crash-reports",
}

func ValidateInstanceID(id string) error {
	id = strings.TrimSpace(id)
	if !reSafeInstanceID.MatchString(id) {
		return fmt.Errorf("недопустимый идентификатор профиля %q", id)
	}
	if strings.Contains(id, "..") || strings.ContainsAny(id, `/\:`) {
		return fmt.Errorf("небезопасный идентификатор профиля %q", id)
	}
	return nil
}

func cleanAbsPath(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("пустой путь")
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("управляющие символы в пути запрещены")
		}
	}
	abs, err := filepath.Abs(s)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func samePathCI(a, b string) bool {
	ca, errA := filepath.Abs(a)
	cb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return strings.EqualFold(filepath.Clean(ca), filepath.Clean(cb))
}

func isStrictSubPath(parent, child string) bool {
	pAbs, errP := filepath.Abs(parent)
	cAbs, errC := filepath.Abs(child)
	if errP != nil || errC != nil {
		return false
	}
	rel, err := filepath.Rel(pAbs, cAbs)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func isSameOrParentPath(candidate, protected string) bool {
	if strings.TrimSpace(candidate) == "" || strings.TrimSpace(protected) == "" {
		return false
	}
	return samePathCI(candidate, protected) || isStrictSubPath(candidate, protected)
}

func isFilesystemRoot(absPath string) bool {
	cleaned := filepath.Clean(absPath)
	parent := filepath.Dir(cleaned)
	return samePathCI(cleaned, parent)
}

func isDangerousSystemPath(absPath string) bool {
	if isFilesystemRoot(absPath) {
		return true
	}
	low := strings.ToLower(filepath.Clean(absPath))
	// Reject top-level OS directories on Windows & Unix
	for _, bad := range []string{
		`c:\windows`, `c:\program files`, `c:\program files (x86)`, `c:\users`,
		"/etc", "/usr", "/bin", "/sbin", "/var", "/sys", "/proc", "/dev", "/home", "/root",
	} {
		if low == bad {
			return true
		}
	}
	return false
}

// ValidateSafeInstanceRoot ensures a custom or default instance directory does not traverse,
// target filesystem roots/system dirs, or overlap with shared ModHub infrastructure or another instance.
func ValidateSafeInstanceRoot(targetDir string, protectedDirs []string, otherInstanceDirs []string) (string, error) {
	abs, err := cleanAbsPath(targetDir)
	if err != nil {
		return "", fmt.Errorf("некорректный путь профиля: %w", err)
	}
	if isDangerousSystemPath(abs) {
		return "", fmt.Errorf("нельзя использовать системную или корневую директорию %q для профиля", abs)
	}
	for _, prot := range protectedDirs {
		if strings.TrimSpace(prot) == "" {
			continue
		}
		if isSameOrParentPath(abs, prot) || isStrictSubPath(prot, abs) {
			return "", fmt.Errorf("директория профиля %q конфликтует с системной папкой ModHub %q", abs, prot)
		}
	}
	for _, other := range otherInstanceDirs {
		if strings.TrimSpace(other) == "" {
			continue
		}
		if isSameOrParentPath(abs, other) || isStrictSubPath(other, abs) {
			return "", fmt.Errorf("директория профиля %q пересекается с другим профилем", abs)
		}
	}
	return abs, nil
}

// ValidateSafeDeleteRoot double-checks before removing a profile directory that it cannot touch
// DataDir, shared libraries, runtimes, media cache, filesystem roots, or any other instance.
func ValidateSafeDeleteRoot(targetDir string, instancesDir string, protectedDirs []string, otherInstanceDirs []string) (string, error) {
	abs, err := cleanAbsPath(targetDir)
	if err != nil {
		return "", err
	}
	if isDangerousSystemPath(abs) {
		return "", fmt.Errorf("отказ в удалении системной директории %q", abs)
	}
	if samePathCI(abs, instancesDir) || isStrictSubPath(abs, instancesDir) {
		return "", fmt.Errorf("отказ в удалении корневой папки профилей %q", abs)
	}
	for _, prot := range protectedDirs {
		if isSameOrParentPath(abs, prot) || isStrictSubPath(prot, abs) {
			return "", fmt.Errorf("отказ в удалении: путь %q затрагивает общие ресурсы ModHub", abs)
		}
	}
	for _, other := range otherInstanceDirs {
		if isSameOrParentPath(abs, other) || isStrictSubPath(other, abs) {
			return "", fmt.Errorf("отказ в удалении: путь %q пересекается с другим профилем", abs)
		}
	}
	// If custom directory is outside instancesDir, require instance.json marker inside it before deleting!
	if !isStrictSubPath(instancesDir, abs) {
		if _, err := os.Stat(filepath.Join(abs, "instance.json")); err != nil {
			return "", fmt.Errorf("отказ в удалении внешней директории %q: отсутствует файл instance.json", abs)
		}
	}
	return abs, nil
}

// copyDirIndependent deep-copies regular files & folders physically without creating symlinks or hardlinks.
// Existing symlinks in srcDir are skipped for Windows safety and strict profile isolation.
func copyDirIndependent(srcDir, dstDir string) error {
	info, err := os.Lstat(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, ent := range entries {
		name := ent.Name()
		if name == "" || name == "." || name == ".." || strings.HasSuffix(name, ".lock") {
			continue
		}
		srcPath := filepath.Join(srcDir, name)
		dstPath := filepath.Join(dstDir, name)
		fi, err := os.Lstat(srcPath)
		if err != nil {
			continue
		}
		// Never copy symlinks (preserves strict isolation & Windows compatibility)
		if fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if fi.IsDir() {
			if err := copyDirIndependent(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if err := copyRegularFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyRegularFile(srcPath, dstPath string) error {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dstPath)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dstPath)
		return err
	}
	return nil
}

func statDirRecursive(dir string) (bool, int, int64) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false, 0, 0
	}
	count := 0
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return nil
		}
		count++
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return true, count, total
}
