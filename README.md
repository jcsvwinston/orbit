# Orbit

**The pluggable admin product for the [Nucleus](https://github.com/jcsvwinston/nucleus) framework.**

[![Go Reference](https://pkg.go.dev/badge/github.com/jcsvwinston/orbit.svg)](https://pkg.go.dev/github.com/jcsvwinston/orbit)
![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white)

Orbit is a self-contained admin panel — Data Studio, a live request/SQL feed, a
session viewer, RBAC management, and system metrics — that mounts **in-process**
into any Nucleus application through the framework's extension/module API. It is
a separate Go module with its own release cadence, extracted from the framework
core per nucleus [ADR-019](https://github.com/jcsvwinston/nucleus/blob/main/docs/adrs/ADR-019-extract-admin-to-orbit-module.md)
so the core stays lean and the admin can evolve as its own product.

You add one dependency and one `Mount(...)` call; orbit reads everything it
needs from the running app's `Runtime` and serves its **embedded** React SPA —
no separate asset deployment, no out-of-process sidecar, no database of its own.

---

## Features

| Module | What it does |
|--------|--------------|
| **Data Studio** | Browse, create, edit, and delete records for every model in the app's registry — tenant-aware, with import/export. |
| **Live runtime inspector** | Real-time feed of incoming HTTP requests and executed SQL across the whole app (sourced from the framework's observability event bus), with optional cross-node aggregation. |
| **Session viewer** | List and revoke active server-side sessions. |
| **Access control (RBAC)** | Inspect and manage the Casbin policies and roles backing the app's authorizer. |
| **System metrics** | Runtime and resource consumption — CPU, memory, goroutines, database pool. |
| **Audit log** | An in-memory ring of admin actions. |
| **Overview & Health** | Dashboard and health at a glance. |

The UI ships **embedded in the binary** (`go:embed`), version-pinned to the
orbit module — a consumer who mounts orbit gets the full admin offline, in a
single binary.

## Install

```bash
go get github.com/jcsvwinston/orbit@latest
```

The current tagged release is **v1.2.0**; pin `@v1.2.0` for reproducible
builds. **v1.0 promise:** the public surfaces (the root module and
`datasource`) are frozen for the life of v1.x — enforced by
`contracts/freeze_test.go` (see `docs/V1_GATE.md`).

> **Requires** Go 1.26+ and a [Nucleus](https://github.com/jcsvwinston/nucleus)
> application to mount into.

## Quick start

Mount orbit on the application builder:

```go
import (
    "os"

    "github.com/jcsvwinston/nucleus/pkg/nucleus"
    "github.com/jcsvwinston/orbit"
)

func main() {
    app, err := nucleus.New().
        FromConfigFile("nucleus.yml").
        Mount(orbit.Module(orbit.Config{
            Prefix:            "/admin",
            Title:             "Acme Admin",
            BootstrapUsername: "admin",
            BootstrapEmail:    "admin@acme.test",
            // When BootstrapPassword is empty, bootstrapping is skipped —
            // provision the admin user another way (e.g. nucleus createuser).
            BootstrapPassword: os.Getenv("ADMIN_BOOTSTRAP_PASSWORD"),
        })).
        Build()
    if err != nil {
        panic(err)
    }
    _ = app.Start()
}
```

Start the app, open `/admin`, and sign in with the bootstrap user. Orbit
self-registers its prefix with the framework's default-deny RBAC and enforces
its own session-based auth below that prefix.

The zero value is valid too — `orbit.Module(orbit.Config{})` mounts under
`/admin` with sensible defaults.

## Configuration

`orbit.Config` is bound from the `modules.orbit.*` subtree of your `nucleus.yml`
(or set it directly in Go). All fields are optional.

| Key (`modules.orbit.*`) | Type | Default | Description |
|---|---|---|---|
| `prefix` | string | `/admin` | URL path orbit mounts under. |
| `title` | string | — | Heading shown in the UI. |
| `environment` | string | — | Label shown in the UI (e.g. `production`). |
| `bootstrap_username` | string | — | Admin user created on first boot. |
| `bootstrap_email` | string | — | Email for the bootstrap user. |
| `bootstrap_password` | string | — | Password for the bootstrap user; empty → bootstrapping is skipped (provision the admin user another way, e.g. `nucleus createuser`). |
| `auth_database` | string | app default | DB alias whose handle backs admin auth + the bootstrap user (use a dedicated DB for the admin user store). |
| `migrations_path` | string | `migrations` | Directory the migrations view reads. |
| `audit_max_size` | int | `10000` | In-memory audit-log ring size. |
| `multitenant_enabled` | bool | `false` | Filter records by the request's resolved tenant. |
| `multitenant_default` | string | — | Default tenant when none is resolved. |
| `multitenant_ids` | []string | — | Known tenant IDs for the selector UI. |
| `live_exclude_patterns` | []string | — | Path patterns excluded from the live HTTP feed. |
| `trace_url_template` | string | — | External trace-explorer URL template (supports `{trace_id}`). |
| `cluster_enabled` | bool | `false` | Aggregate the live feed across nodes via a Redis relay. |
| `cluster_redis_url` | string | — | Redis URL for the live-telemetry relay. |
| `cluster_channel` | string | `nucleus:admin:live:v1` | Pub/sub channel for the relay. |
| `cluster_node_id` | string | runtime id | Explicit node identifier in the relay. |
| `cluster_token` | string | — | Shared secret to reject untrusted relay messages. |

```yaml
# nucleus.yml
modules:
  orbit:
    prefix: /admin
    title: Acme Admin
    environment: production
    bootstrap_username: admin
    bootstrap_email: admin@acme.test
```

## How it works

Orbit is a `nucleus.ModuleSpec`. On startup it captures the app's `Runtime` and
builds its panel from the framework's public accessors — the model registry, all
managed database handles, the session manager, the RBAC enforcer, the live event
bus, and storage. It never reaches into framework internals:

- **In-process** — it sees live runtime state (sessions, SQL, model registry,
  metrics) that an out-of-process sidecar could not, without any IPC surface.
- **Self-contained auth** — it owns a session-based login (`DatabaseAdminAuth`)
  against the `nucleus_admin_users` table and self-registers its prefix with the
  framework's default-deny RBAC, so the framework middleware never double-gates
  it.
- **Embedded SPA** — the React UI is built into the module and served under the
  mount prefix.

## Cluster observability (optional)

For fleet-wide live telemetry, orbit ships sibling modules that are releasable
independently:

| Module | Role |
|---|---|
| [`orbit/proto`](proto) | Connect-RPC contract + generated stubs. |
| [`orbit/agent`](agent) | In-process agent (`agent.NewExtension`) that ships events to an admin server. |
| [`orbit/server`](server) | Standalone admin-server binary that receives them. |

Most applications only need the root `orbit` module for the in-process panel.

## Relationship to Nucleus

Orbit is a "dogfooding" consumer of Nucleus: mounting a real, deep admin
exercises and hardens the framework's extension/`Runtime` surface. The admin
used to live in the framework core as `pkg/admin`; ADR-019 extracted it here as
a clean break. Nucleus itself no longer ships any admin code.

## Requirements

- **Go 1.26+**
- A **Nucleus** application to mount into.
- *(Optional)* **Redis** — only for the cross-node live-telemetry relay.

## Development

```bash
go build ./...        # build the root module
go test ./...         # test it
go work sync          # Go workspace: ./ ./agent ./proto ./quarkbridge ./quarkdatasource ./server
```
