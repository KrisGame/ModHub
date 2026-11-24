//go:build !windows

package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const secretDPAPIPrefix = "dpapi:"

func ProtectSecret(plain string) string {
	return strings.TrimSpace(plain)
}

func UnprotectSecret(stored string) string {
	s := strings.TrimSpace(stored)
	if strings.HasPrefix(s, secretDPAPIPrefix) {
		return ""
	}
	return s
}

func loadOrCreateLocalVaultKey(vaultDir string) ([]byte, error) {
	if strings.TrimSpace(vaultDir) == "" {
		return nil, errors.New("пустая директория защищённого хранилища")
	}
	if err := os.MkdirAll(vaultDir, 0o700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(vaultDir, ".vault.key")
	if raw, err := os.ReadFile(keyPath); err == nil && len(raw) == 32 {
		return raw, nil
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
	_ = tmp.Sync()
	_ = tmp.Close()
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
