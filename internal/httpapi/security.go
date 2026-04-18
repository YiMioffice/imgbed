package httpapi

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"machring/internal/resource"
)

const (
	defaultUploadRequestLimit = 12
	defaultUploadWindow       = time.Minute
	defaultLoginFailureLimit  = 5
	defaultLoginFailureWindow = 10 * time.Minute
	defaultJSONBodyLimit      = 1 << 20
	maxRateLimitEntries       = 8192
)

type fixedWindowRateLimiter struct {
	limit  int
	window time.Duration

	mu      sync.Mutex
	entries map[string]rateLimitEntry
}

type rateLimitEntry struct {
	count   int
	resetAt time.Time
}

func newFixedWindowRateLimiter(limit int, window time.Duration) *fixedWindowRateLimiter {
	return &fixedWindowRateLimiter{
		limit:   limit,
		window:  window,
		entries: make(map[string]rateLimitEntry),
	}
}

func (l *fixedWindowRateLimiter) Allow(key string, now time.Time) (bool, time.Time, int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := l.currentEntryLocked(key, now)
	if entry.count >= l.limit {
		return false, entry.resetAt, max(l.limit-entry.count, 0)
	}
	entry.count++
	l.entries[key] = entry
	return true, entry.resetAt, max(l.limit-entry.count, 0)
}

func (l *fixedWindowRateLimiter) IsBlocked(key string, now time.Time) (bool, time.Time, int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := l.currentEntryLocked(key, now)
	if entry.count >= l.limit {
		return true, entry.resetAt, 0
	}
	return false, entry.resetAt, max(l.limit-entry.count, 0)
}

func (l *fixedWindowRateLimiter) AddFailure(key string, now time.Time) (bool, time.Time, int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := l.currentEntryLocked(key, now)
	entry.count++
	l.entries[key] = entry
	return entry.count >= l.limit, entry.resetAt, max(l.limit-entry.count, 0)
}

func (l *fixedWindowRateLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *fixedWindowRateLimiter) currentEntryLocked(key string, now time.Time) rateLimitEntry {
	l.pruneExpiredLocked(now)
	entry, ok := l.entries[key]
	if !ok || !now.Before(entry.resetAt) {
		entry = rateLimitEntry{
			resetAt: now.Add(l.window),
		}
	}
	return entry
}

func (l *fixedWindowRateLimiter) pruneExpiredLocked(now time.Time) {
	if len(l.entries) == 0 {
		return
	}
	for key, entry := range l.entries {
		if !now.Before(entry.resetAt) {
			delete(l.entries, key)
		}
	}
	if len(l.entries) <= maxRateLimitEntries {
		return
	}
	for key := range l.entries {
		delete(l.entries, key)
		if len(l.entries) <= maxRateLimitEntries {
			return
		}
	}
}

func sanitizeUploadFilename(filename string) string {
	filename = strings.TrimSpace(filename)
	filename = strings.ReplaceAll(filename, "\\", "/")
	filename = path.Base(filename)

	var builder strings.Builder
	for _, r := range filename {
		switch {
		case r == 0, r < 32, r == 127:
			continue
		case strings.ContainsRune(`\/:*?"<>|`, r):
			builder.WriteRune('-')
		default:
			builder.WriteRune(r)
		}
	}

	cleaned := strings.TrimSpace(strings.Trim(builder.String(), ". "))
	switch cleaned {
	case "", ".", "..":
		return "upload"
	default:
		return cleaned
	}
}

func normalizeContentType(value string) string {
	base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(value)), ";")
	return strings.TrimSpace(base)
}

func validateUploadMetadata(meta resource.Metadata, sniffContentType string) error {
	allowedTypes, strict := expectedContentTypes(meta.Extension)
	if !strict {
		return nil
	}
	for _, allowedType := range allowedTypes {
		if matchesAllowedContentType(sniffContentType, allowedType) {
			return nil
		}
	}
	return fmt.Errorf("file extension .%s does not match detected content type %s", meta.Extension, sniffContentType)
}

func expectedContentTypes(ext string) ([]string, bool) {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "jpg", "jpeg":
		return []string{"image/jpeg"}, true
	case "png":
		return []string{"image/png"}, true
	case "gif":
		return []string{"image/gif"}, true
	case "webp":
		return []string{"image/webp"}, true
	case "bmp":
		return []string{"image/bmp"}, true
	case "ico":
		return []string{"image/x-icon", "image/vnd.microsoft.icon", "application/octet-stream"}, true
	case "avif":
		return []string{"image/avif"}, true
	case "svg":
		return []string{"image/svg+xml", "text/xml", "application/xml", "text/plain"}, true
	case "html", "htm", "xhtml":
		return []string{"text/html", "application/xhtml+xml"}, true
	case "js", "mjs", "cjs":
		return []string{"application/javascript", "text/javascript", "application/x-javascript", "text/plain"}, true
	case "css":
		return []string{"text/css", "text/plain"}, true
	case "zip":
		return []string{"application/zip", "application/x-zip-compressed", "application/octet-stream"}, true
	case "pdf":
		return []string{"application/pdf"}, true
	default:
		return nil, false
	}
}

func matchesAllowedContentType(actual, allowed string) bool {
	if actual == "" || allowed == "" {
		return false
	}
	if actual == allowed {
		return true
	}
	if strings.HasSuffix(allowed, "/*") {
		return strings.HasPrefix(actual, strings.TrimSuffix(allowed, "*"))
	}
	return false
}

func isDangerousResource(record resource.Record) bool {
	if record.Type == resource.TypeExecutable || record.Type == resource.TypeScript {
		return true
	}
	switch strings.ToLower(strings.TrimPrefix(record.Extension, ".")) {
	case "svg", "html", "htm", "xhtml", "js", "mjs", "cjs":
		return true
	default:
		return false
	}
}

func applyResourceSecurityHeaders(w http.ResponseWriter, record resource.Record) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if isDangerousResource(record) {
		w.Header().Set("Content-Security-Policy", "sandbox")
		w.Header().Set("X-Frame-Options", "DENY")
	}
}

func secureMiddleware(next http.Handler) http.Handler {
	return securityHeadersMiddleware(limitRequestBodyMiddleware(originGuardMiddleware(next)))
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		if h.Get("X-Content-Type-Options") == "" {
			h.Set("X-Content-Type-Options", "nosniff")
		}
		if h.Get("Referrer-Policy") == "" {
			h.Set("Referrer-Policy", "same-origin")
		}
		if h.Get("X-Frame-Options") == "" {
			h.Set("X-Frame-Options", "DENY")
		}
		if h.Get("Permissions-Policy") == "" {
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		}
		next.ServeHTTP(w, r)
	})
}

func limitRequestBodyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isUnsafeMethod(r.Method) && r.URL.Path != "/api/v1/resources/upload" {
			r.Body = http.MaxBytesReader(w, r.Body, defaultJSONBodyLimit)
		}
		next.ServeHTTP(w, r)
	})
}

func originGuardMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isUnsafeMethod(r.Method) && !hasSameOrigin(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "cross-origin request rejected"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func hasSameOrigin(r *http.Request) bool {
	source := strings.TrimSpace(r.Header.Get("Origin"))
	if source == "" {
		source = strings.TrimSpace(r.Header.Get("Referer"))
	}
	if source == "" {
		return true
	}
	sourceURL, err := url.Parse(source)
	if err != nil || sourceURL.Host == "" {
		return false
	}
	return strings.EqualFold(sourceURL.Host, requestHost(r))
}

func requestHost(r *http.Request) string {
	if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" && requestFromTrustedProxy(r) {
		return strings.Split(forwardedHost, ",")[0]
	}
	return r.Host
}

func clientIP(r *http.Request) string {
	if requestFromTrustedProxy(r) {
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			parts := strings.Split(forwarded, ",")
			if len(parts) > 0 {
				if ip := strings.TrimSpace(parts[0]); ip != "" {
					return ip
				}
			}
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
			return realIP
		}
	}
	return remoteIP(r)
}

func requestFromTrustedProxy(r *http.Request) bool {
	ip := net.ParseIP(remoteIP(r))
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	})
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if requestFromTrustedProxy(r) {
		return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
	}
	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
