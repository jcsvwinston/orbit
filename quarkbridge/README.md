# quarkbridge

`github.com/jcsvwinston/orbit/quarkbridge`

An opt-in [Quark](https://github.com/jcsvwinston/quark) middleware that publishes
the SQL statements a Quark client executes onto a [Nucleus](https://github.com/jcsvwinston/nucleus)
observability feed, so they appear in Orbit's live SQL view — correlated to the
originating HTTP request.

## Why it exists

Orbit's live SQL view drains Nucleus's event bus (`nucleus.EventBus.SubscribeSQL`).
The framework's own CRUD layer already feeds that bus, but an application that runs
its queries through the Quark ORM instead does not — those statements never reach
the feed. This bridge closes that gap **without teaching Orbit about Quark or
Quark about Nucleus**: it maps each executed statement to a `nucleus.SQLEvent` and
publishes it through Nucleus's public SQL ingest (`EmitSQL`, Nucleus ADR-020),
which Orbit already consumes.

It is deliberately a separate, opt-in module that depends on **both** Quark and
Nucleus — it does not belong in either product's core (suite decision
[QADR-0006](https://github.com/jcsvwinston/quantum/blob/main/docs/adr/QADR-0006-integracion-quark-orbit.md)).

## Usage

```go
import (
    "github.com/jcsvwinston/orbit/quarkbridge"
    "github.com/jcsvwinston/quark"
)

// rt is the nucleus.Runtime handed to your module's OnStart hook.
bridge := quarkbridge.New(rt.Observability())

client, err := quark.New("pgx", dsn, quark.WithMiddleware(bridge))
```

Every statement `client` runs is now timed, mapped, and published to the live
feed. `rt.Observability()` returns a `nucleus.EventBus`, which satisfies the
bridge's `SQLSink` directly.

## Request correlation

`RequestID`, `TraceID`, and `UserID` are read from the `context.Context` Quark
threads through the middleware, using Nucleus's own context helpers
(`pkg/observe`). This is why the bridge is a **`quark.Middleware`** (which
receives `ctx`) and not a `quark.QueryObserver` (which does not): without `ctx`
the feed would lose the link to the request.

`ModelName` is left empty — the model/table name is available to Quark's
`QueryObserver`, not to a middleware, which sees only the rendered SQL.
`Operation` is derived from the leading SQL keyword (`SELECT`/`INSERT`/…).

## Redaction

By default (`RedactArgs`) bind arguments are masked the same way Nucleus masks
its own SQL feed: `string` and `[]byte` values become `type(len):***` markers,
while numeric, `bool`, `time.Time`, and `nil` values are kept verbatim (so a
`WHERE id = ?` key still reads as e.g. `42`). Bridged statements therefore render
consistently alongside framework ones.

Opt into raw values for local debugging only:

```go
bridge := quarkbridge.New(rt.Observability(), quarkbridge.WithRedaction(quarkbridge.IncludeArgs))
```

`WithNodeID("...")` tags events with the framework process id, matching the
`NodeID` Nucleus's own observer stamps.

## Relationship to OpenTelemetry

OTel (`quark/otel`) is complementary, **not** the transport for this feed: its
spans are exported in batch for durable tracing and would not be real time. Run
both if you want durable traces too — sharing the same tracer nests Quark's spans
under the request span — but the live feed goes through this bridge.

## Status

Pre-1.0, alongside Orbit. Its Nucleus dependency is pinned to a line that exposes
the public SQL ingest (`EmitSQL`), which is newer than the pseudo-version the rest
of `orbit/*` pins today; see the module's `go.mod`.
