// Package auth holds the two thin auth surfaces of the admin server:
//
//   - Agent: shared bearer token (today) or mTLS (Phase 6). Validated on
//     every Connect-RPC call from an agent.
//   - UI: trusted-proxy header pass-through (X-Auth-User / X-Auth-Email)
//     with optional bearer fallback. Per decision 14, the canonical
//     deployment runs oauth2-proxy or equivalent in front of the UI
//     listener; the server does NOT implement OIDC itself.
package auth

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// Identity is the minimal set of facts the rest of the server may rely
// on after a request has been authenticated. Used for observability,
// audit attribution, and the read-only gate on Data Studio mutations.
type Identity struct {
	Subject string // "agent:<NodeID>" for agents, the UI user for UI
	Email   string // empty for agents, optional for UI
	Role    string // "agent" | "ui-operator"

	// ReadOnly marks an operator whose mutations must be refused
	// (Data Studio create/update/delete/bulk). Set from the trusted
	// proxy's role header or forced globally via UIConfig.ForceReadOnly.
	ReadOnly bool
}

// AgentMiddleware returns an http middleware that enforces shared-token
// auth on the agent listener. When token is empty the middleware is a
// pass-through (the listener is presumed bound to a private network or
// using mTLS at the listener layer).
//
// logger receives a rate-limited WARN (one per minute per remote IP,
// with a count of the 401s suppressed in between) every time a request
// is rejected, so an operator can see in the server log that agents
// with a bad token are calling (OR5-2). Pass nil for slog.Default.
func AgentMiddleware(token string, logger *slog.Logger) func(http.Handler) http.Handler {
	expected := strings.TrimSpace(token)
	if logger == nil {
		logger = slog.Default()
	}
	limiter := newFailureLimiter()
	warns := newWarnLimiter()
	return func(next http.Handler) http.Handler {
		if expected == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := remoteIPString(r)
			if limiter.blocked(ip) {
				http.Error(w, "too many failed attempts", http.StatusTooManyRequests)
				return
			}
			got := bearerFromHeader(r)
			if subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
				// Only presented-and-wrong credentials count toward the
				// lockout: a request with no bearer at all is not a
				// brute-force attempt.
				if got != "" {
					limiter.fail(ip)
				}
				if suppressed, ok := warns.allow(ip); ok {
					logger.Warn("admin server rejected agent request: invalid or missing bearer token; check the agent's --agent-token",
						"remote_ip", ip,
						"token_presented", got != "",
						"suppressed_since_last_warn", suppressed)
				}
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UIConfig groups the UI-listener auth knobs.
type UIConfig struct {
	// BearerToken, when non-empty, lets a request authenticate by sending
	// "Authorization: Bearer <BearerToken>". The token is checked AFTER
	// trusted-proxy header pass-through.
	BearerToken string

	// AuthHeader and EmailHeader name the headers the trusted reverse
	// proxy uses to forward identity (X-Auth-User / X-Auth-Email by
	// default, configurable per Config.UIAuthHeader / UIEmailHeader).
	AuthHeader  string
	EmailHeader string

	// TrustedCIDRs is the allowlist of remote addresses whose
	// trusted-proxy headers are honoured. Empty = "127.0.0.1/32, ::1/128".
	TrustedCIDRs []string

	// ProxySecret, when non-empty, is a shared secret the trusted proxy
	// must echo in the ProxySecretHeader ("X-Auth-Proxy-Secret") for its
	// forwarded identity to be honoured. CIDR membership alone is then no
	// longer sufficient — it defends against any co-located process that
	// can source packets from a trusted CIDR but does not know the secret.
	// Empty keeps the CIDR-only behaviour.
	ProxySecret string

	// RoleHeader names the trusted-proxy header carrying the operator's
	// role (default "X-Auth-Role"). Honoured only on the trusted-proxy
	// path, together with AuthHeader. Value "viewer" / "readonly" /
	// "read-only" (case-insensitive) marks the identity read-only; any
	// other value — including absent — keeps the operator read-write.
	RoleHeader string

	// ForceReadOnly marks EVERY authenticated UI identity read-only,
	// regardless of role header or credential mode. See
	// Config.UIReadOnly.
	ForceReadOnly bool
}

// ProxySecretHeader is the header the trusted proxy uses to present
// UIConfig.ProxySecret. Fixed (not configurable) to keep the contract
// between proxy and server unambiguous.
const ProxySecretHeader = "X-Auth-Proxy-Secret"

// UIMiddleware authenticates UI requests. It returns a generic 401 on
// failure rather than leaking which credential mode was attempted.
func UIMiddleware(cfg UIConfig) func(http.Handler) http.Handler {
	trusted := parseCIDRs(cfg.TrustedCIDRs)
	authHeader := strings.TrimSpace(cfg.AuthHeader)
	if authHeader == "" {
		authHeader = "X-Auth-User"
	}
	emailHeader := strings.TrimSpace(cfg.EmailHeader)
	if emailHeader == "" {
		emailHeader = "X-Auth-Email"
	}
	bearer := strings.TrimSpace(cfg.BearerToken)
	proxySecret := strings.TrimSpace(cfg.ProxySecret)
	roleHeader := strings.TrimSpace(cfg.RoleHeader)
	if roleHeader == "" {
		roleHeader = "X-Auth-Role"
	}
	limiter := newFailureLimiter()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := remoteIPString(r)
			if limiter.blocked(ip) {
				http.Error(w, "too many failed attempts", http.StatusTooManyRequests)
				return
			}
			// 1) trusted-proxy header path. When a proxy secret is
			// configured the header identity is honoured only if the
			// request also carries the matching secret — CIDR membership
			// alone is not enough. A trusted-CIDR request that fails the
			// secret check falls through to the bearer path rather than
			// short-circuiting, so a bad secret never blocks a valid bearer.
			if from := remoteIP(r); from != nil && cidrsContain(trusted, from) && proxySecretOK(r, proxySecret) {
				if user := strings.TrimSpace(r.Header.Get(authHeader)); user != "" {
					id := Identity{
						Subject:  user,
						Email:    strings.TrimSpace(r.Header.Get(emailHeader)),
						Role:     "ui-operator",
						ReadOnly: cfg.ForceReadOnly || readOnlyRole(r.Header.Get(roleHeader)),
					}
					next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
					return
				}
			}
			// 2) bearer fallback
			if bearer != "" {
				got := bearerFromHeader(r)
				if subtle.ConstantTimeCompare([]byte(got), []byte(bearer)) == 1 {
					id := Identity{Subject: "ui-bearer", Role: "ui-operator", ReadOnly: cfg.ForceReadOnly}
					next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
					return
				}
				// A presented-and-wrong bearer (or a wrong proxy secret
				// alongside it) counts toward the per-IP lockout;
				// credential-less requests do not.
				if got != "" {
					limiter.fail(ip)
				}
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	}
}

// readOnlyRole maps a trusted-proxy role header value onto the read-only
// flag. Unknown values default to read-write so existing deployments
// (which send no role header) keep today's behaviour.
func readOnlyRole(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "viewer", "readonly", "read-only":
		return true
	default:
		return false
	}
}

// IdentityFromRequest extracts the configured identity. Returns
// ("", "") for the agent path; UIs may have non-empty values.
func IdentityFromRequest(r *http.Request, cfg UIConfig) Identity {
	if r == nil {
		return Identity{Role: "unknown"}
	}
	user := strings.TrimSpace(r.Header.Get(cfg.AuthHeader))
	if user != "" {
		return Identity{
			Subject: user,
			Email:   strings.TrimSpace(r.Header.Get(cfg.EmailHeader)),
			Role:    "ui-operator",
		}
	}
	if strings.TrimSpace(bearerFromHeader(r)) != "" {
		return Identity{Subject: "ui-bearer", Role: "ui-operator"}
	}
	return Identity{Role: "ui-anonymous"}
}

// ErrTrustedProxyMisconfigured is returned by parseCIDRs when an entry
// is malformed and the caller asked to fail-fast.
var ErrTrustedProxyMisconfigured = errors.New("admin server: malformed trusted_cidrs entry")

func parseCIDRs(cidrs []string) []*net.IPNet {
	if len(cidrs) == 0 {
		// Defaults: localhost only.
		_, v4loop, _ := net.ParseCIDR("127.0.0.1/32")
		_, v6loop, _ := net.ParseCIDR("::1/128")
		return []*net.IPNet{v4loop, v6loop}
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		_, network, err := net.ParseCIDR(raw)
		if err != nil {
			continue
		}
		out = append(out, network)
	}
	return out
}

func cidrsContain(networks []*net.IPNet, ip net.IP) bool {
	for _, n := range networks {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func remoteIP(r *http.Request) net.IP {
	if r == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(strings.TrimSpace(host))
}

// proxySecretOK reports whether the request satisfies the shared
// proxy-secret requirement. When expected is empty the check is disabled
// (returns true); otherwise the request must present the exact secret in
// ProxySecretHeader, compared in constant time.
func proxySecretOK(r *http.Request, expected string) bool {
	if expected == "" {
		return true
	}
	if r == nil {
		return false
	}
	got := strings.TrimSpace(r.Header.Get(ProxySecretHeader))
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func bearerFromHeader(r *http.Request) string {
	if r == nil {
		return ""
	}
	v := strings.TrimSpace(r.Header.Get("Authorization"))
	if v == "" {
		return ""
	}
	parts := strings.SplitN(v, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
