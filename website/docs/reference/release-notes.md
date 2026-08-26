---
title: Release notes
sidebar_position: 1
description: What changed in each Orbit release, in plain terms.
---

# Release notes

The current release is **v1.7.4**. <!-- x-release-please-version -->

Every heading below is a version of the **root module**
(`github.com/jcsvwinston/orbit`) — the one an application mounts for the
in-process panel.

The fleet modules (`agent`, `server`, `proto`) release independently with their
own tags, so each entry also lists the fleet tags cut alongside it. The
complete tag history lives on the
[GitHub releases page](https://github.com/jcsvwinston/orbit/releases).

## v1.7.3 — 2026-08-25

**Changed**

- **Fleet modules aligned to the patched suite.** `agent`, `server`,
  `quarkbridge` and `quarkdatasource` are republished against nucleus
  v1.12.1 and quark v1.6.1, and `server` moves to `agent` v0.5.16. No orbit
  behaviour changes; this release exists so the published module tags carry
  the same dependency floor as the root.

## v1.7.2 — 2026-08-25

**Changed**

- **Dependency alignment.** Every module moves to nucleus v1.12.1 and quark
  v1.6.1, both of which carry fixes from an external audit of the published
  suite: a row-level-security preflight that certified a real cross-tenant
  leak as correct, a module unable to declare its own mount root, and a
  storage delete that reported success without removing anything. No orbit
  behaviour changes.

## v1.7.1 — 2026-08-25

**Fixed**

- **Configuring the panel from `nucleus.yml` now works.** The README has
  documented a `modules.orbit.*` subtree — prefix, title, environment and
  the bootstrap user's credentials — and the framework did its part: it
  extracted that subtree and handed the bound configuration to the module.
  Orbit discarded it and used the values passed in Go instead, so anyone
  configuring the panel through YAML got the defaults with nothing to
  indicate it. Because the module *is* mounted, the framework's warning
  about configuration aimed at an unmounted module stayed quiet too.

  It was ignored in complete silence, and that includes
  `bootstrap_password`: someone who believed they had set the admin
  password had not set it.

  The YAML subtree is now overlaid on whatever you passed in Go, key by
  key — Go supplies the base, YAML wins for what it sets.

  One exception, and it fails loudly rather than surprising you: `prefix`.
  The framework mounts the module before it reads the subtree, so that key
  cannot move the panel. Setting it to something other than the mount point
  stops startup and names both values, instead of serving a panel whose own
  links point where it is not.

## v1.7.0 — 2026-08-25

**Added**

- **Versioned documentation.** The suite site now serves this documentation
  under a version path as well as at `/orbit/`. Until now Orbit only ever
  served its current docs, so anyone running an older release read the notes
  for whatever version shipped last — with nothing on the page saying so.
  Nucleus and Quark have worked this way for a while; Orbit was the odd one
  out.

  Nothing moved: `/orbit/…` still serves the current documentation, and the
  snapshots appear alongside it. The version picker in the top bar switches
  between them.

  The archive starts at **1.6.7**, the release that was current when
  versioning was installed. Earlier minors have no snapshot and none will be
  fabricated: a back-dated snapshot would claim that today's documentation
  was the documentation of the time.

**Fixed**

- **The module compatibility matrix no longer goes stale after a tag.** It was
  generated from git tags alone, and a tag does not exist until the release
  merges — so the committed copy went stale the moment a root tag was cut, and
  the freshness check (which only runs on pull requests) slept until an
  unrelated change tripped over it. It now also reads the release manifest, so
  the release itself carries its own row.

## v1.6.7 — 2026-08-25

**Changed**

- **Dependency alignment.** The root panel, `server`, `agent` and
  `quarkbridge` move to nucleus v1.12.0. That release freezes the
  framework's default security posture against a measured baseline, adds
  `nucleus doctor --check security` for the settings that load fine and
  expose you anyway, and judges cookie-name prefixes when the configuration
  file is read instead of at boot. `quarkdatasource` and `proto` do not
  depend on the framework and are unchanged. No orbit behaviour changes.

**Documentation**

- Each module's README now states that it builds against Nucleus and points
  at its own `go.mod` for the exact floor, rather than repeating a version
  number that would go stale.

Fleet tags cut alongside this release line: `agent`, `server` and
`quarkbridge` (`proto` and `quarkdatasource` unchanged).

## v1.6.6 — 2026-08-24

**Changed**

- **Dependency alignment.** Every module — the root panel, `server`,
  `agent`, `quarkbridge` and `quarkdatasource` — moves to nucleus v1.11.0
  and quark v1.6.0. Those releases bring configuration that is validated
  the same way wherever it is loaded, a test kit that reaches the database,
  a graceful outbox shutdown, and a preflight for native row-level security.
  No orbit behaviour changes.

Fleet tags cut alongside this release line: `agent`, `server`,
`quarkbridge` and `quarkdatasource` (`proto` unchanged).

## v1.6.5 — 2026-08-23

**Changed**

- **`server` now pins `agent/v0.5.13`.** v1.6.4 published that agent tag, but
  `server` was left requiring v0.5.12. Installing the server from a cold cache
  now resolves the current agent code. No behaviour changes.

Fleet tag cut alongside this release: `server/v0.9.9` (the rest of the
fleet is unchanged from v1.6.4).

## v1.6.4 — 2026-08-21

**Changed**

- **`agent` completes the dependency alignment to nucleus v1.10.0.** v1.6.3
  moved the root panel, `server`, `quarkbridge` and `quarkdatasource`, but
  shipped with `agent` still requiring nucleus v1.9.1 — and said nothing about
  it. Every module now builds against the same framework version. No behaviour
  changes.

Fleet tag cut alongside this release: `agent/v0.5.13` (the rest of the
fleet is unchanged from v1.6.3).

## v1.6.3 — 2026-08-21

**Changed**

- **Dependency alignment.** The root panel and `server` move to nucleus
  v1.10.0 (the vertical-slice module release: module-declared policy rows
  and CSRF exemptions, applicable embedded migrations, embedded
  templates); `quarkbridge` moves to nucleus v1.10.0 and quark v1.5.2;
  `quarkdatasource` moves to quark v1.5.2 (the `migrate up` CLI fix). No
  orbit behaviour changes. Known miss, fixed in v1.6.4: `agent` was left
  on nucleus v1.9.1.

Fleet tags cut alongside this release: `server/v0.9.8`,
`quarkbridge/v0.3.12`, `quarkdatasource/v0.2.11` (`proto/v0.4.2`
unchanged).

## v1.6.2 — 2026-08-18

**Changed**

- **Dependency alignment.** The root panel and the fleet modules that build
  on the framework (`agent`, `server`, `quarkbridge`) move to nucleus
  v1.9.1 — the server-side render layer fixes (recursive template loading,
  prefix modules receiving the engine and session manager, template
  function registration from the builder) and the outbox dispatcher
  starting after extensions attach. No orbit behaviour changes.

Fleet tags cut alongside this release: `agent/v0.5.12`, `server/v0.9.7`,
`quarkbridge/v0.3.11` (`proto/v0.4.2` and `quarkdatasource/v0.2.10`
unchanged).

## v1.6.1 — 2026-08-16

**Fixed**

- **These notes.** v1.6.0 shipped without its section on this page, and
  `quarkdatasource` still required root v1.4.3 — two minors behind. Both are
  corrected: the section below documents v1.6.0, and `quarkdatasource` builds
  against root v1.6.0.

Fleet tags cut alongside this release: `quarkdatasource/v0.2.10` (all
other modules unchanged).

## v1.6.0 — 2026-08-16

The developer-experience minor: it closes the gaps a new user hit in Orbit's
onboarding surface.

**Added**

- **Compatibility matrix, generated.** `website/docs/reference/module-matrix.md`
  lists the six Go modules with their published versions and cross-module
  pins, produced by a generator with a CI freshness check — the table can
  no longer drift from the released tags.

**Fixed**

- **The quick start compiles.** The first snippet a new user copies used
  `app.Start()` — a method that does not exist on the built application.
  It now shows the real entry point (`nucleus.Run(app)`), matching the
  README and the minimal example.
- **`make test` covers the six modules.** The Makefile stopped at four;
  `quarkbridge` and `quarkdatasource` — exactly the two modules that
  materialize the Quark↔Orbit integration — are now in every target.

**Changed**

- **Dependency alignment.** Root and fleet modules build against
  nucleus v1.8.0 and quark v1.5.0.

Fleet tags cut alongside this release: `agent/v0.5.11`, `server/v0.9.6`,
`quarkbridge/v0.3.10`, `quarkdatasource/v0.2.9` (`proto/v0.4.2`
unchanged).

## v1.5.4 — 2026-08-16

**Changed**

- **Dependency alignment.** The root panel and all fleet modules build
  against nucleus v1.7.0 (global authorization sees JWT claims, S3 bucket
  bootstrap, service health in `/healthz`); no orbit behaviour changes.

Fleet tags cut alongside this release: `agent/v0.5.10`, `server/v0.9.5`,
`quarkbridge/v0.3.9` (`quarkdatasource/v0.2.8` and `proto/v0.4.2`
unchanged).

## v1.5.3 — 2026-08-16

**Changed**

- **`proto/v0.4.2` joins the set.** The security toolchain bump (Go 1.26.6)
  had touched `proto/go.mod` without a release, leaving unreleased module
  code in the certified tree; `proto/v0.4.2` publishes it (no functional
  changes). Its ripple re-pins the module graph: `agent/v0.5.9` and
  `server/v0.9.4` (proto and agent pins current, nothing else changes).
  This root cut contains all of them; `quarkbridge/v0.3.8` and
  `quarkdatasource/v0.2.8` continue from v1.5.2.

## v1.5.2 — 2026-08-16

**Changed**

- **Dependency alignment.** The root panel and all fleet modules build
  against quark v1.4.1 (its CLI repair release) and nucleus v1.6.2 (its
  scaffold-dialect and fixture-ordering repairs); no orbit behaviour
  changes.
- **Security.** Go toolchain floor moves to 1.26.6 (standard-library
  advisories), `google.golang.org/grpc` to v1.82.1 and
  `go.opentelemetry.io/otel` to v1.44.0 — all flagged as reachable by the
  vulnerability scanner.

Fleet tags cut alongside this release: `agent/v0.5.8`, `server/v0.9.3`,
`quarkbridge/v0.3.8`, `quarkdatasource/v0.2.8` (`proto/v0.4.1` unchanged).

## v1.5.1 — 2026-07-22

**Changed**

- **Dependency alignment.** The root panel and all fleet modules build
  against nucleus v1.6.0 (its webhook-registration hardening); no orbit
  behaviour changes. `golang.org/x/text` stays at v0.39.0.

Fleet tags cut alongside this release: `agent/v0.5.6`, `server/v0.9.1`,
`quarkbridge/v0.3.6` (`quarkdatasource/v0.2.7` and `proto/v0.4.1` unchanged).
Not user-facing, but worth recording: the repository's own version checks
became stricter. The one allowance for a module lagging behind the root is now
confined to the single edge that structurally requires it
(`root↔quarkdatasource`), and that edge is only accepted after the frozen
`datasource` contract is verified identical across the lagging tag.

## v1.5.0 — 2026-07-22

**Fixed**

- **The in-process live feed now shows HTTP traffic.** The panel consumed only
  the SQL lane of the event bus, so `/api/live/snapshot` reported
  `requests: 0` however much HTTP traffic the host app served. The feed now
  consumes the bus's HTTP events too, and requests appear alongside queries.

  The traffic middleware remains the sole source of the session lane, which
  needs the `*http.Request` that the bus event does not carry, and it
  de-duplicates the admin prefix so events are not counted twice.

  **Correction to the v1.4.4 note:** that release described the in-process
  live feed as working end to end. It did not — the HTTP lane was dead until
  this version.
- **Dependency alignment to the 1.9.0 set.** The root panel and all fleet
  modules build against nucleus v1.5.0, the Quark integrations
  (`quarkbridge`, `quarkdatasource`) require Quark v1.4.0, and
  `golang.org/x/text` is raised to v0.39.0 (GO-2026-5970) across every
  module.

**Added**

- **UI backlog closed.** The last three outstanding interface items land:
  centralized i18n strings, table accessibility roles across the fleet
  pages, and the in-process panel's two parallel tables consolidated to one.

Fleet tags cut alongside this release: `agent/v0.5.5`, `server/v0.9.0`,
`quarkbridge/v0.3.5`, `quarkdatasource/v0.2.7`.

## v1.4.4 — 2026-07-20

**Fixed**

- The agent's **auth-suspicion warning is now per endpoint**. A frame accepted
  on one endpoint proves that endpoint's auth path and nothing else, so it no
  longer clears the frameless-cycle evidence of a sibling endpoint that keeps
  rejecting every frame. In a failover pair, one healthy endpoint used to be
  able to silence — or worse, mislabel — the warning that belonged to the
  broken one. The warning now fires against the endpoint that earned it, with
  its own evidence.
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
