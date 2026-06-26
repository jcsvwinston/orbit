---
title: How it works
sidebar_position: 5
description: Orbit's in-process runtime model.
---

# How it works

Orbit is a `nucleus.ModuleSpec`. On startup it captures the application's
`Runtime` and builds its panel from the framework's **public accessors** — the
model registry, all managed database handles, the session manager, the RBAC
enforcer, the live event bus, and storage. It never reaches into framework
internals.

## In-process

Because it runs inside the application process, Orbit sees live runtime state —
sessions, SQL, the model registry, metrics — that an out-of-process sidecar
could not, and it does so without any IPC surface.

## Self-contained auth

Orbit owns a session-based login (`DatabaseAdminAuth`) against the
`nucleus_admin_users` table, and self-registers its prefix with the framework's
default-deny RBAC so the framework middleware never double-gates it. Point
`auth_database` at a dedicated DB alias to keep the admin user store separate
from application data.

## Embedded SPA

The React UI is built into the module (`go:embed`) and served under the mount
prefix. There is no separate asset deployment: a consumer who mounts Orbit gets
the full admin offline, in a single binary, version-pinned to the module.

## Relationship to Nucleus

Orbit is a "dogfooding" consumer of [Nucleus](/nucleus/): mounting a real, deep
admin exercises and hardens the framework's extension/`Runtime` surface. The
admin used to live in the framework core as `pkg/admin`; nucleus ADR-019
extracted it into this module as a clean break — Nucleus itself no longer ships
any admin code.
