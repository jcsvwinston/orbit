---
title: Features
sidebar_position: 3
description: What the Orbit admin panel includes.
---

# Features

Orbit is a single panel made of focused modules. Each one reads live state from
the host application's `Runtime`. There is no separate data store to feed or
keep in sync.

## Data Studio

Browse, create, edit, and delete records for every model in the application's
registry. It is **tenant-aware** (when multitenancy is enabled) and supports
import/export.

**Tenant scope.** With `multitenant_enabled` on, every Data Studio operation is
confined to the tenant the host application resolves for the request (in
Nucleus, from the subdomain or the configured header), or to
`multitenant_default` when it resolves none. Confined means: the list and the
CSV export only show that tenant's rows; a record of another tenant is *not
found* by id (get, update, delete, bulk delete); a create or an update cannot
name another tenant under any key the backend resolves to the tenant field —
its storage column, its Go field name or, for a Nucleus model, the JSON key
its records carry, in any letter case — and a payload naming the field under
two of those keys is a 400. The tenant value itself is compared exactly:
both backends store it verbatim, so a padded spelling of the request's
tenant (`" acme "`) is another tenant and is refused the same way, and a
payload naming the request's tenant has that value replaced by the resolved
tenant before it reaches the backend, so the stored column is always the
tenant the request resolved. Both backends refuse a payload that names one
field twice (Nucleus 422, Quark 400) whichever keys it uses, so a key the
panel does not resolve cannot outvote the tenant it stamps; Quark's store
applies the keys of an update by column and Go name only, so a JSON-tag
alias in an update is dropped, never applied. A tenant column hidden from
JSON (`json:"-"`) is confined the same way: the records both backends emit
carry no tenant key, so a row is confirmed by id through a list filtered by
tenant and primary key instead of read off the record, the guard knows the
field by its column and Go name, and a create is stamped there — Quark's
store sets a schema column hidden from JSON on the entity itself, since
`json.Unmarshal` never would. Exports, imports and fixtures
work inside that tenant whatever `tenant_id` their request body carries — a
row that names another tenant fails, a row whose id belongs to another
tenant's record fails as *not found*, and an export job (`/api/exports`, its
status and its download) is listed and served only to requests scoped to the
tenant it was produced for. Models without a tenant column are not scoped. A
request the host resolves no tenant for, with no default configured, is
refused with a 403 rather than opened to every tenant — unless it comes from
a superuser or a subject granted `tenant_switch`, who is then unscoped (every
tenant) without an audit entry: only an explicit `?tenant=` switch is
recorded. Looking at another tenant, or at all of them, is that explicit
switch: `?tenant=<id>` or `?tenant=all` on the request, accepted only from a
superuser or a subject granted the `tenant_switch` action on `admin:*` (a
policy granting every action, `*`, on `admin:*` includes it), and recorded in
the [audit log](#audit-log) as `tenant.override`. Anyone else gets a 403. Without an auth provider (the open posture, warned at mount) there is no operator to gate: `?tenant=` is accepted from any client and a request with no resolved tenant is unscoped.
The audit log itself is not filtered by tenant (see below). The confinement
is only as strong as the host's resolution: a
tenant read from a request header the client can set (Nucleus' `header`
resolver with no proxy overwriting that header) is the client's choice, so
resolve it from the host name, or from a header a trusted proxy sets, for the
scope to hold.

**Record ids.** Ids are strings everywhere the API exchanges them — record
paths, the bulk endpoint's `ids` and `errors[].id`, the export's `?ids=`,
fixture `pk` values — so a UUID key works like an integer one. Numbers are
still accepted on input. An id the backend cannot narrow to the model's key
type is a 400 on a single-record call and a per-id entry in `errors[]` on a
bulk one.

**Search.** `?search=` looks in the fields a model declares searchable: in
Nucleus, fields tagged `admin:"search"`, listed in `ModelConfig.SearchFields`,
or switched on in the panel's Field settings; Quark models search every
string column. A Nucleus model that declares none is not searchable today —
the registry does not yet default search to its string columns — so a search
on it answers `400` naming the model and how to enable search, rather than
every row, and the grid disables its search box. The two backends match
differently (Nucleus lower-cases both sides;
Quark escapes `%` and `_` in the text per engine), so do not expect identical
results across them.

**Filters that carry an operator.** A filter used to mean one thing — this
column equals this value — and the query string carries the comparison now:

```
GET /api/models/Invoice?status=unpaid&total__gt=1000&due_date__lte=2026-06-30
GET /api/models/Article?title__contains=hammer&archived_at__isnull=true
GET /api/models/Order?status__in=open,paused&customer__startswith=ACME
```

The operator goes after the field, separated by two underscores. The twelve are
`eq`, `ne`, `gt`, `gte`, `lt`, `lte`, `contains`, `startswith`, `endswith`,
`in`, `not_in` and `isnull`; `in` and `not_in` take a comma-separated list, and
`isnull` a boolean. A plain `?field=value` still means equality, and the two
forms are ANDed, so `?status=open&views__gt=100` is one query.

Three things to know before you rely on it:

- **An operator this panel does not know is refused**, not read as equality. A
  filter that is quietly dropped answers every row and looks like a result.
- **A wildcard in the value is data.** `?title__contains=50%25` looks for the
  per-cent sign. On PostgreSQL, MySQL and SQL Server it is escaped; on an
  engine whose `LIKE` has no escape character (SQLite, Oracle) the Quark-backed
  data source refuses the query instead of answering it wrongly.
- **An empty `in` matches nothing.** `?status__in=` is a question with an
  answer, not an absent filter.

A field the model does not offer as a filter is refused through the operator
form exactly as it is through the plain one, and an excluded field is answered
as if the column did not exist — see below.

The grid's own filter row still sends equality; the operator forms are typed
into the URL, and a [saved view](#saved-views) keeps one — which is what a
query an operator returns to every morning usually is.

**A total the pager can divide.** A filtered list used to answer `total: -1`
with `is_estimated: true`, which no pager can turn into a page count. The list
endpoint now asks the data source for a real count over the same filters the
page uses, so `total` and `total_pages` describe the query you sent. It costs
one extra count per page request — paid by the screen that draws a pager, and
not by the exports, imports and fixtures that walk every page without one.

**What the grid refuses.** A column the panel does not show is not a sort key
and not a filter. `?order_by=` and the filter parameters resolve only against
the fields the panel would render, so a request naming an excluded field —
`password_hash`, say — is refused instead of reaching the store's `ORDER BY`,
where it would have paginated the table in that hidden column's order and
turned the page numbers into a comparison oracle over a value the schema
endpoint, the exporters and the audit redactor all take care never to emit.
The import validator reads a cell against the width its column declares, in
arbitrary precision rather than through the validator's own conversions, so
`300` in an `int8` column and `70000` in a `uint16` one fail the row instead
of reaching the writer.

![Data Studio with the Articles model selected: a sidebar listing the registered models with their record counts, and a grid showing seven article records with their real column values](./img/orbit-data-studio-light.png)

What it lists comes entirely from the host application: a model appears
here when your app registers it (in Nucleus, by listing the struct in a
module's `Models`). An app that registers no models gets an empty Data
Studio — see
[the quick start](./quick-start.md#4-register-a-model-so-data-studio-has-something-to-show)
for the three lines that populate it.

Data Studio does not speak the framework's types directly. It reads and writes
through a neutral data-source contract (`orbit/datasource`), with the Nucleus
model registry as the default backend. Applications built on the
[Quark](https://github.com/jcsvwinston/quark) ORM can point it at their Quark
models instead: add the opt-in
[`quarkdatasource`](https://github.com/jcsvwinston/orbit/tree/main/quarkdatasource)
module and set `orbit.Config.DataSource`.

`datasource.Query` is a frozen shape, so the operator filters and the exact
count arrived as new fields beside the old ones rather than as a richer
`Filters`: `Where []Filter` and `ExactTotal bool`. A data source written before
them keeps compiling and keeps answering what it answered, which is the point
of adding rather than changing — but a list it serves will then ignore an
operator filter the panel sent, so a third-party implementation should read
both. Two rules it has to keep, because each is a way to answer more rows than
were asked for while looking like a filter: a pattern operator matches its
value LITERALLY (a `%` in the text is a per-cent sign), and an `in` with no
values matches nothing rather than being dropped. `ExactTotal` may be ignored
by a source that always counts exactly, which is what `quarkdatasource` does.

## Live runtime inspector

A real-time feed of incoming HTTP requests and executed SQL across the whole
application, sourced from the framework's observability event bus.

![The Network Inspector's request log capturing traffic against the showcase application's public API: GET and POST requests to /api/articles and /api/authors with their status codes and durations](./img/orbit-live-feed-light.png)

On a single node the feed is filled from three lanes:

- **HTTP requests** — from the framework's event bus.
- **SQL statements** — from the framework's event bus.
- **Session activity** — recorded by the panel's own surface.

The panel's own admin traffic is kept out of the request feed by default: the
admin prefix ships as the default exclude pattern. Remove that pattern to see
it.

Two keys shape the rest of the feed. `live_exclude_patterns` keeps noisy paths
(health checks, static assets) out, and `trace_url_template` deep-links each
entry into an external trace explorer.

### Seeing more than one node

The feed can aggregate across nodes in either of two ways, and they are
independent of each other:

- **The Redis live-feed relay** (`cluster_*` in
  [Configuration](./configuration.md)) — the nodes of one application share a
  single feed, with no extra process to deploy.
- **The [fleet plane](./cluster/overview.md)** — a standalone observability
  server that application nodes stream to.

### Bridging Quark ORM statements

The SQL lane is fed by the framework's event bus, which the framework's own
CRUD layer publishes to. Applications that run their queries through the
[Quark](https://github.com/jcsvwinston/quark) ORM can surface those statements
in the same live view with the opt-in
[`quarkbridge`](https://github.com/jcsvwinston/orbit/tree/main/quarkbridge)
module.

`quarkbridge` is a Quark middleware. It maps each executed statement to a
Nucleus SQL event, correlates it to the request, and publishes it through the
framework's public SQL ingest. It respects Quark's argument redaction and needs
no change to Orbit itself. OpenTelemetry remains complementary for durable
tracing.

## Session viewer

List active server-side sessions and revoke them individually.

## Operators

The people who sign in to the panel are managed from it: create an account,
give it a role, reset a password somebody forgot, deactivate the account of
somebody who left. Until this existed the panel could edit the policies and
not the people they applied to — an account could only be created with
`nucleus createuser` on the server.

| What you do | Route |
|---|---|
| List operators, with the roles each one holds | `GET /admin/api/admin-users` |
| Create one (optionally with roles) | `POST /admin/api/admin-users` |
| Change an email, promote or demote a superuser | `PUT /admin/api/admin-users/{id}` |
| Set a new password | `POST /admin/api/admin-users/{id}/password` |
| Deactivate / reactivate | `POST /admin/api/admin-users/{id}/disable` · `/enable` |
| Grant or revoke a role | `POST` · `DELETE /admin/api/admin-users/{id}/roles` |
| Delete the account | `DELETE /admin/api/admin-users/{id}` |

**Deactivating is not deleting.** A deactivated operator cannot sign in and
their existing session stops working on its next request — the panel re-reads
the account on every request — but the account and everything the audit log
records about it stay. Deleting removes the row; the trail of what that person
did remains.

Two refusals are built in, because locking everyone out is a single click:
you cannot deactivate, delete or demote **your own** account, and nobody can
deactivate, delete or demote the **last active superuser**. Both answer `409`
with the reason.

Passwords set here are hashed like any other credential and are never written
to the audit log — the entry records that a password changed, not what to.

Operator management needs an admin authentication provider that owns the
accounts, which is the default one (backed by `nucleus_admin_users`). An
application that authenticates its operators elsewhere gets `501` on these
routes rather than a second, competing account store.

### Saved views

The filter set an operator returns to every morning used to live in the URL
and nowhere else. A saved view stores a name, the model and the query string
the grid was showing:

```
GET    /api/views?model=Invoice
POST   /api/views      {"model":"Invoice","name":"Unpaid over 90 days","query":"status=unpaid&order_by=due_date+asc"}
PUT    /api/views/{id}
DELETE /api/views/{id}
```

The query is stored as **text** and is not parsed against today's schema: a
view is a shortcut to a URL, so one that stops making sense fails on the list
endpoint with that endpoint's message rather than being silently dropped.

A view belongs to whoever saved it. `is_shared` makes it visible to everyone;
editing and removing stay with its owner (a superuser may tidy up any). There
is no permission of its own — creating a view needs the **list** permission of
the model it points at, and a view of a model an operator cannot list is not
shown to them, because the row would disclose both the model and what somebody
filters it by. Every change is audited (`view.create`, `view.update`,
`view.delete`).

Views need a database handle to live in; a panel without one answers `501`
rather than failing later.

### Forms that hold a relation, a document and a file

A form needs three things a table of scalars does not.

**What a foreign key points at.** The schema marks the key (`is_fk`), and two
endpoints resolve it:

```
GET /api/models/{model}/options?q=&limit=            the candidates of a model
GET /api/models/{model}/fields/{field}/options       the candidates a field may point at
```

Each option carries the `value` a record stores and a `label` a person reads
(the model's first searchable text field, then its first listed one). The
permission is the **target's**: resolving what an Author id means is reading
Authors, so an operator who may edit the record and not browse the target gets
a `403` and a form that falls back to the raw id — the panel does not widen a
grant to render a nicer widget. Search, tenant confinement and row scope apply
exactly as they do to a list.

**Children, edited with the parent.** A model whose foreign key names another
appears in the parent's schema as an inline, and the parent's own payload
carries the children:

```json
{ "title": "Kind of Blue",
  "tracks": [ { "title": "So What" },
              { "id": 12, "title": "Blue in Green" },
              { "id": 13, "_delete": true } ] }
```

A row with an id is an edit, one without is an insert, and a row that should
go **says so** — absence never deletes, because a form that loaded two of five
lines would otherwise remove the three it never showed. The key pointing at
the parent is stamped by the panel, so a child cannot be filed under another
record. Writing children needs the **child model's** own permissions, and they
are checked before the parent is written.

This is deliberately **not transactional**: the panel's data contract writes
one row at a time, so the parent is saved first and each child reported on its
own in the response (`inlines`). A form that needs all-or-nothing needs a
transactional data source underneath it, and saying so is better than implying
otherwise.

**Documents, rich text and files.** The schema's widget vocabulary was scalar;
it now also names `json`, `richtext`, `file` and `image`. A JSON document is
inferred from the column type; the other two are claims about intent that no
type carries, so the application declares them:

```yaml
modules:
  orbit:
    field_widgets:
      Album.Notes: richtext
      Album.cover: image
```

A file field holds a storage **key**, and `POST /api/models/{model}/upload`
(multipart, `file` plus an optional `field`) produces one: the bytes go to the
application's own storage and the answer carries the key the form writes into
the record. The route refuses a field that does not hold a file, caps an
upload at 32 MB, keeps only the base name of what the client called it, and is
audited (`field.upload`).

## Access control (RBAC)

Inspect and manage the Casbin policies and roles that back the application's
authorizer. Orbit registers its own prefix with the framework's default-deny
RBAC, so the admin surface is gated like any other route.

A policy is `(subject, object, action)`. The subject is an operator's id, role
or username; the action is the verb a handler checks (`list`, `retrieve`,
`create`, `update`, `delete`, `export_csv`, `bulk_delete`, `bulk_export`, and
the panel-wide ones such as `list_models` or `audit_view`). The object names
what the verb applies to, at three levels of resolution:

| Object | Means |
|---|---|
| `admin:Post` | the whole model, every row and every field |
| `admin:Post#own` | the same verb, confined to the rows that belong to this operator |
| `admin:Post.title` | one field of the model |

A superuser bypasses all three, as it always has.

### Per-row permissions

`admin:Post#own` is the grant an editorial admin needs: an author lists, opens
and edits their own posts and does not see anybody else's. Lists are filtered
by the owner column, a row owned by somebody else answers `404` on the record
endpoints (the same answer as a row that does not exist, so ids are not
disclosed), a create stamps the operator as the owner, and an update cannot
hand a row over.

Which column says who owns a row is the application's answer, not a guess:

```yaml
modules:
  orbit:
    row_owner_fields:
      Post: author        # the column of Post that holds the operator
      "*": owner          # the default for every other model
    row_owner_subject: username   # or "id"
```

A `#own` grant on a model with no entry there is **refused** with a `403` that
says why. It is never widened to every row: an ownership rule that silently
degrades to "everything" is the failure this is built to prevent.

### Per-field permissions

A policy whose object names a field narrows one column, in either of the two
shapes an admin needs — the exception, or the whole permitted set:

| Policy | Means |
|---|---|
| `(editors, admin:Post.price, deny)` | editors neither read nor write `price` |
| `(editors, admin:Post.title, update)` | an allow-list: editors update `title`, and nothing else |
| `(editors, admin:Post.title, create)` | the same, for creates |
| `(editors, admin:Post.title, write)` | both of the two above |
| `(editors, admin:Post.title, read)` | a read allow-list: only the named fields are emitted |

An allow-list only applies to a subject that holds at least one field policy of
that action for the model; a subject with none keeps the model-level grant it
always had. A write that names a field the operator may not write is refused
with a `403` **naming the field** — not dropped silently, because a form that
believes it saved a value it did not save is worse than one that is told. A
field the operator may not read is left out of the record, the list, the CSV
export and the schema.

Field policies narrow a grant; they never widen one. An operator who cannot
update the model at all is refused before any field is consulted.

### What a screen is told

The payloads a model screen loads carry what this operator may do, so the panel
can disable what it may not instead of finding out by being refused:

- `GET /api/models` and `GET /api/models/{name}/schema` carry `permissions`
  (action → boolean), `can_create`, `can_update`, `can_delete`, and `row_scope`
  — the actions confined to the operator's own rows;
- each field of the schema carries `can_edit` (`can_read` is true for every
  field that arrives: the ones it is false for are not in the schema).

They are a rendering aid. Every one of them is enforced again on the request
that follows, and a client that ignores them is refused exactly as before.

## System metrics

Runtime and resource consumption at a glance — CPU, memory, goroutines, and the
database connection pool.

![System Pulse showing live runtime metrics of the showcase application: goroutine and heap-allocation counters, a runtime trend chart, database pool health, and outbox delivery state](./img/orbit-system-pulse-light.png)

## Audit log

A trail of admin actions, kept **in the database** the panel already uses: a
table it creates and owns (`nucleus_admin_audit`). It survives a restart or a
deploy, every replica writes to and reads from the same trail, and the answer
to "what happened before the incident" does not begin when the process did.

| `audit_store` | What you get |
|---|---|
| `database` (default when the application has a database) | the durable trail described here |
| `memory` | the process-lifetime ring — bounded by `audit_max_size`, cleared by a restart, private to each replica |

An application with no database handle gets the ring either way, and so does
one whose table cannot be created: the panel logs a warning and keeps working
rather than refusing to start.

The listing (`GET /api/audit`) says which of the two it is serving
(`persistent`), so an empty page is never mistaken for "nothing happened".

### Retention

`audit_retention_days` drops entries older than that many days — a **period**,
which is what a compliance window is. (`audit_max_size` is a count of entries
and bounds the in-memory ring only.) Zero keeps entries until somebody clears
the log. The window is applied when the panel comes up and at most hourly
afterwards, on the writing path — there is no background sweeper to start,
stop or leak.

An operator can read the policy and change the window in effect from the
panel:

```
GET /api/audit/retention     → {"retention_days": 30, "configured_retention_days": 90, "store": "database", …}
PUT /api/audit/retention     {"retention_days": 30}
```

A change made this way applies immediately and is audited
(`audit.retention.set`); it does **not** rewrite the application's
configuration, so a restart comes back to `configured_retention_days`, which
the payload carries for exactly that reason.

### Export

`GET /api/audit?format=csv` streams the trail as a CSV file, carrying the same
filters as the listing — what you export is what you were reading. The export
is itself recorded (`audit.export`, with the filters and the number of
entries): who took a copy of the log is the kind of thing the log is for.

### The history of one record

`GET /api/models/{model}/{id}/history` is the same trail read by record: what
this row said before, and who changed it. It is gated by the record's own
permission (`retrieve`), not by `audit_view` — and the row scope and field
permissions of that grant apply, so a history never shows a row the operator
cannot open or a field they may not read.

It goes back as far as the trail does, which the payload states
(`persistent`, `retention_days`) so a short history is read as a retention
window rather than as a quiet one.

If you want the trail written in the same transaction as the change itself,
that belongs at the data layer: applications on the Quark ORM can enable its
transactional `quark_audit` log (`EnableAuditLog`). The panel's trail
complements it — it also covers panel-only actions (logins, session
terminations, tenant switches recorded as `tenant.override` with the requested
tenant as `record_id`) that never touch a model.

### What is recorded

Every write the panel performs leaves an entry, recorded by the handler that
performed it. The `action` names the operation:

| Surface | Actions |
|---|---|
| Data Studio | `create`, `update`, `delete` (each with the record's values before and/or after the change), `bulk_delete` (one summary plus one `delete` per row), `bulk_export`, `export.csv`, `schema.update` (field metadata edits, with the fields before and after) |
| Access control | `rbac.policy.add`, `rbac.policy.remove`, `rbac.role.assign`, `rbac.role.remove` |
| Feature flags and jobs | `flag.create`, `flag.set`, `flag.delete`, `jobs.queue.<action>` |
| Operations | `migration.apply`, `cache.flush`, `live.exclude.add`, `live.exclude.remove`, `audit.clear` (the one entry that survives the clear) |
| The trail itself | `audit.export` (a CSV copy was taken, with the filters and the count), `audit.retention.set` (the window in effect changed, with the old and new values) |
| Files | `field.upload` (a file was stored for a model's field, with the key it was stored under) |
| Saved views | `view.create`, `view.update`, `view.delete` (with the name, the query and whether it is shared) |
| Data management | `export.create`, `fixtures.dumpdata`, `import.upload`, `import.validate`, `import.execute`, `fixtures.loaddata` — exports are recorded whether they completed or failed |
| Sessions | `login`, `login.failed`, `login.locked`, `logout`, `session.terminate` |
| Tenant scope | `tenant.override` (an accepted `?tenant=` switch, with the requested tenant as `record_id`; a refused switch leaves no entry) |

`old_value` and `new_value` are redacted before they are stored, because the
log is readable by any operator with `audit_view`: fields the model excludes
from Data Studio and credential-shaped names (password, secret, token, hash,
salt…) appear as `[redacted]`, string values longer than 4 KB are truncated,
a Redis URL loses its password, a session token is shortened, imports and
exports record counts rather than rows, and login entries carry the attempted
username, never the password.

Entries are not filtered by tenant. With `multitenant_enabled`, an operator
granted `audit_view` reads every entry the ring holds — the redacted old and
new values of rows written by other tenants' operators, and every
`tenant.override` — not only the entries of the tenant the request resolved
to.

Entries are bounded as well as redacted. The user id, username, model,
record id and client IP are cut at 256 bytes and the User-Agent at 512, with
a `…[truncated]` marker on anything cut. The login route is the one place an
anonymous client writes to the log, so it is bounded twice more: the panel
caps the login POST body at 16 KB (whichever layer parses the form first,
the entry a failed attempt leaves is cut to the field bounds above), and
per client IP and lockout window (one minute) the log keeps at most 10
`login.failed` entries and one `login.locked` — the lockout keeps answering
429 for the rest of the window without adding entries, and a successful
login from that IP starts the count again. The client IP is the full
remote address: a client that rotates source addresses, as an IPv6 `/64`
allows, is bounded per address, not per client. A single address can
neither inflate the ring's memory nor push the entries recorded before its
attempts out of it.

`GET /api/audit` pages newest first; `total` and `total_pages` count the
entries that match the `user_id`, `model` and `action` filters, so a filtered
listing does not page into nothing. `POST /api/audit/clear` answers
`{"cleared": true, "dropped": <n>}`.

## Overview & Health

A dashboard summarizing the above, plus a health-at-a-glance view.
