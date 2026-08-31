# examples/minimal

The smallest runnable Orbit setup: a [Nucleus](https://github.com/jcsvwinston/nucleus)
app with the **in-process admin panel** (`orbit.Module`) mounted at `/admin`.
This is the product most apps use — Data Studio, the live HTTP/SQL feed,
sessions, RBAC, metrics and audit, served from the same binary.

## Run

```bash
cd examples/minimal
ADMIN_BOOTSTRAP_PASSWORD=change-me-please go run .
```

Then open <http://localhost:8080/admin> and sign in as `admin` with the
password you set.

`ADMIN_BOOTSTRAP_PASSWORD` seeds the first admin user. Leaving it unset is
the framework's secure default — bootstrapping is skipped and no admin is
created (provision one another way, e.g. `nucleus createuser`).

The example registers one model (`Note`), so Data Studio opens with
something to browse. That is not decoration: Orbit reads the **host
application's** model registry, and a panel mounted on an app that
registers no models shows an empty Data Studio. The `notes` module in
`main.go` is the smallest wiring that populates it — a struct listed in
`Module.Models` plus a dev-mode `rt.AutoMigrate` to create its table.

## What to look at

- [`main.go`](main.go) — the whole wiring: a one-model `notes` module and
  `nucleus.New().Mount(...).Mount(orbit.Module(...)).Build()`.
- [`nucleus.yaml`](nucleus.yaml) — a minimal config (one SQLite database).

For the cluster-observability leg (an agent shipping to a standalone
admin server), see [`../../agent/examples/fleet-app`](../../agent/examples/fleet-app).
