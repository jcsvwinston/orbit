// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/nucleus/pkg/observability"
	"github.com/jcsvwinston/nucleus/pkg/observe"

	"github.com/jcsvwinston/orbit/agent"
	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
	server "github.com/jcsvwinston/orbit/server"
)

// operatorName is who the UI clients say they are. The server trusts
// X-Auth-* headers from loopback (its trusted-proxy default), which is the
// same door the fleet integration tests walk through.
const operatorName = "bench-operator"

// env is the harness every probe shares: nothing is booted up front. A
// probe starts the server(s) and agent(s) it needs with the configuration
// it needs, and everything it starts is stopped when the probe ends. The
// fleet plane has no single "application" the way the panel has; the
// controls here disagree on TLS, tokens, allowlists and ports, so one
// shared server would measure the harness's compromise, not the product.
type env struct {
	tb testing.TB

	rootOnce sync.Once
	root     string
}

func newEnv(tb testing.TB) *env { return &env{tb: tb} }

func discardLogger() *slog.Logger {
	if os.Getenv("ADMIN_SERVER_DEBUG") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ---- server ---------------------------------------------------------------

// runningServer is one server.Server the probe started, with the handle
// that stops it.
type runningServer struct {
	*server.Server
	cancel context.CancelFunc
	done   chan error
	once   sync.Once
}

// stop shuts the server down and waits for Run to return. Idempotent, so a
// probe that stops a server on purpose does not fight the cleanup.
func (r *runningServer) stop() {
	r.once.Do(func() {
		r.cancel()
		select {
		case <-r.done:
		case <-time.After(5 * time.Second):
		}
	})
}

// startServer boots a real server.Server with the given config, waits for
// both listeners to bind and registers a cleanup that stops it. Empty
// addresses default to loopback ephemeral ports; a nil Logger discards.
func (e *env) startServer(t *testing.T, cfg server.Config) *runningServer {
	t.Helper()
	rs, err := e.tryStartServer(t, cfg)
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	return rs
}

// tryStartServer is startServer without the fatal: it returns the error
// Run produced when the server refused to start or could not bind, which
// is itself a measurement for the fail-closed controls.
func (e *env) tryStartServer(t *testing.T, cfg server.Config) (*runningServer, error) {
	t.Helper()
	if strings.TrimSpace(cfg.AgentAddr) == "" {
		cfg.AgentAddr = "127.0.0.1:0"
	}
	if strings.TrimSpace(cfg.UIAddr) == "" {
		cfg.UIAddr = "127.0.0.1:0"
	}
	if cfg.Logger == nil {
		cfg.Logger = discardLogger()
	}
	srv := server.New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	rs := &runningServer{Server: srv, cancel: cancel, done: done}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if srv.AgentAddr() != "" && srv.UIAddr() != "" {
			// Only a server that bound gets a cleanup: one whose Run
			// already returned has nothing left to stop.
			t.Cleanup(rs.stop)
			return rs, nil
		}
		select {
		case err := <-done:
			cancel()
			if err == nil {
				err = errors.New("server.Run returned before binding")
			}
			return nil, err
		case <-time.After(10 * time.Millisecond):
		}
	}
	rs.stop()
	return nil, errors.New("server did not bind its listeners in 3s")
}

// ---- TCP relay ------------------------------------------------------------------------
//
// Stopping a server.Server in-process is not the same as its process
// dying: Run ends with http.Server.Shutdown, which closes the listener and
// waits for connections to go idle, and an open bidi stream never does.
// The handler goroutines keep serving the agent on the old connection, so
// an agent "connected" to a stopped server notices nothing. A real
// restart kills the TCP connections too. The relay is how the harness
// does that: the agent dials the relay, the relay forwards to whichever
// server is current, and a restart drops every forwarded connection and
// points the relay at the new server. What the agent sees is what it
// would see from a process that died and came back on the same address.

type relay struct {
	ln     net.Listener
	mu     sync.Mutex
	target string
	conns  map[net.Conn]struct{}
	closed bool
}

// startRelay listens on an ephemeral loopback port and forwards every
// connection to target until retargeted or closed.
func startRelay(t *testing.T, target string) *relay {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("relay listen: %v", err)
	}
	r := &relay{ln: ln, target: target, conns: map[net.Conn]struct{}{}}
	go r.serve()
	t.Cleanup(r.close)
	return r
}

func (r *relay) addr() string { return r.ln.Addr().String() }

func (r *relay) serve() {
	for {
		client, err := r.ln.Accept()
		if err != nil {
			return
		}
		r.mu.Lock()
		target, closed := r.target, r.closed
		if !closed {
			r.conns[client] = struct{}{}
		}
		r.mu.Unlock()
		if closed {
			_ = client.Close()
			return
		}
		go r.forward(client, target)
	}
}

func (r *relay) forward(client net.Conn, target string) {
	upstream, err := net.DialTimeout("tcp", target, 2*time.Second)
	if err != nil {
		r.forget(client)
		_ = client.Close()
		return
	}
	r.mu.Lock()
	r.conns[upstream] = struct{}{}
	r.mu.Unlock()
	var wg sync.WaitGroup
	wg.Add(2)
	pipe := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		_ = dst.Close()
		_ = src.Close()
	}
	go pipe(upstream, client)
	go pipe(client, upstream)
	wg.Wait()
	r.forget(client)
	r.forget(upstream)
}

func (r *relay) forget(c net.Conn) {
	r.mu.Lock()
	delete(r.conns, c)
	r.mu.Unlock()
}

// retarget points new connections at a different server.
func (r *relay) retarget(target string) {
	r.mu.Lock()
	r.target = target
	r.mu.Unlock()
}

// dropConnections severs every forwarded connection, both ends: the
// agent's next frame fails and its next dial goes to the current target.
func (r *relay) dropConnections() {
	r.mu.Lock()
	conns := make([]net.Conn, 0, len(r.conns))
	for c := range r.conns {
		conns = append(conns, c)
	}
	r.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

// close stops accepting and drops every connection: the address is dead.
func (r *relay) close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()
	_ = r.ln.Close()
	r.dropConnections()
}

// freePort asks the kernel for an ephemeral loopback port and hands it
// back closed, for the probes that need a listener address BEFORE the
// process that binds it exists (the agent's metrics listener).
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	return port
}

// ---- agent ----------------------------------------------------------------

// runningAgent is one agent.Agent the probe started, with its bus (the
// probe emits events on it the way a framework would) and the handle that
// stops it.
type runningAgent struct {
	*agent.Agent
	bus    *observability.Bus
	cancel context.CancelFunc
	done   chan error
	once   sync.Once
}

func (r *runningAgent) stop() {
	r.once.Do(func() {
		r.cancel()
		select {
		case <-r.done:
		case <-time.After(5 * time.Second):
		}
	})
}

// startAgent constructs and runs a real agent.Agent. It does NOT wait for
// registration: the probes disagree on whether registration is expected.
// Defaults fill what every probe would otherwise repeat: a fresh bus, a
// temporary state dir, a fast heartbeat and a short drain.
func (e *env) startAgent(t *testing.T, cfg agent.Config) *runningAgent {
	t.Helper()
	if cfg.Bus == nil {
		cfg.Bus = observability.NewBus(discardLogger())
	}
	if cfg.StateDir == "" {
		cfg.StateDir = t.TempDir()
	}
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 100 * time.Millisecond
	}
	if cfg.DrainTimeout == 0 {
		cfg.DrainTimeout = 500 * time.Millisecond
	}
	if cfg.Logger == nil {
		cfg.Logger = discardLogger()
	}
	ag, err := agent.New(cfg)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ag.Run(ctx) }()
	ra := &runningAgent{Agent: ag, bus: cfg.Bus, cancel: cancel, done: done}
	t.Cleanup(ra.stop)
	return ra
}

// waitRegistered polls the server's registry for nodeID.
func waitRegistered(srv *server.Server, nodeID string, within time.Duration) bool {
	return pollUntil(within, func() bool {
		_, ok := srv.State().Nodes.Lookup(nodeID)
		return ok
	})
}

// pollUntil evaluates cond every 20 ms until it holds or within elapses.
func pollUntil(within time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(within)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ---- agent database ---------------------------------------------------------

// TestArticle is the model the Data Studio probes browse and mutate: the
// same shape the fleet integration tests use, so a probe here and a test
// there disagree only when the product does.
type TestArticle struct {
	ID    uint   `db:"pk;column:id" json:"id"`
	Title string `db:"column:title;required" json:"title"`
	Body  string `db:"column:body" json:"body"`
}

// TestTenantArticle declares a tenant column the way the framework
// documents it (`tenant` in the db tag), so the tenancy probe can ask
// whether the fleet plane honours the declaration.
type TestTenantArticle struct {
	ID       uint   `db:"pk;column:id" json:"id"`
	TenantID string `db:"column:tenant_id;tenant" json:"tenant_id"`
	Title    string `db:"column:title;required" json:"title"`
}

// agentDBURL keeps the fast lane fast: in-memory SQLite. The fleet
// integration suite's ORBIT_TEST_DATASTUDIO_URL override is honoured so
// the bench can be pointed at a real engine the same way.
func agentDBURL() string {
	if u := strings.TrimSpace(os.Getenv("ORBIT_TEST_DATASTUDIO_URL")); u != "" {
		return u
	}
	return "sqlite://:memory:"
}

func articleDDL(system string) (create, insert string) {
	switch system {
	case "postgresql", "postgres":
		return `CREATE TABLE test_articles (
			id SERIAL PRIMARY KEY,
			title TEXT NOT NULL,
			body TEXT
		)`, `INSERT INTO test_articles (id, title, body) VALUES ($1, $2, $3)`
	case "mysql", "mariadb":
		return `CREATE TABLE test_articles (
			id INT AUTO_INCREMENT PRIMARY KEY,
			title VARCHAR(255) NOT NULL,
			body TEXT
		)`, `INSERT INTO test_articles (id, title, body) VALUES (?, ?, ?)`
	default: // sqlite
		return `CREATE TABLE test_articles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			body TEXT
		)`, `INSERT INTO test_articles (id, title, body) VALUES (?, ?, ?)`
	}
}

func tenantArticleDDL(system string) (create, insert string) {
	switch system {
	case "postgresql", "postgres":
		return `CREATE TABLE test_tenant_articles (
			id SERIAL PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			title TEXT NOT NULL
		)`, `INSERT INTO test_tenant_articles (id, tenant_id, title) VALUES ($1, $2, $3)`
	case "mysql", "mariadb":
		return `CREATE TABLE test_tenant_articles (
			id INT AUTO_INCREMENT PRIMARY KEY,
			tenant_id VARCHAR(64) NOT NULL,
			title VARCHAR(255) NOT NULL
		)`, `INSERT INTO test_tenant_articles (id, tenant_id, title) VALUES (?, ?, ?)`
	default:
		return `CREATE TABLE test_tenant_articles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id TEXT NOT NULL,
			title TEXT NOT NULL
		)`, `INSERT INTO test_tenant_articles (id, tenant_id, title) VALUES (?, ?, ?)`
	}
}

// agentDB opens the agent's database with three seeded TestArticle rows
// and, when withTenant is set, a TestTenantArticle table with two rows
// for tenant "a" and one for tenant "b". Returns the handle and the model
// registry the agent registers with the server.
func (e *env) agentDB(t *testing.T, withTenant bool) (*db.DB, *model.Registry) {
	t.Helper()
	logger := observe.NewLogger("error", "text")
	d, err := db.New(db.Config{
		Engine:          db.EngineSQL,
		DatabaseURL:     agentDBURL(),
		DatabaseMaxOpen: 1,
		DatabaseMaxIdle: 1,
	}, logger)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	sqlDB, err := d.SqlDB()
	if err != nil {
		t.Fatal(err)
	}
	system := d.System()
	create, insert := articleDDL(system)
	mustExec(t, sqlDB, `DROP TABLE IF EXISTS test_articles`)
	mustExec(t, sqlDB, create)
	for i := 1; i <= 3; i++ {
		mustExec(t, sqlDB, insert, i, "seed article "+strconv.Itoa(i), "body "+strconv.Itoa(i))
	}
	if system == "postgresql" || system == "postgres" {
		mustExec(t, sqlDB, `SELECT setval(pg_get_serial_sequence('test_articles', 'id'), (SELECT MAX(id) FROM test_articles))`)
	}

	reg := model.NewRegistry()
	if err := reg.Register(&TestArticle{}); err != nil {
		t.Fatalf("register TestArticle: %v", err)
	}
	if withTenant {
		create, insert = tenantArticleDDL(system)
		mustExec(t, sqlDB, `DROP TABLE IF EXISTS test_tenant_articles`)
		mustExec(t, sqlDB, create)
		mustExec(t, sqlDB, insert, 1, "a", "tenant a, first")
		mustExec(t, sqlDB, insert, 2, "a", "tenant a, second")
		mustExec(t, sqlDB, insert, 3, "b", "tenant b, only")
		if system == "postgresql" || system == "postgres" {
			mustExec(t, sqlDB, `SELECT setval(pg_get_serial_sequence('test_tenant_articles', 'id'), (SELECT MAX(id) FROM test_tenant_articles))`)
		}
		if err := reg.Register(&TestTenantArticle{}); err != nil {
			t.Fatalf("register TestTenantArticle: %v", err)
		}
	}
	return d, reg
}

func mustExec(t *testing.T, sqlDB *sql.DB, q string, args ...interface{}) {
	t.Helper()
	if _, err := sqlDB.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// startPair boots a plaintext server with the given mutation allowlist and
// a real agent carrying the seeded model registry, and waits until the
// server lists the agent. This is the Data Studio probes' common ground.
func (e *env) startPair(t *testing.T, allowedModels ...string) (*runningServer, *runningAgent) {
	t.Helper()
	srv := e.startServer(t, server.Config{DataStudioAllowedModels: allowedModels})
	d, reg := e.agentDB(t, false)
	ag := e.startAgent(t, agent.Config{
		Endpoints: []string{"http://" + srv.AgentAddr()},
		Registry:  reg,
		Databases: map[string]*db.DB{"default": d},
	})
	if !waitRegistered(srv.Server, ag.NodeID(), 4*time.Second) {
		t.Fatalf("agent did not register with the server in 4s")
	}
	return srv, ag
}

// ---- UI clients -----------------------------------------------------------------

// headerInjector stamps the trusted-proxy identity headers on every
// request, the way an auth-aware reverse proxy in front of the UI listener
// would.
type headerInjector struct {
	next    http.RoundTripper
	headers map[string]string
}

func (h *headerInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	return h.next.RoundTrip(req)
}

// uiClient talks to the UI listener as a trusted operator. extra headers
// ride along (X-Auth-Role: viewer for the read-only probes).
func uiClient(extra map[string]string) *http.Client {
	headers := map[string]string{"X-Auth-User": operatorName}
	for k, v := range extra {
		headers[k] = v
	}
	return &http.Client{Transport: &headerInjector{
		next:    http.DefaultTransport.(*http.Transport).Clone(),
		headers: headers,
	}}
}

// bareClient carries no credential at all.
func bareClient() *http.Client {
	return &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone(), Timeout: 3 * time.Second}
}

func uiURL(srv *server.Server) string { return "http://" + srv.UIAddr() }

func (e *env) control(srv *server.Server) adminv1connect.ControlServiceClient {
	return adminv1connect.NewControlServiceClient(uiClient(nil), uiURL(srv))
}

func (e *env) controlAs(srv *server.Server, headers map[string]string) adminv1connect.ControlServiceClient {
	return adminv1connect.NewControlServiceClient(uiClient(headers), uiURL(srv))
}

func (e *env) dataStudio(srv *server.Server) adminv1connect.DataStudioServiceClient {
	return adminv1connect.NewDataStudioServiceClient(uiClient(nil), uiURL(srv))
}

func (e *env) dataStudioAs(srv *server.Server, headers map[string]string) adminv1connect.DataStudioServiceClient {
	return adminv1connect.NewDataStudioServiceClient(uiClient(headers), uiURL(srv))
}

func (e *env) manage(srv *server.Server) adminv1connect.ManageServiceClient {
	return adminv1connect.NewManageServiceClient(uiClient(nil), uiURL(srv))
}

// viewerHeaders makes the operator read-only through the role header the
// server honours from a trusted proxy.
func viewerHeaders() map[string]string { return map[string]string{"X-Auth-Role": "viewer"} }

// ---- agent-side raw clients ----------------------------------------------------

// h2cClient is an HTTP/2-over-cleartext client (prior knowledge), which is
// what a bidi Connect stream needs against the plaintext agent listener.
func h2cClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	p := new(http.Protocols)
	p.SetUnencryptedHTTP2(true)
	tr.Protocols = p
	return &http.Client{Transport: tr}
}

// tlsH2Client is an HTTP/2-over-TLS client with the given config.
func tlsH2Client(cfg *tls.Config) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = cfg
	tr.ForceAttemptHTTP2 = true
	return &http.Client{Transport: tr}
}

// registerRaw opens an AgentService stream with the given client, sends
// one NodeRegistration and returns the stream (so the caller decides
// whether to keep it open and silent) and the first error it saw. Nothing
// but the registration is ever sent: a raw client is how a probe puts a
// node on the wire without an agent's behaviour attached.
func registerRaw(ctx context.Context, client *http.Client, baseURL, nodeID string, opts ...connect.ClientOption) (*connect.BidiStreamForClient[adminv1.Frame, adminv1.Frame], error) {
	c := adminv1connect.NewAgentServiceClient(client, baseURL, opts...)
	stream := c.Stream(ctx)
	err := stream.Send(&adminv1.Frame{Body: &adminv1.Frame_Registration{Registration: &adminv1.NodeRegistration{
		NodeId: nodeID, Version: "bench",
	}}})
	return stream, err
}

// ---- event helpers ------------------------------------------------------------------

// subscribeHTTP opens a StreamEvents subscription for HTTP request events
// and returns the channel it delivers on. Cancelling ctx ends it.
func subscribeHTTP(ctx context.Context, client adminv1connect.ControlServiceClient, includeRecent bool) (<-chan *adminv1.Event, <-chan error) {
	out := make(chan *adminv1.Event, 256)
	errCh := make(chan error, 1)
	go func() {
		defer close(out)
		stream, err := client.StreamEvents(ctx, connect.NewRequest(&adminv1.StreamEventsRequest{
			Filter:        &adminv1.Filter{Types: []adminv1.EventType{adminv1.EventType_EVENT_TYPE_HTTP_REQUEST}},
			IncludeRecent: includeRecent,
		}))
		if err != nil {
			errCh <- err
			return
		}
		for stream.Receive() {
			out <- stream.Msg()
		}
		if err := stream.Err(); err != nil {
			errCh <- err
		}
	}()
	return out, errCh
}

// emitHTTP puts one HTTP request event on the agent's bus with the given
// path, the way the framework's HTTP middleware would.
func emitHTTP(bus *observability.Bus, nodeID, path string) {
	ev := observability.AcquireHTTPRequestEvent(time.Now(), nodeID)
	ev.Method = "GET"
	ev.Path = path
	ev.Status = 200
	bus.Emit(ev)
}

// collectPaths drains up to want events from ch, waiting at most within,
// and returns the HTTP paths seen in order.
func collectPaths(ch <-chan *adminv1.Event, want int, within time.Duration) []string {
	var paths []string
	deadline := time.After(within)
	for len(paths) < want {
		select {
		case ev, ok := <-ch:
			if !ok {
				return paths
			}
			if h := ev.GetHttpRequest(); h != nil {
				paths = append(paths, h.GetPath())
			}
		case <-deadline:
			return paths
		}
	}
	return paths
}

// waitDemand waits until the server's aggregate subscription has reached
// the agent's bus for HTTP events — before that, the agent forwards
// nothing, by design.
func waitDemand(bus *observability.Bus, within time.Duration) bool {
	return pollUntil(within, func() bool { return bus.HasSubscribers(observability.KindHTTPRequest) })
}

// ---- server log capture ---------------------------------------------------------------

// logBuffer captures a server's slog output from its goroutines.
type logBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (l *logBuffer) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(l, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// ---- test CA -----------------------------------------------------------------------------

// testCA is a throwaway CA plus one server leaf it signed for 127.0.0.1.
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
		Subject:               pkix.Name{CommonName: "orbit-fleetbench-ca"},
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

// serverTLS is a server-only TLS config (encrypts, authenticates nobody).
func (ca *testCA) serverTLS() *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{ca.leaf}, MinVersion: tls.VersionTLS12}
}

// mutualTLS requires and verifies a client certificate signed by the CA.
func (ca *testCA) mutualTLS() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{ca.leaf},
		MinVersion:   tls.VersionTLS12,
		ClientCAs:    ca.pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
}

// clientTLS trusts the CA and verifies the server as 127.0.0.1.
func (ca *testCA) clientTLS() *tls.Config {
	return &tls.Config{RootCAs: ca.pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}
}

// clientTLSWithCert is clientTLS plus a client certificate for cn.
func (ca *testCA) clientTLSWithCert(t *testing.T, cn string) *tls.Config {
	t.Helper()
	leaf, _ := ca.issue(t, cn, x509.ExtKeyUsageClientAuth)
	cfg := ca.clientTLS()
	cfg.Certificates = []tls.Certificate{leaf}
	return cfg
}

// ---- repository tree -----------------------------------------------------------------------

// repoRoot walks up from the package directory to the directory holding
// go.work: the static probes read the repository as it is checked out.
func (e *env) repoRoot() string {
	e.rootOnce.Do(func() {
		dir, err := os.Getwd()
		if err != nil {
			e.tb.Fatalf("getwd: %v", err)
		}
		for {
			if fileExists(filepath.Join(dir, "go.work")) {
				e.root = dir
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				e.tb.Fatalf("no go.work above %s", dir)
			}
			dir = parent
		}
	})
	return e.root
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// readFile reads a repository file or fails the probe: a static probe
// whose input is missing measures nothing.
func (e *env) readFile(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(e.repoRoot(), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

// fileSize returns the size of a repository file, or -1 when it is absent.
func (e *env) fileSize(rel string) int64 {
	info, err := os.Stat(filepath.Join(e.repoRoot(), rel))
	if err != nil {
		return -1
	}
	return info.Size()
}

// ---- Prometheus scrape -------------------------------------------------------------------------

// scrapeFamilies GETs a /metrics endpoint and returns the metric family
// names it exposes (the name before the first '{' or ' ' on each sample
// line). Families, not samples: the question every probe asks is "does
// this process publish anything of its own", not a value.
func scrapeFamilies(t *testing.T, url string) map[string]bool {
	t.Helper()
	resp, err := bareClient().Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s answered %d", url, resp.StatusCode)
	}
	families := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		end := strings.IndexAny(line, "{ ")
		if end < 0 {
			end = len(line)
		}
		families[line[:end]] = true
	}
	return families
}

func familiesWithPrefix(families map[string]bool, prefixes ...string) []string {
	var out []string
	for f := range families {
		for _, p := range prefixes {
			if strings.HasPrefix(f, p) {
				out = append(out, f)
				break
			}
		}
	}
	return out
}
