package server_test

// The server certificate rotates without a restart: TLSFromFiles serves the
// pair the files hold NOW, re-reading them when a handshake finds them
// changed. These tests write v1, handshake, write v2, handshake — and break
// the pair on purpose to see the previous certificate keep serving.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	server "github.com/jcsvwinston/orbit/server"
)

// writeKeyPair writes leaf as PEM into certFile/keyFile and moves their
// modification time forward, so a rewrite within the same clock tick is
// still a change the stamp can see.
func writeKeyPair(t *testing.T, certFile, keyFile string, leaf tls.Certificate, at time.Time) {
	t.Helper()
	keyDER, err := x509.MarshalECPrivateKey(leaf.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Certificate[0]}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{certFile, keyFile} {
		if err := os.Chtimes(f, at, at); err != nil {
			t.Fatal(err)
		}
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// leafSeen dials the agent listener and returns the Common Name of the
// certificate the handshake presented.
func leafSeen(t *testing.T, addr string) string {
	t.Helper()
	conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // the test reads the leaf, it trusts nothing
	if err != nil {
		t.Fatalf("tls.Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return ""
	}
	return certs[0].Subject.CommonName
}

func TestTLSFromFiles_RotatesWithoutRestart(t *testing.T) {
	ca := newTestCA(t)
	v1, _ := ca.issue(t, "server-v1", x509.ExtKeyUsageServerAuth)
	v2, _ := ca.issue(t, "server-v2", x509.ExtKeyUsageServerAuth)
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key")
	t0 := time.Now().Add(-10 * time.Second)
	writeKeyPair(t, certFile, keyFile, v1, t0)

	logs := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	cfg, err := server.TLSFromFiles(certFile, keyFile, logger)
	if err != nil {
		t.Fatalf("TLSFromFiles: %v", err)
	}
	srv, stop := startTLSServer(t, cfg, nil, "token")
	defer stop()

	if got := leafSeen(t, srv.AgentAddr()); got != "server-v1" {
		t.Fatalf("first handshake saw %q, want server-v1", got)
	}

	// Rotation: two files rewritten, no restart, no signal.
	writeKeyPair(t, certFile, keyFile, v2, t0.Add(2*time.Second))
	if got := leafSeen(t, srv.AgentAddr()); got != "server-v2" {
		t.Fatalf("after rewriting the files the handshake saw %q, want server-v2", got)
	}
	if !strings.Contains(logs.String(), "tls certificate reloaded from files") {
		t.Fatalf("a reload must be logged; log was:\n%s", logs.String())
	}

	// A broken rotation — the certificate rewritten, the key not yet —
	// keeps the previous certificate serving and says so once.
	v3, _ := ca.issue(t, "server-v3", x509.ExtKeyUsageServerAuth)
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: v3.Certificate[0]}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(certFile, t0.Add(4*time.Second), t0.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if got := leafSeen(t, srv.AgentAddr()); got != "server-v2" {
			t.Fatalf("with a key that does not match the new certificate the handshake saw %q, want the previous server-v2", got)
		}
	}
	if n := strings.Count(logs.String(), "could not be loaded; keeping the previous certificate"); n != 1 {
		t.Fatalf("the broken rotation must be warned exactly once across three handshakes, got %d; log was:\n%s", n, logs.String())
	}

	// The key catches up: the pair is whole again and v3 is presented.
	writeKeyPair(t, certFile, keyFile, v3, t0.Add(6*time.Second))
	if got := leafSeen(t, srv.AgentAddr()); got != "server-v3" {
		t.Fatalf("after the key caught up the handshake saw %q, want server-v3", got)
	}
}

func TestTLSFromFiles_FirstLoadMustSucceed(t *testing.T) {
	dir := t.TempDir()
	if _, err := server.TLSFromFiles(filepath.Join(dir, "absent.crt"), filepath.Join(dir, "absent.key"), nil); err == nil {
		t.Fatal("TLSFromFiles accepted files that do not exist")
	}
	if _, err := server.TLSFromFiles("", filepath.Join(dir, "k"), nil); err == nil {
		t.Fatal("TLSFromFiles accepted an empty certificate path")
	}
}
