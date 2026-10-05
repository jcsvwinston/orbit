# Admin bench — what an operator can and cannot do with the panel today

This is the numerator of the A6 gate ("Orbit as an admin product"), and from
the baseline of A11 ("extensibility and catalog") it carries that arc's
numerator on this side of the suite too: the extension family, what an
application adds to the panel. It exists because a gate needs a number, and a
number needs something that produces it.

**Measured on 2026-09-12 against the panel at v1.9.6, and kept current as the
arc closes its gaps; the extension family was recorded on 2026-10-04 against
v1.18.0 and is kept current as A11 closes it. The numbers below are what
the suite produced on its last run.** Run
it with:

```bash
go test ./internal/adminbench/ -run TestAdminBench -v
go test ./internal/adminbench/ -run TestAdminBenchSummary -v                    # per-family counts
ORBIT_ADMIN_BENCH_TABLE=1 go test ./internal/adminbench/ -run TestAdminBenchTable   # writes internal/adminbench/bench-table.md
```

The last command writes `internal/adminbench/bench-table.md`, a generated
file that is not committed: the headline, the per-family summary under "The
result" and the extension family's table further down are pasted from it
when a verdict moves, so the page and the catalogue say the same thing.

The bench is not prose. Every control is a Go probe in
`internal/adminbench/` that boots a Nucleus application with
`orbit.Module(...)` mounted, signs in as the bootstrap admin, and asks the
panel's own HTTP surface the question an operator would ask of the UI.
`TestAdminBench` asserts the **recorded verdict** rather than success, so
closing a gap turns the suite red with "this one is present now, update the
verdict" — which is what keeps this page honest.

There was already a capability table for this panel: the maturity audit of
2026-09-03 scored ten dimensions by reading v1.8.17. Four releases later parts
of it are still true, parts were fixed, and nothing says which is which. That
is the failure mode this bench exists to prevent.

## The verdicts

| verdict | meaning |
|---|---|
| **present** | the control exists and its probe exercised it end to end |
| **partial** | a piece exists; the case records exactly what is missing |
| **absent** | no surface at all — the probe measures the absence, never the lack of a grep hit |

A control that cannot be probed does not belong in the bench.

## The result

**67 of 72 controls present. 0 partial. 5 absent.**

| family | present | partial | absent |
|---|---|---|---|
| data-studio | 17 | 0 | 0 |
| permissions | 9 | 0 | 0 |
| audit | 7 | 0 | 0 |
| operations | 17 | 0 | 0 |
| customization | 7 | 0 | 0 |
| interface | 2 | 0 | 0 |
| extension | 8 | 0 | 5 |
| **total** | **67** | **0** | **5** |

The six families A6 measured are complete as of that arc's eighth session:
59 of 59. The seventh, `extension`, was recorded at the baseline of A11 and
is that arc's gap ([below](#what-an-application-adds-at-the-baseline-of-a11)):
1 present at the baseline, 4 after the arc's first session in this
repository (O1), 6 after its second (O2), 8 after its fifth (O5).
The number is not
the end of the work: what this bench measures is a list of controls somebody
wrote down, and a control that is present is one whose probe exercised it —
not one that is good. What it is for is that the next change to the panel
cannot quietly take one of them away.

## What the shape of it says

The panel **browses and operates well, and administers poorly**.

- Everything an operator does to *data* works and is exercised here: CRUD with
  model validation, search, server-side ordering, bulk actions, import,
  export, fixtures, multi-tenant confinement, and a panel mounted over a
  backend that is not the framework's.
- Everything an operator does to *the application* works too, and this is the
  part no comparable product has: a live request and SQL feed, a runtime
  pulse, feature flags, migrations, storage, async exports.
- Everything an operator does to *other operators* used to be missing, and is
  the first gap the arc closed: accounts are created, listed with their
  roles, re-credentialled and deactivated from the panel (`PERM-02`,
  `PERM-03`). The probes measure it by effect — the created account signs in,
  the reset password works and the old one does not, the deactivated one
  cannot get back in — not by the status code of the call that changes it.
- The permission model used to stop at the model boundary, and no longer
  does: the object of a policy also names a field (`admin:Post.title`) or
  carries the `#own` qualifier (`admin:Post#own`), so "an editor may change
  the title but not the price" and "an author may edit their own posts" are
  now things a policy can say (`PERM-06`, `PERM-07`). The payloads a screen
  loads carry the operator's verbs with them (`PERM-09`), so a UI disables
  what it may not do instead of discovering it by being refused. Three things
  worth keeping in mind about how it is measured: the row probe creates the
  operator's own row THROUGH the panel and somebody else's as the superuser,
  the field probe reads the value back after the refusal (a 403 that wrote
  the row anyway would be worse than no permission at all), and the hint
  probe checks each hint against the answer the panel actually gives.
- The audit trail is no longer a process-lifetime buffer: it is a table the
  panel owns, with a retention window an operator can declare, a CSV export
  that carries the filters the screen was showing, and the history of one
  record read from the same trail (`AUD-05`, `AUD-06`, `AUD-07`, `DS-16`).
  The probes measure the part a status code cannot show — a SECOND
  application on the same database reads what the first recorded, and an
  entry dated outside the declared window is gone while one inside it stays.

## Four defects the bench found, none of them a missing capability

These are not gaps in the product's plan; they are things that do not work,
and a probe that boots the real application is what found them. Three are
fixed and their controls have moved; one is still open.

1. **FIXED — the live websocket panicked in any mounted panel.**
   `/admin/api/live/ws` answered 500: the framework's session middleware wraps
   the response writer in `auth.flashSweepWriter`, which implemented `Flush`
   and `Unwrap` but not `Hijack`, so the upgrade could not take the
   connection. The panel's own tests wire it without that middleware, which is
   why they passed. The fix belonged upstream and is in the framework's
   wrapper, which now hijacks through `http.ResponseController`; `OPS-06`
   opens the stream and receives `stream.ready`.
2. **FIXED — the pager had no total.** A list answered `total: -1` with
   `is_estimated: true`, filtered or not, with five rows in a SQLite table, so
   no UI could say how many pages there were. The framework's query options
   gained an exact count over the same `WHERE` the page uses, and the list
   endpoint asks for it: one extra count per page request, paid by the screen
   that draws a pager and not by the exports that walk every page.
3. **FIXED — a session row never said whose it was.** The row carried a
   `user` field and the panel filled it from the identity keys an
   application might store, never from the keys its OWN authentication
   provider writes, so every operator it signed in was listed as nobody and
   revoking from the viewer was done blind. The panel's own keys come first
   now; `OPS-16` reads its operator's name on its own row.
4. **The migrations view fails on an application that has none.** No
   migrations directory answers 500 rather than an empty list.

## What this bench does not measure, and why

- **The fleet plane** (below) — and, until the arc's tenth session, the
  browser. That half now has its own instrument and its own numerator, kept
  beside this one rather than folded into it: a number that mixed what two
  different instruments can see would be a number nobody could check.
- **The fleet plane.** A separate product surface with its own agent, server
  and protocol. It is measured where it lives.

## The browser half, after the arc's tenth session

Everything above is measured through HTTP. Contrast, focus order, keyboard
reach and whether a dialog can be dismissed do not exist until a browser has
laid the page out and computed its styles — asserting them from Go would be a
claim, not a measurement.

`internal/adminbench/browser` is that instrument: Playwright and axe-core,
driven from `browserbench_test.go` so it measures THE SAME application the
HTTP probes boot. Its verdicts are recorded in Go, next to theirs, and the
suite goes red when one moves.

```bash
cd internal/adminbench/browser && npm ci && npx playwright install chromium
go test ./internal/adminbench/ -run TestBrowserBench -v
```

**9 of 10 controls present**, plus the one that measures the instrument. The
last four belong to the extension family
([below](#what-an-application-adds-at-the-baseline-of-a11)): `UIX-07` and
`UIX-08` were recorded absent at the baseline of A11 and `UIX-07` closed in
the arc's first session (O1); `UIX-10` was added, present, in its second
(O2), and `UIX-11` in its fifth (O5). There is no `UIX-09` on this page yet:
that number belongs to the control of the arc's third session (O3),
measured on a stack of its own that joins this one later:

| control | what it asks |
|---|---|
| **UIX-00** | the instrument bites: a planted violation is caught |
| **UIX-01** | the login screen is legible: text meets contrast |
| **UIX-02** | the panel is legible on the screens an operator opens |
| **UIX-03** | every control says what it is: names, roles and labels |
| **UIX-04** | the document says what it is: language, landmarks, one main heading |
| **UIX-05** | the keyboard reaches the navigation, and the focus is visible |
| **UIX-06** | a dialog can be opened and dismissed from the keyboard |
| **UIX-07** | the login screen draws the logo the application declared — and the browser loaded it |
| **UIX-08** | the record view offers the action the application declared — **absent** |
| **UIX-10** | the first frame wears the theme the application configured, and the operator's own choice wins on reload |
| **UIX-11** | a second dashboard draws a series to the operator granted it, and is neither listed nor served to one who is not |

Three things worth keeping about how it is built:

- **UIX-00 measures the instrument, not the panel.** An accessibility engine
  with a renamed rule reports zero violations, and every control below then
  passes by measuring nothing — the same failure the umbrella's
  guard-of-guards exists for. So the probe plants a button with no accessible
  name and text at about 1.1:1, and fails if the engine does not catch both.
  It was verified by breaking it: with a rule name that does not exist, UIX-00
  goes red.
- **It skips when the browser is not installed, and CI refuses to.** A
  developer running `go test ./...` on a laptop should not be told their
  change broke something because a browser is missing; a lane that went green
  for that reason would be worse than no lane. `ORBIT_BENCH_BROWSER=required`
  turns the skip into a failure, and the CI job sets it.
- **The audit's claim was checked, not inherited.** The maturity audit of
  2026-09-03 recorded "0 `aria-*` attributes and contrast of 1.9–2.3:1" for
  this panel. Measured now, against the built interface: no contrast
  violation on the login screen or on the overview, Data Studio and audit
  screens, no unnamed control, a language on the document, one main landmark,
  and a focus ring that is reachable by keyboard and visible when it lands.
  Whatever was true in August is not true of this build — which is the whole
  reason the number has to come from an instrument and not from a document.

## A form, after the arc's fourth session

Everything an operator does to *data* now includes the part a table of scalars
cannot do: a foreign key is picked from what it may point at (`DS-10`), the
children of a record are edited with it (`DS-11`), and a document, a file or
rich text have something to be edited with (`DS-12`). Two things worth
retaining about how those are drawn: the lookup is the TARGET model's
permission, so it never widens a grant to render a nicer widget; and the
children are written without a transaction, each reported on its own, because
the data contract writes one row at a time and pretending otherwise would be
the rollback it cannot do.

## What the bench got wrong about itself

Four of the first run's readings were the bench measuring itself, and they are
recorded here because the next person to add a probe will hit the same edges.

- **The panel's single-page fallback answers 200 for any unmatched path**,
  `/api/*` included, so "there is no such endpoint" arrives as `200
  text/html` — and as `405` when the probe asks with POST, because only the
  fallback's GET pattern matches. Six absence probes read that as "the
  surface exists".
- **The helper that renders a body for logs truncates it.** Three probes
  asked whether a payload contained something, got the first 400 bytes, and
  recorded an absence.
- **Grants accumulate on a subject.** One shared non-superuser operator made
  "an operator granted only `list` created a record" look like a finding; it
  was a grant an earlier probe had made. Every permission probe now takes its
  own operator.
- **A model with one filterable column measures the filter language through a
  keyhole.** `DS-05` asks for a range, a substring and a set, and the bench's
  only filterable field was `status`. Every operator form but equality came
  back refused — for the FIELD, not for the operator — and the probe would
  have recorded "no operators" for a panel that had them. Two more fields are
  tagged `filter` now. The general shape: when a probe reads an absence, ask
  whether the fixture could have expressed the presence.
- **The bench shares ONE application across every probe.** A probe that filters
  the whole table is answered by whatever ran before it: `DS-05`'s first
  version compared against an exact set of rows and read the pagination
  probe's leftovers as a broken filter. Every list assertion is scoped to rows
  that probe created.
- **A foreign key has to be declared.** The bench's first model had a
  `NoteID` field and no relation declaration, so nothing marked it as a key —
  and the probe read that as "the panel has no relation metadata". It was
  measuring the model the bench wrote.
- **A config key is not a capability.** `CUST-03` ("dashboards and widgets an
  application declares") matched the substring "widget" in the names of the
  mount surface, so adding `field_widgets` — which says how a FIELD is edited
  and has nothing to do with a landing screen — moved that control from absent
  to partial on nothing at all. A probe that reads the NAMES of a config is
  measuring names.
- **A record id is a short number, and `Contains` finds it anywhere.** AUD-05
  asked whether the id of a record written before a restart appeared in the
  restarted process's audit payload. It does: in that process's own login
  entry, in a timestamp. The probe reported a trail that survives restarts
  for an application whose trail is a buffer in memory — and it did so only
  on CI, where the ids happened to line up. Probes now look for THE entry
  (action, model, record id), not for a substring.
- **A verdict read over the whole list changes with the `-run` filter.**
  `OPS-02` counted how many rows of the shared session list carried an
  address, and recorded `partial` in the full run because earlier probes had
  driven the superuser's session through the panel; run on its own it read
  `absent`. Same list, different history. It is the shared-application trap
  again, one family over: a session probe reads ITS operator's row, after
  that operator has made one request through the panel — the panel stamps a
  session on requests that go through it, and the store sees the stamp once
  that response is committed, so a sign-in alone leaves nothing to read.

## A list you can ask a question of, after the arc's fifth session

A filter used to mean one thing: this column equals this value. A list can now
be asked for a range, a substring, a set or a null, and it answers with a total
a pager can divide (`DS-04`, `DS-05`; saved views are `DS-17`).

The query string carries the operator: `?views__gt=100`,
`?created_at__gte=2026-01-01&created_at__lte=2026-06-30`,
`?title__contains=hammer`, `?status__in=open,paused`, `?archived_at__isnull=true`.
The twelve are `eq`, `ne`, `gt`, `gte`, `lt`, `lte`, `contains`, `startswith`,
`endswith`, `in`, `not_in` and `isnull`; an operator outside that set is
refused, because one that fell back to equality would list every row while
reading as a filter.

Four decisions the probes pin, each of them a way a filter can lie:

- **A wildcard in the value is data.** `?title__contains=%` matches the
  per-cent sign, not everything. On an engine whose `LIKE` has no escape
  character the Quark-backed data source refuses the query rather than
  answering it wrongly.
- **An empty `in` matches nothing.** `?status__in=` is a question with an
  answer; dropping it would return every row.
- **The two halves compose.** `?status=open&views__gt=100` is one query, and
  the exact-match filter and the operator filter are ANDed — which is also how
  every list assertion in the bench isolates its own rows.
- **A field the model does not offer as a filter is refused through the
  operator form too**, and an excluded field is answered as if the column did
  not exist. The operator path is a second door to the same room, and the fuzz
  corpus walks it.

## Whose session, from what, after the arc's sixth session

The session viewer used to list rows an operator could end and could not
read: no name on any of them and no device, so "that one is not me" was a
guess over a token prefix. A row now says whose it is (`OPS-16`), from what
(`OPS-02` — the browser and the platform, with the raw agent beside them),
and which one is the operator's own, and every session of one account can be
ended in one call (`OPS-04`).

Three decisions the probes pin:

- **The name in the row is the name the revocation matches on.** `user` is
  the operator the panel's own authentication signed in, or the identity key
  an application stores, and `POST /admin/api/sessions/revoke-all` takes
  that same string. What an operator reads is exactly what "revoke every
  session of this user" acts on; a second identifier the row did not show
  would be a second thing to be wrong about.
- **The request that revokes never revokes itself.** Revoking your own
  account keeps the session of the browser you are doing it from and ends
  every other device, which is what "sign out everywhere else" means; a call
  that signed the operator out mid-action would leave a screen with no one
  behind it. The answer says how many were ended and whether the caller's
  own was among the matches and kept, and the audit entry records the same,
  whether the call completed or not.
- **The panel records the device itself, under the framework's key.** The
  framework stamps a session's agent at most every thirty seconds; the panel
  refreshes a session on every panel request, so a viewer fed by the
  framework alone would name the device of a session that just signed in and
  nothing about one that has been open all morning. Both write the same key,
  so a row reads the same whichever wrote it last.

The probes measure by effect: the same account signed in from two clients,
both locked out after one call, the superuser who made it still signed in
afterwards — and then that same superuser revoking their own account and
still being signed in.

## What an application adds to the panel, after the arc's seventh session

The panel's verbs are the ones every table has, and its screens are the ones
every application has. A product always grows at least one of each that is
only its own, and until this session the only extension point was
`DataSource` — which replaces the backend and leaves the interface alone. Two
contracts close that (`DS-09`, `CUST-04`):

- **An action of the application's own, on one of its models.**
  `orbit.Config.Actions` declares a verb ("publish"), the label on the button
  and the function that runs it; the grid draws it beside Delete, and the
  panel supplies everything an application should not have to rebuild.
- **A screen of the application's own.** `orbit.Config.Pages` mounts an
  ordinary `http.Handler` under the panel's prefix, behind its session, gated
  by its RBAC and listed in its navigation. The panel puts the operator on
  the request context; the application writes the page.

Four decisions the probes pin:

- **The verb IS the permission.** An action named `publish` is authorized as
  `publish` on `admin:<Model>`, and an `admin:<Model>#own` grant confines it
  exactly as it confines a delete: the ids the application's function
  receives are the ones this operator may touch, and the rest come back as
  per-id failures. That confinement is the whole reason an action runs
  through the panel instead of being an endpoint of the application's own —
  one that bypassed it would be the way around every row policy the panel
  enforces. A selection that is refused whole never reaches the function at
  all, because an action told "no ids" would be indistinguishable from one
  invoked over the entire table.
- **A declaration the panel cannot honour refuses to start.** An unknown
  model, a duplicate verb, one of the panel's own verbs, a page with no
  handler or an id with a slash in it: each of these would otherwise be a
  control that silently never appears. They are checked when the module
  mounts.
- **A page is a link, not a frame.** The panel sends `X-Frame-Options: DENY`
  and `frame-ancestors 'none'` on every response, so a screen embedded in the
  SPA would be blocked by the browser while every Go test still read a 200 —
  and relaxing that header for the whole panel to embed one screen trades a
  clickjacking defence for a layout. The same hardening applies to the page's
  own response, so its scripts come from files rather than inline `<script>`.
- **An action an operator may not run is not offered.** The schema carries
  only the actions they hold, so the grid draws no button for the others; the
  permission hints still carry the verb as `false`, exactly like every record
  verb they do not hold.

Both probes measure by effect. `DS-09` runs the action and reads the row back
changed — a 200 from the bulk endpoint only says the verb was routed, and this
bench has already recorded one control as present on the strength of a 200
that meant nothing (`OPS-15`, below). `CUST-04` asks for the three things that
make a screen part of the panel: the navigation lists it, it is served under
the panel's prefix, and it knows which operator is reading it.

## The panel wearing the product's clothes, after the arc's eighth session

Three things a product needs and the panel could not be told (`CUST-02`,
`CUST-03`, `CUST-05`):

- **Branding** — `orbit.Config.Branding`: the logo on the sidebar and the
  login screen, the icon in the browser tab, the accent colour. It travels on
  the document as meta tags, the same channel as the prefix and the title,
  which is what makes it available on the LOGIN page, before any API call
  could carry it. (Measured at A11's baseline: the login screen did not
  draw it — the document carried it and nothing rendered it there. It does
  since the arc's first session, `UIX-07`, below.)
- **The landing screen** — `orbit.Config.Widgets`: cards the application
  declares, each with the function that reads its value. The panel supplies
  the screen, the authorization (`admin:dashboard`), a three-second bound and
  the degradation.
- **The language** — `orbit.Config.Locale` and `Config.Messages`: the panel's
  own chrome, in the languages it ships (English and Spanish) or in one an
  application brings itself.

Five decisions the probes pin:

- **The branding values are validated, not escaped.** A `javascript:` logo
  would be script execution on every page of the panel, granted by a line of
  YAML — so the URL is either an absolute http(s) one or a same-site path,
  and the colour is a hex colour or the application does not start. Escaping
  would have made the injection inert in the document and left the value in
  place for the next thing that read it.
- **The brand colour decides the text drawn on it.** A colour is chosen to
  look like a brand, not to contrast with white: the panel computes the
  foreground from the lightness, because white on a pale yellow button is a
  contrast failure the panel would have introduced on the application's
  behalf.
- **A widget that fails is drawn saying so.** A screen that dropped the card
  would report a broken query as "nothing to see"; one that waited for it
  would make the whole overview feel broken. Each card is bounded
  independently, and a panic inside one is that card's error, not the
  process's.
- **A card is a reading of the application, so it is authorized.** The
  per-widget permission on `admin:dashboard` is what lets "this role sees the
  finance numbers" be a policy instead of a fork.
- **Translation stops where the application begins.** The catalogue covers
  the chrome — navigation, buttons, empty states — and never the data: a
  model called Invoice is called Invoice in every language, a field label
  comes from the application's struct tags, and an error the application
  returns is its own sentence. The catalogues merge rather than replace, so a
  phrase nobody translated reads in English rather than as `nav.audit`, and
  an application can translate the panel into a language the panel does not
  ship.

## The views that reported their configuration, after the arc's ninth session

Three operations screens answered with how they were set up rather than with
what was happening, and the API prefix answered with a web page. All four are
closed, and operations is the first family to reach 59-scale completeness.

- **The cache view now shows the cache the application has** (`OPS-11`). It
  knew exactly one cache — a Redis URL in configuration — so every other
  application was told "redis url is not configured", including the ones that
  have no Redis and never asked for one. An application now declares its
  cache through `orbit.Config.Cache`, and the view counts its entries and
  empties it. An application with no cache is told there is none, and the
  flush button is withheld rather than offered and refused.
- **The email view now shows delivery** (`OPS-13`): whether the sender
  answers when asked, and what is waiting in the outbox — queued, failed,
  the oldest message still pending. Configuration is not delivery, and an
  SMTP host can be spelled correctly and refuse every connection.
- **The migrations view degrades** (`OPS-17`): an application with no
  migrations directory gets an empty list and the reason, not the migrator's
  error as a 500. A path that exists and is NOT a directory still fails,
  because a misconfigured `migrations_path` read as "nothing to apply" would
  stay hidden until a deploy needed the migrations that were never listed.
- **`/api/*` answers JSON**: an endpoint that does not exist returns 404
  JSON instead of the single-page app's HTML, and a path that exists for
  another method still returns 405 rather than claiming to be missing.

### What the bench got wrong about itself, the fifth and sixth times

- **A probe that accepts any 200 measures the web server, not the panel.**
  `OPS-15` — an export that runs as a job — was recorded `present` from the
  first run. It was reading the single-page fallback: the id of an export is
  its storage key, which contains a slash, so `GET /api/exports/{id}` never
  matched the ids the panel itself hands out, and polling one fell through to
  the SPA and came back as HTML with a 200. The probe asserted the status
  code and stopped there. Closing the API prefix (above) turned that 200 into
  a 404 and exposed a route that had never worked. The route takes the rest
  of the path now, and the lesson generalises: on this panel, a 200 is not
  evidence until something in the body is.
- **Looking for a word in the payload is not a measurement.** The first
  `OPS-13` probe searched the response for "queue", "outbox" or "sent". A
  view that merely NAMES its outbox passes that, and so does one reporting a
  queue of the wrong size. It now queues a message and asserts the view
  counts it.
- **An assertion about a queue must not race the dispatcher.** The probe
  first asserted that the pending count went up by one. The dispatcher is
  running: between the two reads it can lease the message, and the probe
  would lose that race at random. It asserts the TOTAL, which a queued
  message raises whichever state it is sitting in.
- **A second application belongs to the probe that starts it.** Two probes
  need an application the default one is not — one with a declared cache and
  a running outbox — and caching it on the env was wrong twice: the server
  is stopped when the probe that started it ends, so the next probe dials a
  closed port, and a shared cache would let one probe's flush decide
  another's count. Each caller gets a fresh one. (`migrationsApp` predates
  this and is used by a single probe, which is why it never showed.)

### What the bench got wrong about itself, the eighth time

- **A probe that reads a truncated body measures the truncation.** `UI-01`
  ("the panel ships a built interface") asked whether the served page
  contained `<div id="root"`, through the helper that shortens a body FOR
  LOGS. The verdict flipped to `partial` the day the panel started injecting
  branding and locale meta tags: the head grew, the marker moved past the cut,
  and nothing about the built interface had changed. It is the same trap this
  bench recorded in its first run — found again from the other side, two
  hundred commits later.
- **A shared handle belongs to the application that owns it.** The widget of
  the bench's application reads through a database handle captured at
  startup, and the module that captured it was mounted on every application
  the bench boots — so a probe that started its own, shorter-lived one
  overwrote it, and the shared application's card then read "database is
  closed". The capture now lives in a module mounted only on the shared
  application. Same shape as the second-application lesson `S9` wrote down.

### What the bench got wrong about itself, the seventh time

- **A method-less route beside a GET-only catch-all does not start.** The
  application pages were first registered for every method with `Handle`,
  which Go's `ServeMux` reads as ambiguous against the SPA fallback's `GET
  /{path...}` — and refuses to build the router, so the whole application
  failed to boot. The failure was loud and immediate, which is the good case:
  the methods are spelled out now. Worth remembering when adding any route
  next to that fallback.

### A note the first measurement got wrong

`OPS-11` was recorded with the note "an application whose cache is
in-process, which is the default". There is no default: `pkg/cache` is a
library an application builds with, and nothing in the framework wires a
cache into an application at all — only the CLI's `createcachetable` uses the
package. That is why the contract asks the application to declare its cache
instead of the panel discovering one. The finding was right about the
symptom and wrong about the cause, which is the same trap A4 and A5 recorded
from the other side: a comment is not a measurement, and neither is a note.

## What an application adds, at the baseline of A11

A6 left four extension points, each measured present: an action of the
application's own over a selection (`DS-09`), the brand's logo, favicon and
accent (`CUST-02`), cards on the overview (`CUST-03`) and a screen of the
application's own (`CUST-04`). The extension family asks what an application
that leans on those points asks next — a form before the action, the action
on the record it is about, a chart, a second screen of cards, its own script
and its own field renderer, the theme its operators open on — and two
properties every extension point should have: a declaration the panel cannot
honour stops the application, and what the configuration accepts reaches the
browser. It is A11's numerator on this side of the suite. It was recorded
before the arc changed anything at 1 present, 3 partial and 9 absent; the
table is the reading after the arc's fifth session (O5, below):

### extension — 8 present · 0 partial · 5 absent (HTTP)

| id | control | verdict | what is missing |
|---|---|---|---|
| `EXT-01` | an action asks the operator for input before it runs (a form) | **absent** | ModelAction declares no input and the action descriptor publishes none; an input posted with the call is dropped before Run |
| `EXT-02` | an action is offered on the record view, for that one record | **absent** | the bulk endpoint runs an action over one id, but nothing says which actions belong on one record and the record payload names none — the record view has no action surface |
| `EXT-03` | an action answers with a file to download or a page to open | **absent** | ActionResult is a message, a count and an untyped Data map the panel echoes; no Location, no Content-Disposition, no typed member a screen could follow |
| `EXT-04` | a widget draws a series (a chart), not only a value or a list | **present** | — |
| `EXT-05` | cards on a screen other than the overview: a second dashboard, or a page made of cards | **present** | — |
| `EXT-06` | the application's own script runs in the panel (a client-side hook), declared and allowed by the CSP | **absent** | no knob declares a script; the document loads only the panel's bundle under script-src 'self' |
| `EXT-07` | a field drawn by a renderer the application provides | **absent** | the widget vocabulary is closed (json, richtext, file, image); a field declared with another widget refuses to start (EXT-08), and nothing registers a renderer |
| `EXT-08` | a field widget the panel cannot draw refuses to start | **present** | — |
| `EXT-09` | a default theme (dark, light, system) set by configuration decides the first frame | **present** | — |
| `EXT-10` | a palette by configuration, each colour validated | **present** | — |
| `EXT-11` | branding the configuration accepts is loadable under the panel's own CSP | **present** | — |
| `EXT-12` | the configuration reference documents every key an application can bind | **present** | — |
| `EXT-13` | what an application adds answers to the panel's RBAC: card, screen and verb withheld without a grant | **present** | — |

Four controls of the same family can only be measured in a browser and
are recorded in the browser half, with their own numerator: `UIX-07` (the
login screen draws the declared logo — **present** since O1), `UIX-08` (the
record view offers the declared action — **absent**, the drawing half of
`EXT-02`), `UIX-10` (the first frame is in the configured theme, and the
operator's choice wins on reload — **present** since O2, the painting half
of `EXT-09`) and `UIX-11` (a second dashboard draws a series for the
operator granted it and is withheld from one who is not — **present** since
O5, the drawing half of `EXT-04` and `EXT-05`).

### What the shape of it says

- **An action is a verb over a selection, and nothing more.** It asks the
  operator nothing (`EXT-01`), it is not offered where the record is
  (`EXT-02`, `UIX-08`), and it answers with a toast: `ActionResult.Data` is
  echoed as an untyped map, so "export these as PDF" or "open the
  reconciliation" has no contract to ride (`EXT-03`).
- **The cards were one screen of numbers** (closed in O5, below). A value,
  a detail line or a list of rows, all on the overview (`EXT-04`, `EXT-05`). A screen of the
  application's own is an `http.Handler` that writes its own document; a
  page "declared in Go" that the panel draws from cards does not exist.
- **The SPA is closed.** No script of the application's runs in it
  (`EXT-06`), and the field widgets are the four the panel ships (`EXT-07`).
  Both are the same missing piece seen from two sides: a client-side
  registration the CSP allows.
- **The theme belonged to each operator** (closed in O2, below). The first
  frame followed the browser's preference, then the operator's last toggle;
  the application could not say "this control room opens dark" (`EXT-09`),
  and its palette was one accent colour (`EXT-10`).
- **What an application adds answers to the panel's RBAC** (`EXT-13`,
  present): an operator with no grant is not shown the card, the screen or
  the verb, the screen refuses them, and the grant opens each one. Every
  surface this arc adds is one more place to keep that true; the control is
  there so that a new one cannot forget it in silence.

### What the baseline found that was not on the plan

The first four were closed by the arc's first session (O1, below).

- **FIXED — `field_widgets` was the one declaration the panel did not check**
  (`EXT-08`). Actions, pages, widgets, branding and the locale refuse at
  startup what the panel cannot honour; a field widget the panel does not
  ship, or one on a field that does not exist, starts — and the schema
  publishes the field as its column type, as if nothing had been declared.
- **FIXED — the configuration accepted branding the panel's own CSP refused**
  (`EXT-11`). The validation accepts an absolute `https://` logo or favicon
  explicitly; the panel sends `img-src 'self' data:`, so a browser fetches
  neither. `CUST-02` reads the document, which carries the URL, and measured
  present. The baseline also wrote that the same-site logo the bench
  declares does load; the policy allowed it, and the bench served nothing at
  that path (O1, below).
- **FIXED — the login screen drew no logo** (`UIX-07`). The section on the arc's
  eighth session above says the logo is on the sidebar and the login screen;
  the login document carries it as a meta tag and the login screen renders
  nothing from it. The sidebar does — which is also what proves the
  instrument can see a logo when one is drawn.
- **FIXED — the configuration reference was five keys short** (`EXT-12`):
  `branding.logo_url`, `branding.favicon_url`, `branding.primary_color`,
  `locale` and `messages` bind from `nucleus.yml` and have no row in
  `website/docs/configuration.md`. The probe reads the keys from the binding
  contract — the koanf tags, nested structs included — not from a list.
- **The plan calls the type `orbit.Action`; what shipped is
  `orbit.ModelAction`**, documented under that name. That is not a control:
  a name is not a capability, and a probe that checked for one would measure
  the name. An alias would add a second name for one type to a frozen
  surface; the plan should use the name the product has.

### How the family is measured

- **A surface that appears turns the probe red.** Most of these controls are
  absences, and an absence cannot be exercised. Each probe therefore reads
  what the panel SERVES — a payload, a document, a header, whether the
  application's own function received what was posted — and also the contract
  types an application writes against (`ModelAction`, `ActionResult`, and
  until O5 `Widget`, `WidgetValue` and `Page`) and the whole mount surface. When one of
  those grows a member that could carry the capability, the probe answers
  `partial`, the suite goes red against the recorded `absent`, and that is
  the moment to grow it into the behaviour check the new surface makes
  possible. An absence is never decided by a missing name alone.
- **A question of refusal boots its own application.** `EXT-08`, `EXT-10`
  and `EXT-11` ask whether the panel refuses a declaration, which the
  bench's usual boot cannot answer: it fails the test when an application
  does not start. These boot through `tryStart`, which hands the refusal
  back as a value.
- **A browser absence must fail for its own reason.** A spec fails when the
  capability is missing and also when it broke for any other reason — a
  selector that no longer matches, a page that did not load — and both read
  as absent. `UIX-07` and `UIX-08` check their preconditions first, with
  messages of their own, and the Go side records the substring the failure
  must carry (`failsWith`). Writing `UIX-08` showed why: its first run
  failed on a precondition — a request made from outside the browser that
  signed in is refused with a 401 — and would have been recorded as the
  absence it was looking for.

## The panel keeps what its configuration promises, after A11's first session

Four of the baseline's findings were one property seen four times: what an
application declares, the panel either honours or refuses at startup — never
accepts and then drops. The first session of the arc in this repository (O1)
closed them (`EXT-08`, `EXT-11`, `EXT-12`, `UIX-07`): the HTTP bench reads 63
of 72, the browser half 7 of 8.

- **A field widget the panel cannot apply stops the application, by name.**
  `field_widgets` is checked when the module mounts, against the four widgets
  the panel draws and against the models of THIS application, through the
  same spellings the schema reads (the Go name or the column, either case).
  An unknown widget, a key that names no field, and two keys for one field
  with different widgets each refuse to start, and the error names every
  entry at once.
- **An absolute branding URL loads.** The panel's `img-src` names the origin
  of an absolute logo or favicon — scheme, host and port, never the path —
  and nothing else changes in the policy. A same-site path adds nothing,
  because `'self'` already covers it.
- **The login screen draws the logo**, in place of the panel's mark, with an
  empty `alt`: the title below names the product in text, and a screen
  reader would otherwise read it twice. A logo that fails to load gives the
  panel's mark back rather than leaving a hole.
- **The configuration reference carries the five keys** that bound from
  `nucleus.yml` without a row: `branding.logo_url`, `branding.favicon_url`,
  `branding.primary_color`, `locale` and `messages`.

Three decisions worth keeping:

- **Refusing an entry that never worked is a fix, not a break.** The
  suite's rule is that nothing incompatible ships before the major that
  gathers them (QADR-0010), and an application that started yesterday with
  a misspelled widget does not start today. Every entry the check refuses
  was dropped before it existed: an unknown widget resolved to nothing and
  the field fell back to its column type, and a key naming no field matched
  nothing. A conflicting pair was worse than dropped, since which one won
  could change between two requests. What stops starting is an application
  carrying a declaration with no effect, and the error says which one —
  there was no behaviour to keep behind a warning. The internal comment that
  promised a `Model.*` wildcard was wrong: none was ever read, and the check
  refuses one like any other key that names no field.
- **The policy names an origin, so the origin has to be one a policy can
  name.** The host goes into a response header: a host carrying a `;` would
  end `img-src` and begin another directive, and a wildcard would widen it.
  The URL validation therefore also refuses, at startup, a host a CSP source
  cannot express — an IPv6 literal, a wildcard, a host with no name, a
  character outside letters, digits, hyphens and dots — and converts an
  internationalised name to the ASCII form a browser matches against. Each
  of those was accepted before and never loaded, so this is the same kind of
  refusal as the first decision, not a narrowing of what worked.
- **A logo is drawn when the browser loaded it.** `UIX-07` asks the browser
  for the image's natural width on the login screen, not for an `<img>`
  element: an image the policy refuses, or one that answers 404, is in the
  document and on nobody's screen.

### What the bench got wrong about itself, the ninth time

- **The bench's own logo was a 404.** The bench application declared
  `/static/bench-logo.svg` and served nothing there, so the sidebar the
  baseline used as `UIX-07`'s proof that "the instrument can see a logo" was
  showing a broken image, and the first spec would have recorded the login
  screen as present on one too. Counting `<img>` elements cannot tell a logo
  from a broken image; asking the browser whether it loaded can. The bench
  application serves the file now, and `UIX-07` fails, with its own message,
  when the image it finds did not load.
- **A refusal is evidence only when it is about the entry.** `EXT-08` first
  counted any failure to start as the refusal it was looking for. It now
  requires the error to name the entry, and requires an entry the panel CAN
  draw to start — a check that refused every `field_widgets` entry would
  otherwise have read as present.

Every change was verified by breaking it: without the check at mount,
`EXT-08` reads absent; letting an unknown widget through, partial; a check
that refuses everything, partial; the policy without the branding origins,
`EXT-11` partial; the reference without `locale`, `EXT-12` partial; the login
screen without the logo, or the bench without its file, `UIX-07` absent.

## The theme it opens in and a palette per theme, after A11's second session

The second session of the arc in this repository (O2) closed `EXT-09` and
`EXT-10` and added `UIX-10`, the browser half of the first: the HTTP bench
reads 65 of 72, the browser half 8 of 9.

- **A theme by configuration, decided before the first frame.**
  `branding.theme` is `dark`, `light` or `system`. The document carries it
  and loads, in `<head>`, after the value and ahead of the bundle, a classic
  script from the panel's own origin (`<prefix>/theme.js`, served before
  sign-in like the bundle's assets). The parser stops for it before there is
  a body to paint, so the first frame is already in the theme the panel
  keeps, and `script-src` stays `'self'`: no inline script, no nonce, no
  hash. With no theme configured, nothing is injected and the document is
  the one the panel served before, byte for byte.
- **The operator's own choice wins.** The toggle now records the choice in
  a key of its own, and the script applies it over the configured theme on
  every load.
- **A palette per theme, checked against its own ground.**
  `branding.light` and `branding.dark` each take `primary_color`,
  `surface_color` and `text_color`. Every one that is set is checked at
  startup against the colours that theme will actually draw with — the
  configured ones over the panel's own: text on the surface and the panel's
  secondary text on the surface at 4.5:1, the accent against the surface at
  3:1, and the text the panel draws on the accent at 4.5:1. The palette is
  written into the document as the custom properties the stylesheet
  already reads, one rule per theme, so it is in the first frame too.

Four decisions worth keeping:

- **A per-theme colour that falls short is refused; `primary_color` that
  falls short is warned about.** The per-theme keys are new, so no
  application that started yesterday carries one, and the panel can be
  strict from their first day: the error names the theme, the keys and the
  ratio. `branding.primary_color` was accepted on any hex colour, in both
  themes, before anything checked it per theme, and a dark navy that reads
  at 18:1 on white is 1.1:1 on the dark ground. Refusing it now would stop an
  application that changed nothing — the break QADR-0010 keeps for the
  major — so it starts, and the panel logs a warning naming the theme and
  the `branding.<theme>.primary_color` that fixes it. `EXT-10` measures both
  halves: the per-theme navy refused, the old-key navy started.
- **A theme stored before this version is not a choice.** The panel used to
  write the browser's preference into its theme key (`gf-theme`) on every
  first visit, and the toggle wrote the same key, so a stored value cannot
  say whether anybody chose it. Honouring it over a configured theme would
  make the setting invisible to every operator who had ever opened the
  panel; ignoring it overrides, once, an operator who did toggle before this
  version and has not toggled since. The second is the smaller harm and it
  happens only when the application opts in; from the first toggle on, the
  choice is kept. Without a configured theme, the old key is read exactly as
  before.
- **The script that decides the first frame lives in the root, not in the
  bundle.** The server already writes the document; putting the script next
  to it means the HTTP half can measure the whole server side in every lane,
  including the one that builds the root against the `ui` module by tag.
  The script also leaves the theme it applied where every bundle looks for
  the last one, so an older bundle does not switch it after the first frame.
- **No border colour.** The baseline's note listed a border among what a
  palette lacks. There is no contrast rule for it that the panel's own
  border passes — about 1.2:1 on white, a separator rather than the edge of
  a control — and a colour the panel cannot check would contradict "each
  colour validated". `EXT-10` asks for the accent, the surface and the text
  of each theme.

### What the work found that was not on the plan

- **The text drawn on a brand colour was chosen by lightness, and the docs
  promised more.** The features page said the panel picks the foreground so
  that "white on a pale yellow button" cannot happen; by lightness alone a
  saturated yellow, cyan or green (all at 50%) got white text, at 1.07, 1.25
  and 1.37 to 1. The bundle and the server now draw whichever of the two
  inks reads better on the colour — never worse than before, since the old
  choice is one of the two — and the palette check uses the same rule.
- **The login page wore branding the panel had refused.** The panel
  validates its branding and ignores one that does not validate; the login
  page received it raw. Through `orbit.Module` an invalid branding stops the
  application first, so only a hand-wired panel could show it, and it now
  validates the same way.
- **The bench measures contrast in the light theme only.** `UIX-01` and
  `UIX-02` run against the bench's own application, which opens light. A
  one-off run of the same engine over the themed application of `UIX-10`
  (dark, with a surface of its own) found no contrast violation on the
  login, overview, Data Studio and audit screens; it is not a control.

### What the bench got wrong about itself, the tenth time

- **The first version of `UIX-10` passed a script that does not stop the
  parser.** Held at the network, the bundle cannot decide the first frame,
  so the spec read the frame while it waited — and with the theme script
  turned into a module, or deferred, it still passed: on a fast loopback the
  script arrived before Chromium painted. The race was won by the network,
  not by the document. `UIX-10` now delays every script the document loads
  except the bundle and its chunks by 400 ms, so a script that does not stop
  the parser loses to the first paint and the frame shows it. The HTTP half
  (`EXT-09`) refuses such a script on sight; the browser half now sees it
  too.

Every change was verified by breaking it:

| mutation | control |
|---|---|
| a theme that is not one accepted | EXT-09 → partial |
| no theme script on the document | EXT-09 → partial; UIX-10 → absent |
| the script ahead of the value it reads | EXT-09 → partial |
| the script deferred | EXT-09 → partial; UIX-10 → absent |
| the script a module | EXT-09 → partial; UIX-10 → absent |
| the login page, or the panel's document, without it | EXT-09 → partial |
| injected with no theme configured | EXT-09 → partial |
| `script-src` loosened with `'unsafe-inline'` | EXT-09 → partial |
| no per-theme contrast refusal | EXT-10 → partial |
| `primary_color` refused like the per-theme keys | EXT-10 → partial |
| one rule for both themes (the dark ground) | EXT-10 → partial |
| the palette not written | EXT-10 → partial; UIX-10 → absent |
| the palette's selectors swapped | EXT-10 → partial |
| per-theme colours not checked as hex | EXT-10 → partial |
| the head script ignoring the operator's choice | UIX-10 → absent |
| the toggle recording no choice (dist rebuilt) | UIX-10 → absent |
| the bundle deciding again after the head script (dist rebuilt) | UIX-10 → absent |
| the root's head script with O1's bundle | UIX-10 → absent (first frame right; the choice lost on reload) |

## Cards of every kind and a second dashboard, after A11's fifth session

The fifth session of the arc in this repository (O5) closed `EXT-04` and
`EXT-05` and added `UIX-11`, their browser half: the HTTP bench reads 67 of
72, the browser half 9 of 10. `EXT-13` stays present and asks about the two
surfaces the session added.

- **A card's kind says what it draws, and each kind reads a function of its
  own type.** `Widget.Kind` is `stat` (`Stat`: a figure, its change, a
  trend and whether the change is good news), `line` or `bar` (`Series`:
  one value per label for each series), `table` (`Table`: columns and rows
  of text) or `records` (the newest rows of a model). An empty kind is the
  value card A6 shipped, read from `Load`. The application writes no
  frontend code for any of them.
- **A `records` card has no function.** The panel lists the rows itself,
  through the same list the grid uses, as the operator who is looking: the
  tenant, the `#own` scope and the field permissions apply, and an operator
  who may not list the model is not shown the card at all. A function of
  the application's would return rows none of those policies saw — the
  same reason an action runs through the panel.
- **Dashboards beside the overview.** `Config.Dashboards` declares screens
  of cards, each with its columns and its cards in the order declared, each
  listed in the navigation and gated as a whole by `view` on
  `admin:dashboard:<id>`. An operator without it does not see it listed,
  and its API (`GET <prefix>/api/ui/dashboards/<id>`) answers 403; an
  unknown one is a 404. The overview is not one of them and does not
  change: its cards, their resource and their order are A6's.
- **A declaration the panel cannot draw stops the application, naming
  it**: an unknown kind, a kind without its function, a function its kind
  never reads, a `records` card on a model, field or order the application
  does not have, a span wider than its screen, a dashboard with no cards.
  What a function returns that its card cannot draw — a series shorter
  than its axis, a value that is not a finite number, a ragged table, an
  unknown trend — is that card's error, never the screen's.

Four decisions worth keeping:

- **A value card's payload is A6's, byte for byte.** Every member a newer
  kind adds is omitted when empty, and the navigation payload carries
  `dashboards` only when the application declared one; an internal test
  pins both against the literal JSON. A panel declared before this session
  renders as it did (QADR-0010).
- **The overview's cards moved into a chunk of their own.** The grid that
  draws every kind is loaded by the overview only when the application
  declared cards, and by the dashboard route; the chart library is a
  further chunk, loaded only by a screen with a chart, and shared with
  System Pulse, which already used it. Measured with the same script
  before and after (KiB, gzip at zlib's default level): the first load's
  JavaScript went from 99.8 to 100.0, in the same five files, and its
  stylesheet from 7.3 to 7.4; everything the panel ships went from 506.7
  to 510.5. Left to itself the bundler cut the shared code of the first
  load into five chunks once the grid became a chunk both the entry and a
  lazy route load; a `codeSplitting` group (`app`) keeps it in one.
- **The bounds are a card's, not a report's.** At most 8 series and 500
  points, 12 columns and 100 rows, 50 records. Past them the card says so
  in its error instead of shipping the payload: a widget is a glance, and
  a truncated table that looks complete would be the worse answer.
- **The bench's series is fed by the application's table, not by a
  constant.** `EXT-04` creates a note and requires today's reading to move
  by exactly one, and `UIX-11` requires the point of the highest reading to
  be drawn above the point of the lowest — a chart drawn from nothing, or
  from the wrong numbers, passes neither. `UIX-11` also holds the new
  screen to the contrast and naming rules `UIX-02` and `UIX-03` hold the
  panel's own screens to, so a card kind cannot add an unreadable colour
  or an unnamed picture in silence.

### What the work found that was not on the plan

- **An operator whose role opens only a dashboard could not sign in.** The
  interface decides whether a session is signed in by asking for the model
  list, and read the 403 an operator without `list_models` gets as "signed
  out": after every successful sign-in it drew the login screen again, with
  nothing saying why. A 403 is now a session that was accepted; each screen
  says what it may not show. `UIX-11` found it — its first run had the
  granted operator back on the login form.
- **The document named its assets relative to itself.** The build writes
  `./assets/…`, which resolves only while the interface's path is one
  segment deep — and every screen was, until `<prefix>/dashboards/<id>`.
  Opened by its address, reloaded or bookmarked, a dashboard asked for
  `<prefix>/dashboards/assets/…`, was answered the document instead of the
  script, and drew a blank page. The panel now writes the bundle's paths
  from its own root on the documents it serves (the panel's and the login
  page's); the chunks the bundle loads later already resolved against the
  bundle's own URL. An internal test asks for a two-segment screen.
- **SQLite's date functions cannot read the timestamps the framework
  writes.** The driver stores a `time.Time` as Go prints one
  (`2026-10-04 22:30:06.67 +0000 UTC`), and `date(created_at)` answers
  NULL for it. The bench's first series was a flat line of zeros; the
  probe caught it by creating a note and watching nothing move. The bench
  reads the day as a prefix of the text. An application writing its own
  series against SQLite will meet the same thing.
- **A value JSON cannot carry fails the screen with a 200.** Without the
  check on each value, a `NaN` reaches the encoder after the status is
  written, and the whole overview arrives as an empty body with a 200 —
  the kind of 200 that meant nothing this bench recorded at `OPS-15`. The
  check makes it the card's error; `EXT-04` asks for it, and refuses a body
  that does not decode rather than stopping on it.

### What the bench got wrong about itself, the eleventh time

- **The baseline's `EXT-05` would not have seen the dashboards.** It looked
  for a placement member on `Widget`, a layout member on `Page`, and a
  route at `<prefix>/api/ui/dashboards` with no id. Dashboards arrived as a
  list on `Config` and a route with an id, and the probe went on reading
  `absent` while the panel served them. `EXT-04` did notice its surface
  (`Kind` and `Series` on `Widget`). Both are now behaviour checks.
- **A probe that stops is not a probe that measures.** The first
  `EXT-04` decoded the screen with a helper that fails the test on a body
  that is not JSON, so the NaN mutation below stopped it instead of
  measuring `partial`.

Every change was verified by breaking it:

| mutation | control |
|---|---|
| the series card served without its series | EXT-04 → partial |
| a NaN left unchecked (the screen arrives empty, with a 200) | EXT-04 → partial |
| an unknown kind accepted | EXT-04 → partial |
| a kind without its function accepted | EXT-04 → partial |
| the bench's series drawn from a constant | EXT-04 → partial |
| the dashboards left out of the navigation | EXT-05 → partial; EXT-13 → partial |
| a dashboard's cards ordered by ID | EXT-05 → partial |
| a dashboard's spans dropped | EXT-05 → partial |
| a dashboard served to anyone signed in | EXT-05 → partial; EXT-13 → absent; UIX-11 → absent |
| the overview's grant opening every dashboard | EXT-05 → partial |
| a dashboard's cards also on the overview | EXT-05 → partial |
| an unknown dashboard answered 200 | EXT-05 → partial |
| a dashboard with no cards accepted | EXT-05 → partial |
| a records card shown without `list` on its model | EXT-13 → partial |
| the navigation drawing no dashboard (dist rebuilt) | UIX-11 → absent |
| the line marking no point (dist rebuilt) | UIX-11 → absent |
| the readings drawn upside down (dist rebuilt) | UIX-11 → absent |
| a stat's good-news colour below contrast in the light theme (dist rebuilt) | UIX-11 → absent |
| a 403 read as signed out (dist rebuilt) | UIX-11 → absent |
| the document's asset paths relative again | UIX-11 → absent |
| the grid and the chart imported into the first load | `ui/embed_test.go` → over budget (690.5 KiB raw against 512) |
