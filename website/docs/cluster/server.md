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

:::warning Fleet mutations bypass the app's RBAC and tenant filtering
A Data Studio operation routed through the fleet plane executes on the
agent with the agent's own database access: **no operator identity crosses
the stream**, so the application's per-model RBAC and multi-tenant
filtering do not run. The `Access control` screen does not change that: it
is a read-only snapshot of each node's own policy, and it does not gate
the operator's fleet-plane actions, which are audited rather than
authorized per verb and object.

Because of that, **Data Studio mutations are refused by default**. The
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
chain: h2c by default, TLS when configured. `/healthz` is public on both,
carved out of auth so load balancers can probe it.

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
