---
title: Quick start
sidebar_position: 2
description: Mount Orbit on a Nucleus app and sign in.
---

# Quick start

Orbit mounts on the application builder as a Nucleus module. Add the dependency:

```bash
go get github.com/jcsvwinston/orbit@v0.1.0
```

`@latest` resolves to v0.1.0; pin `@v0.1.0` for reproducible builds.

## Mount it

```go
import (
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
            // BootstrapPassword empty → a random one is generated and printed
            // to stderr once on first boot. Capture it and rotate.
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

When `BootstrapPassword` is empty, Orbit generates a random password and prints
it to stderr **once** on first boot. Capture it from the logs and rotate it.

See [Configuration](./configuration.md) for every option, or
[How it works](./how-it-works.md) for the runtime model.
