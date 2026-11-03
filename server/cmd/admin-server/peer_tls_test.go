package main

// The binary forms the fleet of servers under mutual TLS. Peers speak on
// each other's agent listener, so when that listener verifies client
// certificates (--agent-client-ca) a server has to present one to its
// peers. No flag used to fill server.Config.PeerTLS: two servers started
// with --agent-client-ca and --peers https://… failed every handshake and
// never shared a node. These tests build the configuration the way a
// deployment does — from the command line — and run it.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
	server "github.com/jcsvwinston/orbit/server"
)

// pki is a throwaway CA written to a directory, issuing certificates for
// 127.0.0.1 the way an operator's CA would.
type pki struct {
	dir    string
	caFile string
	cert   *x509.Certificate
	key    *ecdsa.PrivateKey
	pool   *x509.CertPool
}

func newPKI(t *testing.T, name string) *pki {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := &pki{dir: dir, cert: cert, key: key, pool: x509.NewCertPool()}
	p.pool.AddCert(cert)
	p.caFile = p.write(t, name+".crt", "CERTIFICATE", der)
	return p
}

func (p *pki) write(t *testing.T, name, typ string, der []byte) string {
	t.Helper()
	path := filepath.Join(p.dir, name)
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// leaf issues a certificate for cn with the given extended key usages.
func (p *pki) leaf(t *testing.T, cn string, usages ...x509.ExtKeyUsage) tls.Certificate {
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
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.cert, &key.PublicKey, p.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// pair writes a leaf for cn as name.crt/name.key and returns the paths.
func (p *pki) pair(t *testing.T, name, cn string, usages ...x509.ExtKeyUsage) (certFile, keyFile string) {
	t.Helper()
	l := p.leaf(t, cn, usages...)
	keyDER, err := x509.MarshalECPrivateKey(l.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	return p.write(t, name+".crt", "CERTIFICATE", l.Certificate[0]), p.write(t, name+".key", "EC PRIVATE KEY", keyDER)
}

// syncBuffer collects what the binary logs while its server goroutines
// write to it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// clientCN is the Common Name of the certificate cfg presents to a peer
// that asks for one, or "" when it presents none.
func clientCN(t *testing.T, cfg *tls.Config) string {
	t.Helper()
	if cfg == nil || cfg.GetClientCertificate == nil {
		return ""
	}
	c, err := cfg.GetClientCertificate(&tls.CertificateRequestInfo{})
	if err != nil || c == nil || len(c.Certificate) == 0 {
		t.Fatalf("GetClientCertificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return leaf.Subject.CommonName
}

func mustConfig(t *testing.T, stderr *syncBuffer, args ...string) server.Config {
	t.Helper()
	cfg, err := configFromArgs(append([]string{"--log-format", "text"}, args...), stderr)
	if err != nil {
		t.Fatalf("configFromArgs: %v", err)
	}
	return cfg
}

func TestPeerTLS_DefaultsToTheAgentListenerUnderMutualTLS(t *testing.T) {
	ca := newPKI(t, "fleet-ca")
	cert, key := ca.pair(t, "server-a", "server-a", x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)

	cfg := mustConfig(t, &syncBuffer{}, "--agent-cert", cert, "--agent-key", key, "--agent-client-ca", ca.caFile,
		"--peers", "https://127.0.0.1:1")
	if cfg.PeerTLS == nil {
		t.Fatal("a server whose agent listener runs mutual TLS has no client TLS for its peers")
	}
	if cfg.PeerTLS.RootCAs == nil || !cfg.PeerTLS.RootCAs.Equal(ca.pool) {
		t.Fatal("the peers' certificates are not verified against the --agent-client-ca bundle")
	}
	if got := clientCN(t, cfg.PeerTLS); got != "server-a" {
		t.Fatalf("the server presents %q to its peers, want its --agent-cert (server-a)", got)
	}
}

func TestPeerTLS_FlagsWinOverTheDefault(t *testing.T) {
	ca := newPKI(t, "fleet-ca")
	peerCA := newPKI(t, "peer-ca")
	cert, key := ca.pair(t, "server-a", "server-a", x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
	peerCert, peerKey := ca.pair(t, "peer-a", "peer-a", x509.ExtKeyUsageClientAuth)

	cfg := mustConfig(t, &syncBuffer{}, "--agent-cert", cert, "--agent-key", key, "--agent-client-ca", ca.caFile,
		"--peers", "https://127.0.0.1:1",
		"--peer-ca", peerCA.caFile, "--peer-cert", peerCert, "--peer-key", peerKey)
	if cfg.PeerTLS == nil || cfg.PeerTLS.RootCAs == nil || !cfg.PeerTLS.RootCAs.Equal(peerCA.pool) {
		t.Fatal("--peer-ca did not replace the --agent-client-ca bundle as the peers' roots")
	}
	if got := clientCN(t, cfg.PeerTLS); got != "peer-a" {
		t.Fatalf("the server presents %q to its peers, want the --peer-cert (peer-a)", got)
	}
}

func TestPeerTLS_EnvironmentNamesTheFiles(t *testing.T) {
	ca := newPKI(t, "fleet-ca")
	peerCert, peerKey := ca.pair(t, "peer-a", "peer-a", x509.ExtKeyUsageClientAuth)
	t.Setenv("NUCLEUS_ADMIN_PEER_CA", ca.caFile)
	t.Setenv("NUCLEUS_ADMIN_PEER_CERT", peerCert)
	t.Setenv("NUCLEUS_ADMIN_PEER_KEY", peerKey)

	cfg := mustConfig(t, &syncBuffer{}, "--peers", "https://127.0.0.1:1")
	if cfg.PeerTLS == nil || !cfg.PeerTLS.RootCAs.Equal(ca.pool) || clientCN(t, cfg.PeerTLS) != "peer-a" {
		t.Fatal("NUCLEUS_ADMIN_PEER_CA/_CERT/_KEY did not configure the peer links")
	}
}

func TestPeerTLS_NoneWithoutMutualTLS(t *testing.T) {
	ca := newPKI(t, "fleet-ca")
	cert, key := ca.pair(t, "server-a", "server-a", x509.ExtKeyUsageServerAuth)

	// No TLS at all, and a server certificate without client verification:
	// neither gives the peer links anything to default to — the system
	// trust store, as before.
	for _, args := range [][]string{
		{"--peers", "http://127.0.0.1:1"},
		{"--agent-cert", cert, "--agent-key", key, "--agent-token", "t", "--peers", "https://127.0.0.1:1"},
	} {
		if cfg := mustConfig(t, &syncBuffer{}, args...); cfg.PeerTLS != nil {
			t.Fatalf("%v: PeerTLS = %+v, want nil", args, cfg.PeerTLS)
		}
	}
}

// The agent certificate taken by default may have been issued for
// serverAuth alone: it served the agent listener before this default
// existed, so the server keeps starting — presenting no certificate, and
// saying why once.
func TestPeerTLS_AgentCertificateWithoutClientAuth_WarnsAndPresentsNone(t *testing.T) {
	ca := newPKI(t, "fleet-ca")
	cert, key := ca.pair(t, "server-a", "server-a", x509.ExtKeyUsageServerAuth)

	logs := &syncBuffer{}
	cfg := mustConfig(t, logs, "--agent-cert", cert, "--agent-key", key, "--agent-client-ca", ca.caFile,
		"--peers", "https://127.0.0.1:1")
	if cfg.PeerTLS == nil || !cfg.PeerTLS.RootCAs.Equal(ca.pool) {
		t.Fatal("the CA default must stand even when the certificate cannot be presented")
	}
	if got := clientCN(t, cfg.PeerTLS); got != "" {
		t.Fatalf("a serverAuth-only certificate was presented to the peers as %q", got)
	}
	if !strings.Contains(logs.String(), "cannot authenticate this server to its peers") || !strings.Contains(logs.String(), "--peer-cert") {
		t.Fatalf("no WARN naming the way out; log:\n%s", logs.String())
	}

	// Without an https:// peer the certificate never leaves the server:
	// nothing to warn about.
	quiet := &syncBuffer{}
	mustConfig(t, quiet, "--agent-cert", cert, "--agent-key", key, "--agent-client-ca", ca.caFile)
	if strings.Contains(quiet.String(), "cannot authenticate") {
		t.Fatalf("WARN without any https:// peer; log:\n%s", quiet.String())
	}
}

func TestPeerTLS_ExplicitCertificateWithoutClientAuth_RefusesToStart(t *testing.T) {
	ca := newPKI(t, "fleet-ca")
	peerCert, peerKey := ca.pair(t, "server-only", "server-a", x509.ExtKeyUsageServerAuth)

	_, err := configFromArgs([]string{"--peers", "https://127.0.0.1:1", "--peer-cert", peerCert, "--peer-key", peerKey}, &syncBuffer{})
	if err == nil || !strings.Contains(err.Error(), "--peer-cert") || !strings.Contains(err.Error(), "clientAuth") {
		t.Fatalf("err = %v, want a refusal naming --peer-cert and clientAuth", err)
	}
	if _, err := configFromArgs([]string{"--peer-cert", peerCert}, &syncBuffer{}); err == nil || !strings.Contains(err.Error(), "--peer-key") {
		t.Fatalf("a --peer-cert without --peer-key: err = %v, want a refusal", err)
	}
}

// freeAddr returns a loopback address nothing listens on, so two servers
// can name each other before either binds.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func startFromArgs(t *testing.T, args ...string) *server.Server {
	t.Helper()
	logs := &syncBuffer{}
	cfg := mustConfig(t, logs, args...)
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
		t.Fatalf("server did not bind: %v\n%s", <-done, logs.String())
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("server did not shut down")
		}
		if t.Failed() {
			t.Logf("log of the server on %s:\n%s", srv.AgentAddr(), logs.String())
		}
	})
	return srv
}

func waitFor(within time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// TestAdminServer_PeersFormTheMeshUnderMutualTLS is the deployment the
// documentation recommends: two servers verifying client certificates,
// peered over https://, configured by flags alone. A node that registers
// on A is listed by B.
func TestAdminServer_PeersFormTheMeshUnderMutualTLS(t *testing.T) {
	ca := newPKI(t, "fleet-ca")
	certA, keyA := ca.pair(t, "server-a", "server-a", x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
	certB, keyB := ca.pair(t, "server-b", "server-b", x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
	addrA, addrB := freeAddr(t), freeAddr(t)

	a := startFromArgs(t, "--server-id", "server-a", "--agent-addr", addrA, "--ui-addr", "127.0.0.1:0", "--ui-bearer", "u",
		"--agent-cert", certA, "--agent-key", keyA, "--agent-client-ca", ca.caFile,
		"--agent-advertise-addr", "https://"+addrA, "--peers", "https://"+addrB)
	b := startFromArgs(t, "--server-id", "server-b", "--agent-addr", addrB, "--ui-addr", "127.0.0.1:0", "--ui-bearer", "u",
		"--agent-cert", certB, "--agent-key", keyB, "--agent-client-ca", ca.caFile,
		"--agent-advertise-addr", "https://"+addrB, "--peers", "https://"+addrA)

	if !waitFor(5*time.Second, func() bool {
		return len(a.State().Peers.LivePeers()) == 1 && len(b.State().Peers.LivePeers()) == 1
	}) {
		t.Fatalf("the servers did not form the mesh in 5s (A sees %v, B sees %v)", a.State().Peers.LivePeers(), b.State().Peers.LivePeers())
	}

	// An agent with a certificate for node-a registers on A.
	node := ca.leaf(t, "node-a", x509.ExtKeyUsageClientAuth)
	client := adminv1connect.NewAgentServiceClient(&http.Client{Transport: &http2.Transport{TLSClientConfig: &tls.Config{
		RootCAs: ca.pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{node},
	}}}, "https://"+addrA)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream := client.Stream(ctx)
	if err := stream.Send(&adminv1.Frame{Body: &adminv1.Frame_Registration{Registration: &adminv1.NodeRegistration{
		NodeId: "node-a", StartedAt: timestamppb.Now(),
	}}}); err != nil {
		t.Fatalf("register node-a on A: %v", err)
	}
	if !waitFor(5*time.Second, func() bool { _, ok := a.State().Nodes.Lookup("node-a"); return ok }) {
		t.Fatal("node-a did not register on A")
	}
	var via string
	if !waitFor(5*time.Second, func() bool {
		e, ok := b.State().Nodes.Lookup("node-a")
		if ok {
			via = e.Info.Via
		}
		return ok
	}) {
		t.Fatal("B does not list the node registered on A")
	}
	if via != "server-a" {
		t.Fatalf("B lists node-a via %q, want server-a", via)
	}
}
