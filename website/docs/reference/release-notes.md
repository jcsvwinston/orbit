---
title: Release notes
sidebar_position: 1
description: What changed in each Orbit release, in plain terms.
---

# Release notes

The current release is **v1.8.19**. <!-- x-release-please-version -->

Every heading below is a version of the **root module**
(`github.com/jcsvwinston/orbit`) — the one an application mounts for the
in-process panel.

The fleet modules (`agent`, `server`, `proto`) release independently with their
own tags, so each entry also lists the fleet tags cut alongside it. The
complete tag history lives on the
[GitHub releases page](https://github.com/jcsvwinston/orbit/releases).

## v1.8.19 — 2026-09-04

A pin fix, the structural kind: `agent` and `server` are cut from the same
commit as `proto`, so at the moment they are tagged they still require the
previous `proto`; likewise `server` for `agent`. This release moves
`agent` to `proto/v0.4.3` and `server` to `proto/v0.4.3` and `agent/v0.6.11`,
so `go install .../admin-server` builds with the fleet code released in
v1.8.18 (TLS on the listeners, https-capable agent). Nothing else changes.

Fleet tags cut alongside: `agent/v0.6.12`, `server/v0.10.13`.

## v1.8.18 — 2026-09-04

The maturity audit of 2026-09-03, applied. Every module moves to Nucleus
v1.23.1 and Quark v1.10.1, so the tags cut here certify against the same set
as the umbrella.

Fleet tags cut alongside: `proto/v0.4.3`, `agent/v0.6.11`, `server/v0.10.12`,
`quarkbridge/v1.8.18`, `quarkdatasource/v1.8.18`.

**Fleet: TLS is real now.** `--agent-cert/--agent-key` and `--ui-cert/--ui-key`
used to be accepted and then ignored — the listeners always served plain
h2c, and configuring a certificate even counted as authentication. The
listeners now wrap TLS with ALPN, `--agent-client-ca` enables client
certificate verification (identity `agent:<CN>`), and the agent speaks
`https://` endpoints. The docs no longer promise mTLS where none existed.

**Panel: no secrets in the system snapshot.** Environment values whose name
suggests a URL, DSN, password or credential are masked, and `user:pass@` is
redacted inside any URL-shaped value.

**Data Studio validates.** Writes run the model's `validate` tags and answer
`422` per field; a value of the wrong type is rejected instead of being
coerced; unknown keys are refused; a non-numeric id answers `400`, not `500`.
Export paginates until the end and respects tenant and database alias;
export downloads are confined to the export area; imports are size-limited
and file names sanitised.

**Admin UI.** Import now runs validate and execute instead of showing success
after the upload; "Load more" appends; batch sizes stop at the API's limit;
JSON fields are shown and edited as JSON; `deny` policies are labelled;
toasts close; audit filters and pagination work; server-side sorting; a
403 shows a permission page. ESLint and Vitest run in CI.

**Housekeeping.** `examples/minimal` imports the SQLite driver module and CI
boots it; `go mod tidy` is a no-op in every module and CI checks it;
reconnecting agents cancel the previous stream; Connect messages are capped
at 4 MiB; `/api/health` reports the real version and uptime; and the
direction is settled: the fleet plane will consume the same datasource
contract as the in-process panel, so per-model permissions and tenant
filtering apply in both.

## v1.8.17 — 2026-09-02

The Quark bridges move to Quark v1.10.0, closing the set. Nothing else
changes.

Fleet tags cut alongside: `quarkbridge/v1.8.17`, `quarkdatasource/v1.8.17`.

Both bridges jump from the `0.x` line to `1.8.17`. A `Release-As` in the
release train applied to every package in the repository, and by the time it
was noticed the tags were published and served by the Go module proxy, which
is immutable — and `@latest` always resolves to the highest version, so going
back to `0.x` would have left a public `go get` returning something this set
does not certify. The number stands; from here both modules follow the `1.x`
line. Nothing about their code or their API changed in this release.

## v1.8.16 — 2026-09-02

A pin fix. `server` was requiring `agent v0.6.9` while `agent/v0.6.10` was
already published — the alignment script runs before the tags are cut, so it
fixes the sibling at whatever was current then and the cut publishes the next
one immediately after.

It matters here in a way the same lag would not elsewhere: `go install
github.com/jcsvwinston/orbit/server/cmd/admin-server@server/vX` resolves the
agent that `server` requires, and nothing else in that build raises it, so the
binary would ship the older agent.

Fleet tags cut alongside: `server/v0.10.11`. Everything else is unchanged from
v1.8.15.

## v1.8.15 — 2026-09-02

An alignment release: Orbit moves to Nucleus v1.23.0 and Quark v1.9.0, which
take the database drivers, the cloud storage backends and the telemetry
exporters out of the framework and into modules of their own.

**Nothing in Orbit changes.** The panel does not open databases — it uses the
`*sql.DB` your application hands it — so all six modules build against the new
versions untouched.

**What your application needs to know.** The host application now links the
driver for its own engine, one blank import:

```go
import _ "github.com/jcsvwinston/nucleus/drivers/postgres"
```

or `nucleus add postgres`, which writes it for you. Your configuration does
not change. If you mount Orbit in an application that has not added its driver
module, the application stops at startup with an error naming the import —
before Orbit is reached.

Worth knowing if you rely on the admin bootstrap: the driver module registers
the driver **and** how that driver reports a unique-constraint violation.
Without the classifier the framework's predicate does not fail, it answers
"no", and a duplicate administrator username would surface as an internal
error rather than as the duplicate it is. Importing the module — rather than
the driver package on its own — gets both halves.

Fleet tags cut alongside: `agent/v0.6.10`, `server/v0.10.10`,
`quarkbridge/v0.4.10`, `quarkdatasource/v0.2.19`. `proto` stays at `v0.4.2`.

## v1.8.14 — 2026-08-31

The release that carries an end-to-end audit of the panel. Most of it is about
the panel telling you the truth: about your data, about itself, and about what
the fleet plane does and does not enforce.

**The panel is Orbit.** It signed itself "Nucleus Admin" everywhere and ignored
the `Title` you configured; both are fixed, and the docs now show what the
panel actually looks like — the first screenshots this documentation has ever
had, of Data Studio, the live inspector and the metrics view.

**Data Studio over Quark shows your data.** Every cell rendered as "—" because
the schema declared snake_case columns while the records arrived keyed by Go
field names; records now expose each field under its schema column. Model
counts were also crossed between models — a sort reordered the slice the
counters pointed into — and rows deleted with soft-delete no longer inflate
them.

**The live inspector shows the traffic it promised.** It only accepted one
event shape, so requests never appeared and the SQL statements Quark publishes
were never drawn at all; both are rendered now, correlated by request, with
durations in microseconds instead of a rounded zero.

**Fixed.** A data race in the panel's tenant-field cache, reachable from every
schema fetch. The session viewer served the full session token to any
authenticated operator; it now shows an opaque handle. The audit log's JSON
contract (`id`, `total_pages`, `record_id`) is coherent. Field labels no longer
render as "I D".

**Getting in is possible without a bootstrap password.** The admin schema is
created whenever the module mounts, so `nucleus createuser` works instead of
failing on a table that did not exist yet — and the quick start no longer ends
on an empty panel: the minimal example registers a model, and the docs explain
how a model reaches Data Studio.

**Said plainly, not fixed.** The fleet plane's Data Studio does not apply
per-model RBAC or tenant filtering; its package documentation claimed it did.
The claim is gone, a deny-by-default gate for mutations is in place, and the
direction that closes it properly is written down as a decision record.


## v1.8.13 — 2026-08-30

**Changed**

- **The root module aligned on Nucleus v1.21.0.** The in-process panel now
  builds against the certified framework release, completing the set
  alignment the module releases began.

## v1.8.12 — 2026-08-30

**Changed**

- **Fleet and datasource modules aligned on the current set.** `agent`,
  `server`, `quarkbridge` and `quarkdatasource` now require Nucleus v1.21.0
  and Quark v1.7.1, up from the previous set. The root module carries no code
  change; this release exists to seal the aligned module tags as ancestors of
  a certifiable root, which is what lets the suite certify with no declared
  cross-repo lag.

## v1.8.11 — 2026-08-30

**Fixed**

- **The storage browser confines the caller's path to the upload root.** With
  a storage backend configured — the production path — the panel's file
  browser passed the requested path straight to the store, so a session with
  `storage_view` could list any prefix of the bucket, traversal (`../`) and
  sibling prefixes included. The path is now confined the same way the
  filesystem branch already was; a path outside the root is refused. The
  guard function that promised this had existed all along but nothing called
  it.

**Changed**

- **`quarkdatasource` pins the current root minor (v1.8.10, not v1.8.0).** The
  edge from `quarkdatasource` to the root is topologically forced to lag, and
  the pin guard tolerates one minor of it — but the pin had drifted ten
  patches back within the same minor, one root minor bump away from failing a
  certification mid-flight. Raised to the current minor to keep the edge
  current.

**Documentation**

- Two archived documentation snapshots (v1.7.0 and v1.8.0) announced an
  earlier version than their own, and still did so on the published site. Both
  were corrected, and a guard — ported from the framework, which Orbit lacked
  — now asserts every snapshot announces its own version.

## v1.8.10 — 2026-08-30

**Changed**

- **Every module aligned on Nucleus v1.20.1.** Nothing in Orbit changes.
  Until this release the root module tracked the framework while `agent`,
  `server` and `quarkbridge` were still pinned three releases back, at
  v1.17.1. That is invisible to an application — Go's minimal version
  selection raises the requirement to whatever the application itself asks
  for — but it is exactly what the suite manifest refuses to certify, and
  for a reason: a module pinned to a framework release nobody tests it
  against is a compatibility claim nobody has checked.

  Cut alongside it: `agent` v0.6.7, `server` v0.10.7 and `quarkbridge`
  v0.4.7, in that order, so each module tag is an ancestor of this one.

## v1.8.9 — 2026-08-30

**Changed**

- **Aligned with Nucleus v1.20.0.** Nothing in Orbit changes; these are
  fixes in the framework it requires, and three of them are behaviour
  changes worth knowing about before you upgrade.

  A third-party request interceptor now sees who is calling. The chain was
  mounted outside the bearer decode, so an interceptor got nothing from the
  request's claims while the handler behind it saw the same request
  authenticated. It now runs after the decode and still before the
  default-deny layer, so it also observes requests that are about to be
  denied. Nothing moved relative to the request ID, the real-IP resolution,
  the rate limiter or CSRF.

  `X-Real-IP` is now filtered the way `X-Forwarded-For` already was: an
  address that is itself a trusted proxy is not a client. Under a catch-all
  `trusted_proxies` the unfiltered fallback was a spoofing vector — a forged
  client IP, rate-limit evasion and an audit trail recording the attacker's
  choice. A correctly configured deployment sees no change.

  `nucleus doctor --json` now exits non-zero when the report says
  `unhealthy`. The verdict came from the text renderer only, so the same
  report exited 1 as text and 0 as JSON — and the mode that never failed was
  the one CI consumes.

## v1.8.8 — 2026-08-30

**Fixed**

- **Every `modules.orbit.*` key except three now reaches the panel.** Orbit's
  `Config` declared only `yaml` struct tags, and Nucleus binds a module's
  configuration subtree with the `koanf` tag. Sixteen of its nineteen keys
  were dropped in silence — including `bootstrap_username`,
  `bootstrap_password`, `auth_database` and the whole multi-tenant block.
  Exactly the single-word keys survived (`prefix`, `title`, `environment`),
  because the binder falls back to matching the field name when it finds no
  tag, and nothing in `snake_case` can map that way.

  Nothing warned about this. The panel started, mounted where it was told,
  and used the values the host application had passed in code, so an
  operator configuring the panel from `nucleus.yml` saw a working admin that
  was quietly ignoring most of the file.

- **A duplicate bootstrap admin no longer aborts startup on a database that
  does not speak English.** Orbit decides whether the first admin already
  exists by classifying the error the driver returns, and it was matching
  English fragments of the message. PostgreSQL, MySQL, Oracle and SQL Server
  all translate those messages when the server runs in another language, so
  the duplicate went unrecognised and the module failed to start — taking
  the whole application with it.

  To be precise about when this bites: a normal restart never reached it,
  because the row count short-circuits before the insert. It fires on a
  first boot with several replicas starting at once against an empty admin
  table. One wins; the others used to crash-loop.

**Changed**

- **Aligned with Nucleus v1.19.0.** Three of its fixes land directly in what
  the panel does.

  The active-sessions view works again on a configured session store. Nucleus
  wrapped every store installed from configuration in an adapter that carried
  only three methods, and enumeration is discovered by type assertion — so
  with `session_store: redis` or `sql` the view answered 200 with "not
  supported" and zero rows while sessions were sitting in the store.

  A backend that rejects now ends the login attempt instead of falling
  through to the next one. This is what Orbit's own README has always
  promised when it says a local admin row is not a bypass: a revoked
  directory account can no longer get in through a stale local row. Note the
  consequence, because it is not optional — a chain is a fallback for an
  unreachable backend, not a way to serve two separate user populations.

  And `storage.cleanup.enabled: false` now actually disables the cleaner,
  which until this release deleted aged objects under the cleanup prefix on
  every boot.

## v1.8.7 — 2026-08-29

**Changed**

- **Aligned with Nucleus v1.17.1 and Quark v1.7.0.** Nothing in Orbit
  changes; both are fixes in the products it requires.

  Quark now recognises PostgreSQL errors reported by `lib/pq`, not only by
  `pgx`. It classifies those errors to decide three things on its own —
  whether to retry a transaction the engine chose as a deadlock victim,
  whether a duplicate link row can be ignored, and whether a read should
  fail over off an unreachable replica — and under `lib/pq` none of the
  three recognised anything, silently. Quark also exports that
  classification now, so an application can tell a duplicate key from any
  other failure without importing a database driver, and it no longer
  reports a scan error in place of the engine's own error when SQL Server
  rejects an insert.

  Nucleus v1.17.1 corrects its published documentation: archived versions
  announced the wrong release number, and the version marker was leaking
  into the page description shown in search results and link previews.

  The `agent` (`v0.6.6`), `server` (`v0.10.6`), `quarkbridge` (`v0.4.6`)
  and `quarkdatasource` (`v0.2.15`) modules move with it, and `server`
  pins the freshly cut `agent`.

## v1.8.6 — 2026-08-29

**Changed**

- **Aligned with Nucleus v1.17.0**, which adds two extension seams —
  federated sign-in for identity providers, and request interceptors that
  register by name — and fixes a defect where installing a process-wide
  SQL observer replaced any other. That last one matters here: Orbit's
  live SQL view is fed by the framework's own observer, so an application
  that watched SQL used to turn the panel's feed off by doing it. Nothing
  in Orbit changes; the fix is in the framework it now requires. The
  `agent` (`v0.6.5`), `server` (`v0.10.5`) and `quarkbridge` (`v0.4.5`)
  modules move with it, and `server` pins the freshly cut `agent`.

## v1.8.5 — 2026-08-29

**Changed**

- **Aligned with Nucleus v1.16.1**, a packaging release of the framework: it
  is the first one that cuts the LDAP provider module and the framework
  release that contains it from the same commit, so installing either one
  resolves to the same tree. Nothing about Orbit's behaviour changes with it.
  The `agent` (`v0.6.4`), `server` (`v0.10.4`) and `quarkbridge` (`v0.4.4`)
  modules move with it, and `server` pins the freshly cut `agent`.

## v1.8.4 — 2026-08-29

**Changed**

- **Aligned with Nucleus v1.16.0**, which moves the contracts a third-party
  backend or storage provider implements into leaf packages and adds a
  conformance suite for authentication backends. Nothing about Orbit's
  behaviour changes with it: the names Orbit uses are aliases of the same
  types. The `agent` (`v0.6.3`), `server` (`v0.10.3`) and `quarkbridge`
  (`v0.4.3`) modules move with it, and `server` pins the freshly cut
  `agent`.

## v1.8.3 — 2026-08-28

**Changed**

- **Aligned with Nucleus v1.15.1.** A packaging release of the framework —
  it unblocked the LDAP provider's own release machinery — so nothing about
  Orbit's behaviour changes with it. The `agent` (`v0.6.2`), `server`
  (`v0.10.2`) and `quarkbridge` (`v0.4.2`) modules move with it, and
  `server` pins the freshly cut `agent`.

## v1.8.2 — 2026-08-28

**Changed**

- **Aligned with Nucleus v1.15.0.** The root module and the `agent`,
  `server` and `quarkbridge` modules now require the framework release that
  introduces per-backend authentication configuration and the LDAP provider.
  No behaviour of Orbit changes with it: the panel already delegates
  authentication to the framework's chain, and the boundary that matters is
  unchanged — a directory user who is not an administrator here is still
  refused, so connecting a corporate directory does not silently turn the
  whole company into panel administrators.
- The `agent` (`v0.6.1`), `server` (`v0.10.1`) and `quarkbridge` (`v0.4.1`)
  modules move with it. `server` also pins the freshly cut `agent`, so
  installing either one standalone resolves to the same set.

## v1.8.1 — 2026-08-27

**Fixed**

- **`orbit/quarkdatasource` now installs against the current root module.**
  The optional Quark datasource module still required the root at `v1.6.0`,
  two minor lines behind. Inside the repository nothing noticed — the
  workspace resolves the root from the checkout — but anyone adding
  `github.com/jcsvwinston/orbit/quarkdatasource` to a project pulled a root
  two minors old alongside it. It now requires `v1.8.0`. The datasource
  contract itself did not move (it has been frozen since v1.0), so nothing
  you wrote against it changes.

  Fleet tags cut alongside: `quarkdatasource/v0.2.14`.

## v1.8.0 — 2026-08-26

**Added**

- **Sign in against your directory.** If the host application declares an
  authentication chain, the admin panel uses it:

  ```yaml
  auth_backends: [ldap, local]
  ```

  Orbit ships no LDAP client. It asks the framework's chain, so whatever
  the operator configured for the application applies to the panel too.

  **Authentication is delegated; authorization is not.** The chain answers
  who the credentials belong to; this panel's own admin table still decides
  whether that person may enter. A directory account that is not an
  administrator here is refused — otherwise connecting a corporate
  directory would quietly make every employee in the company an
  administrator of your admin panel.

  It works in the other direction too: a local admin row is not a bypass.
  With a chain configured the chain still has to accept the password, so a
  revoked directory account cannot get in through a row nobody cleaned up.

  Without `auth_backends`, nothing changes: the panel validates against its
  own table exactly as before.

## v1.7.4 — 2026-08-26

**Changed**

- **Aligned to nucleus v1.13.0**, which makes the parts of the framework
  you are most likely to need to replace — storage backends, session
  stores, authentication backends — pluggable by name. No orbit behaviour
  changes.

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
