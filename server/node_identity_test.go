package server_test

// The node identity is the certificate's, not the agent's word. A mutual-TLS
// listener verifies who holds the certificate and then, until this test
// existed, registered the node under whatever node_id the first frame
// declared — an agent with a certificate for node-a could sit in the fleet
// as node-b. These tests pin both regimes of AgentIdentityFromCertificate:
// bound (a mismatch is refused with PermissionDenied, a match registers) and
// unbound, the default (registered as declared, but never silently).

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
	server "github.com/jcsvwinston/orbit/server"
)

// syncBuffer is a bytes.Buffer the server's logger may write to from its
// handler goroutines while the test reads it.
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

// startMTLSServer runs a server whose agent listener requires a client
// certificate signed by ca, with the identity binding as asked, logging
// into the returned buffer at WARN and above: what the tests assert is
// that an operator reading a production log sees it.
func startMTLSServer(t *testing.T, ca *testCA, bind bool) (*server.Server, *syncBuffer, func()) {
	t.Helper()
	logs := &syncBuffer{}
	srv := server.New(server.Config{
		AgentAddr: "127.0.0.1:0",
		UIAddr:    "127.0.0.1:0",
		AgentTLS: &tls.Config{
			Certificates: []tls.Certificate{ca.leaf},
			MinVersion:   tls.VersionTLS12,
			ClientCAs:    ca.pool,
			ClientAuth:   tls.RequireAndVerifyClientCert,
		},
		AgentIdentityFromCertificate: bind,
		Logger:                       slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() { doneCh <- srv.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (srv.AgentAddr() == "" || srv.UIAddr() == "") {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.AgentAddr() == "" || srv.UIAddr() == "" {
		cancel()
		t.Fatalf("server did not bind: %v", <-doneCh)
	}
	return srv, logs, func() {
		cancel()
		select {
		case <-doneCh:
		case <-time.After(3 * time.Second):
			t.Error("server did not shut down")
		}
	}
}

// registerWithCert opens a stream presenting a client certificate for cn,
// declares nodeID and reports what the server did: nil when the node
// appeared in the registry, the stream's error when it was refused. It
// waits for whichever happens first — the fact, not a clock.
func registerWithCert(t *testing.T, ca *testCA, srv *server.Server, cn, nodeID string) error {
	t.Helper()
	leaf, _ := ca.issue(t, cn, x509.ExtKeyUsageClientAuth)
	cfg := &tls.Config{RootCAs: ca.pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{leaf}}
	client := adminv1connect.NewAgentServiceClient(tlsH2Client(cfg), "https://"+srv.AgentAddr())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream := client.Stream(ctx)
	if err := stream.Send(&adminv1.Frame{Body: &adminv1.Frame_Registration{Registration: &adminv1.NodeRegistration{
		NodeId: nodeID, StartedAt: timestamppb.Now(),
	}}}); err != nil {
		return err
	}
	// The server answers a registration one way or the other: an accepted
	// node receives the aggregate Subscribe frame at once, a refused one
	// receives the stream's error. Either is the fact this helper waits for.
	answer := make(chan error, 1)
	go func() {
		_, err := stream.Receive()
		answer <- err
	}()
	select {
	case err := <-answer:
		if err != nil {
			return err
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("node %q neither registered nor refused within 3s", nodeID)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := srv.State().Nodes.Lookup(nodeID); ok {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("node %q was served a frame but is not in the registry", nodeID)
	return nil
}

func TestServer_AgentIdentityFromCertificate_Bound(t *testing.T) {
	ca := newTestCA(t)
	srv, logs, stop := startMTLSServer(t, ca, true)
	defer stop()

	t.Run("certificate_and_node_id_agree", func(t *testing.T) {
		if err := registerWithCert(t, ca, srv, "node-a", "node-a"); err != nil {
			t.Fatalf("matching registration refused: %v", err)
		}
	})

	t.Run("mismatch_is_refused_with_permission_denied", func(t *testing.T) {
		err := registerWithCert(t, ca, srv, "node-b", "node-impostor")
		if err == nil {
			t.Fatal("a node_id that is not the certificate's CN registered")
		}
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("code = %v, want PermissionDenied: %v", connect.CodeOf(err), err)
		}
		if !strings.Contains(err.Error(), `"node-impostor"`) || !strings.Contains(err.Error(), `"node-b"`) {
			t.Fatalf("the refusal must name both identities: %v", err)
		}
		if _, ok := srv.State().Nodes.Lookup("node-impostor"); ok {
			t.Fatal("the refused node is in the registry")
		}
		if _, ok := srv.State().Nodes.Lookup("node-b"); ok {
			t.Fatal("the server renamed the node after the certificate instead of refusing")
		}
		if !strings.Contains(logs.String(), "node_id does not match its client certificate") {
			t.Fatalf("the refusal must be logged; log was:\n%s", logs.String())
		}
	})
}

func TestServer_AgentIdentityFromCertificate_Unbound_WarnsAndRegisters(t *testing.T) {
	ca := newTestCA(t)
	srv, logs, stop := startMTLSServer(t, ca, false)
	defer stop()

	t.Run("mismatch_registers_as_declared", func(t *testing.T) {
		if err := registerWithCert(t, ca, srv, "node-c", "node-declared"); err != nil {
			t.Fatalf("unbound server refused a mismatched registration: %v", err)
		}
	})
	if _, ok := srv.State().Nodes.Lookup("node-declared"); !ok {
		t.Fatal("unbound server did not register the declared node_id")
	}
	// Tolerated, never silent: the operator can see which certificate
	// registered under which name and how to stop it.
	got := logs.String()
	for _, want := range []string{"differs from its client certificate", "node_id=node-declared", "certificate_cn=node-c", "--agent-identity-from-cert"} {
		if !strings.Contains(got, want) {
			t.Errorf("WARN lacks %q; log was:\n%s", want, got)
		}
	}
}

func TestServer_AgentIdentityFromCertificate_WithoutMutualTLS_RefusesToStart(t *testing.T) {
	// Even on loopback: the knob promises a binding the listener cannot
	// deliver, so the server must not pretend it holds.
	srv := server.New(server.Config{
		AgentAddr:                    "127.0.0.1:0",
		UIAddr:                       "127.0.0.1:0",
		AgentToken:                   "token",
		AgentIdentityFromCertificate: true,
		Logger:                       discardLogger(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := srv.Run(ctx)
	if err == nil {
		t.Fatal("Run started with AgentIdentityFromCertificate and no client-certificate verification")
	}
	if !strings.Contains(err.Error(), "--agent-client-ca") {
		t.Fatalf("the refusal must say what is missing: %v", err)
	}
}
