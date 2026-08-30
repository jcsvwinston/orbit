---
title: Module compatibility matrix
sidebar_position: 3
description: Which fleet-module tags shipped with each orbit root release.
---

<!-- GENERATED — do not edit by hand. Regenerate with
     bash scripts/ci/gen_module_matrix.sh (a CI check fails on drift). -->

# Module compatibility matrix

One row per root release; each cell is the submodule version recorded in the
repository's release manifest at that root tag. Remember the operational
rule from the [upgrade guide](../operations/upgrade.md): as a consumer you
only ever choose two versions — the root (with your app) and the
server/agent pair (with your fleet).

| orbit (root) | proto | agent | server | quarkbridge | quarkdatasource |
| --- | --- | --- | --- | --- | --- |
| `v1.8.10` | `v0.4.2` | `v0.6.7` | `v0.10.7` | `v0.4.7` | `v0.2.15` |
| `v1.8.9` | `v0.4.2` | `v0.6.6` | `v0.10.6` | `v0.4.6` | `v0.2.15` |
| `v1.8.8` | `v0.4.2` | `v0.6.6` | `v0.10.6` | `v0.4.6` | `v0.2.15` |
| `v1.8.7` | `v0.4.2` | `v0.6.6` | `v0.10.6` | `v0.4.6` | `v0.2.15` |
| `v1.8.6` | `v0.4.2` | `v0.6.5` | `v0.10.5` | `v0.4.5` | `v0.2.14` |
| `v1.8.5` | `v0.4.2` | `v0.6.4` | `v0.10.4` | `v0.4.4` | `v0.2.14` |
| `v1.8.4` | `v0.4.2` | `v0.6.3` | `v0.10.3` | `v0.4.3` | `v0.2.14` |
| `v1.8.3` | `v0.4.2` | `v0.6.2` | `v0.10.2` | `v0.4.2` | `v0.2.14` |
| `v1.8.2` | `v0.4.2` | `v0.6.1` | `v0.10.1` | `v0.4.1` | `v0.2.14` |
| `v1.8.1` | `v0.4.2` | `v0.6.0` | `v0.10.0` | `v0.4.0` | `v0.2.14` |
| `v1.8.0` | `v0.4.2` | `v0.6.0` | `v0.10.0` | `v0.4.0` | `v0.2.13` |
| `v1.7.4` | `v0.4.2` | `v0.5.17` | `v0.9.13` | `v0.3.16` | `v0.2.13` |
| `v1.7.3` | `v0.4.2` | `v0.5.16` | `v0.9.12` | `v0.3.15` | `v0.2.13` |
| `v1.7.2` | `v0.4.2` | `v0.5.15` | `v0.9.11` | `v0.3.14` | `v0.2.12` |
| `v1.7.1` | `v0.4.2` | `v0.5.15` | `v0.9.11` | `v0.3.14` | `v0.2.12` |
| `v1.7.0` | `v0.4.2` | `v0.5.15` | `v0.9.11` | `v0.3.14` | `v0.2.12` |
| `v1.6.7` | `v0.4.2` | `v0.5.15` | `v0.9.11` | `v0.3.14` | `v0.2.12` |
| `v1.6.6` | `v0.4.2` | `v0.5.14` | `v0.9.10` | `v0.3.13` | `v0.2.12` |
| `v1.6.5` | `v0.4.2` | `v0.5.13` | `v0.9.9` | `v0.3.12` | `v0.2.11` |
| `v1.6.4` | `v0.4.2` | `v0.5.13` | `v0.9.8` | `v0.3.12` | `v0.2.11` |
| `v1.6.3` | `v0.4.2` | `v0.5.12` | `v0.9.8` | `v0.3.12` | `v0.2.11` |
| `v1.6.2` | `v0.4.2` | `v0.5.12` | `v0.9.7` | `v0.3.11` | `v0.2.10` |
| `v1.6.1` | `v0.4.2` | `v0.5.11` | `v0.9.6` | `v0.3.10` | `v0.2.10` |
| `v1.6.0` | `v0.4.2` | `v0.5.11` | `v0.9.6` | `v0.3.10` | `v0.2.9` |
| `v1.5.4` | `v0.4.2` | `v0.5.10` | `v0.9.5` | `v0.3.9` | `v0.2.8` |
| `v1.5.3` | `v0.4.2` | `v0.5.9` | `v0.9.4` | `v0.3.8` | `v0.2.8` |
| `v1.5.2` | `v0.4.1` | `v0.5.8` | `v0.9.3` | `v0.3.8` | `v0.2.8` |
| `v1.5.1` | `v0.4.1` | `v0.5.6` | `v0.9.1` | `v0.3.6` | `v0.2.7` |
| `v1.5.0` | `v0.4.1` | `v0.5.5` | `v0.9.0` | `v0.3.5` | `v0.2.7` |
| `v1.4.4` | `v0.4.1` | `v0.5.4` | `v0.8.4` | `v0.3.4` | `v0.2.6` |
| `v1.4.3` | `v0.4.1` | `v0.5.3` | `v0.8.3` | `v0.3.3` | `v0.2.5` |
| `v1.4.2` | `v0.4.1` | `v0.5.2` | `v0.8.2` | `v0.3.2` | `v0.2.4` |
| `v1.4.1` | `v0.4.1` | `v0.5.1` | `v0.8.1` | `v0.3.2` | `v0.2.3` |
| `v1.4.0` | `v0.4.0` | `v0.5.0` | `v0.8.0` | `v0.3.1` | `v0.2.2` |
| `v1.3.0` | `v0.3.0` | `v0.5.0` | `v0.7.0` | `v0.3.1` | `v0.2.2` |
| `v1.2.1` | `v0.3.0` | `v0.4.0` | `v0.6.0` | `v0.3.0` | `v0.2.1` |
| `v1.2.0` | `v0.3.0` | `v0.4.0` | `v0.5.0` | `v0.3.0` | `v0.2.1` |
| `v1.1.0` | `v0.1.1` | `v0.2.1` | `v0.3.1` | `v0.2.1` | `v0.2.1` |
| `v1.0.0` | `v0.1.0` | `v0.2.0` | `v0.2.0` | `v0.1.0` | `v0.1.0` |
| `v0.3.0` | `` | `` | `` | `v0.1.0` | `v0.1.0` |
| `v0.2.0` | `` | `` | `` | `v0.0.0` | `v0.0.0` |

### The `quarkdatasource → orbit v1.4.3` pin

`quarkdatasource/go.mod` requires an older orbit root on purpose: the edge
is topologically forced (the datasource module lives inside the root module's
directory, so requiring the current root would be a cycle at tag-cut time).
It is harmless in practice — Go's minimal version selection raises the root
to whatever YOUR application requires, and the frozen datasource contract is
verified identical across the lagging tag by the sibling-pin guard. It does
not mean the module is unmaintained.
