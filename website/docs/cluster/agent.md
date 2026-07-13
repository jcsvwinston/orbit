---
title: orbit/agent
sidebar_position: 3
description: The in-process observability agent.
---

# orbit/agent

The observability agent embeds in every framework process and ships events to a
standalone [admin server](./server.md) over a single Connect-RPC bidirectional
stream.

## Wiring it into an app

The agent owns its configuration type (`agent.ExtensionConfig`) — the
framework carries no admin-specific config. Populate it directly (e.g. from
your own config file) and pass it to `agent.NewExtension` together with the
framework's state directory and your app's version string:

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

When `ExtensionConfig.Endpoints` is empty the extension is a **no-op** and the
framework runs unchanged. When it is set, the agent starts in parallel with the
framework's `Run`, and observability events flow through the framework's
`pkg/observability` bus into the bidi stream.

## Node identity

The agent resolves a stable **NodeID** — a UUIDv4 persisted at
`${state_dir}/node_id`, with a hostname-derived ephemeral fallback when the
state directory is unavailable. This is the identity the agent registers under
and the value every fleet view keys on: the `Nodes` page, per-node stream
filters, and the metrics cards. Events shipped over the stream carry this same
NodeID (it is stamped over the in-process bus's own node label, which is
host-local and does not correlate with the fleet registry), so an event's
`node_id` always matches a registered node.

## Hot-path cost

The agent never blocks the framework's request thread. Every producer-side path
(the HTTP middleware, the SQL observer) starts with a single atomic load on
`observability.Bus.HasSubscribers(kind)` and short-circuits when nobody is
watching.

## What's inside

The agent is layered: node-identity resolution, event conversion and sampling, a
drop-oldest ring buffer for brief disconnects, an endpoint-failover dialer with
exponential backoff, the bidi stream lifecycle (registration, recv/send/heartbeat,
replay on reconnect), `admin_agent_*` Prometheus collectors, and the read-only
RBAC snapshot handler behind the fleet UI's Access control screen — wired
automatically from the application's authorizer when the extension attaches
(no extra configuration). The top-level `Agent` exposes `New`, `Run`,
`NodeID`, `Connected`, and `Metrics`.
