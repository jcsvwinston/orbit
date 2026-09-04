---
title: Deployment
sidebar_position: 1
description: Run the fleet observability plane — the admin server and its agents.
---

# Deployment

Orbit has two deployment shapes, and most applications only ever use the first:

1. **The in-process panel** — `orbit.Module(...)` mounted on your Nucleus app.
   It ships inside your application binary (the UI is embedded with
   `go:embed`), so there is **nothing separate to deploy**: build your app,
   run your app, open `/admin`. See the [Quick start](../quick-start.md).
2. **The fleet plane** — an optional, standalone **admin server** that many
   application nodes stream telemetry to through an embedded **agent**. This
   page is about deploying that.

Turning on the Redis live-feed relay (the `cluster_*` keys in
[Configuration](../configuration.md)) does not change shape 1. It adds a Redis
instance to your infrastructure; there is still no Orbit process to deploy.

## Topology

```text
                    app node 1 ── orbit/agent ──┐
                    app node 2 ── orbit/agent ──┼──> admin-server ──> operator's browser
                    app node N ── orbit/agent ──┘    │
                                                     │  :9090  agent listener  (bidi stream)
   (each app node also keeps its                     │  :8080  UI listener     (behind your SSO proxy)
    own in-process /admin panel)                     │  :9091  metrics listener (opt-in)
```

Each agent embeds in a framework process and streams events over one
Connect-RPC bidirectional stream. The server fans them out to connected
operator UIs. Agents dial **out** to the server; the server never dials into
your application nodes.

## Install the server binary

The server is a single static binary with the operator UI embedded — no asset
pipeline, no database, no Node toolchain needed:

```bash
go install github.com/jcsvwinston/orbit/server/cmd/admin-server@latest
```

`@latest` resolves the current tag of the `orbit/server` module. For
reproducible installs, pin the tag instead. The current release is stated on
the [Quick start](../quick-start.md) page, and the module tags cut with each
release are listed in the [Release notes](../reference/release-notes.md).

```bash
admin-server --version   # prints the installed tag (or "devel" for source builds)
```

Building from a checkout works too (`cd server && go build ./cmd/admin-server`).

## Configuration

Configuration comes from three sources. Highest precedence first:

1. Command-line flags
2. Environment variables (`NUCLEUS_ADMIN_*`)
3. Built-in defaults

Every flag has an environment-variable counterpart. `admin-server --help`
prints the authoritative list; the tables below cover the flags you will
actually reach for.

### Listeners

| Flag | Environment variable | Default | What it does |
|---|---|---|---|
| `--agent-addr` | `NUCLEUS_ADMIN_AGENT_ADDR` | `:9090` | Address the agent listener binds. Agents dial here. |
| `--ui-addr` | `NUCLEUS_ADMIN_UI_ADDR` | `:8080` | Address the UI/operator listener binds. Browsers (or your reverse proxy) hit this. |
| `--metrics-addr` | `NUCLEUS_ADMIN_METRICS_ADDR` | *(empty — disabled)* | Opt-in third listener serving Prometheus `/metrics` plus `/healthz`. |

### Agent authentication

| Flag | Environment variable | Default | What it does |
|---|---|---|---|
| `--agent-token` | `NUCLEUS_ADMIN_AGENT_TOKEN` | *(empty)* | Shared bearer token every agent must present. |
| `--agent-cert` | `NUCLEUS_ADMIN_AGENT_CERT` | *(empty)* | PEM certificate for the agent listener (enables TLS). |
| `--agent-key` | `NUCLEUS_ADMIN_AGENT_KEY` | *(empty)* | PEM key for the agent listener. |
| `--agent-client-ca` | `NUCLEUS_ADMIN_AGENT_CLIENT_CA` | *(empty)* | PEM CA bundle; when set, agents must present a client certificate signed by it (mutual TLS). Requires `--agent-cert`/`--agent-key`. |
| `--insecure-agent-listener` | `NUCLEUS_ADMIN_INSECURE_AGENT_LISTENER` | `false` | Allow an unauthenticated agent listener on a non-loopback interface. |

The server **refuses to start** when the agent listener would bind a
non-loopback interface with no `--agent-token` and no client-certificate
requirement (a server certificate alone encrypts but does not
authenticate). You have three ways out: authenticate the listener (token
or `--agent-client-ca`), bind it to loopback, or pass
`--insecure-agent-listener` — and that last one only when the network layer
already restricts who can reach it. See [Security](./security.md).

### Operator authentication

| Flag | Environment variable | Default | What it does |
|---|---|---|---|
| `--ui-bearer` | `NUCLEUS_ADMIN_UI_BEARER` | *(empty)* | Fallback bearer token for direct UI access without a reverse proxy. |
| `--ui-auth-header` | `NUCLEUS_ADMIN_UI_AUTH_HEADER` | `X-Auth-User` | Trusted-proxy header carrying the authenticated user. |
| `--ui-email-header` | `NUCLEUS_ADMIN_UI_EMAIL_HEADER` | `X-Auth-Email` | Trusted-proxy header carrying the user's email. |
| `--ui-role-header` | `NUCLEUS_ADMIN_UI_ROLE_HEADER` | `X-Auth-Role` | Trusted-proxy header carrying the operator role; `viewer` means read-only. |
| `--ui-trusted-cidrs` | `NUCLEUS_ADMIN_UI_TRUSTED_CIDRS` | loopback only | Comma-separated CIDRs allowed to set the trusted-proxy headers. |
| `--ui-proxy-secret` | `NUCLEUS_ADMIN_UI_PROXY_SECRET` | *(empty)* | Shared secret the proxy must echo in `X-Auth-Proxy-Secret` before its headers are honoured. |
| `--ui-read-only` | `NUCLEUS_ADMIN_UI_READ_ONLY` | `false` | Make every operator read-only (Data Studio mutations refused). |
| `--ui-cert` | `NUCLEUS_ADMIN_UI_CERT` | *(empty)* | PEM certificate for the UI listener (enables TLS). |
| `--ui-key` | `NUCLEUS_ADMIN_UI_KEY` | *(empty)* | PEM key for the UI listener. |

### Logging and lifecycle

| Flag | Environment variable | Default | What it does |
|---|---|---|---|
| `--log-level` | `NUCLEUS_ADMIN_LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error`. |
| `--log-format` | `NUCLEUS_ADMIN_LOG_FORMAT` | `json` | `json` \| `text` (structured logging via `slog`, to stderr). |
| `--version` | — | — | Print the build version and exit. |

Both listeners serve HTTP/2 — cleartext (h2c) by default, TLS (with ALPN
HTTP/2, nothing served in the clear) when a cert/key pair is supplied.
`/healthz` answers unauthenticated on every listener, so load balancers can
probe without owning a token. The server shuts down gracefully on
`SIGINT`/`SIGTERM`.

## A systemd unit

```ini
# /etc/systemd/system/orbit-admin-server.service
[Unit]
Description=Orbit admin server (fleet observability)
After=network-online.target
Wants=network-online.target

[Service]
User=orbit
Group=orbit
ExecStart=/usr/local/bin/admin-server \
  --agent-addr=:9090 \
  --ui-addr=127.0.0.1:8080 \
  --ui-trusted-cidrs=127.0.0.1/32 \
  --metrics-addr=127.0.0.1:9091
# NUCLEUS_ADMIN_AGENT_TOKEN=…  NUCLEUS_ADMIN_UI_PROXY_SECRET=…
EnvironmentFile=/etc/orbit/admin-server.env
Restart=on-failure
RestartSec=2
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

Keep the tokens in the `EnvironmentFile` (mode `0600`) rather than on the
command line, where the process list would expose them.

Read the two listener choices above as a pair. The UI listener binds loopback,
because your SSO reverse proxy runs on the same host and forwards to it. The
agent listener binds all interfaces, which is exactly why it needs the token.

## A container

```dockerfile
FROM golang:1.26 AS build
RUN CGO_ENABLED=0 go install github.com/jcsvwinston/orbit/server/cmd/admin-server@latest

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /go/bin/admin-server /admin-server
EXPOSE 8080 9090
ENTRYPOINT ["/admin-server"]
```

```bash
docker run -d --name orbit-admin \
  -p 9090:9090 -p 8080:8080 \
  -e NUCLEUS_ADMIN_AGENT_TOKEN="$AGENT_TOKEN" \
  -e NUCLEUS_ADMIN_UI_BEARER="$UI_TOKEN" \
  orbit-admin-server
```

The binary is fully self-contained (UI included), so `distroless/static` is
enough. Use `/healthz` on either published port as the container health
check.

## Wire the agents

Each application node runs the agent as a framework extension. Add the
module and pass the server's agent endpoint plus the shared token:

```bash
go get github.com/jcsvwinston/orbit/agent
```

```go
import (
    "os"

    "github.com/jcsvwinston/nucleus/pkg/app"
    "github.com/jcsvwinston/orbit/agent"
)

a, err := app.New(cfg,
    app.WithExtensions(
        agent.NewExtension(agent.ExtensionConfig{
            Endpoints: []string{"https://admin.internal:9090"},
            Token:     os.Getenv("ORBIT_ADMIN_TOKEN"),
        }, cfg.StateDir, appVersion),
    ),
)
```

The agent is **fail-open** by default, in two senses. With an empty `Endpoints`
list the extension does nothing at all. With endpoints set but the server
unreachable, the application still runs unchanged while the agent retries with
exponential backoff, capped at 30 seconds.

Set `RequireConnection: true` to invert that: the application then refuses to
boot unless an admin endpoint accepts it within `RequireConnectionTimeout`
(default 10 seconds). What that gate actually verifies is worth reading before
you rely on it — see
[Security](./security.md#the-healthz-exemption).

The full agent configuration surface — heartbeat cadence, ring-buffer
sizes, node labels, a standalone metrics listener — is documented in
[orbit/agent](../cluster/agent.md) and the `agent.ExtensionConfig` godoc.

## What you do NOT need to deploy

- **No database.** The server keeps its state (node registry, replay
  buffers, the fleet audit ring) in memory, bounded. Restarting it loses
  replay history; agents reconnect and re-register on their own.
- **No asset server.** The operator UI is embedded in the binary.
- **No separate deployment for the in-process panel.** `orbit.Module`
  runs inside each application; the fleet plane is additive, not a
  replacement.
