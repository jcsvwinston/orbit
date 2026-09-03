package connection

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// echoAgentService records the first registration it receives and answers
// with one frame so the client observes an accepted stream.
type echoAgentService struct {
	mu   sync.Mutex
	seen []string
}

func (s *echoAgentService) Stream(_ context.Context, stream *connect.BidiStream[adminv1.Frame, adminv1.Frame]) error {
	first, err := stream.Receive()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.seen = append(s.seen, first.GetRegistration().GetNodeId())
	s.mu.Unlock()
	return stream.Send(&adminv1.Frame{Body: &adminv1.Frame_Heartbeat{Heartbeat: &adminv1.Heartbeat{}}})
}

func (s *echoAgentService) nodes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.seen...)
}

// startTLSAgentServer runs a fake admin server over TLS with HTTP/2
// enabled (Connect bidi streams need it), plus the /healthz carve-out.
// It also records the Authorization header the probe presented.
func startTLSAgentServer(t *testing.T) (url string, svc *echoAgentService, probeAuth *string, pool *x509.CertPool, stop func()) {
	t.Helper()
	svc = &echoAgentService{}
	var auth string
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle(adminv1connect.NewAgentServiceHandler(svc))

	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()

	pool = x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return srv.URL, svc, &auth, pool, srv.Close
}

// TestDial_HTTPSEndpoint_StreamsOverTLS pins the https:// path end to end:
// the probe and the Connect stream both complete a real TLS handshake.
// Before the fix the dialer's single h2c transport returned a plain TCP
// connection for every scheme, so an https:// endpoint passed the probe
// (a separate client) and then failed every stream.
func TestDial_HTTPSEndpoint_StreamsOverTLS(t *testing.T) {
	url, svc, probeAuth, pool, stop := startTLSAgentServer(t)
	defer stop()

	d := NewDialer(Config{
		Endpoints: []string{url},
		Token:     "s3cret",
		TLSConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		Logger:    discardLogger(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := d.Dial(ctx)
	if err != nil {
		t.Fatalf("Dial https endpoint: %v", err)
	}
	if *probeAuth != "" {
		t.Fatalf("/healthz probe must not carry the bearer (it is auth-exempt), got %q", *probeAuth)
	}

	stream := res.Client.Stream(ctx)
	if err := stream.Send(&adminv1.Frame{Body: &adminv1.Frame_Registration{Registration: &adminv1.NodeRegistration{
		NodeId: "tls-node", StartedAt: timestamppb.Now(),
	}}}); err != nil {
		t.Fatalf("Send over https: %v", err)
	}
	if _, err := stream.Receive(); err != nil {
		t.Fatalf("Receive over https: %v", err)
	}
	if got := svc.nodes(); len(got) != 1 || got[0] != "tls-node" {
		t.Fatalf("server saw registrations %v, want [tls-node]", got)
	}
}

// Without a trust anchor for the server's certificate the https://
// endpoint must be refused, not silently downgraded to cleartext.
func TestDial_HTTPSEndpoint_UntrustedCertificateFails(t *testing.T) {
	url, _, _, _, stop := startTLSAgentServer(t)
	defer stop()

	d := NewDialer(Config{
		Endpoints:          []string{url},
		HealthCheckTimeout: 2 * time.Second,
		Logger:             discardLogger(),
	})
	if _, err := d.Dial(context.Background()); err == nil {
		t.Fatal("Dial must fail when the server certificate is not trusted")
	}
}
