package accounts

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var validMinecraftUsername = regexp.MustCompile(`^[a-zA-Z0-9_]{3,16}$`)

// ValidateMinecraftUsername checks if the nickname satisfies Minecraft Java Edition rules (3-16 chars, a-z, A-Z, 0-9, _).
func ValidateMinecraftUsername(username string) error {
	trimmed := strings.TrimSpace(username)
	if len(trimmed) < 3 || len(trimmed) > 16 {
		return fmt.Errorf("никнейм должен содержать от 3 до 16 символов")
	}
	if !validMinecraftUsername.MatchString(trimmed) {
		return fmt.Errorf("никнейм может содержать только латинские буквы, цифры и символ подчеркивания")
	}
	return nil
}

// GenerateOfflinePlayerUUID creates a standard deterministic Type 3 (MD5-based) UUID
// conforming to Minecraft's offline player UUID generation: UUID.nameUUIDFromBytes(("OfflinePlayer:" + username).getBytes(UTF_8))
func GenerateOfflinePlayerUUID(username string) (rawUUID string, formattedUUID string) {
	trimmed := strings.TrimSpace(username)
	hasher := md5.New()
	hasher.Write([]byte("OfflinePlayer:" + trimmed))
	raw := hasher.Sum(nil)

	// Set version to 3 (0011 in high 4 bits of octet 6)
	raw[6] = (raw[6] & 0x0f) | 0x30
	// Set variant to IETF / RFC 4122 (10 in high 2 bits of octet 8)
	raw[8] = (raw[8] & 0x3f) | 0x80

	rawUUID = hex.EncodeToString(raw)
	formattedUUID = fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
	return rawUUID, formattedUUID
}
