#!/usr/bin/env bash
# gen_module_matrix.sh — regenerates website/docs/reference/module-matrix.md
# from git history (DX-26): one row per root release, columns for the five
# submodules, read from .release-please-manifest.json AT each root tag. The
# answer to "which agent goes with orbit vX.Y.Z?" must be one page, and a CI
# freshness check fails if this file drifts from the manifests.
set -euo pipefail
cd "$(dirname "$0")/../.."

out="website/docs/reference/module-matrix.md"
{
cat <<'HDR'
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
HDR
# The row for the release being CUT right now.
#
# The loop below reads tags, and a tag does not exist until the release PR
# merges — so main's committed copy went stale the instant a root tag was
# cut, and the freshness check (which only runs on PRs) slept until the next
# unrelated PR tripped over it. That happened in two consecutive rounds.
#
# The working-tree manifest is the missing source: in a release PR
# release-please has already written the version about to be cut, and the
# five module entries already hold their just-published versions. Emitting
# that row here makes the release PR carry its own row, so nothing is ever
# stale afterwards. Between releases the version DOES have a tag and this
# block emits nothing — no duplicate.
pending=$(sed -nE 's/.*"\.": *"([^"]+)".*/\1/p' .release-please-manifest.json | head -1)
if [ -n "$pending" ] && ! git rev-parse -q --verify "refs/tags/v$pending" >/dev/null; then
  manifest=$(cat .release-please-manifest.json)
  get() { printf '%s' "$manifest" | sed -nE "s/.*\"$1\": *\"([^\"]+)\".*/v\1/p" | head -1; }
  echo "| \`v$pending\` | \`$(get proto)\` | \`$(get agent)\` | \`$(get server)\` | \`$(get quarkbridge)\` | \`$(get quarkdatasource)\` |"
fi

for tag in $(git tag -l 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V -r); do
  manifest=$(git show "$tag:.release-please-manifest.json" 2>/dev/null) || continue
  get() { printf '%s' "$manifest" | sed -nE "s/.*\"$1\": *\"([^\"]+)\".*/v\1/p" | head -1; }
  echo "| \`$tag\` | \`$(get proto)\` | \`$(get agent)\` | \`$(get server)\` | \`$(get quarkbridge)\` | \`$(get quarkdatasource)\` |"
done
cat <<'FTR'

### The `quarkdatasource → orbit v1.4.3` pin

`quarkdatasource/go.mod` requires an older orbit root on purpose: the edge
is topologically forced (the datasource module lives inside the root module's
directory, so requiring the current root would be a cycle at tag-cut time).
It is harmless in practice — Go's minimal version selection raises the root
to whatever YOUR application requires, and the frozen datasource contract is
verified identical across the lagging tag by the sibling-pin guard. It does
not mean the module is unmaintained.
FTR
} > "$out"
echo "wrote $out"
