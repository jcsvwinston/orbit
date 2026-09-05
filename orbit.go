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
	"net/http"
	"strings"

	"github.com/jcsvwinston/orbit/datasource"
	"github.com/jcsvwinston/orbit/internal/admin"
	dsnucleus "github.com/jcsvwinston/orbit/internal/datasource/nucleus"

	"github.com/jcsvwinston/nucleus/pkg/app"
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
// The struct is flat, but its fields serve two different modes — do not let
// the cluster vocabulary scare a plain panel setup:
//
//   - Panel (what almost every app uses): Prefix, Title, Bootstrap*,
//     AuthDatabase, MultiTenant*, Environment, MigrationsPath, AuditMaxSize,
//     LiveExcludePatterns, TraceURLTemplate. Nothing else is required; the
//     four-field examples/minimal is a complete production shape.
//   - Cluster live-feed relay (opt-in, off by default): the Cluster* fields.
//     They only matter once ClusterEnabled is true; in particular, NO Redis
//     is needed to run the panel — ClusterRedisURL is read exclusively by
//     the relay. The standalone fleet plane (agent/, server/) is configured
//     on its own binaries, not here.
//   - Go-only: DataSource (not bindable from YAML).
//
// Grouping the Cluster* fields into a nested sub-struct would make this
// split structural, but the flat yaml keys (cluster_enabled, ...) are part
// of the frozen surface below, so that reshuffle is deliberately deferred
// to a hypothetical v2; within v1.x the split lives in this comment and in
// the configuration docs.
//
// Config is a frozen v1.0 surface (docs/V1_GATE.md §A-3): every field keeps
// its name, yaml key, type, and zero-value behavior for the life of v1.x.
// Fields may be added; none is removed or renamed without a major. The freeze
// is enforced by contracts/freeze_test.go.
type Config struct {
	// Prefix is the URL path orbit mounts under (default DefaultPrefix).
	Prefix string `yaml:"prefix" koanf:"prefix"`
	// Title is the heading shown in the admin UI.
	Title string `yaml:"title" koanf:"title"`

	// Bootstrap admin user, created on first start if it does not exist. When
	// BootstrapPassword is empty, bootstrapping is skipped (the operator is
	// expected to provision the admin user another way).
	BootstrapUsername string `yaml:"bootstrap_username" koanf:"bootstrap_username"`
	BootstrapEmail    string `yaml:"bootstrap_email" koanf:"bootstrap_email"`
	BootstrapPassword string `yaml:"bootstrap_password" koanf:"bootstrap_password"`

	// AuthDatabase optionally names a managed database alias whose handle backs
	// admin authentication and the bootstrap user. Empty means use the default
	// database. The panel itself (Data Studio etc.) always runs on the default
	// handle; only the auth/bootstrap *sql.DB is redirected.
	AuthDatabase string `yaml:"auth_database" koanf:"auth_database"`

	// Multi-tenant: set these to match the host application so Data Studio is
	// confined to the tenant the app resolves for each request (list, get,
	// create, update, delete, bulk, exports, imports, fixtures). Only a
	// superuser or a subject granted the tenant_switch RBAC action can look at
	// another tenant (?tenant=<id>) or at all of them (?tenant=all), and every
	// switch is audited. Leave disabled for single-tenant apps.
	MultiTenantEnabled bool     `yaml:"multitenant_enabled" koanf:"multitenant_enabled"`
	MultiTenantDefault string   `yaml:"multitenant_default" koanf:"multitenant_default"`
	MultiTenantIDs     []string `yaml:"multitenant_ids" koanf:"multitenant_ids"`

	// Environment is a label shown in the UI (e.g. "production"). Optional.
	Environment string `yaml:"environment" koanf:"environment"`
	// MigrationsPath is the directory the migrations view reads (default "migrations").
	MigrationsPath string `yaml:"migrations_path" koanf:"migrations_path"`
	// AuditMaxSize caps the in-memory audit log ring buffer; zero or negative
	// means the default of 10000 entries.
	AuditMaxSize int `yaml:"audit_max_size" koanf:"audit_max_size"`

	// Live view / cluster telemetry.

	// LiveExcludePatterns lists path patterns excluded from the live HTTP
	// capture feed (e.g. health checks, the admin's own polling endpoints).
	LiveExcludePatterns []string `yaml:"live_exclude_patterns" koanf:"live_exclude_patterns"`
	// ClusterEnabled turns on cluster-aware live telemetry: live request/SQL
	// events are relayed between nodes over Redis so the feed shows the whole
	// fleet, not just the local node. Best-effort — a relay failure never blocks
	// startup.
	ClusterEnabled bool `yaml:"cluster_enabled" koanf:"cluster_enabled"`
	// ClusterRedisURL is the Redis URL backing the live telemetry relay.
	ClusterRedisURL string `yaml:"cluster_redis_url" koanf:"cluster_redis_url"`
	// ClusterChannel is the Redis pub/sub channel the relay publishes on
	// (default nucleus:admin:live:v1).
	ClusterChannel string `yaml:"cluster_channel" koanf:"cluster_channel"`
	// ClusterNodeID is an explicit node identifier for this instance in the
	// relay (defaults to the runtime identity).
	ClusterNodeID string `yaml:"cluster_node_id" koanf:"cluster_node_id"`
	// ClusterToken is a shared secret the relay uses to reject untrusted
	// (cross-tenant or spoofed) messages on the channel.
	ClusterToken string `yaml:"cluster_token" koanf:"cluster_token"`
	// TraceURLTemplate is an external trace-explorer URL template surfaced in
	// the UI; it supports a {trace_id} placeholder.
	TraceURLTemplate string `yaml:"trace_url_template" koanf:"trace_url_template"`

	// DataSource overrides the source Data Studio browses and edits (ADR-001).
	// Nil means the default: a Nucleus-backed adapter over the application's
	// model registry and database handles. Set it to browse another backend —
	// e.g. an app that runs the Quark ORM passes a quarkdatasource adapter
	// (QADR-0006, Caso 2). Go-only wiring; not bindable from YAML. When set,
	// the runtime field-metadata editor is disabled (it mutates the Nucleus
	// registry, which a custom source does not necessarily have).
	DataSource datasource.DataSource `yaml:"-" koanf:"-"`
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
	m := newModule(cfg)

	return nucleus.Module[Config]{
		Name:   "orbit",
		Prefix: cfg.Prefix,
		Config: cfg,

		// The bound Config arrives here as the C parameter: the framework
		// extracts the modules.orbit.* subtree and overlays it on the
		// declared value. Orbit used to discard it and close over the
		// construction-time config, so everything the README documents
		// about configuring the panel from nucleus.yml was inert —
		// including bootstrap_password (QCD-OR-1). Because the module IS
		// mounted, the framework's unmounted-config warning stayed quiet
		// too, so it was ignored in complete silence.
		OnStart: func(ctx context.Context, rt nucleus.Runtime, bound Config) error {
			if err := m.checkPrefixAgreement(bound); err != nil {
				return err
			}
			m.cfg = m.effectiveConfig(bound)
			m.rt = rt
			if err := m.start(ctx); err != nil {
				return err
			}
			rt.Logger().Info("orbit: admin panel ready", "prefix", m.cfg.Prefix)
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

	// The admin users schema is materialized on EVERY mount, independent of the
	// bootstrap password: with an empty password the operator provisions the
	// admin account another way (e.g. `nucleus createuser`), and that path
	// needs the table to exist. Gating the schema on the password left the
	// documented secure default (empty password) with no table, no way to log
	// in, and a createuser error whose advice ("start the app once to create
	// the schema") did not work.
	if err := admin.EnsureBootstrapAdminUsersSchema(ctx, authSQL, authSystem); err != nil {
		return fmt.Errorf("orbit: ensure admin users schema: %w", err)
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
		// The panel authenticates through the application's declared chain
		// when there is one (auth_backends), so an operator who configured
		// a corporate directory gets directory login here too. Authorization
		// stays local: a directory user absent from the admin table is
		// still refused.
		// WithTitle propagates the configured Title to the login page, which
		// renders before any authenticated API call can serve it.
		// WithSystem names the auth database's dialect so the per-request
		// user lookup is a bounded query with the right placeholders.
		Auth:         admin.NewDatabaseAdminAuth(authSQL, rt.Session(), m.cfg.Prefix).WithAuthChain(rt.AuthChain()).WithTitle(m.cfg.Title).WithSystem(authSystem),
		Session:      rt.Session(),
		RBACEnforcer: rt.Authorizer(),
		Store:        rt.Storage(),

		MultiTenantEnabled:    m.cfg.MultiTenantEnabled,
		MultiTenantDefault:    m.cfg.MultiTenantDefault,
		MultiTenantAutoFilter: m.cfg.MultiTenantEnabled,
		MultiTenantIDs:        m.cfg.MultiTenantIDs,
		TenantResolver:        resolvedTenant,

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

// newModule builds the module state from the construction-time config.
func newModule(cfg Config) *module {
	return &module{cfg: cfg}
}

// effectiveConfig resolves what the panel actually runs with.
//
// The framework binds modules.orbit.* ONTO the declared Config
// (bindConfig starts from Module.Config and unmarshals the YAML subtree
// over it), so YAML wins key by key and the Go value supplies the base.
// This method exists to make that precedence explicit and testable rather
// than implied — and to keep the mount prefix out of it, which
// checkPrefixAgreement handles separately.
func (m *module) effectiveConfig(bound Config) Config {
	effective := bound
	// The mount point was fixed when Module() ran; keep the panel's own
	// notion of its prefix in agreement with it.
	effective.Prefix = m.cfg.Prefix
	if effective.MigrationsPath == "" {
		effective.MigrationsPath = m.cfg.MigrationsPath
	}
	if effective.AuditMaxSize <= 0 {
		effective.AuditMaxSize = m.cfg.AuditMaxSize
	}
	// DataSource is a Go value with no YAML representation, so it can only
	// come from construction.
	effective.DataSource = m.cfg.DataSource
	return effective
}

// checkPrefixAgreement refuses a YAML prefix that disagrees with the mount
// point.
//
// The framework reads Module.Prefix to mount the subtree BEFORE it binds
// the YAML, so a different modules.orbit.prefix cannot move the panel. If
// the hooks honoured it anyway, the panel would generate links pointing
// somewhere it is not served — a worse outcome than ignoring the key. It
// fails loudly instead, naming both values.
func (m *module) checkPrefixAgreement(bound Config) error {
	declared := strings.TrimSpace(bound.Prefix)
	if declared == "" || declared == m.cfg.Prefix {
		return nil
	}
	return fmt.Errorf("orbit: modules.orbit.prefix is %q but the module is mounted at %q — the mount point is fixed when the module is built, so this key cannot move it; set the prefix in orbit.Config(...) instead, or remove it from nucleus.yml", declared, m.cfg.Prefix)
}

// resolvedTenant is the panel's TenantResolver: the tenant nucleus resolved
// for the request (its request-scope middleware runs on every route before
// the panel's own, so the scope is already in the context). The panel used
// to read a context key of its own that nothing ever wrote, so the
// "request's resolved tenant" the docs promised never reached it.
func resolvedTenant(r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	tenant := strings.TrimSpace(app.TenantFromContext(r.Context()))
	return tenant, tenant != ""
}
