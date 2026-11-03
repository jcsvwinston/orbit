package peers

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"

	"github.com/jcsvwinston/orbit/server/nodes"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// Config is what the mesh needs to reach its peers.
type Config struct {
	// ServerID names this server to its peers.
	ServerID string
	// AgentEndpoint is the endpoint agents reach this server on; peers
	// learn it to redirect agents here. Empty means this server never
	// receives assignments.
	AgentEndpoint string
	// Peers are the agent-listener endpoints of the other servers
	// (http(s)://host:port). The mesh dials PeerService.Sync on each.
	Peers []string
	// Token is the bearer the peers' agent listeners require.
	Token string
	// TLS is used for https:// peers; nil uses the system trust store.
	TLS *tls.Config
	// Nodes is this server's registry: what it tells its peers, and where
	// the remote nodes they announce are recorded by the inbound side.
	Nodes  *nodes.Registry
	Logger *slog.Logger
	// Backoff bounds the wait between failed dials to one peer.
	MinBackoff, MaxBackoff time.Duration
}

// Mesh keeps one outbound stream per configured peer and pushes this
// server's state down each: hello, the local nodes, and thereafter every
// local node change, event and host-metrics sample. The inbound side
// (what peers push here) is the PeerService handler, which records what
// it hears in the registry, the event bus and the replay ring; a stream
// carries frames in one direction only, so two servers hold two streams
// and neither dedupes.
type Mesh struct {
	cfg  Config
	mu   sync.RWMutex
	live map[string]*peerLink // endpoint → open outbound link

	// answered is, per configured endpoint, the server_id the server there
	// answered with on this server's own link — kept after the link drops,
	// so a peer that reconnects is still known. It is what makes a peer
	// "configured" (Configured, AwaitConfigured): the --peers list names
	// endpoints, and only dialling one says which server sits behind it.
	answered map[string]string
	// learned is closed and replaced whenever answered changes, so a
	// caller waiting for an answer wakes up without polling.
	learned chan struct{}
}

type peerLink struct {
	endpoint string
	serverID string
	send     chan *adminv1.PeerFrame
	dropped  uint64
}

// queueSize bounds what one slow peer may fall behind by; beyond it, the
// newest frame to that peer is dropped and counted. A peer is a relay,
// not retention.
const queueSize = 1024

// New builds a mesh; Run starts it.
func New(cfg Config) *Mesh {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.MinBackoff <= 0 {
		cfg.MinBackoff = 500 * time.Millisecond
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 30 * time.Second
	}
	return &Mesh{cfg: cfg, live: map[string]*peerLink{}, answered: map[string]string{}, learned: make(chan struct{})}
}

// Enabled reports whether any peer is configured.
func (m *Mesh) Enabled() bool { return m != nil && len(Sorted(m.cfg.Peers)) > 0 }

// Run dials every peer, forever, until ctx ends: one goroutine per peer,
// reconnecting with backoff.
func (m *Mesh) Run(ctx context.Context) {
	if m == nil || !m.Enabled() {
		return
	}
	var wg sync.WaitGroup
	for _, ep := range Sorted(m.cfg.Peers) {
		if ep == strings.TrimSpace(m.cfg.AgentEndpoint) {
			continue // not a peer of itself
		}
		wg.Add(1)
		go func(ep string) {
			defer wg.Done()
			m.runPeer(ctx, ep)
		}(ep)
	}
	wg.Wait()
}

func (m *Mesh) runPeer(ctx context.Context, endpoint string) {
	backoff := m.cfg.MinBackoff
	var lastErr string
	for {
		start := time.Now()
		err := m.session(ctx, endpoint)
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) > m.cfg.MaxBackoff {
			backoff = m.cfg.MinBackoff // a session that lasted resets the wait
			lastErr = ""
		}
		// The first failure, and each one that differs from the failure
		// before it, is a WARN: a link that never opens — a peer refusing
		// this server's certificate, say — has to say why on the side that
		// dials, not only as a handshake error in the peer's log. A retry
		// that fails the same way again stays at DEBUG.
		if msg := fmt.Sprint(err); msg != lastErr {
			lastErr = msg
			m.cfg.Logger.Warn("admin server: peer link down", "peer", endpoint, "error", err, "retry_in", backoff)
		} else {
			m.cfg.Logger.Debug("admin server: peer session ended", "peer", endpoint, "error", err, "retry_in", backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > m.cfg.MaxBackoff {
			backoff = m.cfg.MaxBackoff
		}
	}
}

// session opens one Sync stream to the peer and pushes this server's
// state down it until the stream ends.
func (m *Mesh) session(ctx context.Context, endpoint string) error {
	client := m.newClient(endpoint)
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream := client.Sync(sctx)

	link := &peerLink{endpoint: endpoint, send: make(chan *adminv1.PeerFrame, queueSize)}
	// Hello, then every local node, before the link is live: a peer that
	// reads the hello knows the whole set follows.
	hello := &adminv1.PeerHello{ServerId: m.cfg.ServerID, Version: "", AgentEndpoint: m.cfg.AgentEndpoint}
	local := m.cfg.Nodes.Local()
	for _, n := range local {
		hello.NodeIds = append(hello.NodeIds, n.NodeID)
	}
	if err := stream.Send(&adminv1.PeerFrame{Body: &adminv1.PeerFrame_Hello{Hello: hello}}); err != nil {
		return whySendFailed(stream, err)
	}
	for _, n := range local {
		if err := stream.Send(&adminv1.PeerFrame{Body: &adminv1.PeerFrame_Node{Node: NodeToProto(n)}}); err != nil {
			return whySendFailed(stream, err)
		}
	}

	// The peer answers with its own hello: that is acceptance.
	recvErr := make(chan error, 1)
	go func() {
		for {
			fr, err := stream.Receive()
			if err != nil {
				recvErr <- err
				return
			}
			if h := fr.GetHello(); h != nil {
				m.mu.Lock()
				link.serverID = h.GetServerId()
				m.mu.Unlock()
				m.learn(endpoint, h.GetServerId())
			}
		}
	}()

	// Live: node changes from the registry ride the same link.
	changes, unwatch := m.cfg.Nodes.Watch()
	defer unwatch()
	m.mu.Lock()
	m.live[endpoint] = link
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.live, endpoint)
		m.mu.Unlock()
	}()
	m.cfg.Logger.Info("admin server: peer link open", "peer", endpoint)

	for {
		select {
		case <-sctx.Done():
			return sctx.Err()
		case err := <-recvErr:
			return err
		case ch, ok := <-changes:
			if !ok {
				return errors.New("registry watch closed")
			}
			if ch.Info.Remote() {
				continue // never re-announce what a peer told us
			}
			var fr *adminv1.PeerFrame
			if ch.Connected {
				fr = &adminv1.PeerFrame{Body: &adminv1.PeerFrame_Node{Node: NodeToProto(ch.Info)}}
			} else {
				fr = &adminv1.PeerFrame{Body: &adminv1.PeerFrame_NodeGone{NodeGone: &adminv1.NodeGone{NodeId: ch.NodeID}}}
			}
			if err := stream.Send(fr); err != nil {
				return err
			}
		case fr := <-link.send:
			if err := stream.Send(fr); err != nil {
				return err
			}
		}
	}
}

// whySendFailed returns the error that ended the stream when Send reports
// only io.EOF — what a Connect client stream does once the request has
// failed, a refused TLS handshake among the causes; Receive carries the
// cause. It is called before anything else reads the stream.
func whySendFailed(stream *connect.BidiStreamForClient[adminv1.PeerFrame, adminv1.PeerFrame], err error) error {
	if !errors.Is(err, io.EOF) {
		return err
	}
	if _, rerr := stream.Receive(); rerr != nil && !errors.Is(rerr, io.EOF) {
		return rerr
	}
	return err
}

// RelayEvent pushes an event a local agent sent to every open peer link.
func (m *Mesh) RelayEvent(e *adminv1.Event) {
	if m == nil || e == nil {
		return
	}
	m.broadcast(&adminv1.PeerFrame{Body: &adminv1.PeerFrame_Event{Event: e}})
}

// RelayHostMetrics pushes a local node's sample to every open peer link.
func (m *Mesh) RelayHostMetrics(nodeID string, hm *adminv1.HostMetrics, at time.Time) {
	if m == nil || hm == nil {
		return
	}
	m.broadcast(&adminv1.PeerFrame{Body: &adminv1.PeerFrame_HostMetrics{HostMetrics: &adminv1.PeerHostMetrics{
		NodeId: nodeID, Sample: &adminv1.HostMetricsSample{Time: timestampOf(at), Metrics: hm},
	}}})
}

func (m *Mesh) broadcast(fr *adminv1.PeerFrame) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, l := range m.live {
		select {
		case l.send <- fr:
		default:
			l.dropped++
		}
	}
}

// LivePeers lists the endpoints with an open outbound link, sorted.
func (m *Mesh) LivePeers() []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	out := make([]string, 0, len(m.live))
	for ep := range m.live {
		out = append(out, ep)
	}
	m.mu.RUnlock()
	return Sorted(out)
}

// OwnerFor returns the endpoint that owns nodeID among this server and the
// peers it currently reaches, or "" when assignment is not possible (no
// own endpoint, or no live peer to hand a node to).
func (m *Mesh) OwnerFor(nodeID string) string {
	if m == nil || strings.TrimSpace(m.cfg.AgentEndpoint) == "" {
		return ""
	}
	live := m.LivePeers()
	if len(live) == 0 {
		return ""
	}
	return Owner(nodeID, append(live, m.cfg.AgentEndpoint))
}

// Self is this server's agent endpoint.
func (m *Mesh) Self() string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.cfg.AgentEndpoint)
}

// learn records the server_id that answered on the link to endpoint.
func (m *Mesh) learn(endpoint, serverID string) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.answered[endpoint] == serverID {
		return
	}
	m.answered[endpoint] = serverID
	close(m.learned)
	m.learned = make(chan struct{})
}

// dialled is the configured endpoints this server opens a link to: every
// --peers entry except its own.
func (m *Mesh) dialled() []string {
	self := strings.TrimSpace(m.cfg.AgentEndpoint)
	out := make([]string, 0, len(m.cfg.Peers))
	for _, ep := range Sorted(m.cfg.Peers) {
		if ep != self {
			out = append(out, ep)
		}
	}
	return out
}

// Configured reports whether serverID is a configured peer: the server
// that answered on this server's own link to one of its --peers endpoints.
// pending is true while some configured endpoint has not answered yet.
func (m *Mesh) Configured(serverID string) (configured, pending bool) {
	if m == nil {
		return false, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ep := range m.dialled() {
		id, ok := m.answered[ep]
		if !ok {
			pending = true
			continue
		}
		if id == serverID {
			return true, false
		}
	}
	return false, pending
}

// AwaitConfigured is Configured for an inbound peer, which may call before
// this server's own link to it has heard the answer — two servers that
// start together dial each other at once. While a configured endpoint has
// not answered, it waits for it, up to within; it returns as soon as the
// answer is known either way.
func (m *Mesh) AwaitConfigured(ctx context.Context, serverID string, within time.Duration) bool {
	if m == nil {
		return false
	}
	timer := time.NewTimer(within)
	defer timer.Stop()
	for {
		m.mu.RLock()
		learned := m.learned
		m.mu.RUnlock()
		configured, pending := m.Configured(serverID)
		if configured || !pending {
			return configured
		}
		select {
		case <-learned:
		case <-ctx.Done():
			return false
		case <-timer.C:
			return false
		}
	}
}

func (m *Mesh) newClient(endpoint string) adminv1connect.PeerServiceClient {
	opts := []connect.ClientOption{connect.WithReadMaxBytes(4 << 20)}
	if t := strings.TrimSpace(m.cfg.Token); t != "" {
		opts = append(opts, connect.WithInterceptors(bearer{token: t}))
	}
	var hc *http.Client
	if strings.HasPrefix(strings.ToLower(endpoint), "https://") {
		t := &http2.Transport{}
		if m.cfg.TLS != nil {
			t.TLSClientConfig = m.cfg.TLS.Clone()
		}
		hc = &http.Client{Transport: t}
	} else {
		hc = &http.Client{Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var nd net.Dialer
				return nd.DialContext(ctx, network, addr)
			},
		}}
	}
	return adminv1connect.NewPeerServiceClient(hc, endpoint, opts...)
}

type bearer struct{ token string }

func (b bearer) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		req.Header().Set("Authorization", "Bearer "+b.token)
		return next(ctx, req)
	}
}

func (b bearer) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		conn.RequestHeader().Set("Authorization", "Bearer "+b.token)
		return conn
	}
}

func (b bearer) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
