# Admin bench — what an operator can and cannot do with the panel today

This is the numerator of the A6 gate ("Orbit as an admin product"). It exists
because that gate needs a number, and a number needs something that produces
it.

**Measured on 2026-09-12 against the panel at v1.9.6, and kept current as the
arc closes its gaps: the numbers below are what the suite produced on its last
run.** Run it with:

```bash
go test ./internal/adminbench/ -run TestAdminBench -v
go test ./internal/adminbench/ -run TestAdminBenchSummary -v   # the table below
```

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

**54 of 59 controls present. 0 partial. 5 absent.**

| family | present | partial | absent |
|---|---|---|---|
| data studio | 16 | 0 | 1 |
| permissions | 9 | 0 | 0 |
| audit | 7 | 0 | 0 |
| operations | 17 | 0 | 0 |
| customization | 3 | 0 | 4 |
| interface | 2 | 0 | 0 |
| **total** | **54** | **0** | **5** |

Operations is complete as of the arc's ninth session. What remains absent is
four controls of customization (branding, dashboards, actions and extension
points) and one of data studio.

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

- **The browser.** Contrast, focus order, keyboard reach, whether a toast can
  be dismissed: real, unmeasured here, and they need an instrument that runs
  in a browser. Asserting them from Go would be a claim, not a measurement.
- **The fleet plane.** A separate product surface with its own agent, server
  and protocol. It is measured where it lives.

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

### A note the first measurement got wrong

`OPS-11` was recorded with the note "an application whose cache is
in-process, which is the default". There is no default: `pkg/cache` is a
library an application builds with, and nothing in the framework wires a
cache into an application at all — only the CLI's `createcachetable` uses the
package. That is why the contract asks the application to declare its cache
instead of the panel discovering one. The finding was right about the
symptom and wrong about the cause, which is the same trap A4 and A5 recorded
from the other side: a comment is not a measurement, and neither is a note.
