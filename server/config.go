package server

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"github.com/jcsvwinston/orbit/server/alerts"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Config tunes the admin server. Two listeners are exposed: one for
// agents (shared token, optionally over TLS or mutual TLS) and one for
// UI/operators (trusted-proxy headers or bearer fallback).
type Config struct {
	// AgentAddr is the [host]:port the AgentService listens on. Agents
	// dial here. Default ":9090".
	AgentAddr string

	// UIAddr is the [host]:port the ControlService and embedded UI listen
	// on. The web browser hits this address, optionally fronted by an
	// auth-aware reverse proxy (oauth2-proxy, nginx auth_request,
	// traefik forward-auth) per decision 14. Default ":8080".
	UIAddr string

	// AgentTLS configures TLS for the agent listener: Run wraps the
	// listener with tls.NewListener and negotiates HTTP/2 via ALPN. When
	// nil the listener serves h2c (plaintext HTTP/2).
	//
	// A certificate alone encrypts the wire; it authenticates nobody. Only
	// a config that requires and verifies client certificates (ClientCAs
	// set and ClientAuth == tls.RequireAndVerifyClientCert — what the
	// binary's --agent-client-ca produces) counts as agent authentication
	// for the fail-closed guard in Run; otherwise set AgentToken too.
	AgentTLS *tls.Config

	// UITLS configures TLS for the UI listener. When nil the listener
	// serves plain HTTP and relies on a TLS-terminating reverse proxy.
	UITLS *tls.Config

	// AgentToken is the shared bearer token agents present. Empty
	// disables token auth (rely on mutual TLS via AgentTLS, or on the
	// listener being on a private network).
	AgentToken string

	// AgentIdentityFromCertificate binds a node's identity to the client
	// certificate it presented: a registration whose node_id differs from
	// the verified certificate's Common Name is refused with
	// PermissionDenied, so an agent holding a certificate for node-a
	// cannot register as node-b. It requires AgentTLS to require and
	// verify client certificates; Run refuses to start otherwise, because
	// a binding to an identity nobody verified binds to nothing.
	//
	// Off by default: the server verifies the certificate and registers
	// the node_id the agent declares, logging a WARN when the two
	// disagree. Deployments whose certificates name something other than
	// the node (one certificate shared by a fleet) keep working; the
	// default flips in the next major.
	AgentIdentityFromCertificate bool

	// InsecureAgentListener overrides the fail-closed guard that refuses
	// to start the agent listener on a non-loopback interface when it has
	// no authentication (AgentToken == "" and AgentTLS does not require a
	// verified client certificate). Leave false
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

	// UITenantHeader is the trusted-proxy header that carries the tenant
	// the operator is scoped to (default "X-Auth-Tenant"). Honoured only on
	// the same trusted-proxy path as UIAuthHeader. The value travels to the
	// agent with the operator identity on every Data Studio request
	// (ADR-002); an application whose model declares a tenant column then
	// confines that operator to it, as the in-process panel would.
	UITenantHeader string

	// UIRoleHeader is the trusted-proxy header that carries the operator's
	// role (default "X-Auth-Role"). Honoured only on the same trusted-proxy
	// path as UIAuthHeader. Value "viewer" (or "readonly"/"read-only")
	// makes the operator read-only: Data Studio mutations are refused with
	// PermissionDenied. Any other value — including absent — keeps the
	// operator read-write, preserving existing deployments.
	UIRoleHeader string

	// UIInsecureOpen authenticates credential-less UI requests arriving
	// from loopback as the fixed operator "insecure-open". It exists for
	// local development: the embedded SPA cannot present a bearer token,
	// so without a header-setting reverse proxy a browser could never
	// load the UI at all. Fail-closed: Run refuses to start when this is
	// set and UIAddr is not provably loopback (e.g. ":8080" binds every
	// interface), and a WARN is logged on boot. Never set it in any
	// shared or production deployment. Data Studio mutations remain
	// gated by DataStudioAllowedModels and UIReadOnly.
	UIInsecureOpen bool

	// DataStudioAllowedModels is the allowlist of model names Data Studio
	// mutations (create/update/delete/bulk) may touch, matched
	// case-insensitively against the model name the agents register.
	// Deny-by-default: when the list is empty, EVERY Data Studio mutation
	// is refused with PermissionDenied — the fleet plane executes
	// mutations on the agent's database without the application's
	// per-model RBAC or tenant filtering, so writes must be an explicit
	// operator decision. The single entry "*" allows mutations on every
	// model. Reads are never gated by this list.
	DataStudioAllowedModels []string

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

	// DataDir, when non-empty, is where the server keeps what it retains
	// (ADR-013): the events it replays, the fleet audit trail and the
	// host-metrics samples per node, in one SQLite file (store.FileName)
	// that survives a restart. Empty (the default) keeps everything in
	// bounded memory as before, and a restart starts empty.
	DataDir string

	// Retention is how long the store keeps a row. Reads never return a
	// row older than it, and a janitor deletes those rows. Only meaningful
	// with DataDir. Default 7 days; negative keeps everything.
	Retention time.Duration

	// MetricsAddr, when non-empty, runs a third HTTP listener on this
	// address serving Prometheus /metrics — the Go runtime's collectors
	// and the server's own (admin_server_*: nodes, frames, events,
	// alerts) — plus /healthz. Empty (the default) disables the listener;
	// metrics are strictly opt-in.
	MetricsAddr string

	// ServerID names this server to its peers (ADR-014). Default: the
	// host name, with a random suffix when the host name is empty.
	ServerID string

	// AgentAdvertiseAddr is the endpoint agents reach THIS server on
	// (http(s)://host:port of the agent listener), as it appears in the
	// agents' endpoint lists. Peers learn it, and a node assigned here is
	// redirected to it. Empty means this server receives no assignments.
	AgentAdvertiseAddr string

	// PeerAddrs are the agent-listener endpoints of the other admin
	// servers of the fleet (http(s)://host:port). The server keeps one
	// stream to each and pushes its nodes, events and host metrics down
	// it; each peer does the same towards this one. Empty: a single
	// server, as before.
	PeerAddrs []string

	// PeerTLS is the client TLS configuration for https:// peers (root CAs,
	// a client certificate when the peers' listeners verify one). Nil
	// uses the system trust store.
	PeerTLS *tls.Config

	// PeerToken is the bearer this server presents to its peers' agent
	// listeners. Empty uses AgentToken: peers usually share one.
	PeerToken string

	// AssignNodes makes the fleet assign each node to one server by
	// rendezvous hashing over this server and the peers it reaches, and
	// redirect an agent that registered elsewhere to its owner. Off, an
	// agent stays wherever it connected.
	AssignNodes bool

	// AlertRules are the threshold rules the server evaluates against the
	// host metrics every heartbeat carries (server/alerts): a metric, an
	// operator, a threshold, how long it must hold, which nodes, which
	// channels. The binary reads them from --alert-rules-file. Empty
	// evaluates nothing.
	AlertRules []alerts.Rule

	// AlertWebhooks are webhook channels by name: rules refer to the name,
	// the server POSTs the alert as JSON to the URL.
	AlertWebhooks map[string]string

	// AlertSMTP configures the e-mail channel (named "email" unless the
	// config names it). Nil configures none.
	AlertSMTP *alerts.SMTPConfig

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
	if c.Retention == 0 {
		c.Retention = 7 * 24 * time.Hour
	}
	if strings.TrimSpace(c.ServerID) == "" {
		c.ServerID = defaultServerID()
	}
	if strings.TrimSpace(c.PeerToken) == "" {
		c.PeerToken = c.AgentToken
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

// defaultServerID is the host name, or a random name when there is none.
func defaultServerID() string {
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		return strings.TrimSpace(h)
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "server-" + hex.EncodeToString(b[:])
}
