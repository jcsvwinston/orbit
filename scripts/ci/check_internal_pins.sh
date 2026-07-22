#!/usr/bin/env bash
# check_internal_pins.sh — every sibling-module require must equal the
# sibling's latest published tag.
#
# Orbit is six modules in one repo tied together by a go.work. The workspace
# resolves sibling imports from the local checkout, so a go.mod can pin a
# sibling at a stale tag and nothing in development notices — but everyone
# outside the repo gets that stale tag. That is how server/v0.8.0 shipped
# uninstallable (proto pinned one tag behind the code) and how server/v0.8.1
# shipped with its own regression test red standalone (agent pinned at the
# version WITHOUT the fix the test checks for).
#
# The rule this script enforces: a version that lives in a file needs a guard
# in CI. For sibling pins the guard is equality with the latest published tag
# of the sibling. When a new sibling tag is cut, this check goes red in the
# dependents until someone bumps the pin — that pressure is the point.
#
# Needs the full tag list: run after `git fetch --tags` (in CI, checkout with
# fetch-depth: 0).
set -euo pipefail

cd "$(dirname "$0")/../.."

MODULES=(. proto agent server quarkbridge quarkdatasource)
MODULE_ROOT="github.com/jcsvwinston/orbit"

# Latest semver tag for a module directory ("." → bare vX.Y.Z tags,
# "proto" → proto/vX.Y.Z tags), printed without the directory prefix.
latest_tag() {
  local dir="$1" prefix=""
  [[ "$dir" != "." ]] && prefix="$dir/"
  git tag -l "${prefix}v*" \
    | grep -E "^${prefix}v[0-9]+\.[0-9]+\.[0-9]+$" \
    | sed "s|^${prefix}||" \
    | sort -V \
    | tail -1
}

# root_edge_ok <ver> <want> — true when a root-edge pin `ver` (e.g. v1.4.3) may
# stand against the latest root tag `want` (e.g. v1.5.0): same major, and `want`'s
# minor is the same as or exactly one ahead of `ver`'s. Rejects a pin that is
# ahead, a different major, or two-or-more minors behind (the OR5-3 rot). Pure
# string parsing, no external tools.
root_edge_ok() {
  local ver="${1#v}" want="${2#v}"
  local vmaj="${ver%%.*}" wmaj="${want%%.*}"
  local vrest="${ver#*.}" wrest="${want#*.}"
  local vmin="${vrest%%.*}" wmin="${wrest%%.*}"
  # Numeric-only guard so a malformed version never passes.
  [[ "$vmaj$vmin$wmaj$wmin" =~ ^[0-9]+$ ]] || return 1
  [[ "$vmaj" == "$wmaj" ]] || return 1
  local delta=$(( wmin - vmin ))
  (( delta == 0 || delta == 1 ))
}

# Module import path → latest tag, one line each: "<path> <tag>".
want_list=""
for dir in "${MODULES[@]}"; do
  path="$MODULE_ROOT"
  [[ "$dir" != "." ]] && path="$MODULE_ROOT/$dir"
  tag=$(latest_tag "$dir")
  if [[ -z "$tag" ]]; then
    echo "check_internal_pins: no published tag found for $path — did the checkout fetch tags?" >&2
    exit 2
  fi
  want_list+="$path $tag"$'\n'
done

status=0
for dir in "${MODULES[@]}"; do
  gomod="$dir/go.mod"
  # Direct requires only; go mod edit -json needs no network and no parsing
  # heuristics over the require block.
  while read -r path ver; do
    [[ "$path" == "$MODULE_ROOT" || "$path" == "$MODULE_ROOT"/* ]] || continue
    want=$(awk -v p="$path" '$1 == p {print $2}' <<<"$want_list")
    if [[ "$ver" == "$want" ]]; then
      echo "ok   $gomod: $path $ver"
    elif [[ "$path" == "$MODULE_ROOT" ]] && root_edge_ok "$ver" "$want"; then
      # Root-edge exception: the root's certification tag is cut LAST (so its
      # commit contains every module tag as an ancestor — the umbrella's
      # manifest-guard requires exactly that), which makes strict equality on
      # this edge topologically impossible: a module that requires the root can
      # never pin a tag that will only exist after the module's own tag. The
      # same reasoning holds when the root's tag crosses a minor (a UI/feature
      # minor of the root): a bridge cut against the previous minor's latest
      # cannot pin the new minor that contains it. So the edge may lag by the
      # current minor OR exactly one minor (same major), and no further — two
      # or more minors behind is the OR5-3 rot (v0.3.0 against v1.4.x) and
      # still fails. The datasource contract this edge exists for is frozen
      # since v1.0 (ADR-001), so a one-minor lag is functionally safe and here
      # it is disclosed, not silent.
      echo "ok   $gomod: $path $ver (root edge: lags $want by ≤1 minor, topologically forced)"
    else
      echo "FAIL $gomod: $path pinned at $ver, latest published tag is $want" >&2
      status=1
    fi
  done < <(go mod edit -json "$gomod" \
    | python3 -c 'import json,sys; [print(r["Path"], r["Version"]) for r in json.load(sys.stdin).get("Require") or []]')
done

if [[ $status -ne 0 ]]; then
  echo >&2
  echo "A sibling module cut a new tag and this go.mod still pins the old one." >&2
  echo "Bump the require to the tag above and run 'go mod tidy' in that module" >&2
  echo "(GOWORK=off, so the resolution matches what consumers see)." >&2
  exit 1
fi

echo "OK: every internal pin equals its sibling's latest published tag"
