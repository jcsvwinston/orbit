package agent

import (
	"crypto/tls"
	"time"
)

// ExtensionConfig is the framework-facing configuration for the admin
// observability agent, consumed by NewExtension and mapped into the agent's
// internal Config. It used to live in pkg/app as app.AdminAgentConfig (bound
// from the application config's `admin:` subtree), but moved here when the admin
// panel was extracted from the framework core (nucleus ADR-019): the framework
// no longer carries admin-specific configuration, so the agent owns its own
// config type. Callers populate it directly (e.g. from their own config file)
// and pass it to NewExtension.
type ExtensionConfig struct {
	// Endpoints is the ordered list of admin server URLs the agent will
	// try to connect to. Each URL may be http:// (h2c, dev), https://
	// (production), or any other Connect-RPC compatible scheme. Failover
	// happens left-to-right; once every endpoint has failed, the agent
	// enters exponential backoff (cap 30s).
	Endpoints []string `koanf:"endpoints"`

	// Token is the shared bearer token sent on every Connect-RPC call.
	// In production pair it with an https:// endpoint (or let the server
	// authenticate the agent by client certificate via TLS); in dev a
	// plain token over http:// suffices.
	Token string `koanf:"token"`

	// TLS is applied to every https:// endpoint. Nil uses the system
	// trust store. Set RootCAs for a server signed by a private CA and
	// Certificates to present a client certificate when the admin
	// server's agent listener requires one (--agent-client-ca). It is
	// not bound from a config file (koanf:"-"): build it in code from
	// the PEM files your deployment ships.
	TLS *tls.Config `koanf:"-"`

	// HeartbeatInterval defines the cadence of Heartbeat frames the agent
	// sends to the server. Default 10s.
	HeartbeatInterval time.Duration `koanf:"heartbeat_interval"`

	// DrainTimeout caps the time the agent spends flushing buffered
	// events to the stream during graceful shutdown. Default 2s.
	DrainTimeout time.Duration `koanf:"drain_timeout"`

	// MetricsAddr, when non-empty, runs a Prometheus /metrics + /healthz
	// HTTP server on this address. Format: "[host]:port", e.g.
	// "127.0.0.1:9101". Empty disables the standalone server.
	MetricsAddr string `koanf:"metrics_addr"`

	// HTTPBufferSize, SQLBufferSize, SessionBufferSize, CustomBufferSize
	// configure the per-event-kind drop-oldest ring buffer that absorbs
	// bursts while the stream is open (events queue when the stream's
	// send path is busy and drain when it catches up). Events that occur
	// while the agent has no stream at all are dropped, not buffered.
	// Defaults: 256, 256, 64, 64.
	HTTPBufferSize    int `koanf:"http_buffer_size"`
	SQLBufferSize     int `koanf:"sql_buffer_size"`
	SessionBufferSize int `koanf:"session_buffer_size"`
	CustomBufferSize  int `koanf:"custom_buffer_size"`

	// NodeIDOverride pins the NodeID the agent reports in
	// NodeRegistration. Empty means "resolve from
	// ${state_dir}/node_id" (UUIDv4 persisted at first run).
	NodeIDOverride string `koanf:"node_id"`

	// Labels are arbitrary key/value pairs forwarded with NodeRegistration
	// and shown in the admin UI's node topology view.
	Labels map[string]string `koanf:"labels"`

	// DefaultDatabaseAlias is the alias the agent's Data Studio handler
	// uses when a request arrives with an empty database_alias. Falls
	// back to "default" if unset.
	DefaultDatabaseAlias string `koanf:"default_database_alias"`

	// RequireConnection, when true, makes the framework fail to boot if
	// the agent does not establish a stream to any admin endpoint within
	// RequireConnectionTimeout. Default: false (fail-open). Operators in
	// compliance-sensitive environments can set this to true so that the
	// application refuses to serve traffic when its observability lifeline
	// is missing.
	RequireConnection bool `koanf:"require_connection"`

	// RequireConnectionTimeout caps the wait when RequireConnection is
	// true. Default 10s. Ignored when RequireConnection is false.
	RequireConnectionTimeout time.Duration `koanf:"require_connection_timeout"`
}
