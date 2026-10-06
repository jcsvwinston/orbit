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

**72 of 72 controls present. 0 partial. 0 absent.**

| family | present | partial | absent |
|---|---|---|---|
| data-studio | 17 | 0 | 0 |
| permissions | 9 | 0 | 0 |
| audit | 7 | 0 | 0 |
| operations | 17 | 0 | 0 |
| customization | 7 | 0 | 0 |
| interface | 2 | 0 | 0 |
| extension | 13 | 0 | 0 |
| **total** | **72** | **0** | **0** |

The six families A6 measured are complete as of that arc's eighth session:
59 of 59. The seventh, `extension`, was recorded at the baseline of A11 and
is that arc's gap ([below](#what-an-application-adds-at-the-baseline-of-a11)):
1 present at the baseline, 4 after the arc's first session in this
repository (O1), 6 after its second (O2), 8 after its fifth (O5), 9 after
its third (O3), 11 after its fourth (O4), which joined after the fifth, and
13 after its sixth (O6), which completes it.
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
  the row anyway would be worse than no permission at all) and asks the
  hidden field back as a filter, a sort and a search
  ([OR-69](#what-a-query-may-name-or-69)), and the hint probe checks each
  hint against the answer the panel actually gives.
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

**18 of 18 controls present**, plus the one that measures the instrument.
`UIX-18` reads the rest of Data Studio's doors for three operators who are
not superusers, added for OR-65; it arrived present with the fixes it asked
for ([below](#the-rest-of-data-studios-doors-or-65)). OR-66 extended it to
read what the export it offers holds, and it stayed present with the fix
([below](#what-an-export-holds-or-66)); OR-69 extended it to read that a
field kept from the actor is neither offered nor answered as a sort, a
filter or a saved view, and it stayed present with the fix
([below](#what-a-query-may-name-or-69)); OR-67 extended it to read that an
import writes only what the operator could write by hand, and it stayed
present with the fix ([below](#what-an-import-writes-or-67)).
`UIX-16` and `UIX-17` are the screens of two of those operators, added for
OR-64; they found three defects of the panel's own, and arrived present with
their fixes
([below](#the-screens-of-an-operator-who-is-not-a-superuser-or-64)).
`UIX-13` to `UIX-15` arrived present with the arc A12's session O1, each the
browser half of a defect of the panel's own
([below](#the-panel-travels-compressed-after-a12s-session-o1)). The six
before them belong to the extension family
([below](#what-an-application-adds-at-the-baseline-of-a11)): `UIX-07` and
`UIX-08` were recorded absent at the baseline of A11; `UIX-07` closed in the
arc's first session (O1), and `UIX-08` turned present with the record
actions and their answers in its fourth
([O4](#an-action-on-one-record-and-what-it-answers-after-a11s-session-o4)).
`UIX-10` was added, present, in its second (O2), `UIX-11` in its fifth (O5),
`UIX-12` in its sixth
([O6](#the-applications-own-code-in-the-browser-after-a11s-session-o6)),
and `UIX-09` arrived present with the form it measures in its third
([O3](#an-action-that-asks-before-it-runs-after-a11s-session-o3)). The third
and fourth ran on a stack of their own and joined this one after the fifth.
(O2's own pull request numbered its control `UIX-09`; it became `UIX-10` when
the two stacks met, so the third session's kept its number.)

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
| **UIX-08** | an action on one record is offered where the record is, and its page and its file arrive |
| **UIX-09** | an action that asks first draws its form and shows the refusal on the field |
| **UIX-10** | the first frame wears the theme the application configured, and the operator's own choice wins on reload |
| **UIX-11** | a second dashboard draws a series to the operator granted it, and is neither listed nor served to one who is not |
| **UIX-12** | the application's own script loads under the policy and draws a field in the grid and the record view, and a renderer that throws falls back |
| **UIX-13** | a form with its errors showing is legible, in the light theme and in the dark one |
| **UIX-14** | Data Studio's grid draws its icons, and the panel's own policy refuses nothing it loads |
| **UIX-15** | the panel's own scripts and stylesheets reach the browser compressed once, by the build |
| **UIX-16** | an operator is offered a delete only where they hold one: no selection or Delete without it, and no batch Delete for one who may delete a record but not a batch |
| **UIX-17** | an operator who may not update is offered no edit: the row opens the record read-only, and its menu and the record view hold only the actions granted |
| **UIX-18** | an operator is offered a model, a record's history, an export, an import, the field settings and a saved view's removal only where they hold them, each one asked anyway is refused, an export holds only what the operator may list, a field the operator may not read is neither offered nor answered as a sort, a filter or a saved view, and an import writes only what the operator could write by hand |

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
table is kept current as the arc closes its gaps, session by session (below):

### extension — 13 present · 0 partial · 0 absent (HTTP)

| id | control | verdict | what is missing |
|---|---|---|---|
| `EXT-01` | an action asks the operator for input before it runs (a form) | **present** | — |
| `EXT-02` | an action is offered on the record view, for that one record | **present** | — |
| `EXT-03` | an action answers with a file to download or a page to open | **present** | — |
| `EXT-04` | a widget draws a series (a chart), not only a value or a list | **present** | — |
| `EXT-05` | cards on a screen other than the overview: a second dashboard, or a page made of cards | **present** | — |
| `EXT-06` | the application's own script runs in the panel (a client-side hook), declared and allowed by the CSP | **present** | — |
| `EXT-07` | a field drawn by a renderer the application provides | **present** | — |
| `EXT-08` | a field widget the panel cannot draw refuses to start | **present** | — |
| `EXT-09` | a default theme (dark, light, system) set by configuration decides the first frame | **present** | — |
| `EXT-10` | a palette by configuration, each colour validated | **present** | — |
| `EXT-11` | branding the configuration accepts is loadable under the panel's own CSP | **present** | — |
| `EXT-12` | the configuration reference documents every key an application can bind | **present** | — |
| `EXT-13` | what an application adds answers to the panel's RBAC: card, screen and verb withheld without a grant | **present** | — |

Six controls of the same family can only be measured in a browser and
are recorded in the browser half, with their own numerator: `UIX-07` (the
login screen draws the declared logo — **present** since O1), `UIX-08` (the
record view and the row's menu offer the record's actions, and the page and
the file they answer with arrive — **present** since O4, the drawing half of
`EXT-02` and `EXT-03`), `UIX-09` (the form an action declared, with the
server's refusal on the field — **present** since O3, the drawing half of
`EXT-01`), `UIX-10` (the first frame is in the configured theme, and the
operator's choice wins on reload — **present** since O2, the painting half
of `EXT-09`) and `UIX-11` (a second dashboard draws a series for the
operator granted it and is withheld from one who is not — **present** since
O5, the drawing half of `EXT-04` and `EXT-05`) and `UIX-12` (the
application's script loads under the panel's policy, its renderer draws a
field in the grid and on the record view, and one that throws falls back —
**present** since O6, the drawing half of `EXT-06` and `EXT-07`).

### What the shape of it says

- **An action is a verb over a selection.** At the baseline it asked the
  operator nothing (`EXT-01`; it asks since O3, below), it is not offered
  where the record is (`EXT-02`, `UIX-08`), and it answers with a toast:
  `ActionResult.Data` is echoed as an untyped map, so "export these as PDF"
  or "open the reconciliation" has no contract to ride (`EXT-03`). Since O4
  it is offered where it is declared and answers with a page or a file
  ([below](#an-action-on-one-record-and-what-it-answers-after-a11s-session-o4)).
- **The cards were one screen of numbers** (closed in O5, below). A value,
  a detail line or a list of rows, all on the overview (`EXT-04`, `EXT-05`). A screen of the
  application's own is an `http.Handler` that writes its own document; a
  page "declared in Go" that the panel draws from cards does not exist.
- **The SPA was closed** (opened in O6,
  [below](#the-applications-own-code-in-the-browser-after-a11s-session-o6)).
  No script of the application's ran in it (`EXT-06`), and the field widgets
  were the four the panel ships (`EXT-07`). Both were the same missing piece
  seen from two sides: a client-side registration the CSP allows.
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
  types an application writes against (`ActionResult`; `ModelAction` until
  O3 and O4, and `Widget`, `WidgetValue` and `Page` until O5) and the whole
  mount surface. When one of
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

## An action that asks before it runs, after A11's session O3

The third session of the arc in this repository (O3) ran on a stack of its
own, from the baseline, and joined this one after the fifth. It closed
`EXT-01` and added `UIX-09`, its browser half: on its own stack the HTTP
bench read 61 of 72; joined to the first, second and fifth it reads 68 of
72, the browser half 10 of 11. An action declares the inputs it needs next
to its verb — `ModelAction.Fields`, each one text, number, boolean, select
(with its options) or date — and the panel does the rest:

- **At startup**, a field it cannot draw stops the application, naming the
  action and the field: an unknown type, a select with no options, an
  option without a value or declared twice, options on a field that is not
  a select, a name a form cannot carry.
- **On the call**, what was posted is checked against the declaration
  before a row is read and before the application's function runs. A
  refusal is a `422` whose `details` map every field at fault to what is
  wrong with it — required, not a number, not one of the options, not a
  date, not a field of this action — in one answer, not one mistake per
  round trip. What passes reaches `ActionRequest.Input` typed: a string, a
  `float64`, a `bool`, a `time.Time`. The audit entry records it.
- **In the grid**, an action with fields opens a form in a dialog instead of
  the plain confirmation, and the refusal lands on the input it names, as
  that input's accessible description. An action without fields posts and
  runs exactly as before — the server ignores an `input` sent to one.

How it is measured, and what that showed:

- **The form checks nothing itself.** It posts what was entered and shows
  what the server answers; the browser's own constraint checking is off.
  That is a product decision — a second copy of the rule could disagree
  with the first, and a client that skipped it reaches the server anyway —
  and it is also what lets `UIX-09` measure the server: it submits the form
  empty and reads the refusal on the field, then fills it in and reads the
  action's own message. Verified by breaking it three ways: with the
  server's check disabled, `EXT-01` reads partial and `UIX-09` fails on its
  first assertion; with the fields left out of the schema, `EXT-01` reads
  absent and no form opens; with an unknown type accepted at startup,
  `EXT-01` reads partial.
- **The error was unreadable before it was measured.** `UIX-09` runs the
  contrast rule over the dialog while the errors are showing, and its first
  run failed there: `text-destructive` is the colour of a dangerous BUTTON,
  and as text on the background it reads at about 3.8:1 in the light theme
  and 2:1 in the dark one, below the 4.5:1 small text needs. The form uses a
  text token now (`--destructive-text`, 6.5:1 and 7.2:1). Nine other places
  still wrote messages in the fill colour — among them the record form's
  field errors and the import dialog's row errors — and no control measured
  them, because no screen `UIX-02` opens shows an error. **Fixed in A12
  O1**, with `UIX-13` to read them
  ([below](#the-panel-travels-compressed-after-a12s-session-o1)).
- **A selection existed only for an operator who could delete.** The grid
  drew its row checkboxes when the operator held `delete` on the model, so
  one granted `publish` and not `delete` was shown no way to select what to
  publish, and an action over a selection could never be offered to them.
  The checkboxes now appear when anything can be done with a selection. No
  control caught it: the browser half signs in as the superuser.

## An action on one record, and what it answers, after A11's session O4

The fourth session of the arc in this repository (O4) ran on the third's
stack and joined this one with it. It closed `EXT-02` and `EXT-03` and
turned `UIX-08` present: on its own stack the HTTP bench read 63 of 72;
joined to the others it reads 70 of 72, the browser half 11 of 11.

- **Where an action is offered** is part of its declaration:
  `ModelAction.Placement` is the selection (the zero value, so an action
  declared before O4 is offered and runs exactly where it was), one record —
  its record view and its row's menu — or both. The schema publishes it on
  every action, and the server holds the action to it: a record action has
  its own endpoint (`POST /api/models/{name}/actions/{action}/{id}`), a record
  action posted to the bulk endpoint is refused, and a selection action
  posted to the record endpoint is refused too. Both endpoints run the same
  function, so the verb, the tenant and row confinement, the input check and
  the audit entry cannot drift apart; the record's entry names the record,
  so its own history shows what was done to it.
- **What an action answers** is a message, a page of the panel
  (`ActionResult.Redirect`, a path relative to the panel) or a file
  (`ActionResult.Download`: a name, a declared media type and the bytes the
  action produced — never a path). Both are checked when the action answers,
  not when it is declared: a redirect out of the panel, a file over 32 MiB,
  with no type or no name, or both at once, is refused after the action ran,
  with a message that says so, and the trail records which kind of answer
  each call gave.
- **In the SPA**, the record view (the edit dialog) draws the record's
  actions and every row has a menu of them. A redirect to one of the SPA's
  own screens goes through its router; Data Studio keeps its model, database
  and open record in the URL, so `/data-studio?model=Note&record=42` lands on
  that record's view. A file is saved under the name the action gave.

How it is measured, and what that showed:

- **The bench's own application declares the two answers an admin action
  most often has.** `duplicate`, on one record, copies the note and answers
  with the copy's record view; `download_text`, on both, answers with the
  notes as a text file. `EXT-03` follows the redirect to a record the panel
  then serves, reads both files with their headers, and reads the record's
  history for the kind of each answer. The refusals are measured on a
  second application whose action redirects wherever its form says — the
  open redirect the check exists to prevent, built on purpose — and whose
  other action hands back 32 MiB and one byte.
- **`UIX-08` checks that the redirect did not reload the page**, by leaving a
  marker on the window before the click and reading it after the copy's
  view opened: a redirect followed by loading the document would also land
  on the right URL and show the right record.
- Verified by breaking it, one change at a time. With the placement not
  enforced, with the descriptor not publishing it, with the record endpoint
  gone, with the record's input unchecked, or with the audit entry not
  naming the record, `EXT-02` reads partial or absent. With the redirect
  unchecked, with a leading `//` allowed, with `..` segments or backslashes
  allowed, with no ceiling, with the answer's kind missing from the trail,
  with the file not sent as an attachment or without `nosniff`, `EXT-03`
  reads partial. With the record view drawing no action, the row drawing no
  menu, the redirect followed by a document load, the file not saved or the
  record link ignored, `UIX-08` fails, each with its own message.
- **A URL parser and a browser disagree about `///host`.** Go's `url.Parse`
  reads no host in `///evil.example/login`, and a browser resolving it
  against the panel's origin reads `evil.example`. The check refuses a
  leading `//` before parsing for that reason, and the bench's list of
  hostile targets carries `///…` because without it, removing that line did
  not move the verdict: `//evil.example` alone is caught by the host check
  either way.
- **An operator who may run an action and may not edit the record had no
  way to it.** The record view is the edit dialog, offered only with
  `update`, so the record action needs a second door: the row's menu. And a
  record reached through a link opens read-only for such an operator rather
  than as a form whose save would be refused. No control measures either,
  because the browser half signs in as the superuser — the same blind spot
  O3 found with the selection checkboxes.
- **The record endpoint's path is not the obvious one.** The panel's 404/405
  catch-all (`mountAPINotFound`) rebuilds every `/api/` route in a
  method-free mux, and `/api/models/{name}/{id}/actions/{action}` conflicts
  there with `/api/models/{name}/fields/{field}/options`: the application did
  not start. Hence `/api/models/{name}/actions/{action}/{id}`.

## The application's own code in the browser, after A11's session O6

The sixth session of the arc in this repository (O6) closed `EXT-06` and
`EXT-07` and added `UIX-12`, their browser half: the HTTP bench reads 72 of
72 and the browser half 12 of 12. The extension family is complete.

- **Files, not markup.** `Config.Client` (an `orbit.ClientCode`) names
  scripts and stylesheets inside a file system of the application's — an
  `embed.FS` in the bench — and the names of the field renderers those
  scripts register. The panel reads each declared file once, when it
  mounts, and serves those bytes at `<prefix>/client/<path>`, behind its
  session, as JavaScript or CSS by what the declaration said. Its document
  names them at the end of `<head>`, after the bundle — a script deferred,
  so it runs once the bundle has — each with the SHA-384 digest of the
  bytes read (`integrity`) and a version in the URL. `script-src` stays
  `'self'`: the files are `'self'`. A file the application holds and did
  not declare is a 404. With nothing declared there is no route, and the
  document is the one the panel served before.
- **At startup**, a declaration the panel cannot serve stops the
  application, naming the entry: a file that cannot be read, a path that is
  absolute, climbs out with `..`, has an empty or `.` segment, a backslash,
  or a character outside letters, digits, `.`, `-` and `_`, a script that
  does not end in `.js` or a stylesheet in `.css`, a path declared twice,
  files with no file system to read them from, a renderer name that is not
  lowercase letters, digits and dashes, a renderer named like one of the
  panel's own widgets, and a renderer with no script declared to register
  it. A `field_widgets` value that names neither a widget the panel draws
  nor a renderer the application declared stops it too, and the message
  lists the renderers that were declared.
- **`window.orbit`, version 1**, is the whole client contract:
  `version` and `registerFieldRenderer(name, render)`. The bundle installs
  it, read-only, before anything else runs. A renderer is called with the
  value and a context — the model, the field, its column, a frozen copy of
  the record and where it draws (`list` or `record`) — and returns a DOM
  node or a string, which is drawn as text.
- **A field drawn the application's way.** The schema names the renderer
  of a field (`renderer`) next to the panel's own widget (`html_type`), and
  the SPA draws the value with it in the Data Studio grid and on the record
  view: above the input that edits it, and in place of the value when the
  record is shown read-only.

Four decisions worth keeping:

- **The digest is of what was read, and it was read once.** A file that
  changed on disk after startup would otherwise be served under a digest the
  browser refuses — or, with no digest, run as whatever arrived at that URL.
  Reading at mount makes what the panel declared and what the browser runs
  the same bytes, and a file that cannot be read stops the application
  before anything is served.
- **Behind the session, and not on the login screen.** The document that
  names the files is served to signed-in operators, and so are the files.
  Nothing on the login screen draws a record, so the application's code does
  not run on the screen where the password is typed.
- **A renderer draws; it does not edit.** The schema keeps the panel's
  widget next to the renderer: the form edits with it, and it is what the
  panel draws when the renderer fails. A renderer that replaced the input
  would need the form's contract — validation, the payload, the error on the
  field — handed to the application's code, a larger surface than version 1
  needs to carry. A string a renderer returns is a text node: parsing it as
  markup would make every value a renderer echoes an injection.
- **A failure costs the value, not the screen.** The renderer runs inside a
  try, in a layout effect, into an element React does not manage. A
  renderer that throws, or returns anything but a node or a string, leaves
  the panel's own drawing in that cell or field and a line that says the
  renderer failed and why; the console has the rest. A renderer the schema
  names and no script registered draws the panel's way, with a warning in
  the console; one registered after the panel drew is applied when it is
  registered.

### What the work found that was not on the plan

- **The grid's icons never loaded under the panel's own policy.** AG Grid's
  quartz stylesheet carries its icon font as a `data:` URL, and the panel
  sends `font-src 'self'`. The browser refuses the font, `document.fonts`
  reports `agGridQuartz` in error, and the icons the grid draws in it — the
  filter mark among them — are drawn in nothing. `UIX-12` found it: it
  records every policy violation from before the document exists, and
  nothing else in the bench listens for them. The violation is the panel's
  and not the application's, so `UIX-12` counts only violations of the
  script and style policy and those naming the application's files. It was
  not fixed here: allowing `data:` in `font-src`, or serving the font as a
  file, changes the policy of every panel, and wants a control of its own —
  the panel's own screens raise no violation. **Fixed in A12 O1** without
  touching the policy — the grid no longer has a font — and `UIX-14` is the
  control ([below](#the-panel-travels-compressed-after-a12s-session-o1)).
- **Two layers refuse a path that climbs out.** The `io/fs` implementations
  refuse a name with `..` themselves: with the panel's own check removed and
  the path declared from the bench's `embed.FS`, the application still did
  not start and `EXT-06` stayed present. The probe declares
  `../client/note-status.js` from a file system that follows `..` instead,
  as a hand-written adapter can; against that one only the panel's check
  stands, and removing it reads partial. (Within the panel's check, the
  segment rule and `fs.ValidPath` each refuse it, so removing either one
  alone does not move the verdict either.)

### What the bench got wrong about itself, the twelfth time

- **`EXT-09` read every classic script as a theme.** Its "nothing
  configured, nothing changed" check listed any classic script in `<head>`
  as something that could set the first frame, so the bench application's
  own deferred script turned the theme control partial. A deferred script
  runs when the browser may already have painted — the rule `EXT-09` already
  held the theme script to — so the check now counts only the scripts the
  parser stops for.

Every change was verified by breaking it:

| mutation | control |
|---|---|
| the document names no file of the application's | EXT-06 → absent |
| the files named at the start of `<head>`, before the bundle | EXT-06 → partial; UIX-12 → absent |
| the script not deferred | EXT-06 → partial |
| a digest that is not of the bytes read | EXT-06 → partial; UIX-12 → absent |
| `script-src` loosened with `'unsafe-inline'` | EXT-06 → partial |
| a file the application did not declare served | EXT-06 → partial |
| the files served without a session | EXT-06 → partial |
| the script served as `text/plain` | EXT-06 → partial |
| a file that cannot be read accepted | EXT-06 → partial |
| a path with `..` accepted (both layers removed) | EXT-06 → partial |
| the schema without the field's renderer | EXT-07 → absent |
| the panel's widget replaced by the renderer's name | EXT-07 → partial |
| a renderer the application did not declare accepted | EXT-07 → partial |
| a renderer named like one of the panel's widgets accepted | EXT-07 → partial |
| a renderer with no script to register it accepted | EXT-07 → partial |
| the grid drawing no renderer (dist rebuilt) | UIX-12 → absent |
| the record view drawing no renderer (dist rebuilt) | UIX-12 → absent |
| a renderer's throw not caught (dist rebuilt): the screen stops answering | UIX-12 → absent |
| no `window.orbit` (dist rebuilt) | UIX-12 → absent |
| the stylesheet served as other bytes than its digest | UIX-12 → absent |

## The panel travels compressed, after A12's session O1

The arc A12 ("performance, re-audit and closing at 5") gave this repository
one session, O1, for the panel's weight and three defects its own checks had
found: OR-61 (nothing measured what travels, and the admin server sent the
fleet's interface uncompressed), OR-63 (the grid's icon font refused by the
panel's own policy) and OR-62 (errors written in the fill colour). The HTTP
bench is unchanged at 72 of 72; the browser half went from 12 of 12 to 15
of 15, and the fleet bench's browser half from 5 of 6 to 8 of 8
([fleet-bench.md](fleet-bench.md)).

- **Compressed once, by the build.** The ui module's build writes, beside
  every text file of at least 1 KiB, its gzip (pako, level 9) and Brotli
  (brotli-wasm, quality 11) encodings, and both are embedded. The panel and
  the admin server answer `Accept-Encoding` with the one the browser
  accepts — Brotli first on a tie —, with `Content-Encoding`, the encoded
  length, and `Vary: Accept-Encoding` on every answer for a file that has an
  encoding, the plain one too. Nothing is compressed per request. Both
  compressors are pinned code, so the dist rebuilt on a Linux runner with
  Node 22 is byte for byte the one built on a macOS laptop with Node 25 —
  checked once in a container, and by CI's freshness lane on every change.
- **The admin server carries the fleet only.** Each entry is its own
  embedded variable, and the linker keeps an embedded variable only when
  reachable code reads it: the admin server calls `ui.Fleet()`, and its
  binary went from 25.6 MB to 23.8 MB, encodings of the fleet included. The
  ui module's API did not change, so the root and the server build against
  the tagged `ui v1.0.0` as before and pick this up when the release train
  re-pins them.
- **A budget over what travels.** `ui/embed_test.go` walks each entry the
  way a browser loads it — the document's files and their imports, then
  every screen a navigation can load, with what Vite's preload map fetches
  beside it — and reads each file's size as it travels. One constant,
  `compressedBudget`, 400 KiB of gzip, holds the initial load plus the
  heaviest navigation. That is the meaning decision 2 of A12 proposes and
  its owner has not yet confirmed; `budgeted()` is the one function that
  changes if another one is chosen. A file of the dist the walk does not
  reach fails the test, so a missed import cannot lower the number.
- **Data Studio lost a sixth of its compressed weight.** The deep link to Data Studio
  measured 424.6 KiB of gzip with the initial load — over the budget, so the
  budget would have failed the day it was written. AG Grid moved from 32 to
  36, whose features are modules the bundle leaves out unless registered:
  the panel registers five (the client-side row model and its API, row
  selection, column sizing, column state). Its styles come from the
  Theming API inside the chunk, so Data Studio no longer loads a stylesheet,
  and the grid travels in a chunk of its own that changes only when AG Grid
  does. Recharts was measured too: version 3 is no smaller than the 2.15
  the panel pins (about 100 KiB of gzip either way), it is already a chunk of
  its own that only the pulse and the dashboards' series load, and those
  navigations are a quarter of the budget; it stays.
- **No font, so nothing to refuse (OR-63).** AG Grid's Theming API draws
  the quartz icons as SVG images masked in CSS, which `img-src 'self' data:`
  already allowed; `font-src` stays `'self'` and the dist carries no font as
  a `data:` URL, which `TestEmbeddedDist_NoFontTravelsAsData` keeps true.
- **Errors in the text colour (OR-62).** The nine places that wrote a
  message in `text-destructive` — the record form's field errors, its alert
  and its required mark, the import dialog's row errors and alert, the field
  configuration panel, a file field, the health page and the login alert —
  and four hover states write `text-destructive-text`, and a unit test keeps
  the fill class out of the panel's sources.

| KiB | raw before | gzip before | Brotli before | raw after | gzip after | Brotli after |
|---|---:|---:|---:|---:|---:|---:|
| panel: initial load | 354.6 | 109.6 | 95.7 | 354.6 | 109.6 | 95.7 |
| panel: + Data Studio (heaviest) | 1246.0 | 314.9 | 255.0 | 921.4 | 263.8 | 218.7 |
| panel: initial + heaviest (budgeted) | 1600.6 | **424.6** | 350.7 | 1276.1 | **373.4** | 314.5 |
| panel: total, every lazy screen | 2028.3 | 539.8 | 446.9 | 1703.8 | 488.6 | 410.6 |
| fleet: initial = total | 465.6 | 132.9 | 114.1 | 465.6 | 132.9 | 114.1 |

Both columns are measured the same way — the parent commit's dist encoded
with the same two compressors and walked by the same test. What travelled
before is another matter: the admin server sent the fleet's 465.6 KiB as
they were, and the panel travelled gzip-encoded only when the application's
router compressed responses (Nucleus's default middleware does, at level 5,
per request; any other router sent it as it was). The panel's other
navigations: the dashboards and the pulse about 100 KiB each (Recharts), the
operators, sessions and RBAC screens about 21 KiB, the rest under 3 KiB.

Three browser controls, each the browser half of one defect:

- **`UIX-13`** opens the record form in the light theme and in the dark
  one, with a document field that is not JSON so the form's own errors show
  — the field's, its description and the alert — and runs the contrast rule
  over the dialog. `UIX-09` read the form an action declares; nothing read
  this one.
- **`UIX-14`** records every policy violation from before the document
  exists, opens Data Studio, sorts a column so its icon draws, and checks
  that every visible icon of the grid is painted by an image or by a font
  the document loaded, that no font face is in error, and that the policy
  refused nothing at all.
- **`UIX-15`** reads every script and stylesheet the browser fetched for
  the overview and Data Studio: a file of 1 KiB or more must come encoded,
  in Brotli when the browser accepts it, with its length, and saying the
  answer varies. The last two are what tell compressed-by-the-build from
  compressed-on-the-way-out: the bench's own application gzipped the panel
  per request before this session, and the control fails on that.

### What the work found that was not on the plan

- **The panel was already compressed, sometimes.** OR-61 said the server
  compressed nothing, and of the admin server that was true. The panel is
  mounted on the application's router, and Nucleus's default middleware
  gzips every response at level 5 — so in the bench's application the
  panel's files did travel gzip-encoded, per request and never in Brotli,
  and on any other router they did not. `UIX-15`'s first version only asked
  for an encoding and passed against the parent commit; it now asks for the
  build's.
- **The fleet's palette was short in both themes, not in one token.** The
  register expected the light theme's overview; reading every screen in
  both themes found 32 nodes under 4.5:1 in the light theme and 94 in the
  dark one, across ten screens: the muted steps, the status colours on
  their tints and the accent. The fleet bench
  page has the palette rule that replaced them.
- **`UIX-12` read AG Grid's private classes.** It found the grid's rows by
  `.ag-center-cols-container`, a container AG Grid 36 no longer has. It
  finds them by the grid's role and the row ids the panel gives them now,
  which holds on both versions — run against the parent commit, it passes.

Every change was verified by breaking it:

| mutation | what fails |
|---|---|
| the budget at 350 KiB (under today's measure) | `TestEmbeddedDist_EachEntryWithinCompressedBudget` |
| the parent commit's dist (AG Grid 32), encoded the same way | the budget (424.6 KiB) and `TestEmbeddedDist_NoFontTravelsAsData` |
| the walk blind to dynamic imports | the budget: every lazy chunk is "a file no load reaches" |
| one `.gz` with other bytes | `TestEmbeddedDist_PrecompressedSiblingsMatch` |
| one `.br` deleted | the budget and the siblings test |
| `Fleet()` reading the panel's variable | `TestEmbeddedDist_EachEntryLinksAlone`; `TestAdminServerBinary_CarriesTheFleetEntryOnly` |
| the budget computed and not compared | the fleet bench's `UI-10` → partial |
| no negotiation (always the file as it is) | `TestNegotiateEncoding`, `TestServePrecompressed`, `TestPanel_ServesTheDistCompressed` |
| no `Vary` | `TestServePrecompressed`, `TestPanel_ServesTheDistCompressed` |
| `q=0` ignored | `TestNegotiateEncoding` |
| the server's copy of the serving code edited | `TestPrecompressedServing_TwoCopiesStayOne` |
| the record form's field error in `text-destructive` (dist rebuilt) | `UIX-13` → absent (3.76:1 in the light theme); the sources test |
| one muted step of the fleet's light palette back to its old value (dist rebuilt) | `UIF-02` → absent; `ui/tools/fleet-palette.test.ts` |
| the parent commit, with this session's specs | `UIX-13`, `UIX-14` (the sort icon in a font never loaded), `UIX-15` (gzip, streamed); `UIF-02`, `UIF-06`, `UIF-07` |

## The screens of an operator who is not a superuser (OR-64)

Until this change every browser control but `UIX-11` signed in as the
bootstrap admin, and every permission question is answered yes for a
superuser before any policy is read. So the grid drawn for an operator
without `delete` (A11 O3: a selection only when something can be done with
one) and the record's menu for one without `update` (A11 O4: the way to a
record action for an operator who may not edit the record) had been seen
only by whoever wrote them. The browser half goes from 15 of 15 to 17 of 17;
the HTTP bench is unchanged at 72 of 72.

The driver creates two operators and grants them through the panel's own
management API. Neither is a superuser, and neither holds `update` or
`bulk_delete` on `Note`:

| operator | holds on `Note` (and `list_models` on `admin:*`) |
|---|---|
| the viewer | `get_schema`, `list`, `retrieve` |
| the actor | the same, and `delete`, `schedule` (an action over a selection and on a record) and `duplicate` (an action on a record) |

Each control reads what each operator is shown and what they are not, runs
the accessibility rules the panel's own screens are held to over the screen
that operator is shown, and then makes from the page the calls the screen
left out — the operator's own session, as a request typed into the console
arrives — and reads the server's `403`:

- **`UIX-16`** — the viewer's grid has no selection, its row no Delete and
  its toolbar no batch Delete; the actor's grid keeps a selection for
  Schedule, its row offers the one-record Delete the actor holds, and a
  selection offers Schedule and no Delete. The forced delete and batch
  delete are refused, and the note is still there.
- **`UIX-17`** — neither row offers Edit; both offer View, and the record
  view it opens says it is read-only, shows the body (which is not a column
  of the grid), and has no input and no save. The actor's view and row menu
  hold exactly Duplicate and Schedule — not Download as text, which the actor
  was never granted — and the viewer is offered no action anywhere. The
  forced write and the forced record action are refused, and the note still
  says what it said.

Every "not shown" is read beside something the same operator is shown in
the same place — the row's History button, the actor's selection, the
actor's Delete — so a locator that matched nothing anywhere cannot pass for
an absence.

### What the work found that was not on the plan

Three defects of the panel, each caught by the first run of these two
controls and fixed in the same change:

- **FIXED — a batch Delete the server refuses.** The grid offered a
  selection, and the selection a Delete, to any operator who held `delete`.
  The server asks a batch delete for `bulk_delete`, a verb of its own, so an
  operator who may delete one record at a time was offered a button that
  answered `403` — and one who held `bulk_delete` alone was offered no
  selection at all. The grid reads `bulk_delete` from the schema's
  `permissions` now (falling back to `can_delete` when a backend sends no
  map), and `UIX-16` reads the actor's selection with no Delete on it.
- **FIXED — a record nobody without `update` could open.** The read-only
  record view existed (A11 O4), and only a link or an action's redirect
  reached it: the row offered Edit to an operator who may update and nothing
  to one who may only read, so the fields the list leaves out were out of
  reach for every operator granted `retrieve` and not `update` — and for
  every operator of a read-only model. The row offers View to them now, and
  it opens the same read-only view.
- **FIXED — Data Studio with a model open was not legible.** `UIX-02` reads
  the contrast of Data Studio before a model is chosen; nothing read it
  after. With one open, three texts measured under 4.5:1 on the bench's
  palette: the open model's count (3.7:1, white on a fifth of white over the
  accent), its table name (3.26:1, the foreground at 70%) and the database
  engine beside the toolbar (2.29:1, the muted colour at 60% opacity). They
  are written in the full foreground and muted colours now; the estimate
  marks beside a count and the group subtitles of the sidebar, written the
  same way, were changed with them.

Verified by breaking it:

| mutation | what fails |
|---|---|
| the parent commit's interface, with these specs | `UIX-16` (the viewer's screen: three contrast nodes; the actor's selection offers Delete), `UIX-17` (no way to open the record) |
| the server's update handler asking for `retrieve` instead of `update` | `UIX-17`: the server wrote a record for an operator who may not update |

## The rest of Data Studio's doors (OR-65)

`UIX-16` and `UIX-17` read the deletes and the edit. Every other door Data
Studio offers asks the server for a verb of its own, and the screen drew each
of them for anybody: an operator without `export_data` was offered Export, one
without `retrieve` a record's History. OR-65 compared every door of Data
Studio and the record view with the verb its handler asks:

| door | the verb its handler asks | offered before by |
|---|---|---|
| a model in the sidebar | `get_schema`, then `list` | nothing: every model was listed |
| **Fields** (the field settings) | `update_schema`; `501` with no schema registry | nothing |
| **New Record** | `create` | `create` |
| **Export** and its download | `export_data` on `admin:*` | nothing |
| **Import** (upload, validate, execute) | `import_data` on `admin:*` | `create`, which the import never asks |
| search, filters, sort, page size, more rows, database | `list` | the grid itself (`list`) |
| a saved view: save | `list` of its model; `501` with no database | nothing (the save was offered where views answer `501`) |
| a saved view: removal | its owner, or a superuser | nothing |
| a row's **History** | `retrieve` | nothing |
| a row's **Edit**, **View**, **Delete** | `update`; `retrieve`; `delete` | the same (OR-64) |
| a selection and its **Delete** | `bulk_delete` | the same (OR-64) |
| an application action, on a selection or a record | its own verb and its placement; a destructive one is refused on a read-only model | the verb and the placement, not the read-only refusal |
| the record view's save | `update` (`create` for a new record) | the same |
| a child's **Add**, **Remove** and edit in the form | the child model's `create`, `delete`, `update`, checked before the parent is written | nothing; and every loaded child was sent as an update |
| a foreign key's choices, a file upload | `list` of the target (the field falls back to the id when refused); `create` or `update` | the same: they degrade, or sit in a form only a writer is shown |

The schema's `permissions` map now carries the four that were not in it —
`get_schema` and `update_schema` asked of the model, `export_data` and
`import_data` asked of `admin:*`, each through the same function the handler
calls — and each saved view says whether this operator may change it
(`can_edit`). Data Studio offers each door by its verb, offers a view's save
only where the panel answers its views at all, and does not draw an empty
Actions column for an operator who holds none of a row's doors.

The driver adds a third operator and one grant. None of the three holds
`update`, `bulk_delete`, `update_schema` or `import_data`:

| operator | holds on `Note` (and `list_models` on `admin:*`) |
|---|---|
| the viewer | `get_schema`, `list`, `retrieve` |
| the actor | the same, `delete`, `schedule`, `duplicate`, and `export_data` on `admin:*` |
| the lister | `get_schema`, `list` |

**`UIX-18`** reads, for each operator, what is offered beside what is not,
runs the panel's accessibility rules over each screen, and makes from the page
the calls the screen left out:

- **the viewer** is offered Notes and none of the models the payload says
  they may not open; the model's heading and no Fields; the toolbar's
  Filters and no Export or Import; the admin's shared view and not its
  removal, while the view they save beside it is theirs to remove. The forced
  export, import, field settings, schema of another model and removal of the
  shared view are each refused with `403`.
- **the actor** is offered Export — the toggle and the export it opens — and
  no Import. The export the screen offers answers `200`, the import it does
  not answers `403`.
- **the lister** sees the note's row and its column headers, and no History,
  no View and no Actions column. The forced history is refused.

### What the work found that was not on the plan

- **FIXED — the export panel was not legible.** Nothing had opened it in a
  browser before this control: the format not chosen was written in the muted
  colour on the muted fill, 4.34:1. It is written in the full foreground now.
- **FIXED — an import into a read-only model was not refused.** A create, an
  update, a delete and a batch all refused a read-only model; an import, which
  writes rows the same way, did not. Validate and execute refuse it now, and
  Data Studio offers no Import on such a model.
- **FIXED — a destructive action was offered on a read-only model.** The
  server refuses it there whoever asks; the schema no longer offers it.
- **FIXED — the children of a record were all sent as updates.** The form
  sent every child it had loaded once any changed, and the server asks each
  child it is sent for its verb, so an operator who may add a line and not
  edit one was refused the save. A child nobody changed is not sent now (an
  absent child is left as it is), and the form offers Add, Remove and the
  saved lines' inputs by the child's own `create`, `delete` and `update`.
  This one is measured by the interface's unit tests, not by `UIX-18`.

Verified by breaking it:

| mutation | what fails |
|---|---|
| the parent commit's interface, with this spec | `UIX-18`: the sidebar offers the viewer Articles, whose schema or list the server refuses them |
| the history handler asking for `list` instead of `retrieve` | `UIX-18`: the server answered a record's history to an operator who may not open it |
| `export_data` and `import_data` asked of the model instead of `admin:*` | `TestCapabilityHints_AnswerTheScreensOtherDoors`: an import granted on the model only is called held, and refused |
| the saved-view list saying `can_edit` for every view | `TestSavedViews_ListSaysWhoMayChangeEach` |
| no read-only check on the import, or on the destructive action's offer | `TestImport_ReadOnlyModelIsRefused`, `TestDestructiveActionNotOfferedOnAReadOnlyModel` |

The browser half goes from 17 of 17 to 18 of 18; the HTTP bench is unchanged
at 72 of 72.

## What an export holds (OR-66)

`UIX-18` read that the actor is offered Export and that the export answers
`200`. It did not read what the export held, and the export held everything.
The panel's export (`POST /api/exports`, what Data Studio's Export button
asks for) and the fixture dump are granted by `export_data` on `admin:*`,
and walked every model they were asked for — or every model, when asked for
none — with no confinement but the tenant's. An operator granted
`export_data` and confined to their own rows (`admin:<Model>#own`), or kept
off a field (`admin:<Model>.<field> deny`), exported every row and every
field anyway, in CSV, JSON and SQL, of the model on the screen and of the
models they could not open at all. The model's own CSV export
(`export_csv`) had the row and field scope; the screen did not use it.

Every surface that reads rows on an operator's behalf now takes what it may
read from one function: the list, the model's CSV export, the panel's
export, the fixture dump and a dashboard's records card. Each model of an
export carries what that operator's `list` of it shows — their tenant's
rows, their own under an `#own` grant, the fields they may read — and a
model the request names that they may not list is refused with the list's
`403`. An export of every model leaves those models out.

The driver gives the actor two grants more; neither is a door `UIX-16` or
`UIX-17` reads, and the actor still may not open Articles, so the sidebar
those controls read is the one it was:

| operator | holds (beside `list_models` on `admin:*`) |
|---|---|
| the viewer | `get_schema`, `list`, `retrieve` on `Note` |
| the actor | the same, `delete`, `schedule`, `duplicate`, `export_data` on `admin:*`, a `deny` on `Note.meta`, and `list` on `admin:Article#own` |
| the lister | `get_schema`, `list` on `Note` |

**`UIX-18`** now seeds, as the admin, an article the actor owns, one the
viewer owns and a credential, and then cuts an export of every model as the
actor and downloads it through the panel: it holds Notes and Articles and
nothing of Credentials, the actor's article and not the viewer's, and no
note with a `meta` key. The model list says the actor holds neither `list`
nor `export_data` on Credential, and the export of Credential asked by name
is refused with `403`.

### What the work found that was not on the plan

- **FIXED — an export was handed to whoever held `export_data`.** The job
  list, a job's status and its download were scoped by tenant only, and the
  audit trail names each export's key: an operator confined to their own
  rows could list a superuser's export and download every row of it. An
  export is now handed to the operator who cut it and to a superuser; an
  operator who is not a superuser is not served a key the panel holds no job
  for (after a restart, or from another replica, they cut it again).
- **FIXED — two exports in the same second shared a key.** The key was the
  time to the second, twice, so the second export overwrote the first and the
  first's producer downloaded the second's rows. The key carries a random
  suffix now, which also makes it a key nobody guesses.
- **FIXED — a fixture load wrote into a read-only model (OR-68).** The
  import refused one since OR-65; the load, which writes the same way, did
  not. It refuses the whole fixture now, before any row of it is written.
- **FIXED — the export's own filters reached past the scope.** The body's
  `filters` went to the store as they came, beside the tenant's: one on the
  owner column under another spelling sat next to the scope's for the
  backend to choose between, and one on a hidden field answered, by the rows
  it left, what the field held. They are keyed by the column they resolve to
  now, so the scope replaces one on a confined column, and one on a field the
  operator may not read is refused with `400`.
- **FIXED — the schema offered the export of a model the operator may not
  list.** `export_data` in the permissions map is the handler's answer for
  that model now: `export_data` on `admin:*` and `list` of the model.

Verified by breaking it:

| mutation | what fails |
|---|---|
| the parent commit's server, with this spec | `UIX-18`: the actor's export of every model holds a model the actor may not list (a credential); `TestPanelExport_*` |
| the export reading every row (the read scope's filters dropped) | `UIX-18`: the actor's export holds an article another operator owns; `TestPanelExport_CarriesOnlyTheOperatorsRowsAndFields` |
| the JSON export not masking the record | `UIX-18`: the actor's export holds a field of Note the actor may not read (meta) |
| every operator seeing every export job | `TestPanelExport_JobsAreHandedToTheirProducer` |
| the export's filters passed to the store as they came | `TestPanelExport_FiltersCannotWidenTheScopeOrProbeAHiddenField` |
| no read-only check on the fixture load | `TestLoaddata_ReadOnlyModelIsRefused` |

The browser half stays at 18 of 18; the HTTP bench is unchanged at 72 of 72.

## What a query may name (OR-69)

`UIX-18` read what the actor's export holds. It did not ask what the actor's
list answers about the field kept from them, and the list answered
everything. A field a policy keeps from an operator (`admin:<Model>.<field>
deny`, or left out of a `read` allow-list) was masked out of every row, and
then filtered by, sorted by and searched in on that operator's behalf: with a
`deny` on `owner`, `?owner=operator` answered one row and `?owner=nobody`
none, which is the value, one guess at a time, and `order_by=owner` paged
the rows in the hidden field's order. The export refused such a filter since
OR-66; nothing else did.

Every surface that reads rows for an operator now asks the predicate the
columns of an export are chosen by (`fieldRules.readsField`, beside
`requestReadScope`) before it answers a question about a field:

| surface | what it did | what it does |
|---|---|---|
| the list's filters, with or without an operator | filtered by the hidden field | `400 invalid filter field`, the answer a field the model does not have gets |
| the list's `order_by` | sorted by it | `400 invalid order_by`, the same way |
| the list's `?search=` | searched in it | `400` when the search would reach a field the operator may not read, and the schema's `searchable` is false, so the grid disables the box |
| a relation lookup (`/options`) | labelled the options with it, and searched in it | labels them with a field the operator reads; `?q=` is the list's search |
| a field's lookup (`/fields/{field}/options`) | answered for the hidden field | `404`, as a field the model does not have |
| the panel's export, its `filters` | refused since OR-66 | the same refusal, from the same predicate |
| a shared saved view that filters or sorts by it | listed, naming the field and the value somebody looks for | not listed to the operator |
| a dashboard's records card ordered by it | drawn: the rows ranked by the hidden field | not shown, as a card of a model the operator may not list is not |

There is no facet, aggregate or per-field count in the panel to check; the
model list's counts are of the whole table and name no field.

A search cannot be narrowed to the readable fields from the panel: the
backend searches every field it marks searchable, and `datasource.Query`
has no way to say which. So it is refused instead, and the reason is in the
grid's disabled search box. Narrowing it is an addition to the frozen
contract, and another change.

The driver gives the actor one grant more, a `deny` on `Note.views`: a
column the grid sorts by and a filter it offers. Not `title`, which the
controls find a note by searching.

| operator | holds (beside `list_models` on `admin:*`) |
|---|---|
| the viewer | `get_schema`, `list`, `retrieve` on `Note` |
| the actor | the same, `delete`, `schedule`, `duplicate`, `export_data` on `admin:*`, a `deny` on `Note.meta` and on `Note.views`, and `list` on `admin:Article#own` |
| the lister | `get_schema`, `list` on `Note` |

**`UIX-18`** now also shares, as the admin, a view of Notes filtered by
`views`. The viewer, who reads `views`, is shown it and a Views column; the
actor is shown neither, nor a Views filter, and asked anyway the list refuses
the actor `?views=0`, `?views__gte=0` and `order_by=views desc` with `400`,
answers a sort by `title`, and lists the actor no view filtered by `views`.
The actor's export holds no note with a `views` key.

**`PERM-06`** (HTTP) now also asks the field it denies — Note's `title`, a
filter, a sort and its only searchable field — back as `?title=`,
`?title__startswith=`, `order_by=title` and `?search=`, each of which must
answer `400`, and reads the relation lookup of Note for a title.

### What the work found that was not on the plan

- **FIXED — a search reached a field the panel excludes, for every
  operator.** Nucleus searches every field marked searchable, excluded or
  not, and the panel only checked that SOME searchable field was shown: a
  model with a shown searchable field and an excluded one answered a search
  by the excluded field's value, to a superuser too. It counts as a field no
  one reads now, and the search is refused. A configuration that marks an
  excluded field searchable loses its search until one of the two flags
  goes.
- **FIXED — the refusal of a filter on a hidden field said the field
  existed.** A field kept from the operator that the model does not offer as
  a filter answered `filter is not enabled for "secret"`, and a field the
  model does not have answered `invalid filter field`. Both answer the
  second now.

Verified by breaking it:

| mutation | what fails |
|---|---|
| the parent commit's server, with this spec | `UIX-18`: the actor is shown a view filtered by views; `PERM-06`: `?title=` answered `200` |
| the list's filters and sort read without the scope's fields | `UIX-18`: `?views=0` answered to the actor; `PERM-06`; `TestListQuery_*` |
| no refusal of a search that reaches a hidden field | `PERM-06`: `?search=` answered `200`; `TestListSearch_*` |
| a relation lookup labelled from every field | `PERM-06`: the lookup labels Note with its title; `TestModelOptions_StayInTheFieldsTheOperatorReads` |
| a relation lookup's `?q=` reaching a hidden field | `TestModelOptions_StayInTheFieldsTheOperatorReads` |
| a field's lookup answered for a hidden field | `TestFieldOptions_AFieldTheOperatorMayNotReadIsNotFound` |
| saved views listed whatever they name | `UIX-18`: the actor is shown a view filtered by views; `TestSavedViews_AViewNamingAHiddenFieldIsNotListed` |
| a records card's order not checked | `TestRecordsCard_InAnOrderTheOperatorMayNotReadIsNotShown` |

The browser half stays at 18 of 18; the HTTP bench is unchanged at 72 of 72.

## What an import writes (OR-67)

`UIX-18` read that an operator without `import_data` is neither offered an
import nor served one. It did not ask what an import writes for one who holds
it, and the import wrote anything. The import (upload, validate, execute) and
the fixture load are granted by `import_data` on `admin:*`, and wrote any
model, by create or by update, with no grant of the model's, outside the
operator's own rows, and through a field kept from them: whoever could import
could write what they could not write by hand. It is the write twin of OR-66,
and the owner's decision of 2026-10-06 closed it in a minor rather than the
2.0.

Each row of a file now asks what the record form asks, through the function
the form's create and update ask it of (`requestWriteScope`, beside
`requestReadScope`):

| row | what it needs |
|---|---|
| a new row | the model's `create`; the tenant and, under `#own`, the operator stamped; no field the operator may not write |
| an existing row (by primary key, or a unique index within the tenant, when `on_conflict` is `skip` or `update`; a fixture's `pk`) updated | the model's `update`, a row of the tenant and, under `#own`, the operator's own; no field the operator may not write |
| an existing row skipped | nothing: it is not written |
| an existing row of another tenant | not found, skipped or updated alike |

The file is planned whole before any row of it is written, and one row the
operator may not write refuses it with a `403` that names the row, the model
and what it lacks. The validate step answers the same `403`; the import
dialog shows it as its alert, with nothing written. A superuser is unchanged
but for the tenant, which the form holds them to as well.

The driver gives the actor two grants more and the lister one:

| operator | holds (beside `list_models` on `admin:*`) |
|---|---|
| the viewer | `get_schema`, `list`, `retrieve` on `Note` |
| the actor | the same, `delete`, `schedule`, `duplicate`, `create` on `Note`, `export_data` and `import_data` on `admin:*`, a `deny` on `Note.meta` and on `Note.views`, and `list` on `admin:Article#own` |
| the lister | `get_schema`, `list` on `Note`, and `import_data` on `admin:*` |

**`UIX-18`** now reads, for the actor, that the toolbar offers Export /
Import and its panel an Import; that a file of two notes, the second with a
`meta` key, is refused in the dialog with an alert naming row 2, `meta` and
that nothing was written, and no note of it exists; that the same notes
without `meta` validate and land; and that a file updating the admin's note
by its key is refused with `403` by validate and by execute, naming the
`update` the actor does not hold, and the note keeps its title. For the
lister it reads that the schema does not offer the import of `Note`, the
toolbar draws none, and a file of notes to create is refused for the
`create` the lister lacks.

### What the work found that was not on the plan

- **FIXED — the schema offered an import every row of which is refused.**
  `import_data` in the permissions map is false now on a model the operator
  may neither create nor update rows of, and Data Studio offers its import —
  which creates every row it reads — only beside `create`.
- **FIXED — the import dialog counted rows from 0.** A row error read
  `row 0` for the first record of the file; the dialog counts from 1 now, as
  the refusal does, so both name the same row the same way.
- **CHANGED — a row of another tenant refuses the file.** It used to fail
  alone and let the rest of the file through; it refuses the file whole now,
  for every operator.

Verified by breaking it:

| mutation | what fails |
|---|---|
| the parent commit's server and interface, with this spec | `UIX-18`: the dialog does not say why the actor's file with a field kept from them was refused (it validated, and would have imported) |
| the import's payload not held to the write scope | `UIX-18`: the dialog does not say why the actor's file was refused; `TestImport_ADeniedFieldIsRefusedNotDropped`, `TestImport_OwnRowsOnlyForAnOwnGrant`, `TestLoaddata_*` |
| the row's verb not asked | `TestImport_WithoutCreateIsRefusedAndWritesNothing`, `TestImport_UpdatingAnExistingRowNeedsUpdate`, `TestLoaddata_*` |
| an update not confined to the rows the operator reaches | `TestImport_OwnRowsOnlyForAnOwnGrant`, `TestLoaddata_*` |
| rows written as they are planned | `TestImport_AMixedFileWritesNothing`, `TestImport_ARowForAnotherTenantIsRefused`, `TestImport_OwnRowsOnlyForAnOwnGrant` |
| the validate step not planning | `UIX-18`: the dialog does not say why the actor's file was refused; every `TestImport_*` that refuses (validate and execute disagree) |
| the write scope without the field policies | the form's `TestFieldPerms_*` and the import's `TestImport_*` alike: one function |
| `import_data` not narrowed to a write of the model | `UIX-18`: the lister's schema offers an import of Note; `TestCapabilityHints_ImportDataNeedsAWriteOfTheModel` |

The browser half stays at 18 of 18; the HTTP bench is unchanged at 72 of 72.
