---
title: orbit/server
sidebar_position: 4
description: The standalone admin observability server.
---

# orbit/server

The standalone observability server accepts agent connections
(`AgentService.Stream`) and serves the admin web UI plus its `ControlService`
API. Many [agents](./agent.md) stream to one server.

## Run it

```bash
# from the server module (the UI bundle is embedded via go:embed)
cd server && go build -o bin/admin-server ./cmd/admin-server
./bin/admin-server      # defaults: agents on :9090, UI on :8080
```

For local development, make the UI loadable from a plain browser (without
a reverse proxy every UI request answers 401, because the SPA cannot
present a bearer token):

```bash
./bin/admin-server \
  --agent-addr=127.0.0.1:9090 \
  --ui-addr=127.0.0.1:8080 \
  --ui-insecure-open
# then open http://127.0.0.1:8080
```

`--ui-insecure-open` only works on a loopback `--ui-addr` (the server
refuses to start otherwise) and is for local development only.

A production-flavoured invocation:

```bash
./bin/admin-server \
  --agent-addr=:9090 \
  --ui-addr=:8080 \
  --agent-token="$NUCLEUS_ADMIN_TOKEN" \
  --agent-cert=/etc/nucleus/server.crt \
  --agent-key=/etc/nucleus/server.key \
  --ui-trusted-cidrs=10.42.0.0/16 \
  --ui-proxy-secret="$NUCLEUS_ADMIN_UI_PROXY_SECRET" \
  --log-format=json --log-level=info
```

Run `./bin/admin-server --help` (or `--version`) for the full surface. Every
flag has a `NUCLEUS_ADMIN_*` env-var counterpart.

:::info The operator travels with every Data Studio request
A Data Studio operation routed through the fleet plane executes on the
agent, through the same data-access contract the in-process panel uses,
**as the operator the UI auth chain resolved**: every request carries the
subject, role, read-only flag and tenant of the caller. On an agent from
`v0.10.0` on, that identity reaches the application's model hooks, the
application's per-model policy applies to the operator per verb (`list`,
`retrieve`, `create`, `update`, `delete`, `bulk_delete`) through the
agent's authorizer, and an operator scoped to a tenant only sees and
touches that tenant's rows — reads are confined by the model's tenant
column, a create is stamped with the tenant, an update or delete first
confirms the row is the tenant's. The tenant comes from the trusted proxy:
`--ui-tenant-header` (default `X-Auth-Tenant`), honoured on the same path
as `--ui-auth-header`.

Every mutation the server routes leaves an entry in its audit log that
says what changed: the record as written on a create, the record as it was
on a delete, both on an update, and one record per row on a bulk action.
The agent returns the previous values with its answer, so the server writes
both sides without a second round trip. A side larger than 64 KiB is
replaced by a marker that says so and how large it was. The log is the
in-memory ring the `Audit log` screen reads; it does not survive a restart.

An agent older than that ignores the identity and behaves as before — no
policy, no tenant — so the server-side gates below stay in front for every
agent. The `Access control` screen is a read-only snapshot of each node's
policy; enforcement happens on the agent. A `ListRecordsRequest` carries
filters with an operator (`where`, the same twelve the panel accepts) and
the agent applies them — or refuses one it does not know, rather than
dropping it — and every list the fleet serves carries an exact total,
filtered or not.

**Data Studio mutations are refused by default.** The
gates, all server-side:

- `--datastudio-allowed-models` — comma-separated model names Data Studio
  may mutate (create/update/delete/bulk). Empty (the default) refuses
  every mutation with `PermissionDenied`; `"*"` allows all models. Reads
  are not gated by this list.
- `--ui-role-header` (default `X-Auth-Role`) — the trusted proxy sets it to
  `viewer` for a read-only operator: mutations refused, reads keep working;
- `--ui-read-only` — makes **every** operator read-only, turning the server
  into a pure observability plane.

Also set `--ui-proxy-secret` (above), so a co-located process inside the
trusted range cannot forge an operator identity with CIDR membership alone,
and keep `--ui-trusted-cidrs` as narrow as your proxy's real source range.
Treat read-write access to the UI listener as admin access over every
allowlisted model of every connected node.
:::

### Behind an SSO reverse proxy (recommended)

The server does **not** implement OIDC. The canonical deployment runs an
auth-aware reverse proxy (oauth2-proxy, nginx `auth_request`, Traefik
forward-auth) in front of `--ui-addr`, forwarding the authenticated identity in
headers:

- the proxy authenticates the user (OIDC/SSO) and sets `X-Auth-User` — and
  optionally `X-Auth-Email` and `X-Auth-Role` — on every upstream request;
- it also sets `X-Auth-Proxy-Secret: $NUCLEUS_ADMIN_UI_PROXY_SECRET`, so the
  server honours those headers only from the real proxy;
- `--ui-trusted-cidrs` lists the proxy's source network. Requests from outside
  it are never trusted.

An oauth2-proxy sketch:

```
--set-xauthrequest=true                 # emits X-Auth-Request-User/-Email
# map those to the headers the server reads, e.g. via nginx:
#   proxy_set_header X-Auth-User        $upstream_http_x_auth_request_user;
#   proxy_set_header X-Auth-Email       $upstream_http_x_auth_request_email;
#   proxy_set_header X-Auth-Proxy-Secret $ui_proxy_secret;
```

For a proxy-less setup — development, or a trusted internal network — a bearer
token works instead: start with `--ui-bearer` and send
`Authorization: Bearer <token>`.

## What runs inside

**Two listeners**, one for agents and one for UIs, each with its own auth
chain: h2c by default, TLS when configured (mutual TLS on the agent
listener with `--agent-client-ca`, and `--agent-identity-from-cert` to make
the certificate's Common Name the only `node_id` that agent may register
under). Listener certificates are served from their files and re-read when
a handshake finds them changed, so a rotation needs no restart. `/healthz` is
public on both, carved out of auth so load balancers can probe it.

**Routing primitives** move frames between them:

- a registry of connected agents;
- per-UI subscription fanout, dropping newest under backpressure;
- a drop-oldest replay buffer serving `include_recent`;
- request-ID correlation for snapshots, Data Studio operations and RBAC
  snapshots.

**The manage surface** reads two different stores, and it is worth knowing
which is which:

- The **Access control** screen shows a read-only Casbin snapshot routed to a
  connected agent. The application's authorizer stays the single writer.
- The **Audit log** screen shows the server's own fleet-plane audit ring:
  mutations an operator performed *through this server* (Data Studio
  create/update/delete/bulk), attributed to the identity resolved by the UI
  auth chain and to the node the request was routed to. It is in-memory and
  bounded, like event replay. Admin actions performed inside an application
  stay in that node's own in-process Orbit panel.

**Auth** is a shared bearer token for agents, and trusted-proxy or bearer
middleware for UIs. The resolved operator identity travels in the request
context, which is what attributes audit entries.

## Retention

By default the server keeps nothing across a restart: recent events live in
bounded ring buffers, the audit log in a ring of 2048 entries, and host
metrics as the last sample per node. Give it a data directory and it
retains, in one SQLite file it creates there:

```bash
admin-server --data-dir /var/lib/orbit-admin --retention 168h
```

- **Events** it forwards to the UI are written as they arrive, and at start
  the replay buffers are filled from the file: a panel opened after a
  restart shows what the previous process saw.
- **The audit log** is written entry by entry and read from the file, so a
  restart does not lose who changed what.
- **Host metrics** are kept as a series per node, one sample per heartbeat.

`--retention` (env `NUCLEUS_ADMIN_RETENTION`, default 7 days) is the window:
nothing older is served, whether or not the janitor that deletes old rows
has run yet, and it runs every minute. A negative value keeps everything.
The audit log downloads from `GET /api/audit/export?format=csv|json` on the
UI listener, behind the same authentication as the rest of the UI API, up to
10000 rows newest first. Two servers must not share a data directory.

The agent does its part: while its stream is down it keeps listening under
the subscriptions the server had and parks the events in its ring buffer,
which the next stream sends first. That buffer is bounded, so an outage
longer than it keeps the newest events per type and counts the rest as
dropped (`admin_agent_events_parked_total`, `admin_agent_events_dropped_total`).

## Alerts

The server evaluates threshold rules against the host metrics every
heartbeat carries, and tells you where you asked. Rules live in a JSON file:

```json
{
  "rules": [
    {"name": "Hot CPU", "metric": "cpu_percent", "op": ">", "threshold": 90,
     "for": "2m", "severity": "critical", "channels": ["ops"]},
    {"name": "Goroutine leak", "metric": "goroutines", "op": ">", "threshold": 5000,
     "for": "5m", "node_ids": ["api-*"], "channels": ["ops", "email"]}
  ]
}
```

```bash
admin-server --alert-rules-file /etc/orbit-admin/alerts.json   --alert-webhooks ops=https://hooks.example/orbit   --alert-smtp-addr mail.example:587 --alert-smtp-from orbit@example --alert-smtp-to ops@example
```

A rule names a `HostMetrics` field (`cpu_percent`, `rss_bytes`,
`heap_alloc_bytes`, `goroutines`, `gc_pause_p99_ms`, `db_in_use`,
`db_idle`, `db_max_open`), an operator (`>`, `>=`, `<`, `<=`, `==`), a
threshold and how long the condition must hold (`for`, zero fires on the
first breaching sample). It applies to every node or to the node ids and
glob patterns in `node_ids`. When it fires, and again when it resolves, the
server notifies the channels the rule names: a **webhook** POSTs the alert
as JSON (`--alert-webhooks name=url`, several separated by commas), and the
**e-mail** channel sends a plain-text message (`--alert-smtp-addr`,
`--alert-smtp-from`, `--alert-smtp-to`; credentials from
`NUCLEUS_ADMIN_ALERT_SMTP_USER` and `NUCLEUS_ADMIN_ALERT_SMTP_PASSWORD`).
A rule that names a channel you did not configure stops the server at
start: a rule that would notify nobody by mistake is the quiet failure
alerts exist to prevent. A failed delivery is logged, not retried; the
alert itself is raised regardless.

The UI API exposes the state behind the same authentication as the rest:
`AlertService.ListAlertRules`, `ListAlerts` (firing first, resolved on
request) and `StreamAlerts`. The last 1024 resolved alerts are kept in
memory; alerts are not retained across a restart.

## The server's own metrics

With `--metrics-addr`, `/metrics` serves the Go runtime's collectors and
the server's own: `admin_server_nodes_connected` and `nodes_known`,
`frames_received_total`, `events_received_total{type}`,
`heartbeats_received_total`, `datastudio_requests_total{outcome}`,
`alerts_firing`, `alerts_fired_total`, `alerts_resolved_total`,
`replay_buffered_events`, and `events_published_total` /
`events_dropped_total` for the UI subscriptions.

## Operational notes

- `/metrics` is opt-in. `--metrics-addr` (env `NUCLEUS_ADMIN_METRICS_ADDR`)
  runs a third listener serving the Prometheus default registry — `go_*` and
  `process_*` collectors; there are no server-specific collectors yet — plus
  `/healthz`. It is unauthenticated by design, so bind it to a private
  interface. Empty, the default, disables it.
- Structured logging via `slog`, JSON or text.
- **Per-stream events are never persisted.** The replay buffer is in-memory and
  bounded.
- Graceful shutdown on signal: a best-effort `Shutdown` with a 2-second timeout
  per listener.
