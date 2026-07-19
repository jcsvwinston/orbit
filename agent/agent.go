package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observability"

	"github.com/jcsvwinston/orbit/agent/buffer"
	"github.com/jcsvwinston/orbit/agent/connection"
	dstudio "github.com/jcsvwinston/orbit/agent/datastudio"
	"github.com/jcsvwinston/orbit/agent/hostmetrics"
	"github.com/jcsvwinston/orbit/agent/identity"
	"github.com/jcsvwinston/orbit/agent/metrics"
	"github.com/jcsvwinston/orbit/agent/rbac"
	"github.com/jcsvwinston/orbit/agent/stream"
)

// Config bundles every dependency the agent needs. The framework's
// app.New constructs this from nucleus.yml plus the in-process bus and
// passes it to New.
type Config struct {
	// Endpoints is the ordered list of admin server URLs (admin.endpoints
	// in nucleus.yml). At least one is required for the agent to start;
	// passing an empty list returns ErrDisabled from New.
	Endpoints []string

	// DB, when non-nil, lets the heartbeat report the framework database
	// pool stats (in-use / idle / max) alongside the host metrics sample.
	DB *sql.DB

	// Token is the shared bearer token sent on every Connect-RPC call.
	// May be empty if mTLS is in use (Phase 6).
	Token string

	// StateDir is the path under which node_id is persisted (the new
	// top-level state_dir key in nucleus.yml). Empty means "use ephemeral
	// NodeID with WARN", consistent with decision 15.
	StateDir string

	// NodeIDOverride pins the NodeID instead of resolving from StateDir.
	// Empty means resolve via identity.Resolver.
	NodeIDOverride string

	// Version, Labels, StartedAt are forwarded to NodeRegistration.
	Version   string
	Labels    map[string]string
	StartedAt time.Time

	// Bus is the in-process observability bus the agent subscribes to. If
	// nil, the agent runs but no events flow.
	Bus *observability.Bus

	// HeartbeatInterval is the cadence between Heartbeat frames sent to
	// the admin server. Default 10s.
	HeartbeatInterval time.Duration

	// DrainTimeout is the maximum time spent flushing the ring buffer to
	// the stream during graceful shutdown. Default 2s.
	DrainTimeout time.Duration

	// HTTPBufferSize / SQLBufferSize / SessionBufferSize / CustomBufferSize
	// configure the per-kind drop-oldest ring buffers used during
	// microcortes (default 256 each).
	HTTPBufferSize    int
	SQLBufferSize     int
	SessionBufferSize int
	CustomBufferSize  int

	// MetricsAddr, when non-empty, starts a /metrics + /healthz HTTP
	// server on this address.
	MetricsAddr string

	// Registry is the framework's model registry. Required for Data
	// Studio support; nil disables the Data Studio path on this agent.
	Registry *model.Registry

	// Databases are the framework's DB handles keyed by alias. The
	// agent's Data Studio handler uses them to execute model.CRUD
	// operations on behalf of UI requests routed through the admin
	// server. Empty disables the Data Studio path.
	Databases map[string]*db.DB

	// Authorizer is a read-only view of the framework's RBAC state (the
	// *authz.Enforcer satisfies it). Required for the Access control
	// screen of the fleet UI; nil disables the RBAC snapshot path on
	// this agent.
	Authorizer rbac.PolicySource

	// DefaultDatabaseAlias is the alias used when a Data Studio request
	// arrives with an empty database_alias. Falls back to "default" if
	// unset.
	DefaultDatabaseAlias string

	// Logger receives WARN/INFO/DEBUG diagnostics. Pass nil for
	// slog.Default.
	Logger *slog.Logger
}

func (c Config) withDefaults() Config {
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 10 * time.Second
	}
	if c.DrainTimeout <= 0 {
		c.DrainTimeout = 2 * time.Second
	}
	if c.HTTPBufferSize <= 0 {
		c.HTTPBufferSize = 256
	}
	if c.SQLBufferSize <= 0 {
		c.SQLBufferSize = 256
	}
	if c.SessionBufferSize <= 0 {
		c.SessionBufferSize = 64
	}
	if c.CustomBufferSize <= 0 {
		c.CustomBufferSize = 64
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.StartedAt.IsZero() {
		c.StartedAt = time.Now().UTC()
	}
	return c
}

// ErrDisabled is returned by New when no admin endpoints are configured.
// Callers should treat this as "the agent is disabled" rather than as an
// error; it lets fail-open wiring in pkg/app skip the agent without
// noise.
var ErrDisabled = errors.New("admin agent: no endpoints configured (disabled)")

// Agent is the long-lived top-level type. Its Run method blocks until
// ctx is cancelled and returns nil on graceful shutdown.
type Agent struct {
	cfg    Config
	nodeID string

	bufs       *buffer.PerKind
	metrics    *metrics.Metrics
	dialer     *connection.Dialer
	dataStudio *dstudio.Handler
	rbac       *rbac.Handler

	// connectedOnce is closed the first time an admin server ACCEPTS a
	// stream from this agent: the first frame received from the server
	// under auth (stream.Config.OnAccepted), not merely a successful
	// Dial. Dial success only proves the auth-exempt /healthz probe
	// answered; closing here on Dial made require_connection pass with a
	// rejected token (OR6-1). Used by Extension wrappers that implement
	// require_connection (fail boot if no admin accepts a stream within
	// a timeout). Subsequent disconnects/reconnects do NOT re-open it.
	connectedOnce     chan struct{}
	connectedOnceOnce sync.Once

	// authWarnLast rate-limits the "token rejected" WARN to one per
	// authWarnEvery per endpoint. A rejected token retries on the dial
	// backoff cadence; without the limiter the WARN would repeat on
	// every cycle. Guarded by authWarnMu.
	authWarnMu   sync.Mutex
	authWarnLast map[string]time.Time

	mu     sync.Mutex
	closed bool
}

// authWarnEvery is the minimum interval between "agent token rejected"
// WARNs per endpoint.
const authWarnEvery = time.Minute

// New constructs an Agent. Returns ErrDisabled when no endpoints are
// configured.
func New(cfg Config) (*Agent, error) {
	cfg = cfg.withDefaults()
	if len(cfg.Endpoints) == 0 {
		return nil, ErrDisabled
	}

	nodeID := cfg.NodeIDOverride
	if nodeID == "" {
		resolver := identity.New(cfg.StateDir, cfg.Logger)
		resolved := resolver.Resolve()
		nodeID = resolved.NodeID
		cfg.Logger.Info("admin agent NodeID resolved",
			"node_id", nodeID,
			"persistent", resolved.Persistent,
			"source", resolved.Source)
	}

	bufs := buffer.NewPerKind(map[observability.EventKind]int{
		observability.KindHTTPRequest:   cfg.HTTPBufferSize,
		observability.KindSQLStatement:  cfg.SQLBufferSize,
		observability.KindSessionChange: cfg.SessionBufferSize,
		observability.KindCustom:        cfg.CustomBufferSize,
	})

	m := metrics.New()

	dialer := connection.NewDialer(connection.Config{
		Endpoints: cfg.Endpoints,
		Token:     cfg.Token,
		Logger:    cfg.Logger,
	})

	dataStudio := dstudio.New(dstudio.Config{
		Registry:     cfg.Registry,
		Databases:    cfg.Databases,
		DefaultAlias: cfg.DefaultDatabaseAlias,
	})

	return &Agent{
		cfg:           cfg,
		nodeID:        nodeID,
		bufs:          bufs,
		metrics:       m,
		dialer:        dialer,
		dataStudio:    dataStudio,
		rbac:          rbac.New(cfg.Authorizer),
		connectedOnce: make(chan struct{}),
		authWarnLast:  make(map[string]time.Time),
	}, nil
}

// NodeID returns the resolved node identifier.
func (a *Agent) NodeID() string {
	if a == nil {
		return ""
	}
	return a.nodeID
}

// Metrics returns the agent's Prometheus metrics. Useful when the host
// app wants to expose them through its own /metrics rather than via the
// agent's stand-alone metrics server.
func (a *Agent) Metrics() *metrics.Metrics {
	if a == nil {
		return nil
	}
	return a.metrics
}

// Connected returns a channel that is closed the first time an admin
// server accepts a stream from this agent — the first frame received
// from the server under auth (stream.Config.OnAccepted) — NOT on the
// first successful dial: the dial's /healthz probe is auth-exempt, so
// reachability proves nothing about the token (OR6-1). Subsequent
// disconnects/reconnects do NOT re-open the channel.
//
// It is the integration point for the require_connection path: callers
// that need the framework to fail boot when no admin accepts the
// stream select on this channel against a timeout, and abort if the
// timeout fires first.
func (a *Agent) Connected() <-chan struct{} {
	if a == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return a.connectedOnce
}

// Run drives the agent until ctx is cancelled. It blocks. On reconnect
// errors it sleeps according to the dialer backoff and retries. Returns
// nil on graceful shutdown.
func (a *Agent) Run(ctx context.Context) error {
	if a == nil {
		return errors.New("admin agent: nil agent")
	}

	// Optional metrics server.
	metricsSrv := metrics.NewServer(a.cfg.MetricsAddr, a.metrics, a.cfg.Logger)
	metricsErr := make(chan error, 1)
	go func() { metricsErr <- metricsSrv.Run(ctx) }()

	// Periodic ring-buffer gauge update.
	gaugeStop := make(chan struct{})
	go a.updateBufferGauges(ctx, gaugeStop)

	for {
		if err := ctx.Err(); err != nil {
			close(gaugeStop)
			<-metricsErr
			return nil
		}
		if err := a.runOnce(ctx); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				close(gaugeStop)
				<-metricsErr
				return nil
			}
			a.cfg.Logger.Debug("admin agent stream cycle ended", "error", err.Error())
			a.metrics.ReconnectsTotal.Inc()
			// Backoff before the next reconnect attempt.
			sleep := a.dialer.Backoff()
			select {
			case <-ctx.Done():
				close(gaugeStop)
				<-metricsErr
				return nil
			case <-time.After(sleep):
			}
			continue
		}
		// runOnce returned nil → ctx cancelled.
		close(gaugeStop)
		<-metricsErr
		return nil
	}
}

// runOnce performs one connect → register → run-stream cycle.
func (a *Agent) runOnce(ctx context.Context) error {
	res, err := a.dialer.Dial(ctx)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	// Teardown only: the gauge goes to 1 in OnAccepted below. Setting it
	// to 1 here, on Dial, reported "connected" while the token was being
	// rejected — /healthz is auth-exempt, so reachability proves nothing
	// about auth (OR6-1).
	defer a.metrics.Connected.WithLabelValues(res.Endpoint).Set(0)

	// Dial success only proves the endpoint answered the auth-exempt
	// /healthz probe. Announcing "connected" here lied when the token
	// was being rejected (OR5-2); the honest INFO moves to OnAccepted
	// below, once the server has demonstrably accepted the stream.
	a.cfg.Logger.Debug("admin agent endpoint reachable; opening stream",
		"endpoint", res.Endpoint, "node_id", a.nodeID)

	streamCfg := stream.Config{
		NodeID:       a.nodeID,
		Version:      a.cfg.Version,
		Labels:       a.cfg.Labels,
		StartedAt:    a.cfg.StartedAt,
		Bus:          a.cfg.Bus,
		Buffer:       a.bufs,
		Metrics:      a.metrics,
		Logger:       a.cfg.Logger,
		Heartbeat:    a.cfg.HeartbeatInterval,
		DrainTimeout: a.cfg.DrainTimeout,
		Host:         hostmetrics.New(a.cfg.DB),
		// First frame received from the server == stream accepted. Only
		// now is the connection known-good: reset the dial backoff, log
		// the "connected" INFO, flip the Connected gauge, and close the
		// Connected() channel (the require_connection guard). Doing any
		// of these earlier (at Dial time) treated the auth-exempt
		// /healthz probe as proof of a working connection (OR5-2/OR6-1).
		OnAccepted: func() {
			a.dialer.ResetBackoff()
			a.metrics.Connected.WithLabelValues(res.Endpoint).Set(1)
			a.connectedOnceOnce.Do(func() { close(a.connectedOnce) })
			a.cfg.Logger.Info("admin agent connected",
				"endpoint", res.Endpoint, "node_id", a.nodeID)
		},
	}
	// Avoid the typed-nil-into-interface trap: only set the fields when
	// we actually have constructed handlers.
	if a.dataStudio != nil {
		streamCfg.DataStudio = a.dataStudio
	}
	if a.rbac != nil {
		streamCfg.Rbac = a.rbac
	}
	st := stream.New(res.Client, streamCfg)

	// streamLifeCtx is intentionally NOT a child of ctx. It is cancelled
	// only after the agent has had a chance to flush a Goodbye frame and
	// drain the send buffer. Tying it directly to ctx would propagate the
	// parent cancel into the underlying HTTP/2 stream and abort any
	// in-flight Send (including the Goodbye itself).
	streamLifeCtx, streamLifeCancel := context.WithCancel(context.Background())
	defer streamLifeCancel()

	streamDone := make(chan error, 1)
	go func() { streamDone <- st.Run(streamLifeCtx) }()

	select {
	case <-ctx.Done():
		// Graceful shutdown:
		//   1. queue the Goodbye (sendLoop is still alive)
		//   2. wait DrainTimeout for the stream to flush cleanly
		//   3. force-cancel and wait once more so we never block forever.
		st.Goodbye("agent shutting down")

		select {
		case <-streamDone:
		case <-time.After(a.cfg.DrainTimeout):
			streamLifeCancel()
			select {
			case <-streamDone:
			case <-time.After(a.cfg.DrainTimeout):
				a.cfg.Logger.Warn("admin agent stream did not finish drain within deadline; abandoning",
					"drain_timeout", a.cfg.DrainTimeout)
			}
		}
		return ctx.Err()
	case err := <-streamDone:
		if connect.CodeOf(err) == connect.CodeUnauthenticated {
			a.warnTokenRejected(res.Endpoint)
		}
		return err
	}
}

// warnTokenRejected emits the operator-visible WARN for a 401 on the
// bidi stream, at most once per authWarnEvery per endpoint. Without it
// a bad --agent-token failed in complete silence: the auth-exempt
// /healthz probe kept "connecting" and the stream's 401 only surfaced
// at Debug level (OR5-2).
func (a *Agent) warnTokenRejected(endpoint string) {
	a.authWarnMu.Lock()
	now := time.Now()
	if last, ok := a.authWarnLast[endpoint]; ok && now.Sub(last) < authWarnEvery {
		a.authWarnMu.Unlock()
		return
	}
	a.authWarnLast[endpoint] = now
	a.authWarnMu.Unlock()

	a.cfg.Logger.Warn("admin agent token rejected by admin server; check --agent-token",
		"endpoint", endpoint, "node_id", a.nodeID)
}

func (a *Agent) updateBufferGauges(ctx context.Context, stop <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			snap := a.bufs.LenSnapshot()
			for kind, n := range snap {
				a.metrics.BufferSize.WithLabelValues(kind.String()).Set(float64(n))
			}
		}
	}
}
