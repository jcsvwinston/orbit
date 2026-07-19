---
title: Upgrading
sidebar_position: 3
description: Module versions, compatibility, and the order to upgrade in.
---

# Upgrading

Orbit is released as several Go modules from one repository, each with its
own SemVer line and its own tags. Knowing which module you actually depend
on makes upgrades boring — which is the goal.

## The modules and their tags

| Module | Tag format | You use it when… |
|---|---|---|
| `github.com/jcsvwinston/orbit` (root) | `vX.Y.Z` | You mount the **in-process panel** (`orbit.Module`). |
| `github.com/jcsvwinston/orbit/agent` | `agent/vX.Y.Z` | Your app nodes stream to a fleet **admin server**. |
| `github.com/jcsvwinston/orbit/server` | `server/vX.Y.Z` | You run the **admin-server binary** (usually installed, not imported). |
| `github.com/jcsvwinston/orbit/proto` | `proto/vX.Y.Z` | Never directly — it is an internal dependency of agent and server. |
| `github.com/jcsvwinston/orbit/quarkbridge` | `quarkbridge/vX.Y.Z` | Opt-in: Quark ORM statements in the live SQL feed. |
| `github.com/jcsvwinston/orbit/quarkdatasource` | `quarkdatasource/vX.Y.Z` | Opt-in: Data Studio over Quark models. |

Sub-modules use Go's directory-prefixed tags: the tag `agent/v0.5.2` is
what `go get github.com/jcsvwinston/orbit/agent@v0.5.2` resolves. The
[Release notes](../reference/release-notes.md) list which module tags
shipped with each root release.

## What imports what

- **App with the in-process panel** → depends on the **root** module only.
- **App that joins a fleet** → additionally depends on **`orbit/agent`**.
- **The admin server** → the `admin-server` binary, installed from
  **`orbit/server`**. You reinstall it; you do not import it.
- **`orbit/proto`** is pinned by agent and server internally. The server
  module also pins the agent version it was tested against. Consumers only
  ever choose two versions: the root module and (if fleet) the agent.

## The v1.x stability promise

The root module's public API — `orbit` itself and the `datasource`
contract — is **frozen for the life of v1.x**: no breaking changes within
v1. Upgrading the root module across v1.x versions is a
`go get`-and-rebuild operation. The fleet modules (`agent`, `server`,
`proto`) are pre-1.0 and version honestly: their surfaces may still change
between minor versions, and anything of note is called out in the
[Release notes](../reference/release-notes.md).

## The suite manifest (versions.yaml)

Orbit is part of the Quantum suite, alongside the Nucleus framework and the
Quark ORM. The suite publishes a manifest,
[`versions.yaml`](https://github.com/jcsvwinston/quantum/blob/main/versions.yaml),
that certifies **sets** of versions that were tested together. The parts
you care about:

```yaml
quantum: "1.7.1"          # version of the suite itself (its own line)
status: certified         # this set was verified as a whole
modules:
  quark:   "v1.3.1"       # certified module versions —
  nucleus: "v1.3.2"       # what you `go get` for a known-good set
  orbit:   "v1.4.2"
```

Between suite releases, each module cuts its own patch and minor versions
independently — the manifest only certifies combinations. If you want the
conservative path, upgrade to the versions named in the latest certified
set; if you want a specific fix earlier, the individual module tag works
too.

## Upgrade steps

### App with the in-process panel

```bash
go get github.com/jcsvwinston/orbit@<version>
go mod tidy && go build ./...
```

Redeploy. There is nothing else: the panel's UI is embedded in the module,
so app and admin upgrade atomically.

### Fleet: server first, then agents

1. **Reinstall and restart the admin server:**

   ```bash
   go install github.com/jcsvwinston/orbit/server/cmd/admin-server@latest
   admin-server --version   # confirm what you now have
   ```

   Server state is in memory only, so a restart is cheap: agents reconnect
   and re-register on their own, and only the bounded replay history is
   lost.

2. **Bump the agent in each app and roll the nodes:**

   ```bash
   go get github.com/jcsvwinston/orbit/agent@<version>
   go mod tidy && go build ./...
   ```

**Why this order?** The wire contract between agent and server
(`orbit/proto`) is append-only within its v1 package, and its evolution
rules are written for rolling deploys where an agent one release behind
talks to a newer server. There is **no runtime version negotiation** — no
handshake rejects a mismatched peer — so compatibility is carried entirely
by that append-only discipline. Upgrading the server first keeps you inside
the tested direction (older agents → newer server) while your nodes roll.
Keep the gap short: adjacent releases are the assumption, not agents six
months behind the server.

### Verifying what runs where

- `admin-server --version` prints the installed module version (source
  builds print `devel` rather than a made-up number).
- The fleet UI shows the server's version and your operator identity.
- Each node's row in the fleet UI shows the version string **your app**
  passed to `agent.NewExtension` — report a meaningful value there and
  fleet-wide rollouts become visible at a glance.

## Downgrades

The same mechanics in reverse: `go get` an older tag, reinstall an older
`admin-server`. The wire contract's append-only rule means an older server
simply ignores fields it does not know. Check the
[Release notes](../reference/release-notes.md) before downgrading across a
release that hardened defaults — a newer flag your unit file references
will not exist on an older binary.
