package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"

	"modhub/internal/app"
	"modhub/internal/security"
)

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeRawJSON(w http.ResponseWriter, raw []byte, cacheHit bool) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if cacheHit {
		w.Header().Set("X-ModHub-Cache", "HIT")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": security.SanitizeLogText(msg)})
}

func writeServiceErr(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	switch {
	case errors.Is(err, app.ErrValidation):
		msg = strings.TrimSpace(strings.TrimPrefix(msg, app.ErrValidation.Error()+":"))
		writeErr(w, http.StatusBadRequest, msg)
	case errors.Is(err, app.ErrNotFound):
		msg = strings.TrimSpace(strings.TrimPrefix(msg, app.ErrNotFound.Error()+":"))
		writeErr(w, http.StatusNotFound, msg)
	default:
		writeErr(w, http.StatusInternalServerError, msg)
	}
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any, allowEmpty bool) bool {
	if r.Body == nil {
		if allowEmpty {
			return true
		}
		writeErr(w, http.StatusBadRequest, "Тело запроса отсутствует")
		return false
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) && allowEmpty {
			return true
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeErr(w, http.StatusRequestEntityTooLarge, "Размер тела запроса превышает допустимый лимит")
			return false
		}
		writeErr(w, http.StatusBadRequest, "Некорректный формат JSON")
		return false
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "Некорректные данные после JSON-объекта")
		return false
	}
	return true
}

func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isLoopbackHostname(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" {
		return false
	}
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		h = strings.ToLower(strings.Trim(parsedHost, "[]"))
	}
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "wails.localhost" || h == "wails"
}

func isAllowedLocalOrigin(rawOrigin string) bool {
	u, err := url.Parse(strings.TrimSpace(rawOrigin))
	if err != nil {
		return false
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") && !strings.EqualFold(u.Scheme, "wails") {
		return false
	}
	return isLoopbackHostname(u.Hostname())
}

func localAPISecurityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.Host != "" && !isLoopbackHostname(r.Host) {
			writeErr(w, http.StatusForbidden, "Недопустимый заголовок Host")
			return
		}

		isMutating := r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete
		if isMutating {
			if secSite := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); secSite == "cross-site" {
				writeErr(w, http.StatusForbidden, "Кросс-сайтовые запросы к локальному API запрещены")
				return
			}
			if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
				if !isAllowedLocalOrigin(origin) {
					writeErr(w, http.StatusForbidden, "Недопустимый Origin для локального API")
					return
				}
			} else if ref := strings.TrimSpace(r.Header.Get("Referer")); ref != "" {
				if !isAllowedLocalOrigin(ref) {
					writeErr(w, http.StatusForbidden, "Недопустимый Referer для локального API")
					return
				}
			}

			if r.ContentLength != 0 || strings.TrimSpace(r.Header.Get("Content-Type")) != "" {
				ct := strings.TrimSpace(r.Header.Get("Content-Type"))
				mediaType, _, err := mime.ParseMediaType(ct)
				if err != nil || !strings.EqualFold(mediaType, "application/json") {
					writeErr(w, http.StatusUnsupportedMediaType, "Требуется Content-Type: application/json")
					return
				}
			}

			maxBody := int64(4 << 20)
			if r.URL.Path == "/api/download-pack" {
				maxBody = 8 << 20
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}

		next.ServeHTTP(w, r)
	})
}
