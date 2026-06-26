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

```go
import (
    "github.com/jcsvwinston/nucleus/pkg/app"
    "github.com/jcsvwinston/orbit/agent"
)

func main() {
    cfg := app.MustLoadConfig("nucleus.yml")
    a, err := app.New(cfg,
        app.WithExtensions(
            agent.NewExtension(cfg.AdminAgent, cfg.StateDir, "v0.1.0"),
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

When `cfg.AdminAgent.Endpoints` is empty the extension is a **no-op** and the
framework runs unchanged. When it is set, the agent starts in parallel with the
framework's `Run`, and observability events flow through `pkg/observability`
into the bidi stream.

## Hot-path cost

The agent never blocks the framework's request thread. Every producer-side path
(the HTTP middleware, the SQL observer) starts with a single atomic load on
`observability.Bus.HasSubscribers(kind)` and short-circuits when nobody is
watching — about 0.25 ns when idle.

## What's inside

The agent is layered: node-identity resolution, event conversion and sampling, a
drop-oldest ring buffer for brief disconnects, an endpoint-failover dialer with
exponential backoff, the bidi stream lifecycle (registration, recv/send/heartbeat,
replay on reconnect), and `admin_agent_*` Prometheus collectors. The top-level
`Agent` exposes `New`, `Run`, `NodeID`, `Connected`, and `Metrics`.
