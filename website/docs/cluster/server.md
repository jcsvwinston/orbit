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

:::warning Any authenticated operator can write to every node by default
An authenticated UI operator can run **every Data Studio mutation on every
model of every connected node** — the `Access control` screen is a read-only
snapshot of each node's own policy and does **not** gate the operator's
fleet-plane actions (they are audited, not authorized per verb/object). Scope
operators down with:

- `--ui-role-header` (default `X-Auth-Role`): the trusted proxy sets it to
  `viewer` for a read-only operator (mutations refused, reads keep working);
- `--ui-read-only`: makes **every** operator read-only — a pure observability
  plane.

Also set `--ui-proxy-secret` (above) so a co-located process inside the
trusted CIDR can't forge an operator identity with CIDR membership alone, and
keep `--ui-trusted-cidrs` as narrow as your proxy's real source range. Treat
read-write access to the UI listener as full fleet-admin access.
:::

### Behind an SSO reverse proxy (recommended)

The server does **not** implement OIDC; the canonical deployment runs an
auth-aware reverse proxy (oauth2-proxy, nginx `auth_request`, Traefik
forward-auth) in front of `--ui-addr` and forwards the authenticated identity
in headers:

- the proxy authenticates the user (OIDC/SSO) and sets `X-Auth-User` (and
  optionally `X-Auth-Email`, `X-Auth-Role`) on every upstream request;
- it also sets `X-Auth-Proxy-Secret: $NUCLEUS_ADMIN_UI_PROXY_SECRET` so the
  server honours those headers only from the real proxy;
- `--ui-trusted-cidrs` lists the proxy's source network; requests from
  outside it are never trusted.

An oauth2-proxy sketch:

```
--set-xauthrequest=true                 # emits X-Auth-Request-User/-Email
# map those to the headers the server reads, e.g. via nginx:
#   proxy_set_header X-Auth-User        $upstream_http_x_auth_request_user;
#   proxy_set_header X-Auth-Email       $upstream_http_x_auth_request_email;
#   proxy_set_header X-Auth-Proxy-Secret $ui_proxy_secret;
```

For a proxy-less setup (dev, or a trusted internal network), a bearer token
works instead: start with `--ui-bearer` and send
`Authorization: Bearer <token>`.

## Shape

- **Two listeners** — one for agents, one for UIs — each with its own auth chain
  (h2c by default, TLS when configured). `/healthz` is public on both, carved
  out of auth for load balancers.
- **Routing primitives** — a connected-agents registry, per-UI subscription
  fanout (drop-newest under backpressure), a drop-oldest replay buffer for
  `include_recent`, and request-ID correlation for snapshots, Data Studio
  operations and RBAC snapshots.
- **Manage surface** — the Access control screen reads a **read-only Casbin
  snapshot** routed to a connected agent (the application's authorizer stays
  the single writer); the Audit log screen reads the server's own
  **fleet-plane audit ring**: mutations an operator performed THROUGH this
  server (Data Studio create/update/delete/bulk), attributed to the identity
  resolved by the UI auth chain and to the routed node. In-memory and
  bounded, like event replay; per-app admin actions stay in each node's
  in-process Orbit panel.
- **Auth** — a shared bearer token for agents; trusted-proxy/bearer middleware
  for UIs (the resolved operator identity travels in the request context and
  attributes audit entries).

## Operational notes

- `/metrics` is opt-in: `--metrics-addr` (env `NUCLEUS_ADMIN_METRICS_ADDR`)
  runs a third listener serving the Prometheus default registry
  (`go_*`/`process_*` collectors; server-specific collectors are future
  work) plus `/healthz`. Unauthenticated by design — bind it to a private
  interface. Empty (the default) disables it.
- Structured logging via `slog`, JSON or text.
- **Per-stream events are never persisted.** The replay buffer is in-memory and
  bounded.
- Graceful shutdown on signal: best-effort `Shutdown` with a 2-second timeout
  per listener.
