---
title: Configuration
sidebar_position: 4
description: The modules.orbit.* configuration reference.
---

# Configuration

`orbit.Config` is bound from the `modules.orbit.*` subtree of your `nucleus.yml`
(or set directly in Go). **Every field is optional** — the zero value mounts a
working panel under `/admin`.

The tables below group the keys by what they affect.

## Mounting

| Key (`modules.orbit.*`) | Type | Default | Description |
|---|---|---|---|
| `prefix` | string | `/admin` | URL path Orbit mounts under. |
| `title` | string | `Orbit` | Heading shown in the UI: the login page, the sidebar, and the browser tab. |
| `environment` | string | — | Label shown in the UI (e.g. `production`). |

## The bootstrap admin user

| Key (`modules.orbit.*`) | Type | Default | Description |
|---|---|---|---|
| `bootstrap_username` | string | — | Admin user created on first boot. |
| `bootstrap_email` | string | — | Email for the bootstrap user. |
| `bootstrap_password` | string | — | Password for the bootstrap user. Leave it empty to skip creating the user and provision the admin account another way, e.g. `nucleus createuser`. The `nucleus_admin_users` schema is created at mount either way, so `createuser` works without ever setting a bootstrap password. |
| `auth_database` | string | app default | Database alias whose handle backs admin login and the bootstrap user — point it at a dedicated database to keep the admin user store away from application data. Only login and bootstrapping are redirected; the panel itself always reads through the application's default handle. |

## Data and views

| Key (`modules.orbit.*`) | Type | Default | Description |
|---|---|---|---|
| `migrations_path` | string | `migrations` | Directory the migrations view reads. |
| `audit_max_size` | int | `10000` | In-memory audit-log ring size. The ring is per process and not persisted — a restart clears it (see [Audit log](./features.md#audit-log)). |
| `multitenant_enabled` | bool | `false` | Filter records by the request's resolved tenant. |
| `multitenant_default` | string | — | Default tenant when none is resolved. |
| `multitenant_ids` | []string | — | Known tenant IDs for the selector UI. |

## The live feed

| Key (`modules.orbit.*`) | Type | Default | Description |
|---|---|---|---|
| `live_exclude_patterns` | []string | — | Path patterns excluded from the live HTTP feed — use it to keep health checks and static assets out. |
| `trace_url_template` | string | — | External trace-explorer URL template, to deep-link each entry (supports `{trace_id}`). |

## The live-feed relay (the `cluster_*` keys)

By default the live feed shows **this node's** traffic. Turn these keys on and
the nodes of one application relay their live events to each other over Redis,
so the panel on any node shows the whole set.

| Key (`modules.orbit.*`) | Type | Default | Description |
|---|---|---|---|
| `cluster_enabled` | bool | `false` | Aggregate the live feed across nodes via a Redis relay. |
| `cluster_redis_url` | string | — | Redis URL for the relay. |
| `cluster_channel` | string | `nucleus:admin:live:v1` | Pub/sub channel for the relay. |
| `cluster_node_id` | string | runtime id | Explicit node identifier in the relay. |
| `cluster_token` | string | — | Shared secret to reject untrusted relay messages. |

:::note These keys are not the fleet plane
The `cluster_*` keys stay inside your application process: same panel, same
binary, one shared feed. The standalone agent-and-server
[fleet plane](./cluster/overview.md) is a different, heavier option — a
dedicated observability server that application nodes stream to, with no
Redis involved. Most applications need neither.
:::

## Example

```yaml
# nucleus.yml
modules:
  orbit:
    prefix: /admin
    title: Acme Admin
    environment: production
    bootstrap_username: admin
    bootstrap_email: admin@acme.test
```
