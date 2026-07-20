// Package orbit is the pluggable admin product for the Nucleus framework.
//
// Orbit is a separate Go module that mounts in-process into a Nucleus
// application via the framework's extension/module API, and serves a
// self-contained admin UI (Data Studio, live request/SQL feed, session viewer,
// RBAC, system metrics). It was extracted from the framework core per nucleus
// ADR-019 so it can ship, version, and evolve as its own product while the core
// stays lean. Mount it explicitly:
//
//	app, err := nucleus.New().
//	    FromConfigFile("nucleus.yml").
//	    Mount(orbit.Module(orbit.Config{Prefix: "/admin"})).
//	    Build()
//
// Orbit reads everything it needs from the nucleus Runtime — the model registry,
// the managed database handles, the session manager, the RBAC enforcer, the live
// event bus, storage (the accessors added in nucleus ADR-019 Slice 1/2) — so it
// never reaches into the framework's internals.
package orbit

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jcsvwinston/orbit/datasource"
	"github.com/jcsvwinston/orbit/internal/admin"
	dsnucleus "github.com/jcsvwinston/orbit/internal/datasource/nucleus"

	"github.com/jcsvwinston/nucleus/pkg/authz"
	"github.com/jcsvwinston/nucleus/pkg/db"
	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// DefaultPrefix is the URL path orbit mounts under when Config.Prefix is empty.
const DefaultPrefix = "/admin"

// defaultAuditMaxSize is the in-memory audit ring-buffer size when Config leaves
// AuditMaxSize unset.
const defaultAuditMaxSize = 10000

// Config configures the orbit admin module. The zero value is valid (orbit
// mounts under DefaultPrefix); bound from the `modules.orbit.*` subtree of the
// application config when mounted on a config-file app.
//
// Config is a frozen v1.0 surface (docs/V1_GATE.md §A-3): every field keeps
// its name, yaml key, type, and zero-value behavior for the life of v1.x.
// Fields may be added; none is removed or renamed without a major. The freeze
// is enforced by contracts/freeze_test.go.
type Config struct {
	// Prefix is the URL path orbit mounts under (default DefaultPrefix).
	Prefix string `yaml:"prefix"`
	// Title is the heading shown in the admin UI.
	Title string `yaml:"title"`

	// Bootstrap admin user, created on first start if it does not exist. When
	// BootstrapPassword is empty, bootstrapping is skipped (the operator is
	// expected to provision the admin user another way).
	BootstrapUsername string `yaml:"bootstrap_username"`
	BootstrapEmail    string `yaml:"bootstrap_email"`
	BootstrapPassword string `yaml:"bootstrap_password"`

	// AuthDatabase optionally names a managed database alias whose handle backs
	// admin authentication and the bootstrap user. Empty means use the default
	// database. The panel itself (Data Studio etc.) always runs on the default
	// handle; only the auth/bootstrap *sql.DB is redirected.
	AuthDatabase string `yaml:"auth_database"`

	// Multi-tenant: set these to match the host application so the admin filters
	// records by the request's resolved tenant. Leave disabled for single-tenant
	// apps.
	MultiTenantEnabled bool     `yaml:"multitenant_enabled"`
	MultiTenantDefault string   `yaml:"multitenant_default"`
	MultiTenantIDs     []string `yaml:"multitenant_ids"`

	// Environment is a label shown in the UI (e.g. "production"). Optional.
	Environment string `yaml:"environment"`
	// MigrationsPath is the directory the migrations view reads (default "migrations").
	MigrationsPath string `yaml:"migrations_path"`
	// AuditMaxSize caps the in-memory audit log ring buffer; zero or negative
	// means the default of 10000 entries.
	AuditMaxSize int `yaml:"audit_max_size"`

	// Live view / cluster telemetry.

	// LiveExcludePatterns lists path patterns excluded from the live HTTP
	// capture feed (e.g. health checks, the admin's own polling endpoints).
	LiveExcludePatterns []string `yaml:"live_exclude_patterns"`
	// ClusterEnabled turns on cluster-aware live telemetry: live request/SQL
	// events are relayed between nodes over Redis so the feed shows the whole
	// fleet, not just the local node. Best-effort — a relay failure never blocks
	// startup.
	ClusterEnabled bool `yaml:"cluster_enabled"`
	// ClusterRedisURL is the Redis URL backing the live telemetry relay.
	ClusterRedisURL string `yaml:"cluster_redis_url"`
	// ClusterChannel is the Redis pub/sub channel the relay publishes on
	// (default nucleus:admin:live:v1).
	ClusterChannel string `yaml:"cluster_channel"`
	// ClusterNodeID is an explicit node identifier for this instance in the
	// relay (defaults to the runtime identity).
	ClusterNodeID string `yaml:"cluster_node_id"`
	// ClusterToken is a shared secret the relay uses to reject untrusted
	// (cross-tenant or spoofed) messages on the channel.
	ClusterToken string `yaml:"cluster_token"`
	// TraceURLTemplate is an external trace-explorer URL template surfaced in
	// the UI; it supports a {trace_id} placeholder.
	TraceURLTemplate string `yaml:"trace_url_template"`

	// DataSource overrides the source Data Studio browses and edits (ADR-001).
	// Nil means the default: a Nucleus-backed adapter over the application's
	// model registry and database handles. Set it to browse another backend —
	// e.g. an app that runs the Quark ORM passes a quarkdatasource adapter
	// (QADR-0006, Caso 2). Go-only wiring; not bindable from YAML. When set,
	// the runtime field-metadata editor is disabled (it mutates the Nucleus
	// registry, which a custom source does not necessarily have).
	DataSource datasource.DataSource `yaml:"-"`
}

// module holds the runtime-bound state captured in OnStart.
type module struct {
	cfg     Config
	rt      nucleus.Runtime
	panel   *admin.Panel
	stopObs func()
}

// Module returns orbit as a nucleus ModuleSpec, mountable on an application via
// the builder's Mount(...). It is self-contained: it declares its own URL prefix
// and acquires every framework service it needs from the Runtime in OnStart,
// then mounts the admin panel's own router under the prefix in Routes.
func Module(cfg Config) nucleus.ModuleSpec {
	if cfg.Prefix == "" {
		cfg.Prefix = DefaultPrefix
	}
	if cfg.MigrationsPath == "" {
		cfg.MigrationsPath = "migrations"
	}
	if cfg.AuditMaxSize <= 0 {
		cfg.AuditMaxSize = defaultAuditMaxSize
	}
	m := &module{cfg: cfg}

	return nucleus.Module[Config]{
		Name:   "orbit",
		Prefix: cfg.Prefix,
		Config: cfg,

		OnStart: func(ctx context.Context, rt nucleus.Runtime, _ Config) error {
			m.rt = rt
			if err := m.start(ctx); err != nil {
				return err
			}
			rt.Logger().Info("orbit: admin panel ready", "prefix", cfg.Prefix)
			return nil
		},

		OnShutdown: func(ctx context.Context, _ nucleus.Runtime, _ Config) error {
			if m.panel != nil {
				return m.panel.Close(ctx)
			}
			return nil
		},

		Routes: func(r nucleus.Router, _ Config) {
			// Mount the admin panel's own router subtree under the module prefix.
			// The panel owns all routing + auth below here (Router.Mount strips the
			// prefix, mirroring how the framework mounted the admin pre-extraction).
			if m.panel != nil {
				r.Mount("/", m.panel.Handler())
			}
		},
	}.Build()
}

// start builds the admin Panel from the Runtime accessors, provisions the
// bootstrap admin user, and connects the live SQL feed. Called from OnStart, so
// the Panel exists before Routes mounts it.
func (m *module) start(ctx context.Context) error {
	rt := m.rt
	defaultHandle := rt.DatabaseHandle()
	if defaultHandle == nil {
		return fmt.Errorf("orbit: no default database configured (the admin needs a database)")
	}
	handles := rt.DatabaseHandles()

	// Resolve the *sql.DB + dialect that back admin auth + the bootstrap user.
	// Defaults to the default handle; when AuthDatabase names an alias, that
	// handle is used for BOTH instead (the panel itself stays on defaultHandle,
	// below). The dialect must track the AUTH database — not the default — so the
	// bootstrap-user SQL uses the right placeholders when auth lives on a
	// different engine.
	authSQL, authSystem, err := resolveAuthDB(m.cfg.AuthDatabase, defaultHandle, handles)
	if err != nil {
		return err
	}

	// Exempt orbit's prefix from the framework's default-deny RBAC. The panel
	// owns its own session-based auth (NewDatabaseAdminAuth below) and enforces
	// RBAC against this same enforcer, so the framework middleware must not
	// double-gate the prefix — an unauthenticated GET would otherwise 403 before
	// reaching the panel's login flow. This replicates, from the module side,
	// the exemption the framework hardcoded for the in-core admin prefix before
	// the extraction (ADR-019). Registered under the "anonymous" BootstrapSubject
	// the default-deny middleware uses for unauthenticated requests; both the
	// bare prefix (which carries the canonical redirect to prefix+"/") and the
	// subtree need a row, since keyMatch("/admin","/admin/*") is false. Safe
	// no-op on an unbacked runtime (nil enforcer); harmless under WithOpenAuthz
	// (the middleware is not mounted, so the extra allows never fire).
	if enf := rt.Authorizer(); enf != nil {
		prefix := admin.NormalizePrefix(m.cfg.Prefix)
		if err := enf.AddPolicy(authz.BootstrapSubject, prefix, "*"); err != nil {
			return fmt.Errorf("orbit: allow admin prefix %q in authz (bare): %w", prefix, err)
		}
		if err := enf.AddPolicy(authz.BootstrapSubject, prefix+"/*", "*"); err != nil {
			return fmt.Errorf("orbit: allow admin prefix %q in authz (subtree): %w", prefix, err)
		}
	}

	// Provision the bootstrap admin user (dialect-aware) before building the panel.
	if m.cfg.BootstrapPassword != "" {
		if _, err := admin.EnsureBootstrapAdminUser(ctx, authSQL, admin.BootstrapAdminConfig{
			Username: m.cfg.BootstrapUsername,
			Email:    m.cfg.BootstrapEmail,
			Password: m.cfg.BootstrapPassword,
			System:   authSystem,
		}); err != nil {
			return fmt.Errorf("orbit: ensure bootstrap admin user: %w", err)
		}
	}

	// Data Studio speaks Orbit's neutral datasource contract (ADR-001); build the
	// Nucleus-backed adapter from the Runtime accessors and hand it to the panel.
	// The observability bus feeds the live SQL view (ConsumeEventBus below), so
	// the adapter reports the bus connected and installs no per-CRUD observer.
	// Data Studio's backing source (ADR-001): the app's override when provided
	// (e.g. a quarkdatasource adapter — QADR-0006 Caso 2), else the default
	// Nucleus adapter over the Runtime's registry and handles. The field-meta
	// editor (SchemaRegistry) only makes sense against the Nucleus registry, so
	// it is wired only on the default path.
	var src datasource.DataSource
	var schemaRegistry = rt.Models()
	if m.cfg.DataSource != nil {
		src = m.cfg.DataSource
		schemaRegistry = nil
	} else {
		src = dsnucleus.New(dsnucleus.Config{
			Registry:     rt.Models(),
			DefaultAlias: "",
			Resolve: func(alias string) (*db.DB, string, error) {
				h := defaultHandle
				if alias != "" {
					if hh, ok := handles[alias]; ok && hh != nil {
						h = hh
					}
				}
				if h == nil {
					return nil, "", fmt.Errorf("orbit: no database handle for alias %q", alias)
				}
				return h, h.System(), nil
			},
			BusConnected: func() bool { return true },
		})
	}

	m.panel = admin.NewPanel(src, rt.Logger(), admin.PanelConfig{
		Prefix:          m.cfg.Prefix,
		Title:           m.cfg.Title,
		Environment:     m.cfg.Environment,
		SchemaRegistry:  schemaRegistry,
		Databases:       databaseRuntimeInfo(handles, defaultHandle),
		DatabaseHandles: handles,
		// Admin auth uses authSQL (default handle, or AuthDatabase when set) +
		// the framework session.
		Auth:         admin.NewDatabaseAdminAuth(authSQL, rt.Session(), m.cfg.Prefix),
		Session:      rt.Session(),
		RBACEnforcer: rt.Authorizer(),
		Store:        rt.Storage(),

		MultiTenantEnabled:    m.cfg.MultiTenantEnabled,
		MultiTenantDefault:    m.cfg.MultiTenantDefault,
		MultiTenantAutoFilter: m.cfg.MultiTenantEnabled,
		MultiTenantIDs:        m.cfg.MultiTenantIDs,

		AuditEnabled:   true,
		AuditMaxSize:   m.cfg.AuditMaxSize,
		MigrationsPath: m.cfg.MigrationsPath,

		// Live view / cluster telemetry / trace explorer.
		LiveExcludePatterns: m.cfg.LiveExcludePatterns,
		LiveClusterEnabled:  m.cfg.ClusterEnabled,
		LiveClusterRedisURL: m.cfg.ClusterRedisURL,
		LiveClusterChannel:  m.cfg.ClusterChannel,
		LiveClusterNodeID:   m.cfg.ClusterNodeID,
		LiveClusterToken:    m.cfg.ClusterToken,
		TraceURLTemplate:    m.cfg.TraceURLTemplate,
	})

	// Enable the cluster-aware live telemetry relay when configured. Best-effort:
	// a relay failure (e.g. unreachable Redis) is logged but never blocks startup.
	if m.cfg.ClusterEnabled {
		if err := m.panel.EnableLiveClusterRelay(); err != nil {
			rt.Logger().Warn("orbit: live cluster relay disabled", "error", err)
		}
	}

	// Feed the live SQL and HTTP views from the framework's first-party event
	// bus: every model.CRUD query across the app (not just the admin's own
	// browsing) and every host-application HTTP request (emitted by the
	// framework's app-level middleware — the panel's own traffic middleware is
	// not mountable at host level from here, and does not need to be).
	m.stopObs = m.panel.ConsumeEventBus(rt.Observability())
	return nil
}

// databaseRuntimeInfo builds the admin's per-database runtime descriptor from the
// framework's engine-aware *db.DB handles (Engine/System carry the dialect).
func databaseRuntimeInfo(handles map[string]*db.DB, def *db.DB) []admin.DatabaseRuntimeInfo {
	infos := make([]admin.DatabaseRuntimeInfo, 0, len(handles))
	for alias, h := range handles {
		if h == nil {
			continue
		}
		infos = append(infos, admin.DatabaseRuntimeInfo{
			Alias:     alias,
			Engine:    string(h.Engine()),
			Dialect:   h.System(),
			IsDefault: h == def,
		})
	}
	return infos
}

// resolveAuthDB picks the *sql.DB and dialect ("system") that back admin auth
// and the bootstrap user. Empty alias → the default handle; otherwise the named
// handle (clear error if unknown or unresolvable). The returned dialect tracks
// the AUTH database — not the default — so the bootstrap-user SQL uses the right
// placeholders when auth lives on a different engine than the default.
func resolveAuthDB(alias string, defaultHandle *db.DB, handles map[string]*db.DB) (*sql.DB, string, error) {
	h := defaultHandle
	if alias != "" {
		named, ok := handles[alias]
		if !ok || named == nil {
			return nil, "", fmt.Errorf("orbit: auth_database alias %q not found / not resolvable: no such managed database handle", alias)
		}
		h = named
	}
	sqlDB, err := h.SqlDB()
	if err != nil {
		return nil, "", fmt.Errorf("orbit: resolve auth database (alias %q): %w", alias, err)
	}
	return sqlDB, h.System(), nil
}
