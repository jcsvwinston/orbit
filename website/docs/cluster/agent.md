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

`TLS` itself cannot come from a config file, but the PEM files can: four
fields name them, and the agent loads them on top of whatever `TLS` carries
(nil included). Set them in code or, if your application unmarshals its own
configuration into `ExtensionConfig`, through their `koanf` keys:

```go
agent.ExtensionConfig{
    Endpoints:     []string{"https://admin.internal:9090"},
    TLSCertFile:   "/etc/orbit/agent.crt", // client certificate (mutual TLS)
    TLSKeyFile:    "/etc/orbit/agent.key", // both or neither
    TLSCAFile:     "/etc/orbit/ca.crt",    // private CA that signed the server
    TLSServerName: "",                     // when the endpoint host is not a name on the certificate
}
// koanf keys: tls_cert_file, tls_key_file, tls_ca_file, tls_server_name
```

`agent.ExtensionConfig.TLSConfig()` is the resolution the extension applies:
the certificate file replaces `TLS.Certificates`, the CA bundle becomes
`TLS.RootCAs`, the server name overrides the endpoint's host. A certificate
without its key, a missing file or a CA bundle with no PEM certificate fail
the boot with the field named.

**The certificate rotates without a restart.** The agent serves its client
certificate *from* the files: every handshake checks whether the two files
changed (size or modification time) and re-reads them when they did, so the
next connection presents the new certificate. Under a live stream that is the
next reconnect; a rotation is a write to two files. Write the certificate and
the key as a pair — a key that does not match yet keeps the previous
certificate in use, with one WARN, until the pair is whole. Keep the Common
Name across rotations when the server binds node identity to the certificate:
the node's name is fixed at boot. The CA bundle is read once. The `/healthz`
probe the agent sends before opening a stream uses the same configuration and
carries no token.

## Data Studio through the contract

The agent serves the fleet's Data Studio through Orbit's `datasource`
contract — the same one the in-process panel speaks. By default it builds
the Nucleus adapter over the application's model registry and database
handles; an application on the Quark ORM hands its `quarkdatasource`
adapter to `ExtensionConfig.DataSource` (set in code, as for
`orbit.Config.DataSource`) and the fleet browses and edits Quark models.

Each request carries the operator the admin server resolved. The agent runs
it as the panel would: the framework identity reaches your model hooks
(`auth.ClaimsFromContext`), your policy applies per model and verb through
the agent's `Authorizer` (`*authz.Enforcer` decides directly; a policy
source that only exposes rows is compiled into one), and an operator scoped
to a tenant is confined to the rows whose tenant column says so. A request
without an operator — an admin server older than `v0.15.0` — runs with no
identity, no policy and no tenant, exactly as before; the server's own
gates (mutation allowlist, read-only role) stay in front either way.

## Node identity

The agent resolves a stable **NodeID**: a UUIDv4 persisted at
`${state_dir}/node_id`, falling back to a hostname-derived ephemeral value when
the state directory is unavailable.

This is the identity the agent registers under, and the value every fleet view
keys on — the `Nodes` page, per-node stream filters, and the metrics cards.

**With a client certificate from files and no `node_id`, the node is named
after the certificate's Common Name.** The server authenticates that name at
the handshake (`agent:<CN>`), so writing it once, in the certificate, is what
lets a server that binds node identity to the certificate
(`--agent-identity-from-cert`) accept the agent. An explicit `node_id` still
wins — and is then what such a server compares with the certificate: if the
two differ, the registration is refused with `PermissionDenied` and the
server log names both. A server without that flag registers the declared
`node_id` and logs a WARN when it differs from the certificate.
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
