package modpacks

import (
	"archive/zip"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	DefaultMaxArchiveFiles      = 20000
	DefaultMaxUncompressedBytes = int64(4) * 1024 * 1024 * 1024
	DefaultMaxSingleEntryBytes  = int64(512) * 1024 * 1024
	DefaultMaxManifestBytes     = int64(16) * 1024 * 1024
	maxSafeCompressionRatio     = 150
)

var (
	ErrZipSlipDetected   = errors.New("обнаружен небезопасный путь внутри архива (Zip Slip / Path Traversal)")
	ErrArchiveTooMany    = errors.New("архив превышает допустимое количество файлов")
	ErrArchiveTooLarge   = errors.New("распакованный размер архива превышает допустимый лимит")
	ErrArchiveCorrupted  = errors.New("архив повреждён или не является корректным ZIP-файлом")
	ErrDuplicateZipEntry = errors.New("в архиве обнаружены дублирующиеся пути файлов")
)

type ArchiveLimits struct {
	MaxFiles             int
	MaxUncompressedBytes int64
	MaxSingleEntryBytes  int64
}

func DefaultArchiveLimits() ArchiveLimits {
	return ArchiveLimits{MaxFiles: DefaultMaxArchiveFiles, MaxUncompressedBytes: DefaultMaxUncompressedBytes, MaxSingleEntryBytes: DefaultMaxSingleEntryBytes}
}

var winReservedBaseNames = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {}, "com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {}, "lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
}

var suspiciousExtensions = map[string]string{
	".exe": "исполняемый файл Windows (.exe)", ".bat": "командный скрипт Windows (.bat)",
	".cmd": "командный скрипт Windows (.cmd)", ".ps1": "скрипт PowerShell (.ps1)",
	".vbs": "скрипт VBScript (.vbs)", ".scr": "исполняемый файл (.scr)",
	".msi": "установщик Windows (.msi)", ".com": "исполняемый файл (.com)",
	".sh": "shell-скрипт (.sh)", ".dll": "динамическая библиотека (.dll)", ".so": "динамическая библиотека (.so)",
}

func ValidateRelativeGamePath(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("%w: пустой путь", ErrZipSlipDetected)
	}
	s = strings.ReplaceAll(s, "\\", "/")
	if strings.HasPrefix(s, "/") || strings.Contains(s, ":") {
		return "", fmt.Errorf("%w: абсолютный путь или буква диска (%q)", ErrZipSlipDetected, raw)
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%w: управляющие символы в пути (%q)", ErrZipSlipDetected, raw)
		}
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == ".." {
			return "", fmt.Errorf("%w: выход за пределы директории (%q)", ErrZipSlipDetected, raw)
		}
		if seg == "" {
			continue
		}
		if strings.ContainsAny(seg, `<>"|?*`) {
			return "", fmt.Errorf("%w: недопустимые символы Windows в сегменте %q", ErrZipSlipDetected, seg)
		}
		stem := strings.ToLower(strings.TrimSuffix(seg, path.Ext(seg)))
		if dot := strings.IndexByte(stem, '.'); dot >= 0 {
			stem = stem[:dot]
		}
		if _, reserved := winReservedBaseNames[stem]; reserved {
			return "", fmt.Errorf("%w: зарезервированное имя Windows %q", ErrZipSlipDetected, seg)
		}
	}
	cleaned := path.Clean(s)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("%w: %q", ErrZipSlipDetected, raw)
	}
	return cleaned, nil
}

func ResolveSafeDestPath(baseDir, relPath string) (string, error) {
	cleanRel, err := ValidateRelativeGamePath(relPath)
	if err != nil {
		return "", err
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	full := filepath.Join(absBase, filepath.FromSlash(cleanRel))
	relCheck, err := filepath.Rel(absBase, full)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: выход за пределы целевой директории (%q)", ErrZipSlipDetected, relPath)
	}
	return full, nil
}

func CheckSuspiciousFile(relPath string) (bool, string) {
	if reason, bad := suspiciousExtensions[strings.ToLower(path.Ext(relPath))]; bad {
		return true, "Подозрительный файл: " + reason + " (не будет запущен автоматически)"
	}
	return false, ""
}

func IsSensitiveSecretPath(relPath string) bool {
	low := strings.ToLower(strings.ReplaceAll(relPath, "\\", "/"))
	base := path.Base(low)
	if strings.HasPrefix(low, "vault/") || strings.Contains(low, "/vault/") || strings.HasSuffix(base, ".sec") || strings.HasSuffix(base, ".key") {
		return true
	}
	for _, forbidden := range []string{
		"accounts.json", "launcher_accounts.json", "launcher_accounts_microsoft_store.json",
		"launcher_msa_credentials.bin", "realms_persistence.json", "usercache.json",
		"config.json", ".env", "secrets.json", "tokens.json",
	} {
		if base == forbidden {
			return true
		}
	}
	return false
}

func InspectArchiveSafety(zr *zip.ReadCloser, limits ArchiveLimits) ([]string, error) {
	if limits.MaxFiles <= 0 {
		limits.MaxFiles = DefaultMaxArchiveFiles
	}
	if limits.MaxUncompressedBytes <= 0 {
		limits.MaxUncompressedBytes = DefaultMaxUncompressedBytes
	}
	if limits.MaxSingleEntryBytes <= 0 {
		limits.MaxSingleEntryBytes = DefaultMaxSingleEntryBytes
	}
	if len(zr.File) > limits.MaxFiles {
		return nil, fmt.Errorf("%w (%d > %d)", ErrArchiveTooMany, len(zr.File), limits.MaxFiles)
	}
	var totalUncompressed uint64
	seenPaths := map[string]struct{}{}
	var warnings []string
	for _, f := range zr.File {
		rawName := strings.ReplaceAll(f.Name, "\\", "/")
		if strings.TrimSpace(rawName) == "" {
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: символическая ссылка %q внутри архива запрещена", ErrZipSlipDetected, f.Name)
		}
		if f.FileInfo().IsDir() {
			if trimmed := strings.TrimRight(rawName, "/"); trimmed != "" {
				if _, err := ValidateRelativeGamePath(trimmed); err != nil {
					return nil, err
				}
			}
			continue
		}
		cleanRel, err := ValidateRelativeGamePath(rawName)
		if err != nil {
			return nil, err
		}
		lowKey := strings.ToLower(cleanRel)
		if _, dup := seenPaths[lowKey]; dup {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateZipEntry, cleanRel)
		}
		seenPaths[lowKey] = struct{}{}
		if int64(f.UncompressedSize64) > limits.MaxSingleEntryBytes {
			return nil, fmt.Errorf("%w: файл %s превышает лимит (%d МБ)", ErrArchiveTooLarge, cleanRel, limits.MaxSingleEntryBytes/(1024*1024))
		}
		totalUncompressed += f.UncompressedSize64
		if int64(totalUncompressed) > limits.MaxUncompressedBytes {
			return nil, fmt.Errorf("%w (превышен лимит %d МБ)", ErrArchiveTooLarge, limits.MaxUncompressedBytes/(1024*1024))
		}
		if f.UncompressedSize64 > 16*1024*1024 && f.CompressedSize64 > 0 && f.UncompressedSize64/f.CompressedSize64 > maxSafeCompressionRatio {
			return nil, fmt.Errorf("%w: подозрительный коэффициент сжатия у файла %s", ErrArchiveTooLarge, cleanRel)
		}
		if susp, reason := CheckSuspiciousFile(cleanRel); susp {
			warnings = append(warnings, fmt.Sprintf("%s: %s", cleanRel, reason))
		}
	}
	return warnings, nil
}

func ClassifyComponentPath(relPath string, cfClassID int, mrProjectType string) ComponentCategory {
	switch cfClassID {
	case 12:
		return CatResourcePack
	case 6552:
		return CatShader
	case 6945:
		return CatDatapack
	}
	switch strings.ToLower(strings.TrimSpace(mrProjectType)) {
	case "resourcepack":
		return CatResourcePack
	case "shader":
		return CatShader
	case "datapack":
		return CatDatapack
	}
	low := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(relPath), "\\", "/"))
	switch {
	case strings.HasPrefix(low, "mods/"):
		return CatMod
	case strings.HasPrefix(low, "resourcepacks/"):
		return CatResourcePack
	case strings.HasPrefix(low, "shaderpacks/"):
		return CatShader
	case strings.HasPrefix(low, "datapacks/") || strings.Contains(low, "/datapacks/"):
		return CatDatapack
	case strings.HasPrefix(low, "config/") || strings.HasPrefix(low, "defaultconfigs/") || strings.HasPrefix(low, "kubejs/") || strings.HasPrefix(low, "scripts/") || low == "options.txt" || low == "servers.dat":
		return CatConfig
	default:
		return CatUserFile
	}
}

func FolderForCategory(cat ComponentCategory) string {
	switch cat {
	case CatResourcePack:
		return "resourcepacks"
	case CatShader:
		return "shaderpacks"
	case CatDatapack:
		return "datapacks"
	case CatConfig:
		return "config"
	default:
		return "mods"
	}
}

type FileHashes struct {
	SHA1   string
	SHA256 string
	SHA512 string
	Size   int64
}

func ComputeFileHashes(path string) (FileHashes, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileHashes{}, err
	}
	defer f.Close()
	h1, h256, h512 := sha1.New(), sha256.New(), sha512.New()
	n, err := io.Copy(io.MultiWriter(h1, h256, h512), f)
	if err != nil {
		return FileHashes{}, err
	}
	return FileHashes{SHA1: hex.EncodeToString(h1.Sum(nil)), SHA256: hex.EncodeToString(h256.Sum(nil)), SHA512: hex.EncodeToString(h512.Sum(nil)), Size: n}, nil
}

func ComputeReaderSHA256(r io.Reader) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
