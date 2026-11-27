//go:build !windows

package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	secretDPAPIPrefix = "dpapi:"
	secretAESPrefix   = "mh-aesgcm:"
)

func ProtectSecret(plain string) (string, error) {
	return ProtectSecretWithDir(configSecretDataDir(), plain)
}

func ProtectSecretWithDir(dataDir, plain string) (string, error) {
	clean := strings.TrimSpace(plain)
	if clean == "" {
		return clean, nil
	}
	if strings.TrimSpace(dataDir) == "" {
		return "", errors.New("путь данных для защищённого хранения не задан")
	}
	if strings.HasPrefix(clean, secretAESPrefix) {
		if UnprotectSecretWithDir(dataDir, clean) == "" {
			return "", errors.New("не удалось проверить существующий шифротекст")
		}
		return clean, nil
	}
	sealed, err := SealVaultBlob(filepath.Join(dataDir, "vault"), []byte(clean))
	if err != nil {
		return "", err
	}
	return secretAESPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func UnprotectSecret(stored string) string {
	return UnprotectSecretWithDir(configSecretDataDir(), stored)
}

func UnprotectSecretWithDir(dataDir, stored string) string {
	s := strings.TrimSpace(stored)
	if strings.HasPrefix(s, secretAESPrefix) {
		if strings.TrimSpace(dataDir) == "" {
			return s
		}
		sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, secretAESPrefix))
		if err != nil {
			return s
		}
		plain, err := OpenVaultBlob(filepath.Join(dataDir, "vault"), sealed)
		if err != nil {
			return s
		}
		return string(plain)
	}
	if strings.HasPrefix(s, secretDPAPIPrefix) {
		return ""
	}
	return s
}

// Config secrets use the same per-user data directory as the app config.
// Override this path in tests and nonstandard deployments via MODHUB_DATA_DIR.
func configSecretDataDir() string {
	if dir := strings.TrimSpace(os.Getenv("MODHUB_DATA_DIR")); dir != "" {
		return dir
	}
	return ""
}

func loadOrCreateLocalVaultKey(vaultDir string) ([]byte, error) {
	if strings.TrimSpace(vaultDir) == "" {
		return nil, errors.New("пустая директория защищённого хранилища")
	}
	if err := os.MkdirAll(vaultDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(vaultDir, 0o700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(vaultDir, ".vault.key")
	if raw, err := os.ReadFile(keyPath); err == nil {
		if len(raw) != 32 {
			return nil, errors.New("некорректный ключ защищённого хранилища")
		}
		if err := os.Chmod(keyPath, 0o600); err != nil {
			return nil, err
		}
		return raw, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(vaultDir, ".vault.key.*.tmp")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Chmod(0o600)
	if _, err := tmp.Write(key); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	// Recheck before rename to avoid replacing a key created concurrently.
	if raw, readErr := os.ReadFile(keyPath); readErr == nil {
		_ = os.Remove(tmpPath)
		if len(raw) != 32 {
			return nil, errors.New("некорректный ключ защищённого хранилища")
		}
		return raw, nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		_ = os.Remove(tmpPath)
		return nil, readErr
	}
	if err := os.Rename(tmpPath, keyPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	_ = os.Chmod(keyPath, 0o600)
	return key, nil
}

// SealVaultBlob encrypts binary secret payloads with authenticated AES-256-GCM on non-Windows platforms
// so tokens are never written in plaintext anywhere on disk.
func SealVaultBlob(vaultDir string, plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, errors.New("empty secret payload")
	}
	key, err := loadOrCreateLocalVaultKey(vaultDir)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plain, []byte("ModHub-Vault-AESGCM-v1"))
	const header = "MHAESGCM:"
	out := make([]byte, 0, len(header)+len(nonce)+len(ct))
	out = append(out, header...)
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

// OpenVaultBlob decrypts an AES-256-GCM sealed vault blob.
func OpenVaultBlob(vaultDir string, sealed []byte) ([]byte, error) {
	const header = "MHAESGCM:"
	if len(sealed) <= len(header) || string(sealed[:len(header)]) != header {
		return nil, errors.New("повреждённый формат защищённого хранилища")
	}
	key, err := loadOrCreateLocalVaultKey(vaultDir)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	payload := sealed[len(header):]
	if len(payload) <= gcm.NonceSize() {
		return nil, errors.New("повреждённые зашифрованные данные")
	}
	nonce := payload[:gcm.NonceSize()]
	ct := payload[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, []byte("ModHub-Vault-AESGCM-v1"))
	if err != nil {
		return nil, fmt.Errorf("не удалось расшифровать защищённое хранилище: %w", err)
	}
	return plain, nil
}

func VaultBackendName() string {
	return "aes256-gcm-local-vault"
}
