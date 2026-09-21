---
title: orbit/proto
sidebar_position: 2
description: The Connect-RPC contract shared by agent, server, and UI.
---

# orbit/proto

The Connect-RPC v1 contract between the [agent](./agent.md), the
[admin server](./server.md), and the admin UI. You never import it directly;
agent and server pin it for you. It matters when you change the wire format.

The single source of truth is `nucleus/admin/v1/admin.proto`.

## Generated outputs

Both targets are committed, so a fresh checkout compiles without `buf`
installed:

- `gen/go/nucleus/admin/v1/` — Go message structs and Connect-RPC service
  stubs, imported by [`orbit/agent`](./agent.md) and
  [`orbit/server`](./server.md).
- `../ui/src/gen/nucleus/admin/v1/` — TypeScript stubs consumed by the
  Connect-Web client in the UI.

Regenerating is a manual step: run `make proto` after any `.proto` change.
Nothing verifies a clean diff automatically today, so it is on you to
regenerate and commit.

## Day-to-day commands

```bash
# Regenerate Go + TypeScript stubs (committed). Run after any .proto change.
make proto

# Lint with buf's STANDARD ruleset (plus documented exceptions for the shared
# Frame/Event/Snapshot types).
make proto-lint

# Verify nothing in this change is wire-incompatible vs main.
make proto-breaking
```

## Evolution rules

**The wire format is append-only.** A rolling update means old agents talk to
new servers and the reverse, so every change must stay backward- and
forward-compatible. In practice:

- never remove fields;
- never reorder `oneof` tags;
- never reuse field numbers.

Before changing the proto, read
[`EVOLUTION.md`](https://github.com/jcsvwinston/orbit/blob/main/proto/EVOLUTION.md)
in the module for the full rules.

## Version pins

`buf.gen.yaml` pins `bufbuild/es:v1.10.0` and `connectrpc/es:v1.6.1` because
the `connectrpc/es` buf-registry plugin has no v2 published yet; the npm
packages in the UI track the same line. When the v2 plugin lands, moving over
is a small change here plus a single `npm install` in the UI.
