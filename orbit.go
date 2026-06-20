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
	"fmt"

	"github.com/jcsvwinston/orbit/internal/admin"

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
	// AuditMaxSize caps the in-memory audit log (default defaultAuditMaxSize).
	AuditMaxSize int `yaml:"audit_max_size"`
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
	defaultSQL, err := defaultHandle.SqlDB()
	if err != nil {
		return fmt.Errorf("orbit: resolve default *sql.DB: %w", err)
	}
	handles := rt.DatabaseHandles()

	// Provision the bootstrap admin user (dialect-aware) before building the panel.
	if m.cfg.BootstrapPassword != "" {
		if _, err := admin.EnsureBootstrapAdminUser(ctx, defaultSQL, admin.BootstrapAdminConfig{
			Username: m.cfg.BootstrapUsername,
			Email:    m.cfg.BootstrapEmail,
			Password: m.cfg.BootstrapPassword,
			System:   defaultHandle.System(),
		}); err != nil {
			return fmt.Errorf("orbit: ensure bootstrap admin user: %w", err)
		}
	}

	m.panel = admin.NewPanel(defaultHandle, rt.Models(), rt.Logger(), admin.PanelConfig{
		Prefix:          m.cfg.Prefix,
		Title:           m.cfg.Title,
		Environment:     m.cfg.Environment,
		Databases:       databaseRuntimeInfo(handles, defaultHandle),
		DatabaseHandles: handles,
		// Admin auth uses the default database's *sql.DB + the framework session.
		Auth:         admin.NewDatabaseAdminAuth(defaultSQL, rt.Session(), m.cfg.Prefix),
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
	})

	// Feed the live SQL view from the framework's first-party event bus (covers
	// every model.CRUD query across the app, not just the admin's own browsing).
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
