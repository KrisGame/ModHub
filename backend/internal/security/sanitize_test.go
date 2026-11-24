package security

import (
	"strings"
	"testing"
)

func TestSanitizeLogTextAndVaultRoundTrip(t *testing.T) {
	clean := SanitizeLogText("err $2a$10$abcdefghijklmnopqrstuvwxyz1234567890 Bearer SECRET_TOKEN_1234567890")
	if strings.Contains(clean, "$2a$10$") || strings.Contains(clean, "SECRET_TOKEN") {
		t.Fatalf("secret leaked into diagnostic log: %q", clean)
	}

	jwtLog := SanitizeLogText(`{"access_token":"eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.signature1234567890","refresh_token":"M.C512_REFRESH_SECRET"}`)
	if strings.Contains(jwtLog, "eyJhbGciOiJSUzI1NiJ9") || strings.Contains(jwtLog, "M.C512_REFRESH_SECRET") {
		t.Fatalf("OAuth/JWT token leaked into log: %q", jwtLog)
	}

	k := SanitizeCFKey("  x-api-key: \"$$2a$$10$$abc1234567890\"\n")
	if k != "$2a$10$abc1234567890" {
		t.Fatalf("unexpected sanitized key: %q", k)
	}

	dir := t.TempDir()
	plain := []byte(`{"ms_refresh_token":"top-secret-refresh","mc_access_token":"top-secret-mc"}`)
	sealed, err := SealVaultBlob(dir, plain)
	if err != nil {
		t.Fatalf("SealVaultBlob failed: %v", err)
	}
	if strings.Contains(string(sealed), "top-secret-refresh") || strings.Contains(string(sealed), "top-secret-mc") {
		t.Fatal("sealed vault blob contains plaintext secret!")
	}
	opened, err := OpenVaultBlob(dir, sealed)
	if err != nil || string(opened) != string(plain) {
		t.Fatalf("OpenVaultBlob mismatch: got %q err=%v", string(opened), err)
	}
	if _, err := OpenVaultBlob(dir, []byte("corrupted-garbage")); err == nil {
		t.Fatal("expected error for corrupted vault blob")
	}
}
