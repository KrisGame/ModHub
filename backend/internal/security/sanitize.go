package security

import (
	"regexp"
	"strings"
)

var (
	reLogSecretKey   = regexp.MustCompile(`\$2[aby]\$\d{2}\$[A-Za-z0-9./]{20,}`)
	reLogJWT         = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)
	reLogBearer      = regexp.MustCompile(`(?i)(Bearer|XBL3\.0\s+x=[^;\s]+;)\s*[A-Za-z0-9._~+/=-]{8,}`)
	reLogJSONSecret  = regexp.MustCompile(`(?i)("(?:access_token|refresh_token|id_token|identityToken|RpsTicket|Token|code_verifier|code)"\s*:\s*")([^"]+)(")`)
	reLogQuerySecret = regexp.MustCompile(`(?i)((?:access_token|refresh_token|code|code_verifier)=)([^&\s]+)`)
)

func SanitizeLogText(s string) string {
	if s == "" {
		return ""
	}
	out := reLogSecretKey.ReplaceAllString(s, "[REDACTED_KEY]")
	out = reLogJWT.ReplaceAllString(out, "[REDACTED_JWT]")
	out = reLogBearer.ReplaceAllString(out, "[REDACTED_TOKEN]")
	out = reLogJSONSecret.ReplaceAllString(out, `${1}[REDACTED]${3}`)
	out = reLogQuerySecret.ReplaceAllString(out, `${1}[REDACTED]`)
	return out
}

func SanitizeCFKey(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if (r >= 0x200b && r <= 0x200f) || (r >= 0x202a && r <= 0x202e) || r == 0xfeff || r == '\r' || r == '\n' || r == '\t' {
			continue
		}
		b.WriteRune(r)
	}
	s := strings.TrimSpace(b.String())
	if strings.HasPrefix(strings.ToLower(s), "x-api-key:") {
		s = strings.TrimSpace(s[len("x-api-key:"):])
	}
	s = strings.Trim(s, "\"'` ")
	return strings.ReplaceAll(s, "$$", "$")
}

func MaskCFKey(raw string) string {
	clean := SanitizeCFKey(raw)
	if clean == "" {
		return ""
	}
	if len(clean) <= 10 {
		return "••••••"
	}
	return clean[:6] + "••••" + clean[len(clean)-4:]
}
