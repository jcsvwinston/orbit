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

Its scripts and stylesheets are compressed once, when the interface is built:
each one is embedded beside a Brotli and a gzip copy, and the panel answers a
request with the encoding the browser accepts (`Content-Encoding`), or with the
file as it is, and says the answer varies on that (`Vary: Accept-Encoding`).
Nothing is compressed per request, so the panel does not need a compression
middleware or a proxy in front of it to travel compressed; Nucleus's own
compression middleware, if the application installs it, passes an answer that
already carries `Content-Encoding` through untouched.

## Relationship to Nucleus

Orbit is built on the same public [Nucleus](/nucleus/) extension and `Runtime`
API that any other module uses — nothing here is a private back door. The admin
panel used to live in the framework core as `pkg/admin`; it now lives in this
module, and Nucleus itself no longer ships any admin code.
