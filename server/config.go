package server

import (
	"crypto/tls"
	"log/slog"
	"strings"
	"time"
)

// Config tunes the admin server. Two listeners are exposed: one for
// agents (mTLS or shared token) and one for UI/operators (trusted-proxy
// headers or bearer fallback). Empty addresses disable a listener.
type Config struct {
	// AgentAddr is the [host]:port the AgentService listens on. Agents
	// dial here. Default ":9090".
	AgentAddr string

	// UIAddr is the [host]:port the ControlService and embedded UI listen
	// on. The web browser hits this address, optionally fronted by an
	// auth-aware reverse proxy (oauth2-proxy, nginx auth_request,
	// traefik forward-auth) per decision 14. Default ":8080".
	UIAddr string

	// AgentTLS configures mTLS for the agent listener. When nil the
	// listener serves h2c (plaintext HTTP/2). Production deployments
	// MUST configure this.
	AgentTLS *tls.Config

	// UITLS configures TLS for the UI listener. When nil the listener
	// serves plain HTTP and relies on a TLS-terminating reverse proxy.
	UITLS *tls.Config

	// AgentToken is the shared bearer token agents present. Empty
	// disables token auth (rely on mTLS or the listener being on a
	// private network).
	AgentToken string

	// InsecureAgentListener overrides the fail-closed guard that refuses
	// to start the agent listener on a non-loopback interface when it has
	// no authentication (AgentToken == "" && AgentTLS == nil). Leave false
	// in production; set it only when a network-layer control (private
	// subnet, service mesh mTLS, firewall) already restricts who can reach
	// AgentAddr. See Run for the exact condition.
	InsecureAgentListener bool

	// UIBearerToken is the optional fallback token for direct UI access
	// without a reverse proxy. Empty disables this fallback.
	UIBearerToken string

	// UIAuthHeader is the trusted-proxy header that carries the
	// authenticated user identity (default "X-Auth-User"). The server
	// trusts this header only when the connection arrives from
	// UITrustedProxyCIDRs.
	UIAuthHeader string

	// UIEmailHeader is the optional email header (default "X-Auth-Email").
	UIEmailHeader string

	// UITrustedProxyCIDRs is the list of CIDRs allowed to set
	// UIAuthHeader / UIEmailHeader. Empty means "trust 127.0.0.1/32 and
	// ::1/128 only". Configure your reverse proxy's network here.
	UITrustedProxyCIDRs []string

	// UIProxySecret, when non-empty, requires the trusted reverse proxy to
	// also present a shared secret in the "X-Auth-Proxy-Secret" header
	// before the server honours UIAuthHeader / UIEmailHeader. This closes
	// the gap where any process inside a trusted CIDR (a sidecar, a
	// host-networked container, another local process) could forge an
	// operator identity with just the CIDR membership. Empty preserves the
	// CIDR-only behaviour. See auth.UIMiddleware.
	UIProxySecret string

	// UIRoleHeader is the trusted-proxy header that carries the operator's
	// role (default "X-Auth-Role"). Honoured only on the same trusted-proxy
	// path as UIAuthHeader. Value "viewer" (or "readonly"/"read-only")
	// makes the operator read-only: Data Studio mutations are refused with
	// PermissionDenied. Any other value — including absent — keeps the
	// operator read-write, preserving existing deployments.
	UIRoleHeader string

	// UIReadOnly, when true, makes EVERY UI operator read-only regardless
	// of role header or bearer: the fleet UI can observe (streams, nodes,
	// Data Studio reads, RBAC/audit) but every Data Studio mutation is
	// refused. Use it to run the server as a pure observability plane.
	UIReadOnly bool

	// HTTPReplayBufferSize is the per-kind ring buffer capacity for
	// replaying recent events to a freshly opened UI panel. Default 256.
	HTTPReplayBufferSize    int
	SQLReplayBufferSize     int
	SessionReplayBufferSize int
	CustomReplayBufferSize  int

	// SnapshotTimeout caps how long the server waits for an agent to
	// answer a SnapshotRequest before returning an error to the UI.
	// Default 5s.
	SnapshotTimeout time.Duration

	// AgentInactivityTimeout marks a connected agent as "stale" if no
	// frame (event or heartbeat) arrives within this window. Default 45s
	// (3× the agent's default 10s heartbeat + buffer for jitter).
	AgentInactivityTimeout time.Duration

	// EventChannelSize is the per-UI-subscription buffered channel
	// capacity. Subscribers that fall behind by more than this many
	// events see overflow drops. Default 256.
	EventChannelSize int

	// MetricsAddr, when non-empty, runs a third HTTP listener on this
	// address serving Prometheus /metrics (the default registry: go_* and
	// process_* collectors; server-specific collectors are future work)
	// plus /healthz. Empty (the default) disables the listener — metrics
	// are strictly opt-in.
	MetricsAddr string

	// Logger receives diagnostics. Pass nil for slog.Default.
	Logger *slog.Logger
}

func (c Config) withDefaults() Config {
	if strings.TrimSpace(c.AgentAddr) == "" {
		c.AgentAddr = ":9090"
	}
	if strings.TrimSpace(c.UIAddr) == "" {
		c.UIAddr = ":8080"
	}
	if c.HTTPReplayBufferSize <= 0 {
		c.HTTPReplayBufferSize = 256
	}
	if c.SQLReplayBufferSize <= 0 {
		c.SQLReplayBufferSize = 256
	}
	if c.SessionReplayBufferSize <= 0 {
		c.SessionReplayBufferSize = 64
	}
	if c.CustomReplayBufferSize <= 0 {
		c.CustomReplayBufferSize = 64
	}
	if c.SnapshotTimeout <= 0 {
		c.SnapshotTimeout = 5 * time.Second
	}
	if c.AgentInactivityTimeout <= 0 {
		c.AgentInactivityTimeout = 45 * time.Second
	}
	if c.EventChannelSize <= 0 {
		c.EventChannelSize = 256
	}
	if strings.TrimSpace(c.UIAuthHeader) == "" {
		c.UIAuthHeader = "X-Auth-User"
	}
	if strings.TrimSpace(c.UIEmailHeader) == "" {
		c.UIEmailHeader = "X-Auth-Email"
	}
	if strings.TrimSpace(c.UIRoleHeader) == "" {
		c.UIRoleHeader = "X-Auth-Role"
	}
	// MetricsAddr deliberately gets NO default: empty means disabled. (It
	// used to be coerced to ":9091" while nothing consumed the field —
	// dead config whose godoc claimed a listener that never ran.)
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}
