---
title: Release notes
sidebar_position: 1
description: What changed in each Orbit release, in plain terms.
---

# Release notes

The current release is **v1.5.0**. <!-- x-release-please-version -->

Versions below are the **root module** (`github.com/jcsvwinston/orbit`) —
the one an application mounts for the in-process panel. The fleet modules
(`agent`, `server`, `proto`) release independently with their own tags;
each entry lists the fleet tags cut alongside it. The complete tag history
lives on the
[GitHub releases page](https://github.com/jcsvwinston/orbit/releases).

## v1.4.4 — 2026-07-20

**Fixed**

- The agent's **auth-suspicion warning is now per endpoint**. A frame
  accepted on one endpoint proves that endpoint's auth path and nothing
  else: it no longer resets the frameless-cycle evidence of a sibling
  endpoint that keeps rejecting every frame. Before this, in a failover
  pair, one healthy endpoint could silence — or worse, mislabel — the
  warning that belonged to the broken one; the warning now fires against
  the endpoint that earned it, with its own evidence.
- **Dependency alignment across every module.** The root panel and all
  fleet modules build against nucleus v1.4.0, the Quark integrations
  (`quarkbridge`, `quarkdatasource`) require Quark v1.3.3, and
  `quarkdatasource` pins the current root. A cold-cache `go install` of
  any module resolves to the same set the release was tested with — no
  stale sibling versions.

Fleet tags cut alongside this release: `agent/v0.5.4`, `server/v0.8.4`,
`quarkbridge/v0.3.4`, `quarkdatasource/v0.2.6`.

## v1.4.3 — 2026-07-19

**New**

- The agent raises an **auth-suspicion warning** when consecutive stream
  cycles end without the server accepting a single frame. Some transport
  failures swallow the explicit rejection, so "connects, then dies
  frameless" is treated as evidence of a bad token even when no
  authentication error is visible. The warning is rate-limited and is
  never triggered by an unreachable endpoint — see the
  [FAQ](../faq.md#the-agent-logs-consecutive-stream-cycles-ended-without-a-single-accepted-frame).

**Fixed**

- The `RequireConnection` boot gate now waits for real acceptance, not
  reachability: `Connected()` only fires on the first frame the admin
  server accepts under authentication. The dial probe hits the
  auth-exempt `/healthz` endpoint, so a reachable server proves nothing
  about the token — previously a wrong token could pass the gate and the
  application booted "green" without ever being connected.
- Module pins: the opt-in Quark integrations (`quarkbridge`,
  `quarkdatasource`) now require Quark v1.3.1, and the server module pins
  the agent at its latest tag — so cold-cache `go install` resolves to
  current code.

**Upgrade notes**

- **A boot that passed with a rejected token will now fail.** If your
  application sets `RequireConnection: true` and its agent token is
  wrong, boots up to v1.4.2 could pass on mere reachability; from this
  release the boot fails at `RequireConnectionTimeout`, with the
  token-rejected warnings explaining why. That green was false — the
  agent was never connected. Fix the token (see
  [Security](../operations/security.md#rejected-tokens-are-loud)) rather
  than widening the timeout or disabling the gate.

Fleet tags: `agent/v0.5.3`, `server/v0.8.3`. Opt-in module tags:
`quarkbridge/v0.3.3`, `quarkdatasource/v0.2.5`.

## v1.4.2 — 2026-07-19

**Fixed**

- Internal version pins across the repo's modules now always reference the
  latest sibling tags, and a continuous check keeps them that way — so
  `go install github.com/jcsvwinston/orbit/server/cmd/admin-server@<tag>`
  resolves cleanly from a cold cache.

**Security**

- A rejected agent token is now loud on both sides. The agent logs a
  warning (`admin agent token rejected by admin server`), only announces
  `connected` once the server has actually accepted the stream, and backs
  off at growing intervals instead of retrying every second. The server
  logs a rate-limited warning naming the remote IP. Previously a bad token
  could fail almost silently while the health probe kept "succeeding".

Fleet tags: `agent/v0.5.2`, `server/v0.8.2`.

## v1.4.1 — 2026-07-15

**Fixed**

- The agent now attaches its bearer token to the telemetry stream itself,
  not just to unary calls — agents can authenticate against a
  token-protected server's stream endpoint.
- The server module builds standalone again outside the repository
  workspace, and continuous builds now verify that.
- Dependency update: Nucleus v1.3.1, which carries a Postgres primary-key
  fix relevant to Data Studio.

**Security**

- Built with Go 1.26.5, picking up the fix for a TLS vulnerability in the
  Go standard library (GO-2026-5856).

Fleet tags: `agent/v0.5.1`, `server/v0.8.1`.

## v1.4.0 — 2026-07-14

**New**

- The fleet UI shows the admin server's version and the signed-in
  operator's identity.
- Filter bars on the live stream pages, plus a sampling control.
- Data Studio in the fleet UI exposes operations the backend already
  supported, including bulk actions.
- Node detail gained a recent-activity view; model lists are searchable;
  the slow-query highlight threshold is configurable.
- Review tools for the fleet Audit log screen.

Fleet tags: `server/v0.8.0`.

## v1.3.0 — 2026-07-13

**New**

- Fleet UI usability round: action feedback (toasts), Data Studio result
  feedback, pause-with-buffer on live streams, a clear signed-out screen on
  session expiry, and accessibility and contrast improvements.
- Fleet plane reliability: telemetry resumes after reconnects, events
  carry a node identity that correlates with the fleet registry, real
  server-driven sampling, node snapshots, and support for read-only
  operators.

**Fixed / Security (in-process panel)**

- Admin actions are recorded under the authenticated user; sensitive
  values are redacted; sign-in attempts are rate-limited with a lockout;
  CSRF protection and browser security headers were added; and two
  controls that looked functional but were not (session terminate, export)
  now do what they say.

Fleet tags: `agent/v0.5.0`, `server/v0.7.0`.

## v1.2.1 — 2026-07-12

**Security**

- The statement that creates the bootstrap admin user is now fully
  parameterized.
- Hardened admin-server defaults (in `server/v0.6.0`, cut alongside): the
  server refuses to start an unauthenticated agent listener on a
  non-loopback interface unless explicitly overridden, and the
  trusted-proxy mode gained the shared-secret check
  (`X-Auth-Proxy-Secret`) so proxy-forwarded identities cannot be forged
  from inside a trusted network range.

**Upgrade notes**

- Existing fleet deployments may need `--agent-token` (or TLS on the agent
  listener), and proxies must echo the secret once `--ui-proxy-secret` is
  set. See [Security](../operations/security.md).

Fleet tags: `server/v0.6.0`.

## v1.2.0 — 2026-07-11

**New**

- Access control and the Audit log are wired end-to-end on the fleet
  plane: the fleet UI reads each node's policy snapshot, and operator
  mutations performed through the server are recorded and attributed.
- The live SQL stream shows the driver-reported row count per statement.

Fleet tags: `proto/v0.2.0`–`proto/v0.3.0`, `agent/v0.3.0`–`agent/v0.4.0`,
`server/v0.4.0`–`server/v0.5.0`.

## v1.1.0 — 2026-07-11

**New**

- Opt-in Prometheus metrics listener on the admin server
  (`--metrics-addr`), and `--version` now reports the real installed
  version from build information.

**Security**

- Go toolchain pinned to 1.26.5 across all modules (TLS advisory
  GO-2026-5856).

Fleet tags: `server/v0.3.0` (plus the toolchain patches `agent/v0.2.1`,
`server/v0.3.1`).

## v1.0.0 — 2026-07-10

The first stable release.

- The public API — the root module and the `datasource` contract — is
  **frozen for the life of v1.x**.
- The fleet modules (`proto`, `agent`, `server`) became independently
  released modules with their own tags, and every module now resolves and
  builds standalone with plain `go get` / `go install` — no repository
  checkout required.

Fleet tags: `proto/v0.1.0`, `agent/v0.1.0`–`agent/v0.2.0`,
`server/v0.1.0`–`server/v0.2.0`.

## Before v1.0

The 0.x line built the product's foundations: Data Studio was decoupled
behind a neutral datasource contract, the opt-in Quark integrations
arrived (`quarkbridge` for the live SQL feed, `quarkdatasource` for Data
Studio over Quark models), and the admin UI was redesigned. Details are on
the [GitHub releases page](https://github.com/jcsvwinston/orbit/releases).
