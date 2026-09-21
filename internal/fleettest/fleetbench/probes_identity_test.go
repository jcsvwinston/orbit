// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/observability"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/orbit/agent"
	server "github.com/jcsvwinston/orbit/server"
)

// fieldsNamed lists the exported fields of the struct v whose name contains
// any of substrs (case-insensitive). When tagKey is given, fields whose tag
// under that key is "-" are left out: a field a configuration file cannot
// bind is not a configuration surface. It is how the bench asks a Config
// type "do you offer this knob" without a grep over source.
func fieldsNamed(v any, tagKey string, substrs ...string) []string {
	rt := reflect.TypeOf(v)
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	var out []string
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		if tagKey != "" && f.Tag.Get(tagKey) == "-" {
			continue
		}
		lower := strings.ToLower(f.Name)
		for _, s := range substrs {
			if strings.Contains(lower, strings.ToLower(s)) {
				out = append(out, f.Name)
				break
			}
		}
	}
	return out
}

// certFileKnobs are the field-name fragments a certificate-from-files or
// reload-on-rotation surface would carry. Shared by the rotation and the
// configuration probes so all three ask the same question.
var certFileKnobs = []string{"CertFile", "KeyFile", "CAFile", "ClientCA", "Reload", "Rotat"}

// IDENT-01: the agent listener serves TLS when configured, and a cleartext
// client gets nothing from it.
func probeAgentListenerTLS(t *testing.T, e *env) verdict {
	ca := newTestCA(t)
	srv := e.startServer(t, server.Config{AgentTLS: ca.serverTLS(), AgentToken: "bench-token"})

	// Fact 1: a plain http:// request is not served.
	if resp, err := bareClient().Get("http://" + srv.AgentAddr() + "/healthz"); err == nil {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK || strings.TrimSpace(string(body)) == "ok" {
			t.Logf("cleartext /healthz was served on a TLS listener: %d %q", resp.StatusCode, body)
			return absent
		}
	}

	// Fact 2: a TLS client completes the handshake, negotiates h2 (bidi
	// streams need it) and is served.
	c := ca.clientTLS()
	c.NextProtos = []string{"h2", "http/1.1"}
	conn, err := tls.Dial("tcp", srv.AgentAddr(), c)
	if err != nil {
		t.Logf("tls.Dial: %v", err)
		return partial
	}
	proto := conn.ConnectionState().NegotiatedProtocol
	_ = conn.Close()
	if proto != "h2" {
		t.Logf("negotiated %q, not h2", proto)
		return partial
	}
	resp, err := tlsH2Client(ca.clientTLS()).Get("https://" + srv.AgentAddr() + "/healthz")
	if err != nil {
		t.Logf("https GET /healthz: %v", err)
		return partial
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Logf("https GET /healthz answered %d", resp.StatusCode)
		return partial
	}
	return present
}

// IDENT-02: with mutual TLS configured, a client without a certificate is
// refused at the handshake and one with a CA-signed certificate registers.
func probeAgentListenerMTLS(t *testing.T, e *env) verdict {
	ca := newTestCA(t)
	srv := e.startServer(t, server.Config{AgentTLS: ca.mutualTLS()})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := registerRaw(ctx, tlsH2Client(ca.clientTLS()), "https://"+srv.AgentAddr(), "fb-anon")
	if err == nil {
		_, err = stream.Receive()
	}
	if err == nil {
		t.Log("a stream without a client certificate was accepted")
		return absent
	}
	if _, ok := srv.State().Nodes.Lookup("fb-anon"); ok {
		t.Log("a node without a client certificate registered")
		return absent
	}

	if _, err := registerRaw(ctx, tlsH2Client(ca.clientTLSWithCert(t, "fb-node")), "https://"+srv.AgentAddr(), "fb-mtls"); err != nil {
		t.Logf("registration with a valid client certificate failed: %v", err)
		return partial
	}
	if !waitRegistered(srv.Server, "fb-mtls", 3*time.Second) {
		t.Log("the certificate-bearing client never registered")
		return partial
	}
	return present
}

// IDENT-03: an agent listener off loopback with no authentication refuses
// to start — with no TLS and with a server-only certificate, which encrypts
// but authenticates nobody — while a token or verified client certificates
// pass the guard (whatever the bind then does).
func probeAgentListenerFailsClosed(t *testing.T, e *env) verdict {
	// 192.0.2.1 is documentation space (RFC 5737): no host has it, and the
	// guard runs before any bind, so nothing is ever listened on.
	const exposed = "192.0.2.1:0"
	ca := newTestCA(t)
	refused := func(cfg server.Config) bool {
		_, err := e.tryStartServer(t, cfg)
		return err != nil && strings.Contains(err.Error(), "refusing to start")
	}
	if !refused(server.Config{AgentAddr: exposed}) {
		t.Log("an unauthenticated plaintext agent listener was not refused on a non-loopback address")
		return absent
	}
	if !refused(server.Config{AgentAddr: exposed, AgentTLS: ca.serverTLS()}) {
		t.Log("a server-only certificate (no client verification) was taken for authentication")
		return partial
	}
	if refused(server.Config{AgentAddr: exposed, AgentToken: "bench-token"}) {
		t.Log("the guard refuses a token-authenticated listener too")
		return partial
	}
	if refused(server.Config{AgentAddr: exposed, AgentTLS: ca.mutualTLS()}) {
		t.Log("the guard refuses a mutual-TLS listener too")
		return partial
	}
	return present
}

// IDENT-04: a real agent.Agent connects over mutual TLS and registers; the
// same agent without a certificate never does.
func probeRealAgentOverMTLS(t *testing.T, e *env) verdict {
	ca := newTestCA(t)
	srv := e.startServer(t, server.Config{AgentTLS: ca.mutualTLS()})
	endpoint := "https://" + srv.AgentAddr()

	noCert := e.startAgent(t, agent.Config{Endpoints: []string{endpoint}, TLS: ca.clientTLS(), NodeIDOverride: "fb-nocert"})
	if waitRegistered(srv.Server, "fb-nocert", 1500*time.Millisecond) {
		t.Log("an agent without a client certificate registered through the mTLS listener")
		return absent
	}
	noCert.stop()

	e.startAgent(t, agent.Config{Endpoints: []string{endpoint}, TLS: ca.clientTLSWithCert(t, "fb-agent"), NodeIDOverride: "fb-cert"})
	if !waitRegistered(srv.Server, "fb-cert", 5*time.Second) {
		t.Log("the agent with a valid client certificate did not register in 5s")
		return partial
	}
	return present
}

// IDENT-05: the agent's client certificate can be named in configuration
// (file paths) rather than handed over as a Go value.
func probeAgentCertFromConfig(t *testing.T, e *env) verdict {
	files := fieldsNamed(agent.ExtensionConfig{}, "koanf", certFileKnobs...)
	if len(files) == 0 {
		tlsField, ok := reflect.TypeOf(agent.ExtensionConfig{}).FieldByName("TLS")
		if !ok {
			t.Log("ExtensionConfig has no TLS field at all")
			return absent
		}
		if tlsField.Tag.Get("koanf") == "-" {
			t.Logf("ExtensionConfig.TLS is %s with koanf:\"-\": only code can set it", tlsField.Type)
			return absent
		}
		t.Logf("ExtensionConfig.TLS (%s) is bindable but no file fields exist", tlsField.Type)
		return partial
	}
	t.Logf("ExtensionConfig binds certificate files: %v", files)

	// A field is a name, not a surface. Write the PEM files a deployment
	// ships, give the configuration nothing but their paths and no
	// node_id, and boot a real agent through the extension — the path an
	// application takes — against a server that requires a client
	// certificate. The node the server lists must be the certificate's
	// Common Name: the identity written once, in the certificate.
	ca := newTestCA(t)
	srv := e.startServer(t, server.Config{AgentTLS: ca.mutualTLS()})
	caFile, certFile, keyFile := ca.writePEM(t, t.TempDir(), "fb-node-from-files")
	ext := agent.NewExtension(agent.ExtensionConfig{
		Endpoints:   []string{"https://" + srv.AgentAddr()},
		TLSCertFile: certFile,
		TLSKeyFile:  keyFile,
		TLSCAFile:   caFile,
	}, t.TempDir(), "fleetbench")
	logger := discardLogger()
	if err := ext.Attach(&app.App{Logger: logger, Observability: observability.NewBus(logger)}); err != nil {
		t.Logf("the extension refused the file configuration: %v", err)
		return partial
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = ext.Shutdown(ctx)
	})
	if !waitRegistered(srv.Server, "fb-node-from-files", 5*time.Second) {
		t.Log("the extension accepted the files but no node registered under the certificate's Common Name")
		return partial
	}
	return present
}

// IDENT-06: the node identity is bound to the certificate: an agent whose
// certificate says node-a but declares node_id node-b is refused or
// registered as node-a. A control agent whose name and certificate agree
// proves the setup can register at all.
func probeNodeIdentityBoundToCert(t *testing.T, e *env) verdict {
	ca := newTestCA(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Fact 1 — the product's binding, on the wire. A server that binds
	// identity to the certificate must refuse a certificate for node-a
	// declaring node-b, and say so with PermissionDenied: the refusal is
	// read from the stream, not inferred from a node that never appeared.
	// A control agent whose name and certificate agree proves the setup
	// registers at all.
	bound, err := e.tryStartServer(t, server.Config{AgentTLS: ca.mutualTLS(), AgentIdentityFromCertificate: true})
	if err != nil {
		t.Logf("the server refused to start with identity binding: %v", err)
		return absent
	}
	e.startAgent(t, agent.Config{Endpoints: []string{"https://" + bound.AgentAddr()}, TLS: ca.clientTLSWithCert(t, "node-c"), NodeIDOverride: "node-c"})
	if !waitRegistered(bound.Server, "node-c", 5*time.Second) {
		t.Fatal("the control agent (certificate and node_id agree) did not register: cannot tell refusal from failure")
	}
	stream, err := registerRaw(ctx, tlsH2Client(ca.clientTLSWithCert(t, "node-a")), "https://"+bound.AgentAddr(), "node-b")
	if err == nil {
		_, err = stream.Receive()
	}
	switch {
	case err == nil:
		t.Log("bound server served a certificate for node-a registered as node-b")
		return absent
	case connect.CodeOf(err) != connect.CodePermissionDenied:
		t.Logf("bound server refused the mismatch with %v, not PermissionDenied: %v", connect.CodeOf(err), err)
		return partial
	}
	if _, ok := bound.State().Nodes.Lookup("node-b"); ok {
		t.Log("bound server refused the stream and yet lists node-b")
		return absent
	}

	// Fact 2 — the default, recorded so the page says what it is. A
	// server that only verifies the certificate registers the declared
	// node_id: the binding is opt-in until the next major flips it.
	plain := e.startServer(t, server.Config{AgentTLS: ca.mutualTLS()})
	e.startAgent(t, agent.Config{Endpoints: []string{"https://" + plain.AgentAddr()}, TLS: ca.clientTLSWithCert(t, "node-a"), NodeIDOverride: "node-b"})
	if !waitRegistered(plain.Server, "node-b", 5*time.Second) {
		t.Log("the default server no longer registers a mismatched node_id: the note on the page is stale")
		return partial
	}
	t.Log("default: registered as declared (WARN in the server log); bound: refused with PermissionDenied")
	return present
}

// certRecorder remembers the Common Names of the client certificates a
// listener saw, through the tls.Config hook the PROBE owns.
type certRecorder struct {
	mu  sync.Mutex
	cns map[string]bool
}

func (r *certRecorder) record(raw [][]byte, _ [][]*x509.Certificate) error {
	if len(raw) == 0 {
		return nil
	}
	cert, err := x509.ParseCertificate(raw[0])
	if err != nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cns == nil {
		r.cns = map[string]bool{}
	}
	r.cns[cert.Subject.CommonName] = true
	return nil
}

func (r *certRecorder) saw(cn string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cns[cn]
}

// IDENT-07: the server's certificate can be rotated without a restart.
// The generic Go path (a tls.Config whose GetCertificate answers from a
// source the caller swaps) is measured by handshake; the PRODUCT surface —
// a knob that reloads the files the binary was started with — by
// reflection on server.Config.
func probeServerCertRotation(t *testing.T, e *env) verdict {
	ca := newTestCA(t)
	v1, _ := ca.issue(t, "server-v1", x509.ExtKeyUsageServerAuth)
	v2, _ := ca.issue(t, "server-v2", x509.ExtKeyUsageServerAuth)
	var current atomic.Pointer[tls.Certificate]
	current.Store(&v1)
	cfg := &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return current.Load(), nil },
	}
	srv := e.startServer(t, server.Config{AgentTLS: cfg, AgentToken: "bench-token"})

	seen := func() string {
		conn, err := tls.Dial("tcp", srv.AgentAddr(), &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // the probe reads the leaf, it trusts nothing
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
	if got := seen(); got != "server-v1" {
		t.Fatalf("first handshake saw %q, want server-v1", got)
	}
	current.Store(&v2)
	if got := seen(); got != "server-v2" {
		t.Logf("after the swap the handshake still saw %q: the certificate is fixed when the listener is built", got)
		return absent
	}
	// A knob that appears is a name, not a measured rotation: the verdict
	// stays partial until this probe rotates the files and handshakes again.
	if knobs := fieldsNamed(server.Config{}, "", certFileKnobs...); len(knobs) > 0 {
		t.Logf("server.Config offers %v: extend this probe to rotate the files and handshake again", knobs)
	}
	return partial
}

// IDENT-08: the agent picks up a new client certificate for its next
// connection without a restart. Two mTLS servers record the certificates
// they see; the agent's tls.Config answers GetClientCertificate from a
// source the probe swaps between the first connection and the failover.
func probeAgentCertRotation(t *testing.T, e *env) verdict {
	ca := newTestCA(t)
	recA, recB := &certRecorder{}, &certRecorder{}
	mutual := func(rec *certRecorder) *tls.Config {
		c := ca.mutualTLS()
		c.VerifyPeerCertificate = rec.record
		return c
	}
	// Server A sits behind a relay so that "A dies" severs the agent's
	// connection the way a process death would (see env_test.go: an
	// in-process stop leaves an open bidi stream serving).
	srvA := e.startServer(t, server.Config{AgentTLS: mutual(recA)})
	relayA := startRelay(t, srvA.AgentAddr())
	srvB := e.startServer(t, server.Config{AgentTLS: mutual(recB)})

	c1 := ca.clientTLSWithCert(t, "agent-v1").Certificates[0]
	c2 := ca.clientTLSWithCert(t, "agent-v2").Certificates[0]
	var current atomic.Pointer[tls.Certificate]
	current.Store(&c1)
	tlsCfg := ca.clientTLS()
	tlsCfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return current.Load(), nil }

	const node = "fb-rotating"
	e.startAgent(t, agent.Config{
		Endpoints:      []string{"https://" + relayA.addr(), "https://" + srvB.AgentAddr()},
		TLS:            tlsCfg,
		NodeIDOverride: node,
	})
	if !waitRegistered(srvA.Server, node, 5*time.Second) {
		t.Fatal("the agent did not register on the first server")
	}
	if !recA.saw("agent-v1") {
		t.Fatal("the first server did not see the agent-v1 certificate")
	}

	current.Store(&c2)
	srvA.stop()
	relayA.close()
	if !waitRegistered(srvB.Server, node, 10*time.Second) {
		t.Fatal("the agent did not fail over to the second server in 10s")
	}
	switch {
	case recB.saw("agent-v2"):
		// A knob that appears is a name, not a measured rotation: partial
		// until this probe rotates the files instead of the callback.
		if knobs := fieldsNamed(agent.ExtensionConfig{}, "koanf", certFileKnobs...); len(knobs) > 0 {
			t.Logf("ExtensionConfig offers %v: extend this probe to rotate the files instead of the callback", knobs)
		}
		return partial
	case recB.saw("agent-v1"):
		t.Log("the second server saw agent-v1: the certificate was fixed when the agent was built")
		return absent
	default:
		t.Fatal("the second server recorded no client certificate")
		return absent
	}
}

// IDENT-09: a shared token authenticates an agent; a wrong one is refused
// with a rate-limited, operator-facing warning that names the caller. The
// wrong token is presented three times by raw streams, so "exactly one
// warning" is measured against a known number of refusals rather than
// against however many times an agent happened to retry.
func probeSharedTokenAuth(t *testing.T, e *env) verdict {
	logs := &logBuffer{}
	srv := e.startServer(t, server.Config{AgentToken: "right-token", Logger: logs.logger()})
	endpoint := "http://" + srv.AgentAddr()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 3; i++ {
		stream, err := registerRaw(ctx, h2cClient(), endpoint, "fb-wrong", connect.WithInterceptors(bearer("wrong-token")))
		if err == nil {
			_, err = stream.Receive()
		}
		if err == nil {
			t.Log("a stream with the wrong token was accepted")
			return absent
		}
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Logf("refusal %d is not Unauthenticated: %v", i+1, err)
			return partial
		}
	}
	if _, ok := srv.State().Nodes.Lookup("fb-wrong"); ok {
		t.Log("a node with the wrong token registered")
		return absent
	}
	text := logs.String()
	line := ""
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, "rejected agent request") {
			line = l
			break
		}
	}
	if line == "" {
		t.Log("the refusal left no operator-facing log line")
		return partial
	}
	if !strings.Contains(line, "level=WARN") || !strings.Contains(line, "remote_ip=") {
		t.Logf("the refusal line is not a WARN with the remote IP: %q", line)
		return partial
	}
	if n := strings.Count(text, "rejected agent request"); n != 1 {
		t.Logf("three refusals inside one minute were logged %d times: not rate-limited", n)
		return partial
	}

	e.startAgent(t, agent.Config{Endpoints: []string{endpoint}, Token: "right-token", NodeIDOverride: "fb-right"})
	if !waitRegistered(srv.Server, "fb-right", 4*time.Second) {
		t.Log("the agent with the right token did not register")
		return partial
	}
	return present
}

// IDENT-10: /healthz answers without credentials on both listeners, while
// the same listeners refuse an uncredentialled RPC.
func probeHealthzWithoutCredentials(t *testing.T, e *env) verdict {
	srv := e.startServer(t, server.Config{AgentToken: "bench-token", UIBearerToken: "ui-token"})
	answered := 0
	for _, addr := range []string{srv.AgentAddr(), srv.UIAddr()} {
		resp, err := bareClient().Get("http://" + addr + "/healthz")
		if err != nil {
			t.Logf("GET http://%s/healthz: %v", addr, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK && strings.TrimSpace(string(body)) == "ok" {
			answered++
		} else {
			t.Logf("GET http://%s/healthz answered %d %q", addr, resp.StatusCode, body)
		}
	}
	switch answered {
	case 0:
		return absent
	case 1:
		return partial
	}
	// Negative controls: the exemption is an exemption only if each
	// listener otherwise asks for credentials.
	resp, err := bareClient().Post(uiURL(srv.Server)+"/nucleus.admin.v1.ControlService/GetSelf", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST GetSelf: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Log("the UI listener answered GetSelf without any credential: /healthz is not an exemption, the listener is open")
		return partial
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := registerRaw(ctx, h2cClient(), "http://"+srv.AgentAddr(), "fb-untokened")
	if err == nil {
		_, err = stream.Receive()
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Logf("the agent listener took a stream without a token (err=%v): /healthz is not an exemption there", err)
		return partial
	}
	return present
}
