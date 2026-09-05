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

## Access control (RBAC)

Inspect and manage the Casbin policies and roles that back the application's
authorizer. Orbit registers its own prefix with the framework's default-deny
RBAC, so the admin surface is gated like any other route.

## System metrics

Runtime and resource consumption at a glance — CPU, memory, goroutines, and the
database connection pool.

![System Pulse showing live runtime metrics of the showcase application: goroutine and heap-allocation counters, a runtime trend chart, database pool health, and outbox delivery state](./img/orbit-system-pulse-light.png)

## Audit log

An in-memory ring of admin actions, sized by `audit_max_size` (default 10,000
entries). It is **not** persisted: it lives in one process, a restart or
deploy clears it, and in a multi-replica deployment each replica keeps its
own ring. Treat it as a live operational view, not a compliance store.

If you need a durable audit trail, write it at the data layer: applications
on the Quark ORM can enable its transactional `quark_audit` log
(`EnableAuditLog`), which survives restarts and is written in the same
transaction as the change. The panel's ring complements it — it also covers
panel-only actions (logins, session terminations) that never touch a model.

### What is recorded

Every write the panel performs leaves an entry, recorded by the handler that
performed it. The `action` names the operation:

| Surface | Actions |
|---|---|
| Data Studio | `create`, `update`, `delete` (each with the record's values before and/or after the change), `bulk_delete` (one summary plus one `delete` per row), `bulk_export`, `export.csv`, `schema.update` (field metadata edits, with the fields before and after) |
| Access control | `rbac.policy.add`, `rbac.policy.remove`, `rbac.role.assign`, `rbac.role.remove` |
| Feature flags and jobs | `flag.create`, `flag.set`, `flag.delete`, `jobs.queue.<action>` |
| Operations | `migration.apply`, `cache.flush`, `live.exclude.add`, `live.exclude.remove`, `audit.clear` (the one entry that survives the clear) |
| Data management | `export.create`, `fixtures.dumpdata`, `import.upload`, `import.validate`, `import.execute`, `fixtures.loaddata` — exports are recorded whether they completed or failed |
| Sessions | `login`, `login.failed`, `login.locked`, `logout`, `session.terminate` |

`old_value` and `new_value` are redacted before they are stored, because the
log is readable by any operator with `audit_view`: fields the model excludes
from Data Studio and credential-shaped names (password, secret, token, hash,
salt…) appear as `[redacted]`, string values longer than 4 KB are truncated,
a Redis URL loses its password, a session token is shortened, imports and
exports record counts rather than rows, and login entries carry the attempted
username, never the password.

Entries are bounded as well as redacted. The user id, username, model,
record id and client IP are cut at 256 bytes and the User-Agent at 512, with
a `…[truncated]` marker on anything cut. The login route is the one place an
anonymous client writes to the log, so it is bounded twice more: the login
POST body is capped at 16 KB (a larger one is answered with 400 before any
credential is checked, and leaves no entry), and per client IP and lockout
window (one minute) the log keeps at most 10 `login.failed` entries and one
`login.locked` — the lockout keeps answering 429 for the rest of the window
without adding entries, and a successful login from that IP starts the
count again. A single client can neither inflate the ring's memory nor
push the entries recorded before its attempts out of it.

`GET /api/audit` pages newest first; `total` and `total_pages` count the
entries that match the `user_id`, `model` and `action` filters, so a filtered
listing does not page into nothing. `POST /api/audit/clear` answers
`{"cleared": true, "dropped": <n>}`.

## Overview & Health

A dashboard summarizing the above, plus a health-at-a-glance view.
