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

Two processes on one machine. Ports: the admin server takes **:9090**
(agents) and **:8080** (UI); this app serves on **:8000**
(`nucleus.yaml`), so nothing collides.

First the admin server (from the `orbit/server` module):

```bash
cd ../../../server
go run ./cmd/admin-server \
  --agent-addr=127.0.0.1:9090 \
  --ui-addr=127.0.0.1:8080 \
  --ui-insecure-open
```

`127.0.0.1` keeps both listeners on loopback for local dev.
`--ui-insecure-open` is what makes the UI reachable from a plain
browser: the UI listener normally authenticates operators through a
header-setting reverse proxy (or a bearer token, which a browser cannot
send for page loads), so without this flag every request — including
`index.html` — gets a 401. The flag only works on a loopback
`--ui-addr` and is for local development only; for any shared
deployment, front the UI with an auth proxy (see the
[server README](../../../server/README.md)).

Then this app, pointing at it:

```bash
cd orbit/agent/examples/fleet-app
ORBIT_ADMIN_ENDPOINT=http://127.0.0.1:9090 go run .
```

Open <http://127.0.0.1:8080>. This node shows up in the topology; send
some requests to the app (it listens on <http://127.0.0.1:8000>) and
they stream into the live feed.

For any non-loopback deployment, authenticate the agent listener with
`--agent-token` (then set the same value here via `ORBIT_ADMIN_TOKEN`)
or with TLS (`--agent-cert`/`--agent-key`).

Data Studio mutations are refused by default on the fleet plane: to
edit records of a model from the fleet UI, opt it in explicitly with
`--datastudio-allowed-models=ModelName` (or `"*"`).

## What to look at

- [`main.go`](main.go) — wiring the agent as an `app.Extension` behind an
  env-gated switch (`ORBIT_ADMIN_ENDPOINT`).
- [`nucleus.yaml`](nucleus.yaml) — a minimal config (one SQLite database).

For the in-process admin panel (no separate server), see
[`../../../examples/minimal`](../../../examples/minimal).
