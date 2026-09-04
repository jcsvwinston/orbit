package server_test

// The listener-level TLS tests. For several releases --agent-cert/--ui-cert
// loaded a certificate onto http.Server.TLSConfig and then served h2c in
// the clear (Serve ignores TLSConfig); nothing noticed because no test ever
// dialed the listener with TLS. These tests do exactly that, against a
// certificate minted in-process.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
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

// testCA is a throwaway CA plus one leaf it signed for 127.0.0.1.
type testCA struct {
	pool     *x509.CertPool
	caCert   *x509.Certificate
	caKey    *ecdsa.PrivateKey
	leaf     tls.Certificate
	leafCert *x509.Certificate
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "orbit-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	ca := &testCA{pool: pool, caCert: caCert, caKey: caKey}
	ca.leaf, ca.leafCert = ca.issue(t, "127.0.0.1", x509.ExtKeyUsageServerAuth, net.ParseIP("127.0.0.1"))
	return ca
}

// issue signs a leaf for cn with the given extended key usage.
func (ca *testCA) issue(t *testing.T, cn string, eku x509.ExtKeyUsage, ips ...net.IP) (tls.Certificate, *x509.Certificate) {
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
		ExtKeyUsage:  []x509.ExtKeyUsage{eku},
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.caCert, &key.PublicKey, ca.caKey)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: parsed}, parsed
}

func startTLSServer(t *testing.T, agentTLS, uiTLS *tls.Config, token string) (*server.Server, func()) {
	t.Helper()
	srv := server.New(server.Config{
		AgentAddr:  "127.0.0.1:0",
		UIAddr:     "127.0.0.1:0",
		AgentTLS:   agentTLS,
		UITLS:      uiTLS,
		AgentToken: token,
		Logger:     discardLogger(),
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
	return srv, func() {
		cancel()
		select {
		case <-doneCh:
		case <-time.After(3 * time.Second):
			t.Error("server did not shut down")
		}
	}
}

// tlsH2Client is an HTTP/2-over-TLS client trusting the test CA.
func tlsH2Client(cfg *tls.Config) *http.Client {
	return &http.Client{Transport: &http2.Transport{TLSClientConfig: cfg}}
}

func TestServer_TLSListeners_HandshakeAndRefuseCleartext(t *testing.T) {
	ca := newTestCA(t)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{ca.leaf}, MinVersion: tls.VersionTLS12}
	srv, stop := startTLSServer(t, serverTLS.Clone(), serverTLS.Clone(), "t0k3n")
	defer stop()

	clientTLS := &tls.Config{RootCAs: ca.pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}

	for name, addr := range map[string]string{"agent": srv.AgentAddr(), "ui": srv.UIAddr()} {
		t.Run(name+"_tls_handshake", func(t *testing.T) {
			// (a) a TLS client completes the handshake and negotiates h2.
			c := clientTLS.Clone()
			c.NextProtos = []string{"h2", "http/1.1"}
			conn, err := tls.Dial("tcp", addr, c)
			if err != nil {
				t.Fatalf("tls.Dial %s: %v", addr, err)
			}
			defer conn.Close()
			if got := conn.ConnectionState().NegotiatedProtocol; got != "h2" {
				t.Fatalf("negotiated %q, want h2 (Connect bidi streams need HTTP/2)", got)
			}
		})
		t.Run(name+"_cleartext_refused", func(t *testing.T) {
			// (b) a plain http:// request is not served: the TLS layer
			// rejects the record (Go answers a fixed 400 when the bytes
			// look like an HTTP request). Whatever the shape, /healthz
			// must never answer "ok" in the clear.
			cli := &http.Client{Timeout: 2 * time.Second}
			resp, err := cli.Get("http://" + addr + "/healthz")
			if err != nil {
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode == http.StatusOK || strings.TrimSpace(string(body)) == "ok" {
				t.Fatalf("cleartext request was served on a TLS listener: status=%d body=%q", resp.StatusCode, body)
			}
		})
	}

	t.Run("healthz_over_https", func(t *testing.T) {
		cli := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLS.Clone()}, Timeout: 2 * time.Second}
		resp, err := cli.Get("https://" + srv.UIAddr() + "/healthz")
		if err != nil {
			t.Fatalf("https GET /healthz: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d, want 200", resp.StatusCode)
		}
	})

	t.Run("agent_stream_over_https", func(t *testing.T) {
		client := adminv1connect.NewAgentServiceClient(tlsH2Client(clientTLS.Clone()), "https://"+srv.AgentAddr(),
			connect.WithInterceptors(bearer("t0k3n")))
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		stream := client.Stream(ctx)
		if err := stream.Send(&adminv1.Frame{Body: &adminv1.Frame_Registration{Registration: &adminv1.NodeRegistration{
			NodeId: "tls-node", Version: "test", StartedAt: timestamppb.Now(),
		}}}); err != nil {
			t.Fatalf("send registration over TLS: %v", err)
		}
		// The registry sees the node only once the server accepted the
		// stream through the TLS listener.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, ok := srv.State().Nodes.Lookup("tls-node"); ok {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("agent did not register through the TLS listener")
	})
}

func TestServer_AgentMutualTLS_RequiresClientCertificate(t *testing.T) {
	ca := newTestCA(t)
	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{ca.leaf},
		MinVersion:   tls.VersionTLS12,
		ClientCAs:    ca.pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	// No token: mutual TLS is the only authentication on this listener.
	srv, stop := startTLSServer(t, serverTLS, nil, "")
	defer stop()

	base := &tls.Config{RootCAs: ca.pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}

	t.Run("without_client_cert_is_rejected", func(t *testing.T) {
		client := adminv1connect.NewAgentServiceClient(tlsH2Client(base.Clone()), "https://"+srv.AgentAddr())
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		stream := client.Stream(ctx)
		err := stream.Send(&adminv1.Frame{Body: &adminv1.Frame_Registration{Registration: &adminv1.NodeRegistration{NodeId: "anon"}}})
		if err == nil {
			_, err = stream.Receive()
		}
		if err == nil {
			t.Fatal("stream without a client certificate must fail the handshake")
		}
		if _, ok := srv.State().Nodes.Lookup("anon"); ok {
			t.Fatal("unauthenticated agent registered through an mTLS listener")
		}
	})

	t.Run("with_client_cert_connects", func(t *testing.T) {
		leaf, _ := ca.issue(t, "node-42", x509.ExtKeyUsageClientAuth)
		cfg := base.Clone()
		cfg.Certificates = []tls.Certificate{leaf}
		client := adminv1connect.NewAgentServiceClient(tlsH2Client(cfg), "https://"+srv.AgentAddr())
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		stream := client.Stream(ctx)
		if err := stream.Send(&adminv1.Frame{Body: &adminv1.Frame_Registration{Registration: &adminv1.NodeRegistration{
			NodeId: "mtls-node", StartedAt: timestamppb.Now(),
		}}}); err != nil {
			t.Fatalf("send with client certificate: %v", err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, ok := srv.State().Nodes.Lookup("mtls-node"); ok {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("agent with a valid client certificate did not register")
	})
}

// bearer is a minimal streaming-aware interceptor for the agent side.
type bearer string

func (b bearer) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		req.Header().Set("Authorization", "Bearer "+string(b))
		return next(ctx, req)
	}
}

func (b bearer) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		conn.RequestHeader().Set("Authorization", "Bearer "+string(b))
		return conn
	}
}

func (b bearer) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
