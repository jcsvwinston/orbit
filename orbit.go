// Package orbit is the pluggable admin product for the Nucleus framework.
//
// Orbit is a separate Go module that mounts in-process into a Nucleus
// application via the framework's extension/module API, and serves a
// self-contained admin UI (Data Studio, live request/SQL feed, session viewer,
// RBAC, system metrics). It was extracted from the framework core per nucleus
// ADR-019 so it can ship, version, and evolve as its own product while the core
// stays lean. Mount it explicitly:
//
//	app := nucleus.New().
//	    FromConfigFile("nucleus.yml").
//	    Mount(orbit.Module(orbit.Config{Prefix: "/admin"})).
//	    Build()
//
// Orbit reads everything it needs from the nucleus Runtime (the model registry,
// all database handles, the session manager, the RBAC enforcer, the live event
// bus, storage) — the accessors added in nucleus ADR-019 Slice 1 — so it never
// reaches into the framework's internals.
//
// STATUS: extraction in progress (ADR-019 Slice 2). This is the module skeleton
// and integration contract; the admin surface is being moved from the former
// pkg/admin in subsequent slices.
package orbit

import (
	"context"
	"net/http"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// DefaultPrefix is the URL path orbit mounts under when Config.Prefix is empty.
const DefaultPrefix = "/admin"

// Config configures the orbit admin module. The zero value is valid (orbit
// mounts under DefaultPrefix).
type Config struct {
	// Prefix is the URL path orbit mounts under (default DefaultPrefix).
	Prefix string `yaml:"prefix"`
}

// module holds the runtime-bound state captured in OnStart.
type module struct {
	cfg Config
	rt  nucleus.Runtime
}

// Module returns orbit as a nucleus ModuleSpec, mountable on an application via
// the builder's Mount(...). The returned spec is self-contained: it declares its
// own URL prefix and acquires the framework services it needs from the Runtime
// in OnStart.
func Module(cfg Config) nucleus.ModuleSpec {
	if cfg.Prefix == "" {
		cfg.Prefix = DefaultPrefix
	}
	m := &module{cfg: cfg}

	return nucleus.Module[Config]{
		Name:   "orbit",
		Prefix: cfg.Prefix,
		Config: cfg,

		OnStart: func(ctx context.Context, rt nucleus.Runtime, _ Config) error {
			m.rt = rt
			rt.Logger().Info("orbit: admin module ready", "prefix", cfg.Prefix)
			return nil
		},

		Routes: func(r nucleus.Router, _ Config) {
			// Slice 2.1 skeleton: a liveness probe that proves the mount and the
			// Runtime wiring against the real framework. The admin surface (Data
			// Studio, live feed, sessions, RBAC, system) moves here from the
			// former pkg/admin in the following slices.
			r.Get("/_orbit/health", m.health)
		},
	}.Build()
}

// health is the skeleton liveness route, mounted at <prefix>/_orbit/health.
func (m *module) health(c *nucleus.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"product": "orbit",
		"status":  "ok",
		"prefix":  m.cfg.Prefix,
	})
}
