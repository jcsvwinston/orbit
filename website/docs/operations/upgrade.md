---
title: Upgrading
sidebar_position: 3
description: Module versions, compatibility, and the order to upgrade in.
---

# Upgrading

Upgrading Orbit should be boring, and it is once you know which of its modules
you actually depend on. Orbit ships several Go modules from one repository,
each with its own SemVer line and its own tags — but as a consumer you only
ever choose two of them.

## The modules and their tags

| Module | Tag format | You use it when… |
|---|---|---|
| `github.com/jcsvwinston/orbit` (root) | `vX.Y.Z` | You mount the **in-process panel** (`orbit.Module`). |
| `github.com/jcsvwinston/orbit/agent` | `agent/vX.Y.Z` | Your app nodes stream to a fleet **admin server**. |
| `github.com/jcsvwinston/orbit/server` | `server/vX.Y.Z` | You run the **admin-server binary** (usually installed, not imported). |
| `github.com/jcsvwinston/orbit/proto` | `proto/vX.Y.Z` | Never directly — it is an internal dependency of agent and server. |
| `github.com/jcsvwinston/orbit/quarkbridge` | `quarkbridge/vX.Y.Z` | Opt-in: Quark ORM statements in the live SQL feed. |
| `github.com/jcsvwinston/orbit/quarkdatasource` | `quarkdatasource/vX.Y.Z` | Data Studio over Quark models — the adapter the panel's `DataSource` takes. |

Sub-modules use Go's directory-prefixed tags. The tag `agent/v0.5.2` is what
`go get github.com/jcsvwinston/orbit/agent@v0.5.2` resolves to. The
[Release notes](../reference/release-notes.md) list which module tags shipped
with each root release.

## What imports what

- **App with the in-process panel** → depends on the **root** module only.
- **App that joins a fleet** → additionally depends on **`orbit/agent`**.
- **The admin server** → the `admin-server` binary, installed from
  **`orbit/server`**. You reinstall it; you do not import it.
- **`orbit/proto`** is pinned by agent and server internally. The server
  module also pins the agent version it was tested against. Consumers only
  ever choose two versions: the root module and (if fleet) the agent.

## The stability promise

The root module's public API — `orbit` itself and the `datasource` contract —
is **frozen for the life of a major**. There are no breaking changes within
one, so moving the root module across the minor versions of a major is a
`go get`-and-rebuild operation. A breaking change is announced in
`docs/deprecations/` at least 90 days before the major that carries it, with
the way to stay where you are.

## Upgrading to 2.0

Two things change at the major, and both were announced in v1.20.0.

**A 2.0 application defines its models with Quark.** Data Studio reads the
data source the application hands it, and that source is a
[`quarkdatasource`](https://github.com/jcsvwinston/orbit/tree/main/quarkdatasource)
adapter over the application's
[Quark](https://github.com/jcsvwinston/quark) models: `orbit.Config.DataSource`
is required, and a panel mounted without one does not start (the error names
the field). The Nucleus adapter over the framework's model registry
(`orbit/datasource/nucleus`), which was the default through 1.x, is gone with
the registry it read, and so are `agent.Config.Registry` and
`agent.Config.Databases`: a fleet node hands the same adapter to
`ExtensionConfig.DataSource` (or `agent.Config.DataSource`), and a node with
none serves no Data Studio. The runtime field-metadata editor (**Fields** in
Data Studio, `PUT /api/models/{name}/schema/fields`) left with the adapter:
which columns are listed, searched and filtered is what the source declares.
An application still on `pkg/model` moves its model layer to Quark first, then
sets the one field:

```go
src := quarkdatasource.New(client) // your *quark.Client
if err := quarkdatasource.Register[Article](src); err != nil {
    log.Fatal(err)
}
orbit.Module(orbit.Config{Prefix: "/admin", DataSource: src})
```

**A server that verifies client certificates binds the node identity to
them.** Under `--agent-client-ca`, an agent registering a `node_id` that is
not its certificate's Common Name is refused with `PermissionDenied` — what
`--agent-identity-from-cert` did on request in 1.x, now the default. A fleet
whose agents share one certificate says so with
`--allow-shared-agent-certificate` and keeps registering as before, with a
WARN per mismatch. An agent whose certificate comes from files and that sets
no `node_id` already registers as its Common Name and notices nothing.
Upgrade the server first, as always; add the flag to its unit file before
the restart if the fleet shares a certificate.

The [Release notes](../reference/release-notes.md) for v2.0.0 list the rest.

The fleet modules (`agent`, `server`, `proto`) are pre-1.0 and version
honestly: their surfaces may still change between minor versions. Anything of
note is called out in the [Release notes](../reference/release-notes.md).

## The suite manifest (versions.yaml)

Orbit is part of the Quantum suite, alongside the Nucleus framework and the
Quark ORM. The suite publishes a manifest,
[`versions.yaml`](https://github.com/jcsvwinston/quantum/blob/main/versions.yaml),
that certifies **sets** of versions tested together. The shape of the parts you
care about — the numbers below are only an illustration, not the current set:

```yaml
quantum: "1.7.1"          # version of the suite itself (its own line)
status: certified         # this set was verified as a whole
modules:
  quark:   "v1.3.1"       # certified module versions —
  nucleus: "v1.3.2"       # what you `go get` for a known-good set
  orbit:   "v1.4.2"
```

Between suite releases each module cuts its own patch and minor versions
independently; the manifest only certifies combinations. So you have two
routes. The conservative one is to upgrade to the versions named in the latest
certified set. If you want a specific fix sooner, the individual module tag
works too.

## Upgrade steps

### App with the in-process panel

```bash
go get github.com/jcsvwinston/orbit@<version>
go mod tidy && go build ./...
```

Redeploy. There is nothing else: the panel's UI is embedded in the module,
so app and admin upgrade atomically. A panel tab that was open across the
redeploy reloads itself once, the first time it opens a page it had not
loaded yet — pages are fetched on demand and the old page still names the
previous build's files. If that first visit happens while the process is
still restarting, the tab does not reload into a connection error: the
content area shows "This page could not be loaded" with a **Reload**
button, the sidebar and the pages already open keep working, and opening
the page again once the server answers reloads the tab as above.

### Fleet: server first, then agents

1. **Reinstall and restart the admin server:**

   ```bash
   go install github.com/jcsvwinston/orbit/server/cmd/admin-server@latest
   admin-server --version   # confirm what you now have
   ```

   Or replace the binary with the signed one from the release page and check
   its signature and provenance before it runs — see
   [Verifying a release](./verifying-releases.md). An upgrade is the moment
   the check is worth most: it is the one time you deliberately run a binary
   you did not have yesterday.

   Server state is in memory only, so a restart is cheap: agents reconnect
   and re-register on their own, and only the bounded replay history is
   lost.

2. **Bump the agent in each app and roll the nodes:**

   ```bash
   go get github.com/jcsvwinston/orbit/agent@<version>
   go mod tidy && go build ./...
   ```

**Why this order?** Because only one direction of mismatch is designed for. The
wire contract between agent and server (`orbit/proto`) is append-only within
its v1 package, and its evolution rules assume rolling deploys in which an
agent one release behind talks to a newer server. Upgrading the server first
keeps you inside that tested direction while your nodes roll.

There is **no runtime version negotiation** — no handshake rejects a mismatched
peer — so the append-only discipline is the only thing carrying compatibility.
Keep the gap short: adjacent releases are the assumption, not agents six months
behind the server.

### Verifying what runs where

- `admin-server --version` prints the version of what you installed: the
  release tag for a binary downloaded from a release page, the `orbit/server`
  module tag for a `go install`, and `devel` for a source build rather than a
  made-up number. [Deployment](./deployment.md) has the table.
- The fleet UI shows the server's version and your operator identity.
- Each node's row in the fleet UI shows the version string **your app**
  passed to `agent.NewExtension` — report a meaningful value there and
  fleet-wide rollouts become visible at a glance.

## Downgrades

The same mechanics in reverse: `go get` an older tag, reinstall an older
`admin-server`. The wire contract's append-only rule means an older server
simply ignores fields it does not know.

One thing to check first. Read the
[Release notes](../reference/release-notes.md) before downgrading across a
release that hardened defaults: a newer flag your unit file references will not
exist on an older binary.
