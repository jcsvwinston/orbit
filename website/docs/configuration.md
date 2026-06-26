---
title: Configuration
sidebar_position: 3
description: The modules.orbit.* configuration reference.
---

# Configuration

`orbit.Config` is bound from the `modules.orbit.*` subtree of your `nucleus.yml`
(or set it directly in Go). All fields are optional.

| Key (`modules.orbit.*`) | Type | Default | Description |
|---|---|---|---|
| `prefix` | string | `/admin` | URL path Orbit mounts under. |
| `title` | string | — | Heading shown in the UI. |
| `environment` | string | — | Label shown in the UI (e.g. `production`). |
| `bootstrap_username` | string | — | Admin user created on first boot. |
| `bootstrap_email` | string | — | Email for the bootstrap user. |
| `bootstrap_password` | string | — | Password for the bootstrap user; empty → a random one is generated and printed once. |
| `auth_database` | string | app default | DB alias whose handle backs admin auth + the bootstrap user (use a dedicated DB for the admin user store). |
| `migrations_path` | string | `migrations` | Directory the migrations view reads. |
| `audit_max_size` | int | `10000` | In-memory audit-log ring size. |
| `multitenant_enabled` | bool | `false` | Filter records by the request's resolved tenant. |
| `multitenant_default` | string | — | Default tenant when none is resolved. |
| `multitenant_ids` | []string | — | Known tenant IDs for the selector UI. |
| `live_exclude_patterns` | []string | — | Path patterns excluded from the live HTTP feed. |
| `trace_url_template` | string | — | External trace-explorer URL template (supports `{trace_id}`). |
| `cluster_enabled` | bool | `false` | Aggregate the live feed across nodes via a Redis relay. |
| `cluster_redis_url` | string | — | Redis URL for the live-telemetry relay. |
| `cluster_channel` | string | `nucleus:admin:live:v1` | Pub/sub channel for the relay. |
| `cluster_node_id` | string | runtime id | Explicit node identifier in the relay. |
| `cluster_token` | string | — | Shared secret to reject untrusted relay messages. |

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

## A note on the cluster keys

The `cluster_*` keys above configure the **single-process Redis relay** for
aggregating the live feed across nodes. That is distinct from the standalone
**agent/server fleet** model (a dedicated observability server that many agents
stream to) documented under [Cluster observability](./cluster/overview.md).
Most applications need neither — the in-process panel works on its own.
