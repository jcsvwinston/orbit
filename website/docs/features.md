---
title: Features
sidebar_position: 3
description: What the Orbit admin panel includes.
---

# Features

Orbit is a single panel composed of focused modules. All of them read live state
from the host application's `Runtime` — there is no separate data store.

## Data Studio

Browse, create, edit, and delete records for every model in the application's
registry. It is **tenant-aware** (when multitenancy is enabled) and supports
import/export.

Data Studio reads and writes through a neutral data-source contract
(`orbit/datasource`), with the Nucleus model registry as the default backend.
Applications that run the [Quark](https://github.com/jcsvwinston/quark) ORM can
point it at their Quark models instead with the opt-in
[`quarkdatasource`](https://github.com/jcsvwinston/orbit/tree/main/quarkdatasource)
module and `orbit.Config.DataSource`.

## Live runtime inspector

A real-time feed of incoming HTTP requests and executed SQL across the whole
application, sourced from the framework's observability event bus. It can
optionally aggregate across nodes — either via the single-process Redis relay
(`cluster_*` config) or the standalone
[agent/server fleet](./cluster/overview.md).

Use `live_exclude_patterns` to keep noisy paths (health checks, static assets)
out of the feed, and `trace_url_template` to deep-link each entry into an
external trace explorer.

### Bridging Quark ORM statements

The SQL feed is sourced from the framework's event bus, which the framework's own
CRUD layer feeds. Applications that run queries through the
[Quark](https://github.com/jcsvwinston/quark) ORM instead can surface those
statements in the same live view with the opt-in
[`quarkbridge`](https://github.com/jcsvwinston/orbit/tree/main/quarkbridge)
module — a Quark middleware that maps each executed statement to a Nucleus SQL
event, correlated to the request, and publishes it through the framework's public
SQL ingest. It respects Quark's argument redaction and requires no change to
Orbit. OpenTelemetry remains complementary for durable tracing.

## Session viewer

List active server-side sessions and revoke them individually.

## Access control (RBAC)

Inspect and manage the Casbin policies and roles that back the application's
authorizer. Orbit self-registers its own prefix with the framework's
default-deny RBAC, so the admin surface is gated like any other route.

## System metrics

Runtime and resource consumption at a glance — CPU, memory, goroutines, and the
database connection pool.

## Audit log

An in-memory ring of admin actions, sized by `audit_max_size` (default 10,000
entries). It is **not** persisted — it is a live operational view, not a
compliance store.

## Overview & Health

A dashboard summarizing the above, plus a health-at-a-glance view.
