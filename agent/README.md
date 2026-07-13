# orbit/agent

The Nucleus admin observability agent. Embeds in every framework
process and ships events to a standalone admin server (`../server`)
over a single Connect-RPC bidi stream.

## Wiring into a Nucleus app

The agent owns its configuration type (`ExtensionConfig`) — the
framework carries no admin-specific config. Populate it directly and
pass it to `NewExtension` with the framework's state directory and
your app's version string:

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

When `ExtensionConfig.Endpoints` is empty, the extension is a no-op and
the framework runs unchanged. When it is set, the agent starts in
parallel with the framework's `Run`; observability events flow through
the framework's `pkg/observability` bus into the bidi stream.

The full configuration surface is the godoc of `ExtensionConfig`
(`extension_config.go`).

## Layered structure

| Sub-package         | Responsibility                                                                                                     |
|---------------------|--------------------------------------------------------------------------------------------------------------------|
| `identity`          | Resolves the persistent NodeID (UUIDv4 in `${state_dir}/node_id`) with hostname-derived ephemeral fallback.        |
| `convert`           | `pkg/observability` events → proto events; `Filter` proto → in-process Filter.                                     |
| `sampler`           | Per-kind sampling rate + HTTP/SQL filter from server-side `Subscribe` commands.                                   |
| `buffer`            | Per-event-kind drop-oldest ring buffer for bridging brief disconnects.                                            |
| `connection`        | Endpoint failover dialer with exponential backoff (cap 30s) and rate-limited disconnect WARN (1/min).             |
| `stream`            | Bidi stream lifecycle: registration, three-goroutine recv/send/heartbeat loop, command dispatch, replay on reconnect. Events ship with the agent's registered NodeID (the in-process bus NodeID is host-local and does not correlate with the fleet registry). Snapshot providers: `GO_RUNTIME` and `REGISTERED_MODELS`; other types answer with a per-type error. |
| `metrics`           | `admin_agent_*` Prometheus collectors + standalone `/metrics` + `/healthz` server.                                |
| `rbac`              | Read-only Casbin snapshot handler for the fleet UI's Access control screen (wired from `app.Authorizer`).         |
| `internal/testserver` | In-process h2c admin server fake for integration tests. (Internal; do not import.)                              |

The top-level `Agent` (`agent.go`) composes everything and exposes:

* `New(cfg)` — constructor; `ErrDisabled` when no endpoints.
* `Run(ctx)` — blocks until ctx cancels; reconnect loop with backoff.
* `NodeID()` — the resolved identifier.
* `Connected()` — channel closed on first successful stream open.
  Used by `NewExtension(...).Attach` when `ExtensionConfig.
  RequireConnection` is true (boot blocks until the stream opens or
  `RequireConnectionTimeout` expires).
* `Metrics()` — the Prometheus registry, in case the host wants to
  serve `/metrics` from its own port.

## Hot-path invariants

The agent never blocks the framework's request thread. Every public
producer-side path (the HTTP middleware, the SQL observer) starts with
a single atomic load on `pkg/observability.Bus.HasSubscribers(kind)`
and short-circuits when nobody is watching.

## Tests

```bash
cd agent && go test -race ./...
```

Integration tests live in `agent_test.go` and `extension_test.go`. The
in-process `testserver` fakes the admin server with enough fidelity to
exercise reconnect, subscribe/unsubscribe, drain, and Goodbye paths.

## Distribution

Released as its own Go module with component tags (`agent/vX.Y.Z`,
via release-please). Consumer apps add it with:

```bash
go get github.com/jcsvwinston/orbit/agent
```

Pre-1.0: the module version signals honestly that the agent's public
surface may still change before its own v1.0.
