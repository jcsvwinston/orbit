# Orbit

**Orbit** is the pluggable admin product for the [Nucleus](https://github.com/jcsvwinston/nucleus) framework.

It is a separate Go module that mounts **in-process** into a Nucleus application
via the framework's extension/module API and serves a self-contained admin UI —
Data Studio (model CRUD), a live request/SQL feed, a session viewer, RBAC
management, and system metrics. Orbit was extracted from the framework core per
nucleus **ADR-019** so it can ship, version, and evolve as its own product while
the core stays lean.

## Mounting

```go
app, err := nucleus.New().
    FromConfigFile("nucleus.yml").
    Mount(orbit.Module(orbit.Config{Prefix: "/admin"})).
    Build()
```

Orbit reads everything it needs from the nucleus `Runtime` (the model registry,
all database handles, the session manager, the RBAC enforcer, the live event
bus, storage) — the accessors added in nucleus ADR-019 Slice 1 — so it never
reaches into the framework's internals, and its UI is embedded in the binary
(no separate asset deployment).

## Status

🚧 **Extraction in progress** (ADR-019 Slice 2). This repository currently holds
the module skeleton and the integration contract; the admin surface is being
moved here from the framework's former `pkg/admin` in subsequent slices.

## Requirements

- Go 1.26+
- A Nucleus application to mount into.
