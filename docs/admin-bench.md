# Admin bench — what an operator can and cannot do with the panel today

This is the numerator of the A6 gate ("Orbit as an admin product"). It exists
because that gate needs a number, and a number needs something that produces
it.

**Measured on 2026-09-12 against the panel at v1.9.6.** Run it with:

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

**32 of 59 controls present. 9 partial. 18 absent.**

| family | present | partial | absent |
|---|---|---|---|
| data studio | 9 | 4 | 4 |
| permissions | 4 | 1 | 4 |
| audit | 4 | 1 | 2 |
| operations | 10 | 3 | 4 |
| customization | 3 | 0 | 4 |
| interface | 2 | 0 | 0 |
| **total** | **32** | **9** | **18** |

## What the shape of it says

The panel **browses and operates well, and administers poorly**.

- Everything an operator does to *data* works and is exercised here: CRUD with
  model validation, search, server-side ordering, bulk actions, import,
  export, fixtures, multi-tenant confinement, and a panel mounted over a
  backend that is not the framework's.
- Everything an operator does to *the application* works too, and this is the
  part no comparable product has: a live request and SQL feed, a runtime
  pulse, feature flags, migrations, storage, async exports.
- Everything an operator does to *other operators* is missing. There is no
  route that creates an admin user, resets one's password, or disables one:
  the only way in is the `nucleus_admin_users` table and the `createuser`
  CLI. Roles and policies can be managed from the panel; the people they
  apply to cannot.
- The permission model stops at the model boundary. `(subject, model, action)`
  has nowhere to put a field or a row, so "an editor may change the title but
  not the price" and "an author may edit their own posts" cannot be
  expressed — and the payloads a screen loads carry no capability hints, so a
  UI can only discover a refusal by being refused.
- The audit trail is a process-lifetime buffer. It covers every mutating
  surface and records both sides of an edit, and a second process on the same
  database sees none of it.

## Four defects the bench found, none of them a missing capability

These are not gaps in the product's plan; they are things that do not work,
and a probe that boots the real application is what found them.

1. **The live websocket panics in any mounted panel.** `/admin/api/live/ws`
   answers 500: the framework's session middleware wraps the response writer
   in `auth.flashSweepWriter`, which implements `Flush` and `Unwrap` but not
   `Hijack`, so the upgrade cannot take the connection. The panel's own tests
   wire it without that middleware, which is why they pass. The feed's
   snapshot works; its stream never connects. The fix belongs upstream, in the
   framework's wrapper.
2. **The pager has no total.** A list answers `total: -1` with
   `is_estimated: true` — filtered or not, with five rows in a SQLite table —
   so no UI can say how many pages there are.
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
- **A foreign key has to be declared.** The bench's first model had a
  `NoteID` field and no relation declaration, so nothing marked it as a key —
  and the probe read that as "the panel has no relation metadata". It was
  measuring the model the bench wrote.
