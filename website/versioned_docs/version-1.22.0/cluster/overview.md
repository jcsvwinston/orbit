---
title: Overview
sidebar_position: 1
description: Cross-node live telemetry — when and how.
---

# Fleet observability

The fleet plane is a **standalone observability server** that many application
nodes stream their HTTP and SQL events to, so every node's live telemetry lands
in one place that keeps running when any single node does not.

**Most applications never need it.** Start from the shape you need:

- **One node, or you only ever look at one node at a time** — the in-process
  panel is complete on its own. Nothing on this page applies.
- **Several nodes of one application, one shared live feed** — turn on the
  Redis relay (the `cluster_*` keys in
  [Configuration](../configuration.md)). Still just the panel.
- **A dedicated, always-on observability server** — that is the fleet plane,
  documented here.

:::note This is not the `cluster_*` configuration
Despite the similar vocabulary, the `cluster_*` keys and this section are
unrelated. Those keys relay the live feed between nodes of one application
over Redis, inside the same process. The fleet plane runs a separate server
binary, uses no Redis, and can serve many applications. You can run either,
both, or neither.
:::

## How the pieces fit

```text
  app node 1 ──[orbit/agent]──┐
  app node 2 ──[orbit/agent]──┼──> [orbit/server] ──> admin UI
  app node N ──[orbit/agent]──┘     (Connect-RPC bidi stream)
```

Each agent embeds in a framework process and streams observability events over a
single Connect-RPC bidirectional stream to the standalone server, which fans
them out to connected admin UIs. Each application node also keeps its own
in-process panel; the fleet plane is additive, not a replacement.

## The three modules

They ship as siblings of the root module and are released independently, with
their own tags:

| Module | Role |
|---|---|
| [`orbit/proto`](./proto.md) | The wire contract shared by all three, plus generated stubs. |
| [`orbit/agent`](./agent.md) | In-process agent that ships events to an admin server. |
| [`orbit/server`](./server.md) | Standalone admin server that receives them. |

To put it into production, see [Deployment](../operations/deployment.md) and
[Security](../operations/security.md).
