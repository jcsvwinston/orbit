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

## Overview & Health

A dashboard summarizing the above, plus a health-at-a-glance view.
