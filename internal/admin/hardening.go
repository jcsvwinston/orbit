package admin

// Hardening middlewares and helpers from the v1.2.1 audit backlog:
//
//   - securityHeadersMiddleware (OR-SEC-P1-4): CSP + nosniff +
//     anti-framing on every panel response.
//   - csrfContentTypeMiddleware (OR-SEC-P2-2): write requests must not
//     be form-submittable. The session cookie is SameSite=Lax by default
//     (pkg/auth), which already blocks cross-site form POSTs in modern
//     browsers; this gate is defense in depth for older browsers and
//     for deployments that relax SameSite.
//   - loginLimiter (OR-SEC-P2-1): per-IP and per-username lockout for
//     failed admin logins.
//   - sensitiveFieldName / redactAuditValues (OR-SEC-P1-2): mask
//     credential-shaped fields before storing an audit OldValue.

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jcsvwinston/orbit/datasource"
)

// securityHeadersMiddleware stamps browser security headers on every
// panel response. The SPA loads nothing from external origins, so a
// strict CSP is cheap; 'unsafe-inline' is needed for style only (the
// login page and the SPA set inline styles). connect-src lists ws:/wss:
// explicitly because some browsers do not extend 'self' to WebSocket
// upgrades (the live feed uses /api/live/ws).
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; font-src 'self'; connect-src 'self' ws: wss:; "+
				"frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// csrfContentTypeMiddleware rejects write requests whose Content-Type a
// cross-site HTML form could produce. The rules:
//
//   - application/json (or any +json type): allowed — forms cannot send it.
//   - multipart/form-data: allowed ONLY on the import upload route.
//   - application/x-www-form-urlencoded, text/plain, or multipart
//     anywhere else: 415. (The admin login form posts to /login, which
//     is mounted OUTSIDE the /api group.)
//   - no Content-Type and no body: allowed (curl-style bodyless POSTs;
//     a browser form always sends a Content-Type).
func (p *Panel) csrfContentTypeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			next.ServeHTTP(w, r)
			return
		}

		ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
		if i := strings.Index(ct, ";"); i >= 0 {
			ct = strings.TrimSpace(ct[:i])
		}

		allowed := false
		switch {
		case ct == "application/json" || strings.HasSuffix(ct, "+json"):
			allowed = true
		case ct == "multipart/form-data":
			allowed = strings.HasSuffix(r.URL.Path, "/api/imports")
		case ct == "":
			allowed = r.ContentLength <= 0
		}
		if !allowed {
			http.Error(w,
				"unsupported media type: admin write endpoints accept application/json "+
					"(multipart/form-data only on the import upload)",
				http.StatusUnsupportedMediaType)
			return
		}
		next.ServeHTTP(w, r)
	})
}

const (
	loginFailureLimit  = 10
	loginFailureWindow = time.Minute
	loginLimiterCap    = 4096
)

// loginLimiter is a fixed-window lockout for failed admin logins, keyed
// by client IP and by username (so a distributed attack on one account
// is still throttled). Deliberately modest — it makes online brute
// force impractical, nothing more.
type loginLimiter struct {
	mu      sync.Mutex
	windows map[string]*loginWindow
}

type loginWindow struct {
	count   int
	resetAt time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{windows: make(map[string]*loginWindow)}
}

func (l *loginLimiter) blocked(key string) bool {
	if l == nil || key == "" {
		return false
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.windows[key]
	if !ok {
		return false
	}
	if now.After(w.resetAt) {
		delete(l.windows, key)
		return false
	}
	return w.count >= loginFailureLimit
}

func (l *loginLimiter) fail(key string) {
	if l == nil || key == "" {
		return
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if w, ok := l.windows[key]; ok && now.Before(w.resetAt) {
		w.count++
		return
	}
	if len(l.windows) >= loginLimiterCap {
		for k, w := range l.windows {
			if now.After(w.resetAt) {
				delete(l.windows, k)
			}
		}
		if len(l.windows) >= loginLimiterCap {
			return // fail-open on tracking, never on auth itself
		}
	}
	l.windows[key] = &loginWindow{count: 1, resetAt: now.Add(loginFailureWindow)}
}

func (l *loginLimiter) reset(key string) {
	if l == nil || key == "" {
		return
	}
	l.mu.Lock()
	delete(l.windows, key)
	l.mu.Unlock()
}

// sensitiveFieldName reports whether a field/column name looks like it
// holds a credential. Used to redact audit OldValue snapshots.
func sensitiveFieldName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, needle := range []string{"password", "secret", "token", "credential", "api_key", "apikey", "private_key"} {
		if strings.Contains(n, needle) {
			return true
		}
	}
	return strings.HasSuffix(n, "hash") || strings.HasSuffix(n, "salt")
}

const redactedPlaceholder = "[redacted]"

// redactAuditValues returns a copy of values with excluded fields
// (datasource.FieldInfo.IsExcluded — the model marked them as never
// shown in Data Studio) and credential-shaped names masked. The audit
// log is readable via /api/audit; storing the full old record would
// leak password hashes and tokens to any operator with audit_view.
func redactAuditValues(mi datasource.ModelInfo, values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	excluded := make(map[string]struct{}, len(mi.Fields))
	for _, f := range mi.Fields {
		if f.IsExcluded {
			excluded[strings.ToLower(f.Name)] = struct{}{}
			excluded[strings.ToLower(f.Column)] = struct{}{}
		}
	}
	out := make(map[string]any, len(values))
	for k, v := range values {
		if _, ok := excluded[strings.ToLower(k)]; ok || sensitiveFieldName(k) {
			out[k] = redactedPlaceholder
			continue
		}
		out[k] = v
	}
	return out
}
