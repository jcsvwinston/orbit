---
title: Quick start
sidebar_position: 2
description: Mount Orbit on a Nucleus app and sign in.
---

# Quick start

One dependency and one `Mount(...)` call give your Nucleus application a full
admin panel at `/admin` — Data Studio, a live request and SQL feed, sessions,
RBAC and metrics. It is served straight from your own binary: nothing separate
to deploy, no sidecar process, no database of its own.

You need Go 1.26 or newer and a Nucleus application to mount into.

## 1. Add the dependency

```bash
go get github.com/jcsvwinston/orbit@latest
```

The current release is v1.8.0; pin that tag rather than `@latest` for reproducible builds. <!-- x-release-please-version -->

## 2. Mount the module

```go
import (
    "os"

    "github.com/jcsvwinston/nucleus/pkg/nucleus"
    "github.com/jcsvwinston/orbit"
)

func main() {
    app, err := nucleus.New().
        FromConfigFile("nucleus.yml").
        Mount(orbit.Module(orbit.Config{
            Prefix:            "/admin",
            Title:             "Acme Admin",
            BootstrapUsername: "admin",
            BootstrapEmail:    "admin@acme.test",
            // When BootstrapPassword is empty, bootstrapping is skipped —
            // provision the admin user another way (e.g. nucleus createuser).
            BootstrapPassword: os.Getenv("ADMIN_BOOTSTRAP_PASSWORD"),
        })).
        Build()
    if err != nil {
        panic(err)
    }
    if err := nucleus.Run(app); err != nil {
        panic(err)
    }
}
```

## 3. Start the app and sign in

Run the application, open `/admin`, and sign in as the bootstrap user.

You do not wire up any protection yourself. Orbit registers its prefix with the
framework's default-deny RBAC and enforces its own session-based login below
that prefix, so the panel is gated from the first request.

## The bootstrap user

`BootstrapPassword` decides whether Orbit creates an admin user at all:

- **Set** — on first start Orbit creates the user named by
  `BootstrapUsername`, unless that user already exists.
- **Empty** — bootstrapping is **skipped** and no admin user is created.
  Provision one another way, for example with the framework's
  `nucleus createuser` command against the same database, before you try to
  sign in.

Reading the password from the environment, as the snippet above does, keeps it
out of your source tree.

## Every option has a default

`orbit.Config`'s zero value is valid. This mounts the panel under `/admin` with
default settings and no bootstrap user:

```go
.Mount(orbit.Module(orbit.Config{}))
```

Next: [Configuration](./configuration.md) for every option, or
[How it works](./how-it-works.md) for the runtime model.
