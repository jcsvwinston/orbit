package peers

import (
	"context"
	"crypto/tls"
	"errors"
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
	return &Mesh{cfg: cfg, live: map[string]*peerLink{}}
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
	for {
		start := time.Now()
		err := m.session(ctx, endpoint)
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) > m.cfg.MaxBackoff {
			backoff = m.cfg.MinBackoff // a session that lasted resets the wait
		}
		m.cfg.Logger.Debug("admin server: peer session ended", "peer", endpoint, "error", err, "retry_in", backoff)
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
		return err
	}
	for _, n := range local {
		if err := stream.Send(&adminv1.PeerFrame{Body: &adminv1.PeerFrame_Node{Node: NodeToProto(n)}}); err != nil {
			return err
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
