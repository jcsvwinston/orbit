package server_test

// Who may be a peer. PeerService.Sync is mounted on the agent listener of
// every server, peers or not, behind the agents' authentication — so a
// holder of the agent token, or of any certificate of the agents' CA, could
// open it, announce a server and push nodes and events attributed to any
// node into this server's registry, bus and replay. These tests pin both
// regimes of Config.PeerAllowlist: on, a peer is accepted only when it is
// the server that answers at one of this server's --peers endpoints and,
// under mutual TLS, when its certificate names it; off — the 1.x default
// (DEP-2026-005) — every such peer is accepted and the ones the check would
// refuse are logged once per server_id.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
	server "github.com/jcsvwinston/orbit/server"
)

const allowlistToken = "fleet-token"

// allowlistAddr is a loopback address nothing listens on yet, so two
// servers can name each other before either binds.
func allowlistAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// allowlistServer runs a server with cfg, logging WARN and above into the
// returned buffer, and stops it with the test.
func allowlistServer(t *testing.T, cfg server.Config) (*server.Server, *syncBuffer) {
	t.Helper()
	logs := &syncBuffer{}
	if cfg.AgentAddr == "" {
		cfg.AgentAddr = "127.0.0.1:0"
	}
	cfg.UIAddr = "127.0.0.1:0"
	cfg.Logger = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	srv := server.New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && (srv.AgentAddr() == "" || srv.UIAddr() == "") {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.AgentAddr() == "" {
		cancel()
		t.Fatalf("server did not bind: %v", <-done)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("server did not shut down")
		}
		if t.Failed() {
			t.Logf("log of %s:\n%s", cfg.ServerID, logs.String())
		}
	})
	return srv, logs
}

func eventually(within time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// announceAsPeer opens PeerService.Sync on srv as a server named serverID
// and announces nodeID behind it — what a holder of the agent credentials
// can do. It returns nil when srv accepted the peer (the node appears in
// its registry) and the stream's error when srv refused it.
func announceAsPeer(t *testing.T, srv *server.Server, hc *http.Client, scheme, serverID, nodeID string, opts ...connect.ClientOption) error {
	t.Helper()
	client := adminv1connect.NewPeerServiceClient(hc, scheme+"://"+srv.AgentAddr(), opts...)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream := client.Sync(ctx)
	if err := stream.Send(&adminv1.PeerFrame{Body: &adminv1.PeerFrame_Hello{Hello: &adminv1.PeerHello{
		ServerId: serverID, AgentEndpoint: scheme + "://127.0.0.1:1", NodeIds: []string{nodeID},
	}}}); err != nil {
		return err
	}
	if err := stream.Send(&adminv1.PeerFrame{Body: &adminv1.PeerFrame_Node{Node: &adminv1.NodeInfo{
		NodeId: nodeID, Connected: true,
	}}}); err != nil {
		return err
	}
	// The server answers with its hello either way; a refusal follows as
	// the stream's error, an acceptance as the node in its registry.
	answer := make(chan error, 1)
	go func() {
		for {
			if _, err := stream.Receive(); err != nil {
				answer <- err
				return
			}
		}
	}()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case err := <-answer:
			return err
		case <-deadline:
			t.Fatalf("peer %q neither accepted nor refused within 10s", serverID)
		case <-time.After(20 * time.Millisecond):
			if e, ok := srv.State().Nodes.Lookup(nodeID); ok && e.Info.Via == serverID {
				return nil
			}
		}
	}
}

// h2cWithToken is what an agent with the fleet token holds.
func h2cWithToken() (*http.Client, connect.ClientOption) {
	return h2cClient(), connect.WithInterceptors(bearer(allowlistToken))
}

// registerAgent registers nodeID on srv over h2c with the fleet token.
func registerAgent(t *testing.T, srv *server.Server, nodeID string) {
	t.Helper()
	hc, auth := h2cWithToken()
	client := adminv1connect.NewAgentServiceClient(hc, "http://"+srv.AgentAddr(), auth)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream := client.Stream(ctx)
	if err := stream.Send(&adminv1.Frame{Body: &adminv1.Frame_Registration{Registration: &adminv1.NodeRegistration{
		NodeId: nodeID, StartedAt: timestamppb.Now(),
	}}}); err != nil {
		t.Fatalf("register %s: %v", nodeID, err)
	}
	go func() {
		for {
			if _, err := stream.Receive(); err != nil {
				return
			}
		}
	}()
	if !eventually(3*time.Second, func() bool { _, ok := srv.State().Nodes.Lookup(nodeID); return ok }) {
		t.Fatalf("%s did not register", nodeID)
	}
}

// twoDiallingServers peers two h2c servers with each other.
func twoDiallingServers(t *testing.T, allowlist bool) (a, b *server.Server, logsA, logsB *syncBuffer) {
	t.Helper()
	addrA, addrB := allowlistAddr(t), allowlistAddr(t)
	a, logsA = allowlistServer(t, server.Config{
		AgentAddr: addrA, ServerID: "server-a", AgentToken: allowlistToken,
		PeerAddrs: []string{"http://" + addrB}, PeerAllowlist: allowlist,
	})
	b, logsB = allowlistServer(t, server.Config{
		AgentAddr: addrB, ServerID: "server-b", AgentToken: allowlistToken,
		PeerAddrs: []string{"http://" + addrA}, PeerAllowlist: allowlist,
	})
	return a, b, logsA, logsB
}

func TestServer_PeerAllowlist_RefusesAPeerItDoesNotDial(t *testing.T) {
	a, logs := allowlistServer(t, server.Config{ServerID: "server-a", AgentToken: allowlistToken, PeerAllowlist: true})

	hc, auth := h2cWithToken()
	err := announceAsPeer(t, a, hc, "http", "rogue", "ghost-node", auth)
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a caller with the agent token announcing a server: err = %v, want PermissionDenied", err)
	}
	if _, ok := a.State().Nodes.Lookup("ghost-node"); ok {
		t.Fatal("the refused peer's node reached the registry")
	}
	if !strings.Contains(logs.String(), "peer refused") || !strings.Contains(logs.String(), "peer=rogue") {
		t.Fatalf("no WARN naming the refused peer; log:\n%s", logs.String())
	}
}

func TestServer_PeerAllowlist_AcceptsTheServersItDials(t *testing.T) {
	for _, allowlist := range []bool{true, false} {
		name := map[bool]string{true: "allowlist", false: "default"}[allowlist]
		t.Run(name, func(t *testing.T) {
			a, b, logsA, logsB := twoDiallingServers(t, allowlist)
			registerAgent(t, a, "node-a")
			if !eventually(8*time.Second, func() bool {
				e, ok := b.State().Nodes.Lookup("node-a")
				return ok && e.Info.Via == "server-a"
			}) {
				t.Fatal("B does not list the node registered on A: the configured peer was not accepted")
			}
			// Each knows the other as the server that answers at its
			// --peers endpoint, so neither refuses nor warns about it.
			for _, c := range []struct {
				srv  *server.Server
				peer string
			}{{a, "server-b"}, {b, "server-a"}} {
				if ok, _ := c.srv.State().Peers.Configured(c.peer); !ok {
					t.Fatalf("%s is not a configured peer", c.peer)
				}
			}
			for _, l := range []*syncBuffer{logsA, logsB} {
				if s := l.String(); strings.Contains(s, "peer refused") || strings.Contains(s, "Orbit 2.0 refuses") {
					t.Fatalf("a configured peer was refused or warned about; log:\n%s", s)
				}
			}
			if allowlist {
				// The configured peers in place, a caller that is not one
				// of them is still refused — at once, nothing pending.
				hc, auth := h2cWithToken()
				if err := announceAsPeer(t, a, hc, "http", "rogue", "ghost-node", auth); connect.CodeOf(err) != connect.CodePermissionDenied {
					t.Fatalf("rogue peer beside configured ones: err = %v, want PermissionDenied", err)
				}
				// Announcing a configured server's name over a token is a
				// claim the token cannot disprove: without client
				// certificates the check compares names.
			}
		})
	}
}

// TestServer_PeerAllowlistOff_AcceptsAnyPeerAndWarnsOnce pins the 1.x
// default (DEP-2026-005): a caller with the agent token is accepted as a
// peer, its node reaches the registry, and the server logs one WARN for it
// per process — naming the notice and the flag — however often it returns.
// Orbit 2.0 refuses it.
func TestServer_PeerAllowlistOff_AcceptsAnyPeerAndWarnsOnce(t *testing.T) {
	a, logs := allowlistServer(t, server.Config{ServerID: "server-a", AgentToken: allowlistToken})

	hc, auth := h2cWithToken()
	if err := announceAsPeer(t, a, hc, "http", "rogue", "ghost-node", auth); err != nil {
		t.Fatalf("1.x refused an unconfigured peer: %v", err)
	}
	const warn = "accepted a peer that Orbit 2.0 refuses"
	if !eventually(3*time.Second, func() bool { return strings.Contains(logs.String(), warn) }) {
		t.Fatalf("no WARN for the unconfigured peer; log:\n%s", logs.String())
	}
	for _, want := range []string{"peer=rogue", "DEP-2026-005", "--peer-allowlist"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("the WARN does not name %q; log:\n%s", want, logs.String())
		}
	}

	// The same peer again: accepted again, and not logged again.
	if err := announceAsPeer(t, a, hc, "http", "rogue", "ghost-node-2", auth); err != nil {
		t.Fatalf("second connection: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // the WARN is written beside the stream
	if n := strings.Count(logs.String(), warn); n != 1 {
		t.Fatalf("%d WARNs for one peer, want 1; log:\n%s", n, logs.String())
	}
}

// ---- under mutual TLS ------------------------------------------------------

// allowlistLeaf issues a certificate for cn, valid for 127.0.0.1, with the
// given extended key usages (testCA.issue takes one).
func allowlistLeaf(t *testing.T, ca *testCA, cn string, usages ...x509.ExtKeyUsage) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  usages,
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.caCert, &key.PublicKey, ca.caKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

// mtlsPeerServer runs a server named id whose agent listener requires a
// certificate of ca, presenting a certificate named id on both ends.
func mtlsPeerServer(t *testing.T, ca *testCA, id, addr, peer string, allowlist bool) (*server.Server, *syncBuffer) {
	t.Helper()
	cert := allowlistLeaf(t, ca, id, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
	return allowlistServer(t, server.Config{
		AgentAddr: addr, ServerID: id, PeerAllowlist: allowlist, PeerAddrs: []string{peer},
		AgentTLS: &tls.Config{
			Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12,
			ClientCAs: ca.pool, ClientAuth: tls.RequireAndVerifyClientCert,
		},
		PeerTLS: &tls.Config{RootCAs: ca.pool, Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
	})
}

// withCert is an HTTP/2-over-TLS client presenting a certificate for cn.
func withCert(t *testing.T, ca *testCA, cn string) *http.Client {
	t.Helper()
	leaf := allowlistLeaf(t, ca, cn, x509.ExtKeyUsageClientAuth)
	return &http.Client{Transport: &http2.Transport{TLSClientConfig: &tls.Config{
		RootCAs: ca.pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{leaf},
	}}}
}

func TestServer_PeerAllowlist_UnderMutualTLSTheCertificateNamesThePeer(t *testing.T) {
	ca := newTestCA(t)
	addrA, addrB := allowlistAddr(t), allowlistAddr(t)
	a, logsA := mtlsPeerServer(t, ca, "server-a", addrA, "https://"+addrB, true)
	b, _ := mtlsPeerServer(t, ca, "server-b", addrB, "https://"+addrA, true)

	// The real peer: B dials A with a certificate named server-b.
	if err := registerWithCert(t, ca, b, "node-b", "node-b"); err != nil {
		t.Fatalf("node-b did not register on B: %v", err)
	}
	if !eventually(8*time.Second, func() bool {
		e, ok := a.State().Nodes.Lookup("node-b")
		return ok && e.Info.Via == "server-b"
	}) {
		t.Fatalf("A does not list B's node: the configured peer was refused under mutual TLS; log:\n%s", logsA.String())
	}

	// An agent's certificate announcing B's name: the name is configured,
	// the certificate is not B's.
	err := announceAsPeer(t, a, withCert(t, ca, "node-x"), "https", "server-b", "ghost-node")
	if connect.CodeOf(err) != connect.CodePermissionDenied || !strings.Contains(err.Error(), `"node-x"`) {
		t.Fatalf("a certificate for node-x announcing server-b: err = %v, want PermissionDenied naming the certificate", err)
	}
	// The same certificate announcing its own name: it matches, and it is
	// not a server A dials.
	err = announceAsPeer(t, a, withCert(t, ca, "node-x"), "https", "node-x", "ghost-node")
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a certificate for node-x announcing node-x: err = %v, want PermissionDenied", err)
	}
	if e, ok := a.State().Nodes.Lookup("ghost-node"); ok {
		t.Fatalf("a refused peer's node reached the registry via %q", e.Info.Via)
	}
}

// Off, a certificate that names another server is accepted — and is one of
// the peers the WARN names, with the reason.
func TestServer_PeerAllowlistOff_WarnsOnACertificateThatNamesAnotherServer(t *testing.T) {
	ca := newTestCA(t)
	a, logs := mtlsPeerServer(t, ca, "server-a", allowlistAddr(t), "https://127.0.0.1:1", false)

	if err := announceAsPeer(t, a, withCert(t, ca, "node-x"), "https", "server-z", "ghost-node"); err != nil {
		t.Fatalf("1.x refused the peer: %v", err)
	}
	if !eventually(3*time.Second, func() bool { return strings.Contains(logs.String(), "Orbit 2.0 refuses") }) {
		t.Fatalf("no WARN; log:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "certificate_cn=node-x") || !strings.Contains(logs.String(), "client certificate names") {
		t.Fatalf("the WARN does not say the certificate names another server; log:\n%s", logs.String())
	}
}
