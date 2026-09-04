// Package connection establishes and maintains the agent's transport to
// an admin server. It is responsible for endpoint resolution, dial-time
// failover across the configured endpoint list, and exponential backoff
// when every endpoint is unreachable.
//
// The package does NOT own the bidi stream itself; that lives in
// admin/agent/stream. connection.Dialer.Dial returns a connected
// AgentService client and the endpoint URL that succeeded; the stream
// layer owns the call.
package connection

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"

	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// Config controls how the dialer probes endpoints and times out.
type Config struct {
	// Endpoints is the ordered list of admin server URLs to try. Each
	// entry is a full URL — http://, https://, or h2c:// for unencrypted
	// HTTP/2. The dialer tries them in order on each attempt.
	Endpoints []string

	// Token, if non-empty, is sent on every Connect-RPC call as
	// "Authorization: Bearer <Token>". Used for the simplest auth mode
	// (decision 13: shared bearer token).
	Token string

	// TLSConfig is applied to every https:// endpoint (both the /healthz
	// probe and the Connect stream). Pass nil to use the system trust
	// store; set RootCAs for a private CA, or Certificates to present a
	// client certificate to a mutual-TLS agent listener.
	TLSConfig *tls.Config

	// HealthCheckTimeout caps each endpoint probe (an HTTP GET to /healthz
	// on the same origin). Default 3s.
	HealthCheckTimeout time.Duration

	// InitialBackoff is the first sleep after a complete failover round
	// fails. Default 1s.
	InitialBackoff time.Duration

	// MaxBackoff caps the exponential growth. Default 30s (decision 9).
	MaxBackoff time.Duration

	// BackoffJitter is multiplied by rand[0,1) and added to each backoff
	// to avoid thundering-herd. Default 0.5.
	BackoffJitter float64

	// Logger is used for the rate-limited disconnect WARN. Pass nil for
	// slog.Default.
	Logger *slog.Logger
}

func (c Config) withDefaults() Config {
	if c.HealthCheckTimeout <= 0 {
		c.HealthCheckTimeout = 3 * time.Second
	}
	if c.InitialBackoff <= 0 {
		c.InitialBackoff = time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 30 * time.Second
	}
	if c.BackoffJitter < 0 {
		c.BackoffJitter = 0
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// Dialer attempts to establish a connection to one of the configured
// endpoints. It is safe to construct once and call Dial repeatedly.
type Dialer struct {
	cfg Config

	// h2cClient is the HTTP/2 client used for http:// endpoints: an
	// http2.Transport that dials plain TCP (cleartext HTTP/2, h2c).
	h2cClient *http.Client

	// tlsClient is the HTTP/2 client used for https:// endpoints: an
	// http2.Transport that performs a real TLS handshake with
	// Config.TLSConfig (system trust store when nil) and negotiates h2
	// through ALPN.
	tlsClient *http.Client

	// healthClient is a vanilla HTTP client for the /healthz probe. Using
	// the default transport keeps the probe interoperable with HTTP/1.1
	// servers (e.g. httptest.NewServer in tests) AND with the real
	// HTTP/2 admin server. It shares TLSConfig with tlsClient so a
	// private CA works for the probe too.
	healthClient *http.Client

	mu             sync.Mutex
	currentBackoff time.Duration
	lastWarnAt     time.Time
}

// NewDialer constructs a Dialer.
func NewDialer(cfg Config) *Dialer {
	cfg = cfg.withDefaults()
	healthTransport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.TLSConfig != nil {
		healthTransport.TLSClientConfig = cfg.TLSConfig.Clone()
	}
	return &Dialer{
		cfg:       cfg,
		h2cClient: newH2CClient(),
		tlsClient: newTLSClient(cfg.TLSConfig),
		healthClient: &http.Client{
			Transport: healthTransport,
			Timeout:   cfg.HealthCheckTimeout,
		},
	}
}

// Result describes a successful Dial.
type Result struct {
	Client   adminv1connect.AgentServiceClient
	Endpoint string
}

// Dial tries each endpoint in order, advancing past the ones that fail
// the health probe. Returns the first one that completes successfully.
// When every endpoint fails, Dial returns the last error and the caller
// should sleep on Backoff() before retrying.
//
// Dial respects ctx cancellation and returns ctx.Err() promptly.
func (d *Dialer) Dial(ctx context.Context) (*Result, error) {
	if d == nil {
		return nil, errors.New("admin agent: nil dialer")
	}
	endpoints := d.cfg.Endpoints
	if len(endpoints) == 0 {
		return nil, errors.New("admin agent: no admin endpoints configured")
	}

	var lastErr error
	for _, ep := range endpoints {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		ep = strings.TrimSpace(ep)
		if ep == "" {
			continue
		}
		if err := healthCheck(ctx, d.healthClient, ep, d.cfg.HealthCheckTimeout); err != nil {
			lastErr = fmt.Errorf("endpoint %s: %w", ep, err)
			d.cfg.Logger.Debug("admin agent endpoint probe failed",
				"endpoint", ep, "error", err)
			continue
		}
		// Deliberately NOT resetting the backoff here. The probe hits
		// /healthz, which the admin server exempts from auth, so a
		// successful Dial proves reachability only — not that the token
		// is accepted. Resetting on Dial made an agent with a rejected
		// token hammer the server at ~InitialBackoff forever (OR5-2).
		// The agent calls ResetBackoff once the server accepts a frame.
		client := d.newClient(ep)
		return &Result{Client: client, Endpoint: ep}, nil
	}

	if lastErr == nil {
		lastErr = errors.New("no endpoints available")
	}
	d.warnRateLimited(lastErr)
	return nil, lastErr
}

// Backoff returns the duration the caller should wait before the next
// Dial. Grows exponentially up to MaxBackoff with jitter; resets to
// InitialBackoff only when the caller invokes ResetBackoff (i.e. after
// the admin server has actually accepted the stream, not merely after a
// successful /healthz probe).
//
// Backoff advances internal state, so call it exactly once per failed
// connect → stream cycle.
func (d *Dialer) Backoff() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.currentBackoff <= 0 {
		d.currentBackoff = d.cfg.InitialBackoff
	}
	sleep := d.currentBackoff
	jitter := d.cfg.BackoffJitter
	if jitter <= 0 {
		jitter = 0.5
	}
	sleep += time.Duration(rand.Float64() * float64(sleep) * jitter)

	d.currentBackoff *= 2
	if d.currentBackoff > d.cfg.MaxBackoff {
		d.currentBackoff = d.cfg.MaxBackoff
	}
	return sleep
}

// ResetBackoff returns the backoff schedule to InitialBackoff. Call it
// only on evidence that the server accepted the connection — in
// practice, when the stream layer receives its first frame from the
// server. A Dial success must NOT reset: the /healthz probe is exempt
// from auth, so it succeeds even when the token is being rejected.
func (d *Dialer) ResetBackoff() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.currentBackoff = 0
	d.lastWarnAt = time.Time{}
	d.mu.Unlock()
}

// warnRateLimited emits at most one WARN per minute regardless of how
// many failed Dial rounds occur (decision 9 / Phase 3 point 6).
func (d *Dialer) warnRateLimited(err error) {
	d.mu.Lock()
	now := time.Now()
	if !d.lastWarnAt.IsZero() && now.Sub(d.lastWarnAt) < time.Minute {
		d.mu.Unlock()
		return
	}
	d.lastWarnAt = now
	d.mu.Unlock()

	d.cfg.Logger.Warn("admin agent cannot reach admin server", "error", err.Error())
}

// maxMessageBytes caps one inbound Connect message from the server. The
// server sends small control frames (Subscribe, SnapshotRequest, Data
// Studio requests); anything larger is a fault, not traffic.
const maxMessageBytes = 4 << 20

func (d *Dialer) newClient(endpoint string) adminv1connect.AgentServiceClient {
	opts := []connect.ClientOption{connect.WithReadMaxBytes(maxMessageBytes)}
	if t := strings.TrimSpace(d.cfg.Token); t != "" {
		opts = append(opts, connect.WithInterceptors(bearerInterceptor{token: t}))
	}
	return adminv1connect.NewAgentServiceClient(d.httpClientFor(endpoint), endpoint, opts...)
}

// httpClientFor picks the transport by URL scheme: https:// endpoints get
// the TLS client, everything else (http://, h2c://) the cleartext one.
// The scheme is the only signal available — x/net/http2's Transport
// calls DialTLSContext for every scheme when AllowHTTP is set, so a
// single transport cannot tell the two apart at dial time. That was the
// bug: one h2c transport served every endpoint, and an https:// endpoint
// got a plain TCP connection that the TLS listener rejected.
func (d *Dialer) httpClientFor(endpoint string) *http.Client {
	if isHTTPS(endpoint) {
		return d.tlsClient
	}
	return d.h2cClient
}

func isHTTPS(endpoint string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(endpoint)), "https://")
}

// bearerInterceptor attaches "Authorization: Bearer <token>" to outbound
// calls. It implements the full connect.Interceptor interface rather than
// using connect.UnaryInterceptorFunc: the agent's only RPC is the bidi
// stream (AgentService.Connect), and unary-only interceptors are never
// invoked for streaming calls — so a unary-only bearer would leave the
// stream unauthenticated and the server would reject it with 401.
type bearerInterceptor struct {
	token string
}

var _ connect.Interceptor = bearerInterceptor{}

func (i bearerInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		req.Header().Set("Authorization", "Bearer "+i.token)
		return next(ctx, req)
	}
}

func (i bearerInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		conn.RequestHeader().Set("Authorization", "Bearer "+i.token)
		return conn
	}
}

// WrapStreamingHandler is a no-op: this interceptor is client-side only.
func (i bearerInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// newH2CClient builds an HTTP/2 client for cleartext (h2c) endpoints.
// x/net/http2.Transport with AllowHTTP upgrades plain http:// URLs to
// HTTP/2 without TLS; DialTLSContext is the only dial hook it offers, so
// it is overridden to return a plain TCP connection.
func newH2CClient() *http.Client {
	t := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			var nd net.Dialer
			return nd.DialContext(ctx, network, addr)
		},
	}
	return &http.Client{Transport: t}
}

// newTLSClient builds an HTTP/2 client for https:// endpoints. The
// transport's default dial performs the TLS handshake with cfg (a clone,
// so the caller's config is never mutated) and negotiates "h2" via ALPN;
// a nil cfg means the system trust store.
func newTLSClient(cfg *tls.Config) *http.Client {
	t := &http2.Transport{}
	if cfg != nil {
		t.TLSClientConfig = cfg.Clone()
	}
	return &http.Client{Transport: t}
}

// healthCheck pings GET /healthz on the endpoint origin to verify the
// admin server is reachable before opening a stream.
//
// The probe carries NO credential: the admin server exempts /healthz
// from auth precisely so probes need no token, and sending the bearer
// anyway put the shared secret on the wire (in the clear on http://
// endpoints) to every configured endpoint, reachable or not.
func healthCheck(ctx context.Context, cli *http.Client, endpoint string, timeout time.Duration) error {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	healthURL := strings.TrimRight(endpoint, "/") + "/healthz"
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, healthURL, nil)
	if err != nil {
		return fmt.Errorf("build health request: %w", err)
	}
	resp, err := cli.Do(req)
	if err != nil {
		return fmt.Errorf("health get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}
