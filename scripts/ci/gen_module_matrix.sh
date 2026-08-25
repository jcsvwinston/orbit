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
# NOTA sobre la ventana rancia (y sobre un arreglo que NO funciona).
#
# Las filas salen de los tags, y un tag no existe hasta que la release se
# fusiona: en el instante del corte, la copia commiteada en main queda
# desfasada, y el check de frescura —que solo corre en PRs— duerme hasta el
# PR siguiente.
#
# El arreglo "obvio" es leer también .release-please-manifest.json para que el
# PR de release lleve su propia fila. Se probó y es PEOR: en el PR de release
# el manifiesto ya dice la versión nueva, así que el generador pide una fila
# que el fichero commiteado no tiene y el PR se pone ROJO — y no se puede
# arreglar, porque empujar un commit a una rama de release-please la deja sin
# disparar CI. Se cambia una ventana rancia inofensiva por una release
# bloqueada.
#
# Se asume, por tanto, la ventana: dura de un tag al PR siguiente, el check la
# caza siempre, y cerrarla es regenerar y commitear. Es un aviso legítimo, no
# ruido.

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
