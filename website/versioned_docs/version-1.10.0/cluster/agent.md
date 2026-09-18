---
title: orbit/agent
sidebar_position: 3
description: The in-process observability agent.
---

# orbit/agent

The observability agent embeds in every framework process and ships events to a
standalone [admin server](./server.md) over a single Connect-RPC bidirectional
stream. One agent per application node; many agents per server.

## Wiring it into an app

The agent owns its configuration type (`agent.ExtensionConfig`) — the framework
carries no admin-specific config. Populate it directly, for example from your
own config file, and pass it to `agent.NewExtension` together with the
framework's state directory and your application's version string:

```go
import (
    "context"
    "log"
    "os"

    "github.com/jcsvwinston/nucleus/pkg/app"
    "github.com/jcsvwinston/orbit/agent"
)

func main() {
    cfg, err := app.LoadConfig("nucleus.yml")
    if err != nil {
        log.Fatal(err)
    }
    a, err := app.New(cfg,
        app.WithExtensions(
            agent.NewExtension(agent.ExtensionConfig{
                Endpoints: []string{"https://admin.internal:9090"},
                Token:     os.Getenv("NUCLEUS_ADMIN_TOKEN"),
            }, cfg.StateDir, "v1.2.3"), // your app's version string
        ),
    )
    if err != nil {
        log.Fatal(err)
    }
    if err := a.Run(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

`Endpoints` is the switch. Leave it empty and the extension is a **no-op**: the
framework runs exactly as it would without it. Set it and the agent starts in
parallel with the framework's `Run`, and observability events flow from the
framework's `pkg/observability` bus into the stream.

### TLS

The scheme of each endpoint picks the transport: `http://` is cleartext
HTTP/2 (h2c, for development), `https://` performs a real TLS handshake and
negotiates HTTP/2 through ALPN. By default `https://` trusts the system
store; for a private CA, or when the admin server requires client
certificates (`--agent-client-ca`), pass a `*tls.Config` in `TLS`:

```go
pool := x509.NewCertPool()
pool.AppendCertsFromPEM(caPEM)
cert, _ := tls.LoadX509KeyPair("agent.crt", "agent.key")

agent.NewExtension(agent.ExtensionConfig{
    Endpoints: []string{"https://admin.internal:9090"},
    TLS: &tls.Config{
        RootCAs:      pool,                     // private CA
        Certificates: []tls.Certificate{cert}, // client certificate (mutual TLS)
        MinVersion:   tls.VersionTLS12,
    },
}, cfg.StateDir, appVersion)
```

`TLS` is not read from a config file; build it in code from the PEM files
your deployment ships. The `/healthz` probe the agent sends before opening
a stream uses the same configuration and carries no token.

## Node identity

The agent resolves a stable **NodeID**: a UUIDv4 persisted at
`${state_dir}/node_id`, falling back to a hostname-derived ephemeral value when
the state directory is unavailable.

This is the identity the agent registers under, and the value every fleet view
keys on — the `Nodes` page, per-node stream filters, and the metrics cards.
Events shipped over the stream carry the same NodeID, so an event's `node_id`
always matches a registered node. (The agent stamps it over the in-process
bus's own node label, which is host-local and does not correlate with the fleet
registry.)

## Hot-path cost

The agent never blocks the framework's request thread. Every producer-side path
— the HTTP middleware, the SQL observer — starts with a single atomic load on
`observability.Bus.HasSubscribers(kind)` and short-circuits when nobody is
watching.

## What's inside

The agent is layered:

- **Node identity** — resolution and persistence, as described above.
- **Event pipeline** — conversion and sampling, then a drop-oldest ring buffer
  that absorbs backpressure while the stream is open (it does not buffer
  across disconnects).
- **Transport** — an endpoint-failover dialer with exponential backoff, and the
  bidirectional stream lifecycle: registration, recv/send/heartbeat, and replay
  on reconnect.
- **Metrics** — the `admin_agent_*` Prometheus collectors.
- **RBAC snapshot** — the read-only handler behind the fleet UI's Access
  control screen, wired automatically from the application's authorizer when
  the extension attaches. No extra configuration.

The top-level `Agent` exposes `New`, `Run`, `NodeID`, `Connected`, and
`Metrics`.
