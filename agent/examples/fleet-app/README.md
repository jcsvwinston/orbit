# agent/examples/fleet-app

The cluster-observability leg of Orbit: a [Nucleus](https://github.com/jcsvwinston/nucleus)
host app wired with the **Orbit agent** (`orbit/agent`). The agent ships
this process's HTTP/SQL observability to a standalone
[admin server](../../../server) over a bidi stream, so one operator can
watch many framework processes ("the fleet") from a single UI.

The agent is strictly opt-in and **fail-open**: it never sits on the
framework's hot path, and if the admin server is unreachable the app runs
unchanged while the agent retries with backoff.

## Run it end-to-end

Two processes. First the admin server (from the `orbit/server` module):

```bash
cd ../../../server
go run ./cmd/admin-server --agent-addr=127.0.0.1:9090
```

`127.0.0.1` keeps the agent listener on loopback for local dev. For any
non-loopback deployment, authenticate the listener with `--agent-token`
(then set the same value here via `ORBIT_ADMIN_TOKEN`) or with TLS
(`--agent-cert`/`--agent-key`).

Then this app, pointing at it:

```bash
cd orbit/agent/examples/fleet-app
ORBIT_ADMIN_ENDPOINT=http://127.0.0.1:9090 go run .
```

Open the admin server UI (its `--ui-addr`, default <http://localhost:8080>).
This node shows up in the topology; send some requests to the app and they
stream into the live feed. If the admin server sets `--agent-token`, pass
the same value here via `ORBIT_ADMIN_TOKEN`.

## What to look at

- [`main.go`](main.go) — wiring the agent as an `app.Extension` behind an
  env-gated switch (`ORBIT_ADMIN_ENDPOINT`).
- [`nucleus.yaml`](nucleus.yaml) — a minimal config (one SQLite database).

For the in-process admin panel (no separate server), see
[`../../../examples/minimal`](../../../examples/minimal).
