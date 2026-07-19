---
title: Orbit
sidebar_position: 1
slug: /
description: Pluggable, in-process admin panel for the Nucleus framework.
---

# Orbit

**The pluggable admin product for the [Nucleus](/nucleus/) framework.**

Orbit is a self-contained admin panel — Data Studio, a live request/SQL feed, a
session viewer, RBAC management, and system metrics — that mounts **in-process**
into any Nucleus application through the framework's extension/module API.

It is a separate Go module with its own release cadence. The admin was moved out
of the framework core and into this module so the core stays lean and the admin
evolves as its own product. You add one
dependency and one `Mount(...)` call; Orbit reads everything it needs from the
running app's `Runtime` and serves its **embedded** React SPA — no separate
asset deployment, no out-of-process sidecar, and no database of its own.

## What you get

| Module | What it does |
|--------|--------------|
| **Data Studio** | Browse, create, edit, and delete records for every model in the app's registry — tenant-aware, with import/export. |
| **Live runtime inspector** | Real-time feed of incoming HTTP requests and executed SQL across the whole app, with optional cross-node aggregation. |
| **Session viewer** | List and revoke active server-side sessions. |
| **Access control (RBAC)** | Inspect and manage the Casbin policies and roles backing the app's authorizer. |
| **System metrics** | Runtime and resource consumption — CPU, memory, goroutines, database pool. |
| **Audit log** | An in-memory ring of admin actions. |
| **Overview & Health** | Dashboard and health at a glance. |

The UI ships **embedded in the binary** (`go:embed`), version-pinned to the
Orbit module: a consumer who mounts Orbit gets the full admin offline, in a
single binary.

## Requirements

- **Go 1.26+**
- A **[Nucleus](/nucleus/)** application to mount into.
- *(Optional)* **Redis** — only for the cross-node live-telemetry relay (see
  [Cluster observability](./cluster/overview.md)).

## Status

The current tagged release is **v1.4.2**. <!-- x-release-please-version --> The
public API — the root module and `datasource` — is stable for the life of v1.x:
it will not change in a breaking way within v1.

Next: [Quick start](./quick-start.md).
