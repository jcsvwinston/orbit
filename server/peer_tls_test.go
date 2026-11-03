package server_test

// The fleet of servers under mutual TLS. Peers speak on each other's agent
// listener, so when that listener verifies client certificates a peer has
// to present one — and for several releases nothing gave the mesh one: the
// binary had no flag that filled Config.PeerTLS, every link died in the
// handshake, and no test ran the mesh over TLS. These tests run it: two
// servers whose agent listeners require a client certificate, peered over
// https://, with the client side built by PeerTLSFromFiles from the same
// files the listeners serve.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	server "github.com/jcsvwinston/orbit/server"
)

// peerPKI is one CA on disk and, per server, a certificate usable on both
// ends of a peer link: serverAuth for its agent listener, clientAuth for
// the links it opens.
type peerPKI struct {
	ca     *testCA
	dir    string
	caFile string
}

func newPeerPKI(t *testing.T) *peerPKI {
	t.Helper()
	ca := newTestCA(t)
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.caCert.Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	return &peerPKI{ca: ca, dir: dir, caFile: caFile}
}

// pair issues a certificate for cn (valid for 127.0.0.1) with the given
// extended key usages — testCA.issue takes one — and writes it as
// name.crt/name.key.
func (p *peerPKI) pair(t *testing.T, name, cn string, usages ...x509.ExtKeyUsage) (certFile, keyFile string) {
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
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.ca.caCert, &key.PublicKey, p.ca.caKey)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(p.dir, name+".crt"), filepath.Join(p.dir, name+".key")
	writeKeyPair(t, certFile, keyFile, tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, time.Now())
	return certFile, keyFile
}

// reserveLoopbackAddr returns a loopback address nothing listens on, so two
// servers can name each other before either binds.
func reserveLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// runPeerServer starts a server with the given configuration and stops it
// with the test.
func runPeerServer(t *testing.T, cfg server.Config) *server.Server {
	t.Helper()
	if cfg.UIAddr == "" {
		cfg.UIAddr = "127.0.0.1:0"
	}
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
	})
	return srv
}

// mutualListener is what the binary builds from --agent-cert, --agent-key
// and --agent-client-ca.
func (p *peerPKI) mutualListener(t *testing.T, certFile, keyFile string) *tls.Config {
	t.Helper()
	tc, err := server.TLSFromFiles(certFile, keyFile, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	tc.ClientCAs = p.ca.pool
	tc.ClientAuth = tls.RequireAndVerifyClientCert
	return tc
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

func TestServer_PeersFormTheMeshOverMutualTLS(t *testing.T) {
	pki := newPeerPKI(t)
	certA, keyA := pki.pair(t, "server-a", "server-a", x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
	certB, keyB := pki.pair(t, "server-b", "server-b", x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
	addrA, addrB := reserveLoopbackAddr(t), reserveLoopbackAddr(t)

	peerTLS := func(cert, key string) *tls.Config {
		tc, err := server.PeerTLSFromFiles(pki.caFile, cert, key, discardLogger())
		if err != nil {
			t.Fatal(err)
		}
		return tc
	}
	a := runPeerServer(t, server.Config{
		AgentAddr: addrA, ServerID: "server-a", AgentAdvertiseAddr: "https://" + addrA,
		AgentTLS: pki.mutualListener(t, certA, keyA), PeerAddrs: []string{"https://" + addrB},
		PeerTLS: peerTLS(certA, keyA), Logger: discardLogger(),
	})
	b := runPeerServer(t, server.Config{
		AgentAddr: addrB, ServerID: "server-b", AgentAdvertiseAddr: "https://" + addrB,
		AgentTLS: pki.mutualListener(t, certB, keyB), PeerAddrs: []string{"https://" + addrA},
		PeerTLS: peerTLS(certB, keyB), Logger: discardLogger(),
	})

	if !waitFor(5*time.Second, func() bool {
		return len(a.State().Peers.LivePeers()) == 1 && len(b.State().Peers.LivePeers()) == 1
	}) {
		t.Fatalf("the servers did not peer over mutual TLS in 5s (A sees %v, B sees %v)", a.State().Peers.LivePeers(), b.State().Peers.LivePeers())
	}
	if err := registerWithCert(t, pki.ca, a, "node-a", "node-a"); err != nil {
		t.Fatalf("node-a did not register on A: %v", err)
	}
	var via string
	if !waitFor(5*time.Second, func() bool {
		e, ok := b.State().Nodes.Lookup("node-a")
		if ok {
			via = e.Info.Via
		}
		return ok
	}) {
		t.Fatal("B does not list the node registered on A: the mesh carried nothing")
	}
	if via != "server-a" {
		t.Fatalf("B lists node-a via %q, want server-a", via)
	}
}

// A server that presents no client certificate to a peer that requires one
// does not join, and says so on its own side: the dialling server used to
// log the failure at DEBUG, so the only trace was a handshake error in the
// peer's log.
func TestServer_PeerWithoutAClientCertificate_LogsTheRefusal(t *testing.T) {
	pki := newPeerPKI(t)
	certA, keyA := pki.pair(t, "server-a", "server-a", x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
	addrA := reserveLoopbackAddr(t)
	runPeerServer(t, server.Config{
		AgentAddr: addrA, ServerID: "server-a",
		AgentTLS: pki.mutualListener(t, certA, keyA), Logger: discardLogger(),
	})

	trustOnly, err := server.PeerTLSFromFiles(pki.caFile, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	runPeerServer(t, server.Config{
		AgentAddr: "127.0.0.1:0", ServerID: "server-b", AgentToken: "unused-by-peers",
		PeerAddrs: []string{"https://" + addrA}, PeerTLS: trustOnly,
		Logger: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	if !waitFor(5*time.Second, func() bool { return strings.Contains(logs.String(), "peer link down") }) {
		t.Fatalf("a link refused in the handshake left no WARN on the dialling server; log:\n%s", logs.String())
	}
	// The WARN carries the cause, not the io.EOF a failed stream's Send
	// reports.
	if !strings.Contains(logs.String(), "certificate") {
		t.Fatalf("the WARN does not say the handshake wanted a certificate; log:\n%s", logs.String())
	}
}

func TestPeerTLSFromFiles_TrustsTheCAAndPresentsTheCertificate(t *testing.T) {
	pki := newPeerPKI(t)
	cert, key := pki.pair(t, "client", "server-a", x509.ExtKeyUsageClientAuth)

	tc, err := server.PeerTLSFromFiles(pki.caFile, cert, key, nil)
	if err != nil {
		t.Fatalf("PeerTLSFromFiles: %v", err)
	}
	if tc.RootCAs == nil {
		t.Fatal("the CA bundle did not become the roots the peers' certificates are verified against")
	}
	if tc.GetClientCertificate == nil {
		t.Fatal("no client certificate: a peer that verifies one refuses the link")
	}
	got, err := tc.GetClientCertificate(&tls.CertificateRequestInfo{})
	if err != nil || got == nil || got.Leaf == nil || got.Leaf.Subject.CommonName != "server-a" {
		t.Fatalf("GetClientCertificate = %v, %v; want the server-a pair", got, err)
	}

	trustOnly, err := server.PeerTLSFromFiles(pki.caFile, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if trustOnly.RootCAs == nil || trustOnly.GetClientCertificate != nil {
		t.Fatal("a CA alone must set the roots and present no certificate")
	}
}

func TestPeerTLSFromFiles_RefusesACertificateWithoutClientAuth(t *testing.T) {
	pki := newPeerPKI(t)
	cert, key := pki.pair(t, "server-only", "server-a", x509.ExtKeyUsageServerAuth)

	_, err := server.PeerTLSFromFiles(pki.caFile, cert, key, nil)
	if !errors.Is(err, server.ErrNotAClientCertificate) {
		t.Fatalf("a serverAuth-only certificate: err = %v, want ErrNotAClientCertificate", err)
	}
}

func TestPeerTLSFromFiles_CertificateAndKeyGoTogether(t *testing.T) {
	pki := newPeerPKI(t)
	cert, key := pki.pair(t, "client", "server-a", x509.ExtKeyUsageClientAuth)
	if _, err := server.PeerTLSFromFiles("", cert, "", nil); err == nil {
		t.Fatal("a certificate without its key was accepted")
	}
	if _, err := server.PeerTLSFromFiles("", "", key, nil); err == nil {
		t.Fatal("a key without its certificate was accepted")
	}
	if _, err := server.PeerTLSFromFiles(filepath.Join(pki.dir, "absent.crt"), "", "", nil); err == nil {
		t.Fatal("a CA bundle that does not exist was accepted")
	}
}
