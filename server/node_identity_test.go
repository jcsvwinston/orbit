package server_test

// The node identity is the certificate's, not the agent's word. A mutual-TLS
// listener verifies who holds the certificate and then, until this test
// existed, registered the node under whatever node_id the first frame
// declared — an agent with a certificate for node-a could sit in the fleet
// as node-b. These tests pin both regimes of a listener that verifies client
// certificates: bound, the default (a mismatch is refused with
// PermissionDenied, a match registers; AgentIdentityFromCertificate states
// the same thing explicitly) and unbound, which AllowSharedAgentCertificate
// asks for (registered as declared, but never silently).

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

// startMTLSServerWith runs a server whose agent listener requires a client
// certificate signed by ca, with the identity fields left to the caller,
// logging into the returned buffer at WARN and above: what the tests assert
// is that an operator reading a production log sees it.
func startMTLSServerWith(t *testing.T, ca *testCA, identity func(*server.Config)) (*server.Server, *syncBuffer, func()) {
	t.Helper()
	logs := &syncBuffer{}
	cfg := server.Config{
		AgentAddr: "127.0.0.1:0",
		UIAddr:    "127.0.0.1:0",
		AgentTLS: &tls.Config{
			Certificates: []tls.Certificate{ca.leaf},
			MinVersion:   tls.VersionTLS12,
			ClientCAs:    ca.pool,
			ClientAuth:   tls.RequireAndVerifyClientCert,
		},
		Logger: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	identity(&cfg)
	srv := server.New(cfg)
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

// A listener that verifies client certificates binds the node identity to
// them (DEP-2026-001): with neither identity field set, and with
// AgentIdentityFromCertificate stating it, the same two facts hold.
func TestServer_AgentIdentityFromCertificate_Bound(t *testing.T) {
	ca := newTestCA(t)
	for name, identity := range map[string]func(*server.Config){
		"by_default":          func(*server.Config) {},
		"stated_by_the_field": func(c *server.Config) { c.AgentIdentityFromCertificate = true },
	} {
		t.Run(name, func(t *testing.T) {
			srv, logs, stop := startMTLSServerWith(t, ca, identity)
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
		})
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

// The binding is the default under mutual TLS, so there is nothing to ask
// the deployment at start: the 1.x WARN naming the two fields (the one the
// DEP-2026-001 window existed for) is gone in every regime. The WARN that
// stays is the per-registration one, on a tolerated mismatch.
func TestServer_MutualTLS_BindingChoiceIsQuietAtStartup(t *testing.T) {
	ca := newTestCA(t)
	const announced = "does not bind node identity"

	for name, identity := range map[string]func(*server.Config){
		"neither_field_set":          func(*server.Config) {},
		"binding_on":                 func(c *server.Config) { c.AgentIdentityFromCertificate = true },
		"shared_certificate_allowed": func(c *server.Config) { c.AllowSharedAgentCertificate = true },
	} {
		t.Run(name, func(t *testing.T) {
			_, logs, stop := startMTLSServerWith(t, ca, identity)
			defer stop()
			if got := logs.String(); strings.Contains(got, announced) || strings.Contains(got, "DEP-2026-001") {
				t.Fatalf("the server was asked to choose a binding at start; log was:\n%s", got)
			}
		})
	}
}

// AllowSharedAgentCertificate is the one way to register a node_id that is
// not the certificate's Common Name on a listener that verifies client
// certificates: registered as declared, with the WARN naming both
// identities and what tolerates it — never silently.
func TestServer_AllowSharedAgentCertificate_KeepsTheDeclaredNodeID(t *testing.T) {
	ca := newTestCA(t)
	srv, logs, stop := startMTLSServerWith(t, ca, func(c *server.Config) { c.AllowSharedAgentCertificate = true })
	defer stop()

	if err := registerWithCert(t, ca, srv, "node-d", "node-shared"); err != nil {
		t.Fatalf("a server allowing a shared certificate refused a mismatched registration: %v", err)
	}
	if _, ok := srv.State().Nodes.Lookup("node-shared"); !ok {
		t.Fatal("the declared node_id is not in the registry")
	}
	got := logs.String()
	for _, want := range []string{"differs from its client certificate", "node_id=node-shared", "certificate_cn=node-d", "--allow-shared-agent-certificate"} {
		if !strings.Contains(got, want) {
			t.Errorf("the tolerated mismatch is not in the log (%q missing); log was:\n%s", want, got)
		}
	}
}

func TestServer_AllowSharedAgentCertificate_ContradictsBinding_RefusesToStart(t *testing.T) {
	// Even without mutual TLS: the two fields answer the same question in
	// opposite ways, and the server must not pick one in silence.
	srv := server.New(server.Config{
		AgentAddr:                    "127.0.0.1:0",
		UIAddr:                       "127.0.0.1:0",
		AgentToken:                   "token",
		AgentIdentityFromCertificate: true,
		AllowSharedAgentCertificate:  true,
		Logger:                       discardLogger(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := srv.Run(ctx)
	if err == nil {
		t.Fatal("Run started with AgentIdentityFromCertificate and AllowSharedAgentCertificate both set")
	}
	for _, want := range []string{"--agent-identity-from-cert", "--allow-shared-agent-certificate", "contradict"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name both fields (%q missing): %v", want, err)
		}
	}
}
