//go:build windows

package security

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const (
	secretDPAPIPrefix       = "dpapi:"
	cryptProtectUIForbidden = 0x01
)

var dpapiVaultEntropy = []byte("ModHub-Vault-DPAPI-v1")

type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	modCrypt32           = syscall.NewLazyDLL("crypt32.dll")
	procCryptProtectData = modCrypt32.NewProc("CryptProtectData")
	procCryptUnprotect   = modCrypt32.NewProc("CryptUnprotectData")
	modKernel32Sec       = syscall.NewLazyDLL("kernel32.dll")
	procLocalFreeSec     = modKernel32Sec.NewProc("LocalFree")
)

func dpapiProtectRaw(raw, entropy []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty secret payload")
	}
	inBlob := dataBlob{cbData: uint32(len(raw)), pbData: &raw[0]}
	var entPtr uintptr
	if len(entropy) > 0 {
		entBlob := dataBlob{cbData: uint32(len(entropy)), pbData: &entropy[0]}
		entPtr = uintptr(unsafe.Pointer(&entBlob))
	}
	var outBlob dataBlob
	r, _, callErr := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0,
		entPtr,
		0,
		0,
		uintptr(cryptProtectUIForbidden),
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if r == 0 || outBlob.pbData == nil || outBlob.cbData == 0 {
		return nil, fmt.Errorf("Windows DPAPI CryptProtectData failed: %v", callErr)
	}
	defer procLocalFreeSec.Call(uintptr(unsafe.Pointer(outBlob.pbData)))
	enc := unsafe.Slice(outBlob.pbData, outBlob.cbData)
	cp := make([]byte, len(enc))
	copy(cp, enc)
	return cp, nil
}

func dpapiUnprotectRaw(enc, entropy []byte) ([]byte, error) {
	if len(enc) == 0 {
		return nil, errors.New("empty encrypted payload")
	}
	inBlob := dataBlob{cbData: uint32(len(enc)), pbData: &enc[0]}
	var entPtr uintptr
	if len(entropy) > 0 {
		entBlob := dataBlob{cbData: uint32(len(entropy)), pbData: &entropy[0]}
		entPtr = uintptr(unsafe.Pointer(&entBlob))
	}
	var outBlob dataBlob
	r, _, callErr := procCryptUnprotect.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0,
		entPtr,
		0,
		0,
		uintptr(cryptProtectUIForbidden),
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if r == 0 || outBlob.pbData == nil || outBlob.cbData == 0 {
		return nil, fmt.Errorf("Windows DPAPI CryptUnprotectData failed: %v", callErr)
	}
	defer procLocalFreeSec.Call(uintptr(unsafe.Pointer(outBlob.pbData)))
	dec := unsafe.Slice(outBlob.pbData, outBlob.cbData)
	cp := make([]byte, len(dec))
	copy(cp, dec)
	return cp, nil
}

// ProtectSecret protects a single config string via Windows DPAPI (preserving backwards compatibility with config.json).
func ProtectSecret(plain string) (string, error) {
	clean := strings.TrimSpace(plain)
	if clean == "" {
		return "", nil
	}
	if strings.HasPrefix(clean, secretDPAPIPrefix) {
		return clean, nil
	}
	enc, err := dpapiProtectRaw([]byte(clean), nil)
	if err != nil {
		return "", err
	}
	return secretDPAPIPrefix + base64.StdEncoding.EncodeToString(enc), nil
}

func ProtectSecretWithDir(_ string, plain string) (string, error) { return ProtectSecret(plain) }
func UnprotectSecretWithDir(_ string, stored string) string       { return UnprotectSecret(stored) }

func UnprotectSecret(stored string) string {
	s := strings.TrimSpace(stored)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, secretDPAPIPrefix) {
		return s
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, secretDPAPIPrefix))
	if err != nil || len(raw) == 0 {
		return ""
	}
	dec, err := dpapiUnprotectRaw(raw, nil)
	if err != nil {
		return ""
	}
	return string(dec)
}

// SealVaultBlob encrypts binary secret payloads strictly via Windows DPAPI (never falls back to plaintext).
func SealVaultBlob(_ string, plain []byte) ([]byte, error) {
	enc, err := dpapiProtectRaw(plain, dpapiVaultEntropy)
	if err != nil {
		return nil, err
	}
	const header = "MHDPAPI1:"
	out := make([]byte, 0, len(header)+len(enc))
	out = append(out, header...)
	out = append(out, enc...)
	return out, nil
}

// OpenVaultBlob decrypts a Windows DPAPI sealed vault blob.
func OpenVaultBlob(_ string, sealed []byte) ([]byte, error) {
	const header = "MHDPAPI1:"
	if len(sealed) <= len(header) || string(sealed[:len(header)]) != header {
		return nil, errors.New("повреждённый формат защищённого хранилища DPAPI")
	}
	return dpapiUnprotectRaw(sealed[len(header):], dpapiVaultEntropy)
}

func VaultBackendName() string {
	return "windows-dpapi"
}
