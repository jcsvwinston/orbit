---
title: How it works
sidebar_position: 5
description: Orbit's in-process runtime model.
---

# How it works

Orbit is a `nucleus.ModuleSpec`, so it starts and stops with your application.
On startup it captures the application's `Runtime` and builds its panel from
the framework's **public accessors**: the model registry, all managed database
handles, the session manager, the RBAC enforcer, the live event bus, and
storage. It never reaches into framework internals.

## In-process

Because it runs inside the application process, Orbit sees live runtime state
that an out-of-process sidecar could not — sessions, SQL, the model registry,
metrics — and it sees it with no IPC surface in between.

## Self-contained auth

Orbit owns its login. A session-based authenticator (`DatabaseAdminAuth`)
checks credentials against the `nucleus_admin_users` table, and Orbit registers
its own prefix with the framework's default-deny RBAC so the framework
middleware never double-gates it.

Point `auth_database` at a dedicated database alias to keep the admin user
store separate from application data. Only login and bootstrapping move to that
handle; the panel itself keeps reading through the application's default one.

## Embedded interface

The React UI is built into the module with `go:embed` and served under the
mount prefix. There is no separate asset deployment: mount Orbit and you get
the whole admin panel offline, in a single binary, version-pinned to the
module.

## Relationship to Nucleus

Orbit is built on the same public [Nucleus](/nucleus/) extension and `Runtime`
API that any other module uses — nothing here is a private back door. The admin
panel used to live in the framework core as `pkg/admin`; it now lives in this
module, and Nucleus itself no longer ships any admin code.
