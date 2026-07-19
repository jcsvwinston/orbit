---
title: Overview
sidebar_position: 1
description: Cross-node live telemetry — when and how.
---

# Fleet observability

The in-process panel shows the live feed for **its own node**. For fleet-wide
live telemetry — every node's HTTP and SQL events aggregated in one place —
Orbit ships three sibling modules, releasable independently:

| Module | Role |
|---|---|
| [`orbit/proto`](./proto.md) | Connect-RPC contract + generated stubs. |
| [`orbit/agent`](./agent.md) | In-process agent that ships events to an admin server. |
| [`orbit/server`](./server.md) | Standalone admin server that receives them. |

**Most applications need none of this.** The root `orbit` module's in-process
panel works on its own, and a single-process deployment can aggregate across
nodes with just the Redis relay (`cluster_*` in
[Configuration](../configuration.md)). Reach for the agent/server fleet when you
want a dedicated, always-on observability server independent of any single
application node.

## How the pieces fit

```text
  app node 1 ──[orbit/agent]──┐
  app node 2 ──[orbit/agent]──┼──> [orbit/server] ──> admin UI
  app node N ──[orbit/agent]──┘     (Connect-RPC bidi stream)
```

Each agent embeds in a framework process and streams observability events over a
single Connect-RPC bidirectional stream to the standalone server, which fans
them out to connected admin UIs. The wire contract between all three is
[`orbit/proto`](./proto.md).
