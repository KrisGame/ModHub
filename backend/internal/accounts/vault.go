package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"modhub/internal/security"
)

var (
	ErrVaultNotFound  = errors.New("защищённые токены аккаунта не найдены")
	ErrVaultCorrupted = errors.New("защищённые данные аккаунта повреждены или недоступны")
	reSafeVaultKey    = regexp.MustCompile(`^[a-zA-Z0-9_-]{4,64}$`)
)

// SecretVault stores encrypted SecretBundle blobs on disk outside SQLite with 0600 permissions.
// On Windows, encryption is strictly Windows DPAPI; on non-Windows, authenticated AES-256-GCM.
type SecretVault struct {
	dir string
}

func NewSecretVault(vaultDir string) *SecretVault {
	return &SecretVault{dir: filepath.Clean(vaultDir)}
}

func (v *SecretVault) Dir() string {
	return v.dir
}

func (v *SecretVault) pathFor(vaultKey string) (string, error) {
	k := strings.TrimSpace(vaultKey)
	if !reSafeVaultKey.MatchString(k) || strings.Contains(k, "..") {
		return "", fmt.Errorf("некорректный ключ хранилища секретов %q", vaultKey)
	}
	return filepath.Join(v.dir, k+".sec"), nil
}

func (v *SecretVault) Save(vaultKey string, bundle SecretBundle) error {
	target, err := v.pathFor(vaultKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(v.dir, 0o700); err != nil {
		return fmt.Errorf("не удалось создать директорию защищённого хранилища: %w", err)
	}
	plain, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	sealed, err := security.SealVaultBlob(v.dir, plain)
	// Wipe plaintext slice in memory as soon as sealed
	for i := range plain {
		plain[i] = 0
	}
	if err != nil {
		return fmt.Errorf("не удалось зашифровать токены аккаунта: %w", err)
	}

	tmp, err := os.CreateTemp(v.dir, vaultKey+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Chmod(0o600)
	if _, err := tmp.Write(sealed); err != nil {
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
	if err := os.Rename(tmpPath, target); err != nil {
		// Windows fallback if destination exists
		_ = os.Remove(target)
		if err2 := os.Rename(tmpPath, target); err2 != nil {
			_ = os.Remove(tmpPath)
			return err2
		}
	}
	_ = os.Chmod(target, 0o600)
	return nil
}

func (v *SecretVault) Load(vaultKey string) (SecretBundle, error) {
	target, err := v.pathFor(vaultKey)
	if err != nil {
		return SecretBundle{}, err
	}
	sealed, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return SecretBundle{}, ErrVaultNotFound
		}
		return SecretBundle{}, fmt.Errorf("%w: %v", ErrVaultCorrupted, err)
	}
	plain, err := security.OpenVaultBlob(v.dir, sealed)
	if err != nil {
		return SecretBundle{}, fmt.Errorf("%w: %v", ErrVaultCorrupted, err)
	}
	var bundle SecretBundle
	unmErr := json.Unmarshal(plain, &bundle)
	for i := range plain {
		plain[i] = 0
	}
	if unmErr != nil {
		return SecretBundle{}, fmt.Errorf("%w: повреждённый JSON секретов", ErrVaultCorrupted)
	}
	return bundle, nil
}

func (v *SecretVault) Exists(vaultKey string) bool {
	target, err := v.pathFor(vaultKey)
	if err != nil {
		return false
	}
	fi, err := os.Stat(target)
	return err == nil && !fi.IsDir() && fi.Size() > 0
}

func (v *SecretVault) Delete(vaultKey string) error {
	target, err := v.pathFor(vaultKey)
	if err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
