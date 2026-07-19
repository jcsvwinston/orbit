---
title: Quick start
sidebar_position: 2
description: Mount Orbit on a Nucleus app and sign in.
---

# Quick start

Orbit mounts on the application builder as a Nucleus module. Add the dependency:

```bash
go get github.com/jcsvwinston/orbit@latest
```

The current tagged release is v1.4.1; pin that tag for reproducible builds. <!-- x-release-please-version -->

## Mount it

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
    _ = app.Start()
}
```

Start the app, open `/admin`, and sign in with the bootstrap user. Orbit
self-registers its prefix with the framework's default-deny RBAC and enforces
its own session-based auth below that prefix.

## Zero config

The zero value is valid — `orbit.Module(orbit.Config{})` mounts under `/admin`
with sensible defaults:

```go
.Mount(orbit.Module(orbit.Config{}))
```

When `BootstrapPassword` is empty, bootstrapping is **skipped** — no admin user
is created. Provision one another way (e.g. the framework's `nucleus createuser`
command against the same database) before signing in.

See [Configuration](./configuration.md) for every option, or
[How it works](./how-it-works.md) for the runtime model.
