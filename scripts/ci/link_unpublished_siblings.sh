#!/usr/bin/env bash
# link_unpublished_siblings.sh <module dir> [--drop] — point a module's
# requirements on sibling modules whose tag does NOT exist yet at the tree.
#
# The standalone lanes run with GOWORK=off on purpose: a consumer's toolchain
# resolves every requirement from the proxy, and building each module that
# way is the only proof that its go.mod is honest. That proof has one blind
# spot, and it is the moment a module is born. A module that leaves the root
# (ADR-012: `datasource`) cannot coexist with any root tag that still
# contains it — Go refuses with `ambiguous import` — so the root and every
# other consumer must name the tag the cut is about to create. That tag
# exists only after the merge; the lane that must approve the merge runs
# before it, and `go build`/`go mod tidy` fail with:
#
#   github.com/jcsvwinston/orbit/datasource@v1.0.0: reading .../go.mod at
#   revision datasource/v1.0.0: unknown revision datasource/v1.0.0
#
# A workspace with a versioned replace covers `go build`, but `go mod tidy`
# ignores the workspace and asks the proxy anyway. What covers every command
# is a directory `replace` in the module's own go.mod, added for the duration
# of the lane and dropped before the tidy diff. It is added ONLY for a
# sibling requirement whose tag is absent from the tree's tag list: once the
# cut publishes the tag, this script adds nothing and the lane is exactly
# `GOWORK=off` again. A replace is never committed (`go install` refuses a
# published module that carries one).
#
# Usage:
#   bash scripts/ci/link_unpublished_siblings.sh <module dir>          # add
#   bash scripts/ci/link_unpublished_siblings.sh <module dir> --drop   # remove
#
# Needs the full tag list (fetch-depth: 0 or `git fetch --tags`).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

module="${1:?module dir}"
mode="${2:-add}"
MODULE_ROOT="github.com/jcsvwinston/orbit"
repo="$(pwd)"

# Sibling import path → directory. The root is "."; the others are their
# path relative to the module root.
sibling_dir() {
  local path="$1"
  if [[ "$path" == "$MODULE_ROOT" ]]; then echo "."; else echo "${path#"$MODULE_ROOT"/}"; fi
}

# tag_exists <dir> <version> — the tag the cut would create for that pin.
tag_exists() {
  local dir="$1" ver="$2" prefix=""
  [[ "$dir" != "." ]] && prefix="$dir/"
  git tag -l "${prefix}${ver}" | grep -qx "${prefix}${ver}"
}

gomod="$module/go.mod"
[[ -f "$gomod" ]] || { echo "link_unpublished_siblings: no $gomod" >&2; exit 2; }

linked=0
while read -r path ver; do
  [[ "$path" == "$MODULE_ROOT" || "$path" == "$MODULE_ROOT"/* ]] || continue
  dir="$(sibling_dir "$path")"
  [[ -f "$dir/go.mod" ]] || continue
  if [[ "$mode" == "--drop" ]]; then
    (cd "$module" && go mod edit -dropreplace "$path")
    continue
  fi
  if tag_exists "$dir" "$ver"; then
    continue
  fi
  echo "link_unpublished_siblings: $gomod requires $path $ver, whose tag does not exist yet — replacing with the tree for this lane"
  (cd "$module" && go mod edit -replace "$path=$repo/$dir")
  linked=$((linked + 1))
done < <(go mod edit -json "$gomod" \
  | python3 -c 'import json,sys; [print(r["Path"], r["Version"]) for r in json.load(sys.stdin).get("Require") or []]')

if [[ "$mode" != "--drop" && $linked -eq 0 ]]; then
  echo "link_unpublished_siblings: every sibling $module requires is published; nothing linked"
fi
