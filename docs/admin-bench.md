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

**48 of 59 controls present. 3 partial. 8 absent.**

| family | present | partial | absent |
|---|---|---|---|
| data studio | 16 | 0 | 1 |
| permissions | 9 | 0 | 0 |
| audit | 7 | 0 | 0 |
| operations | 11 | 3 | 3 |
| customization | 3 | 0 | 4 |
| interface | 2 | 0 | 0 |
| **total** | **48** | **3** | **8** |

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
and a probe that boots the real application is what found them. Two are fixed
and their controls have moved; two are still open.

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
3. **A session row never says whose it is.** The row carries a `user` field
   and the panel leaves it empty, so revoking a session from the viewer is
   done blind.
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
