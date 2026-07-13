# orbit/server

Standalone Nucleus admin observability server. Accepts agent
connections (`AgentService.Stream`) and serves the admin web UI plus
its `ControlService` API.

## Run it

```bash
# from this module (the UI bundle is embedded via go:embed)
go build -o bin/admin-server ./cmd/admin-server
./bin/admin-server      # default flags: agents :9090, UI :8080

# Production-flavoured invocation:
./bin/admin-server \
  --agent-addr=:9090 \
  --ui-addr=:8080 \
  --agent-token="$NUCLEUS_ADMIN_TOKEN" \
  --agent-cert=/etc/nucleus/server.crt \
  --agent-key=/etc/nucleus/server.key \
  --ui-trusted-cidrs=10.42.0.0/16 \
  --log-format=json --log-level=info
```

Run `./bin/admin-server --help` (or `--version`) for the full surface.
Every flag has a `NUCLEUS_ADMIN_*` env var counterpart.

## Security defaults

Read this before exposing either listener beyond localhost.

**Agent listener is fail-closed.** The agent listener (`--agent-addr`,
default `:9090`) accepts agent registrations that then drive Data Studio
CRUD, RBAC snapshots and fleet events. With **no** `--agent-token` and
**no** TLS (`--agent-cert`/`--agent-key`), `AgentMiddleware` is a
pass-through, so an unauthenticated listener on a non-loopback interface
would accept any rogue agent on the network. The server therefore
**refuses to start** in that configuration. To run it you must do one of:

* set `--agent-token` (shared bearer) — the recommended minimum;
* supply `--agent-cert`/`--agent-key` (mTLS at the listener); or
* bind `--agent-addr` to loopback (`127.0.0.1:9090`); or
* pass `--insecure-agent-listener` (env
  `NUCLEUS_ADMIN_INSECURE_AGENT_LISTENER=1`) to override, **only** when a
  network-layer control (private subnet, service mesh, firewall) already
  restricts who can reach the address. The override logs a `WARN` on boot.

**UI trusted-proxy trust and the `X-Auth-Proxy-Secret` gate.** The UI
listener authenticates operators via a trusted reverse proxy that sets
`X-Auth-User` (per decision 14). By default the server honours that header
for any request whose source IP is in `--ui-trusted-cidrs` (default
`127.0.0.1/32, ::1/128`). **Localhost is always trusted by default**, so
any co-located process — a sidecar, a host-networked container, another
local process — can forge an operator identity and falsify audit
attribution. To require proof that the request really came through your
proxy, set `--ui-proxy-secret` (env `NUCLEUS_ADMIN_UI_PROXY_SECRET`): the
proxy must then echo the secret in the `X-Auth-Proxy-Secret` header, and a
trusted-CIDR request without the matching secret falls through to the
bearer path instead of being trusted. Keep `--ui-trusted-cidrs` as narrow
as your proxy's real source range.

**Operators are read-write by default; scope them with roles or
`--ui-read-only`.** An authenticated operator can perform every Data
Studio mutation on every model of every connected node, unless scoped
down by one of two knobs:

* `--ui-role-header` (default `X-Auth-Role`): when the trusted reverse
  proxy sets this header to `viewer` (also `readonly`/`read-only`), that
  operator's Data Studio mutations are refused with `PermissionDenied`
  while every read surface (streams, nodes, Data Studio reads,
  RBAC/audit) keeps working. Any other value — including absent — keeps
  the operator read-write.
* `--ui-read-only` (env `NUCLEUS_ADMIN_UI_READ_ONLY=1`): makes EVERY
  operator read-only, turning the server into a pure observability
  plane.

The `ManageService.GetRbac` surface behind the UI's "Access control"
screen remains a **read-only snapshot** of each node's Casbin policy
(the app's own authorizer); it does **not** gate the operator's
fleet-plane actions, which are audited and gated only by the
viewer/read-write distinction above — per-verb/per-object operator
authorization is still future work. Treat read-write access to the UI
listener as full fleet-admin access.

**Brute-force lockout.** Requests that PRESENT a wrong credential (bad
bearer on either listener) are rate limited per source IP (20 failures
per minute, then `429`); credential-less requests are never counted, so
an unauthenticated browser can't lock anyone out.

**Inactivity expiry.** An agent whose stream goes silent for longer than
`Config.AgentInactivityTimeout` (default 45s) is marked disconnected in
the fleet UI (the entry revives automatically if frames resume) — a hung
peer no longer shows "online" forever.

**Browser security headers.** Every UI-listener response carries a
strict `Content-Security-Policy` (self-contained SPA, no external
origins), `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`
and `Referrer-Policy: no-referrer`.

## Sub-packages

| Sub-package         | Responsibility                                                                                                            |
|---------------------|---------------------------------------------------------------------------------------------------------------------------|
| `config`            | `Config` struct: addresses, TLS, tokens, ring buffer sizes, snapshot timeout, agent inactivity timeout.                   |
| `nodes`             | Connected-agents registry with watchers and per-entry frame-send channels.                                                |
| `routing/eventbus`  | Server-side fanout: per-UI subscriptions, drop-newest on full channel, `AggregateFilter` for the agent-side union sub.    |
| `routing/replay`    | Per-event-kind drop-oldest replay buffer for `include_recent`.                                                           |
| `routing/snapshot`  | Request-ID correlation between UI's `GetSnapshot` and the agent's `SnapshotResponse`.                                    |
| `routing/match`     | HTTP method/glob/status-class + SQL model matchers shared with the in-process Filter.                                    |
| `routing` (rbac)    | Request-ID correlation for RBAC snapshots routed to agents (`RbacRouter`).                                               |
| `routing` (audit)   | Bounded in-memory fleet-plane audit ring (`AuditRing`, drop-oldest, never persisted).                                     |
| `auth`              | Agent shared bearer token + UI trusted-proxy/bearer middlewares (the resolved operator identity travels in the request context). `/healthz` is carved out of auth on both listeners. |
| `services`          | Connect-RPC handlers for `AgentService.Stream`, `ControlService.{ListNodes,StreamEvents,GetSnapshot}`, `DataStudioService` (UI CRUD routed to agents) and `ManageService.{GetRbac,ListAudit}`. |
| `ui`                | `//go:embed all:dist`. Serves the React bundle at `/`, falls back to a placeholder if the dist hasn't been built.        |
| `cmd/admin-server`  | The binary's main: flags, env, signal handling, TLS loading.                                                            |

The top-level `Server` (`server.go`) composes everything:

* Two `http.Server` listeners (h2c by default, TLS when configured)
  with separate auth chains — one for agents, one for UIs.
* `/healthz` public on both listeners (load balancer-friendly).
* Graceful shutdown on ctx cancel: best-effort `http.Server.Shutdown`
  with a 2-second timeout per listener.

## Observability of the observability server

* `/metrics` is opt-in: `--metrics-addr` (env
  `NUCLEUS_ADMIN_METRICS_ADDR`) runs a third listener serving the
  Prometheus default registry (`go_*`/`process_*` collectors;
  server-specific collectors are future work) plus `/healthz`.
  Unauthenticated by design — bind it to a private interface. Empty
  (the default) disables it.
* Structured logging via `slog`. JSON or text format.
* Per-stream events are NEVER persisted. The replay buffer is in-memory
  and bounded.

## Tests

```bash
cd server && go test -race ./...
```

* `server_integration_test.go` — full agent-server-UI happy paths and
  the auth gates (`AgentToken`, `UIBearer`, `UI placeholder`).
* `nodes/registry_test.go`, `routing/eventbus_test.go`,
  `routing/snapshot_test.go` — table-driven unit coverage of the
  routing primitives.

## Distribution

Released as its own Go module with component tags (`server/vX.Y.Z`,
via release-please). It is a deployable, not a library: the supported
artifact is the `admin-server` binary, installable directly once the
module resolves by tag:

```bash
go install github.com/jcsvwinston/orbit/server/cmd/admin-server@latest
```

or built from a checkout as shown above (the UI bundle is embedded via
`go:embed`). Its Go API (`server.New`, config types) carries no
compatibility promise — the frozen v1.0 surfaces of orbit are the root
module and `datasource`.
