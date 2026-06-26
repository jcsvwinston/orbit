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
make build              # builds bin/admin-server with the UI embedded
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
  --log-format=json --log-level=info
```

Run `./bin/admin-server --help` (or `--version`) for the full surface. Every
flag has a `NUCLEUS_ADMIN_*` env-var counterpart.

## Shape

- **Two listeners** — one for agents, one for UIs — each with its own auth chain
  (h2c by default, TLS when configured). `/healthz` is public on both, carved
  out of auth for load balancers.
- **Routing primitives** — a connected-agents registry, per-UI subscription
  fanout (drop-newest under backpressure), a drop-oldest replay buffer for
  `include_recent`, and request-ID correlation for snapshots.
- **Auth** — a shared bearer token for agents; trusted-proxy/bearer middleware
  for UIs.

## Operational notes

- `/metrics` is available when `--metrics-addr` is set (not exposed by default).
- Structured logging via `slog`, JSON or text.
- **Per-stream events are never persisted.** The replay buffer is in-memory and
  bounded.
- Graceful shutdown on signal: best-effort `Shutdown` with a 2-second timeout
  per listener.
