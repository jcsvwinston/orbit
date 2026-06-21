# Orbit

**Orbit** is the pluggable admin product for the [Nucleus](https://github.com/jcsvwinston/nucleus) framework.

It is a separate Go module that mounts **in-process** into a Nucleus application
via the framework's extension/module API and serves a self-contained admin UI —
Data Studio (model CRUD), a live request/SQL feed, a session viewer, RBAC
management, and system metrics. Orbit was extracted from the framework core per
nucleus **ADR-019** so it can ship, version, and evolve as its own product while
the core stays lean.

## Install

```bash
go get github.com/jcsvwinston/orbit@latest
```

Orbit has not cut a tagged release yet, so `@latest` resolves to a pseudo-version
of the current `main`. Pin a specific pseudo-version in your `go.mod` for
reproducible builds until a tag is published.

## Mounting

```go
import (
    "github.com/jcsvwinston/nucleus/pkg/nucleus"
    "github.com/jcsvwinston/orbit"
)

app, err := nucleus.New().
    FromConfigFile("nucleus.yml").
    Mount(orbit.Module(orbit.Config{Prefix: "/admin"})).
    Build()
```

Orbit reads everything it needs from the nucleus `Runtime` (the model registry,
all database handles, the session manager, the RBAC enforcer, the live event
bus, storage) — the accessors added in nucleus ADR-019 — so it never reaches
into the framework's internals, and its UI is embedded in the binary (no
separate asset deployment). It self-registers its prefix with the framework's
default-deny RBAC and owns its own session-based auth.

`orbit.Config` is bound from the `modules.orbit.*` subtree of the application
config (prefix, title, bootstrap admin user, multi-tenant, live-cluster relay,
trace URL template, dedicated auth database, …).

## Cluster observability (optional)

The live admin feed can aggregate telemetry across a fleet via sibling modules,
each releasable independently:

- `github.com/jcsvwinston/orbit/proto` — the Connect-RPC contract + generated stubs.
- `github.com/jcsvwinston/orbit/agent` — the in-process agent (`agent.NewExtension`) that ships events to an admin server.
- `github.com/jcsvwinston/orbit/server` — the standalone admin-server binary that receives them.

Most applications only need the root `orbit` module for the in-process panel.

## Status

**Complete.** The full admin product lives here — the panel and its embedded
SPA, the live runtime inspector, and the cluster observability subsystem —
extracted from the framework's former `pkg/admin` per nucleus ADR-019. nucleus
itself no longer ships any admin code.

## Requirements

- Go 1.26+
- A Nucleus application to mount into.
