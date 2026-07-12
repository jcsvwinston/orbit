// Package server is the top-level admin observability server. Construct
// with New, drive with Run. Implementation details live in the sub-
// packages: nodes, routing, services, auth, ui.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/jcsvwinston/orbit/server/auth"
	"github.com/jcsvwinston/orbit/server/nodes"
	"github.com/jcsvwinston/orbit/server/routing"
	"github.com/jcsvwinston/orbit/server/services"
	"github.com/jcsvwinston/orbit/server/ui"

	adminv1connect "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// Server is the assembled admin observability server. It owns two HTTP
// listeners: one for agents and one for UIs/operators. Both serve over
// HTTP/2 (h2c when no TLS config is provided).
type Server struct {
	cfg Config

	state *services.State

	agentSrv *http.Server
	uiSrv    *http.Server
	// metricsSrv is nil unless Config.MetricsAddr opts in.
	metricsSrv *http.Server

	// listenersMu guards the listener fields. Run writes them once
	// during boot; AgentAddr/UIAddr read them concurrently from test
	// goroutines, so a small mutex is the simplest race-free interface.
	listenersMu     sync.RWMutex
	agentListener   net.Listener
	uiListener      net.Listener
	metricsListener net.Listener

	logger *slog.Logger
}

// New constructs a Server. It performs no IO; call Run to start serving.
func New(cfg Config) *Server {
	cfg = cfg.withDefaults()

	state := &services.State{
		Nodes:    nodes.New(),
		EventBus: routing.NewEventBus(),
		Replay: routing.NewReplay(routing.ReplayCapacities{
			HTTP:    cfg.HTTPReplayBufferSize,
			SQL:     cfg.SQLReplayBufferSize,
			Session: cfg.SessionReplayBufferSize,
			Custom:  cfg.CustomReplayBufferSize,
		}),
		Snapshots:      routing.NewSnapshotRouter(0),
		DataStudio:     routing.NewDataStudioRouter(0),
		Rbac:           routing.NewRbacRouter(0),
		Audit:          routing.NewAuditRing(0),
		Logger:         cfg.Logger,
		SendChanBuffer: 64,
		HeartbeatGrace: cfg.AgentInactivityTimeout,
	}

	s := &Server{
		cfg:    cfg,
		state:  state,
		logger: cfg.Logger,
	}

	// AgentService listener: protected by AgentMiddleware. /healthz is
	// carved out BEFORE auth so load balancers and the agent's dialer can
	// probe without owning the token.
	protectedAgent := http.NewServeMux()
	protectedAgent.Handle(adminv1connect.NewAgentServiceHandler(services.NewAgentService(state)))
	agentRoot := http.NewServeMux()
	agentRoot.HandleFunc("/healthz", healthOK)
	agentRoot.Handle("/", auth.AgentMiddleware(cfg.AgentToken)(protectedAgent))

	// UI listener: protected by UIMiddleware. /healthz is carved out the
	// same way; everything else (ControlService, DataStudioService, and
	// the UI assets) goes through the auth chain.
	controlSvc := services.NewControlService(state, cfg.EventChannelSize, cfg.SnapshotTimeout)
	dataStudioSvc := services.NewDataStudioService(state, cfg.SnapshotTimeout)
	manageSvc := services.NewManageService(state, cfg.SnapshotTimeout)
	protectedUI := http.NewServeMux()
	protectedUI.Handle(adminv1connect.NewControlServiceHandler(controlSvc))
	protectedUI.Handle(adminv1connect.NewDataStudioServiceHandler(dataStudioSvc))
	protectedUI.Handle(adminv1connect.NewManageServiceHandler(manageSvc))
	protectedUI.Handle("/", staticUIHandler())
	uiRoot := http.NewServeMux()
	uiRoot.HandleFunc("/healthz", healthOK)
	uiRoot.Handle("/", auth.UIMiddleware(auth.UIConfig{
		BearerToken:  cfg.UIBearerToken,
		AuthHeader:   cfg.UIAuthHeader,
		EmailHeader:  cfg.UIEmailHeader,
		TrustedCIDRs: cfg.UITrustedProxyCIDRs,
		ProxySecret:  cfg.UIProxySecret,
	})(protectedUI))

	s.agentSrv = newH2CServer(agentRoot, cfg.AgentTLS)
	s.uiSrv = newH2CServer(uiRoot, cfg.UITLS)

	// Optional metrics listener (Config.MetricsAddr): Prometheus default
	// registry (go_* and process_* collectors; server-specific collectors
	// are future work) + /healthz. Unauthenticated by design — bind it to
	// a private interface, like any metrics port.
	if strings.TrimSpace(cfg.MetricsAddr) != "" {
		metricsRoot := http.NewServeMux()
		metricsRoot.HandleFunc("/healthz", healthOK)
		metricsRoot.Handle("/metrics", promhttp.Handler())
		s.metricsSrv = newH2CServer(metricsRoot, nil)
	}

	return s
}

// MetricsAddr returns the resolved metrics address (after Run has bound),
// or "" when the metrics listener is disabled.
func (s *Server) MetricsAddr() string {
	if s == nil {
		return ""
	}
	s.listenersMu.RLock()
	defer s.listenersMu.RUnlock()
	if s.metricsListener == nil {
		return ""
	}
	return s.metricsListener.Addr().String()
}

// State returns the wired-up services.State. Useful for tests.
func (s *Server) State() *services.State { return s.state }

// AgentAddr returns the resolved agent address (after Run has bound).
func (s *Server) AgentAddr() string {
	if s == nil {
		return ""
	}
	s.listenersMu.RLock()
	defer s.listenersMu.RUnlock()
	if s.agentListener == nil {
		return ""
	}
	return s.agentListener.Addr().String()
}

// UIAddr returns the resolved UI address.
func (s *Server) UIAddr() string {
	if s == nil {
		return ""
	}
	s.listenersMu.RLock()
	defer s.listenersMu.RUnlock()
	if s.uiListener == nil {
		return ""
	}
	return s.uiListener.Addr().String()
}

// Run starts both listeners and blocks until ctx is cancelled. Returns
// nil on graceful shutdown, non-nil on listen errors. Not idempotent.
func (s *Server) Run(ctx context.Context) error {
	// Fail-closed: refuse an unauthenticated agent listener bound to a
	// non-loopback interface (see agentListenerGuard).
	warnExposed, err := s.cfg.agentListenerGuard()
	if err != nil {
		return err
	}
	if warnExposed {
		s.logger.Warn("agent listener is exposed without authentication",
			"agent_addr", s.cfg.AgentAddr,
			"reason", "--insecure-agent-listener set; ensure AgentAddr is restricted at the network layer")
	}

	agentLn, err := net.Listen("tcp", s.cfg.AgentAddr)
	if err != nil {
		return fmt.Errorf("admin server: listen agent %s: %w", s.cfg.AgentAddr, err)
	}
	uiLn, err := net.Listen("tcp", s.cfg.UIAddr)
	if err != nil {
		_ = agentLn.Close()
		return fmt.Errorf("admin server: listen ui %s: %w", s.cfg.UIAddr, err)
	}
	var metricsLn net.Listener
	if s.metricsSrv != nil {
		metricsLn, err = net.Listen("tcp", s.cfg.MetricsAddr)
		if err != nil {
			_ = agentLn.Close()
			_ = uiLn.Close()
			return fmt.Errorf("admin server: listen metrics %s: %w", s.cfg.MetricsAddr, err)
		}
	}

	s.listenersMu.Lock()
	s.agentListener = agentLn
	s.uiListener = uiLn
	s.metricsListener = metricsLn
	s.listenersMu.Unlock()

	s.logger.Info("admin server starting",
		"agent_addr", agentLn.Addr(),
		"ui_addr", uiLn.Addr(),
		"metrics_enabled", metricsLn != nil)

	errCh := make(chan error, 3)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		err := s.agentSrv.Serve(agentLn)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("agent listener: %w", err)
		}
	}()
	go func() {
		defer wg.Done()
		err := s.uiSrv.Serve(uiLn)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("ui listener: %w", err)
		}
	}()
	if metricsLn != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.metricsSrv.Serve(metricsLn)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("metrics listener: %w", err)
			}
		}()
	}

	select {
	case <-ctx.Done():
		shutdownErr := s.shutdown(2 * time.Second)
		wg.Wait()
		return shutdownErr
	case err := <-errCh:
		_ = s.shutdown(time.Second)
		wg.Wait()
		return err
	}
}

// shutdown gracefully closes every listener. Best-effort; bounded by
// timeout per server.
func (s *Server) shutdown(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var first error
	if err := s.agentSrv.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		first = err
	}
	if err := s.uiSrv.Shutdown(ctx); err != nil && first == nil && !errors.Is(err, context.DeadlineExceeded) {
		first = err
	}
	if s.metricsSrv != nil {
		if err := s.metricsSrv.Shutdown(ctx); err != nil && first == nil && !errors.Is(err, context.DeadlineExceeded) {
			first = err
		}
	}
	return first
}

// staticUIHandler serves the embedded admin/ui/dist filesystem. When the
// build pipeline has not produced dist (fresh checkout), it serves the
// PlaceholderHTML page.
func staticUIHandler() http.Handler {
	if uiFS := ui.FS(); uiFS != nil {
		return spaHandler(uiFS)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(ui.PlaceholderHTML))
	})
}

// spaHandler serves static assets from fsys with SPA-style fallback to
// index.html. Hash-based asset filenames cache fine; index.html should
// not, so we set Cache-Control: no-store on it.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(fsys, path); err == nil {
			if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, "/index.html") {
				w.Header().Set("Cache-Control", "no-store")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		index, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.Error(w, "admin UI missing index.html", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(index)
	})
}

// agentListenerGuard decides whether the agent listener may start. An
// unauthenticated listener (AgentToken == "" && AgentTLS == nil) on a
// non-loopback interface would let any host on the network register as an
// agent, drive Data Studio CRUD, read RBAC snapshots and inject fleet
// events. It returns:
//
//   - err != nil  → the listener must be refused (caller returns it).
//   - warn == true → the listener starts exposed-and-unauthenticated only
//     because InsecureAgentListener overrode the guard; the caller logs it.
//   - (false, nil) → the listener is authenticated or loopback; start it.
func (c Config) agentListenerGuard() (warn bool, err error) {
	if !agentListenerExposed(c.AgentAddr) || c.AgentToken != "" || c.AgentTLS != nil {
		return false, nil
	}
	if !c.InsecureAgentListener {
		return false, fmt.Errorf("admin server: refusing to start the agent listener on non-loopback address %q without authentication: "+
			"set --agent-token or --agent-cert/--agent-key, bind --agent-addr to loopback, or pass --insecure-agent-listener to override", c.AgentAddr)
	}
	return true, nil
}

// agentListenerExposed reports whether addr binds an interface reachable
// from off-host. It is conservative: anything that is not provably
// loopback counts as exposed, so the fail-closed guard in Run errs toward
// refusing an unauthenticated listener rather than allowing one.
//
//   - ":9090" / "0.0.0.0:9090" / "[::]:9090" — all interfaces → exposed.
//   - "127.0.0.1:9090" / "[::1]:9090" / "localhost:9090" — loopback.
//   - any other specific IP or hostname → exposed (can't prove loopback).
func agentListenerExposed(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		// No port form (or malformed); treat the whole string as the host.
		host = strings.TrimSpace(addr)
	}
	host = strings.TrimSpace(host)
	switch host {
	case "":
		// Empty host binds every interface.
		return true
	case "localhost":
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() {
			return true // 0.0.0.0 / ::
		}
		return !ip.IsLoopback()
	}
	// Non-IP hostname other than "localhost": can't prove it's loopback.
	return true
}

func healthOK(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// newH2CServer constructs an http.Server that serves both HTTP/1.1 and
// HTTP/2 cleartext. Connect-RPC bidi streams require HTTP/2; h2c lets
// the agent dial without TLS in dev. Production deployments should set
// tlsConfig and front the listener with a real cert.
func newH2CServer(handler http.Handler, tlsConfig *tls.Config) *http.Server {
	h2s := &http2.Server{}
	wrapped := h2c.NewHandler(handler, h2s)
	srv := &http.Server{
		Handler:           wrapped,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if tlsConfig != nil {
		srv.TLSConfig = tlsConfig
	}
	return srv
}
