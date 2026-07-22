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

# The ONE module directory whose edge to the root is topologically forced to
# lag, and therefore the ONLY consumer that may benefit from the ≤1-minor
# root-edge exception below. quarkdatasource requires the root for its frozen
# `datasource` contract package (ADR-001) and its own tag is cut before the
# root's certification tag, so it can never pin a root tag that only exists
# after it. No other module requires the root today; if one ever does, it gets
# NO lag tolerance — it must pin exactly, or the exception is widened
# deliberately (a declared change, not a silent one).
DATASOURCE_EDGE_MODULE="quarkdatasource"

# The frozen public-API baseline the root-edge exception rests on. The ≤1-minor
# lag is only safe while the `datasource` contract quarkdatasource implements is
# byte-for-byte identical between the lagged tag and the current one — that is
# the ADR-001 D-freeze promise, guarded on HEAD by contracts/freeze_test.go.
# We do not merely ASSUME the freeze held across the lag; datasource_surface_
# frozen_across() verifies it from the two tags' recorded baselines. Cheap:
# two `git show`s over an already-fetched tag list, no build and no network.
CONTRACT_BASELINE="contracts/baseline/api_exported_symbols.txt"
CONTRACT_PACKAGE_PREFIX="$MODULE_ROOT/datasource "

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
# string parsing, no external tools. This is a NECESSARY condition, not a
# sufficient one: the caller consults it ONLY for the root↔quarkdatasource edge
# (see DATASOURCE_EDGE_MODULE) and ALSO requires the contract freeze to hold
# (datasource_surface_frozen_across) before letting a lag stand.
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

# datasource_surface_frozen_across <ver> <want> — true when the frozen
# `datasource` contract recorded at tag `ver` is identical to the one at tag
# `want`. This is the check that turns "we ASSUME ADR-001 held across the lag"
# into "we VERIFIED it": if the datasource surface drifted between the pinned
# root tag and the current one, the ≤1-minor lag is no longer functionally
# safe and the pin must be bumped, freeze exception or not. Fails CLOSED — if
# either tag's baseline cannot be read, the freeze cannot be confirmed, so the
# lag is not allowed to stand. Requires the tags to be present (the script's
# `git fetch --tags` premise); no build, no network.
datasource_surface_frozen_across() {
  local ver="$1" want="$2" at_ver at_want
  at_ver=$(git show "$ver:$CONTRACT_BASELINE" 2>/dev/null | grep -F "$CONTRACT_PACKAGE_PREFIX") || true
  at_want=$(git show "$want:$CONTRACT_BASELINE" 2>/dev/null | grep -F "$CONTRACT_PACKAGE_PREFIX") || true
  if [[ -z "$at_ver" || -z "$at_want" ]]; then
    echo "check_internal_pins: cannot read the frozen datasource baseline ($CONTRACT_BASELINE) at $ver or $want — cannot confirm the ADR-001 freeze the root-edge lag rests on" >&2
    return 1
  fi
  [[ "$at_ver" == "$at_want" ]]
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
    elif [[ "$path" == "$MODULE_ROOT" && "$dir" == "$DATASOURCE_EDGE_MODULE" ]] \
         && root_edge_ok "$ver" "$want"; then
      # Root-edge exception, NARROWED to the one edge that needs it —
      # root↔quarkdatasource (identified by the consumer directory `$dir`, not
      # merely by the required path being the root). Why this edge and no
      # other: the root's certification tag is cut LAST (so its commit contains
      # every module tag as an ancestor — the umbrella's manifest-guard
      # requires exactly that), which makes strict equality on it topologically
      # impossible: quarkdatasource, whose own tag is cut first, can never pin
      # a root tag that will only exist afterward. The same holds when the
      # root's tag crosses a minor (a UI/feature minor): a module cut against
      # the previous minor's latest cannot pin the new minor that contains it.
      # So this edge may lag by the current minor OR exactly one minor (same
      # major) and no further — two-or-more minors behind is the OR5-3 rot
      # (v0.3.0 against v1.4.x) and still fails.
      #
      # The lag is only SAFE because the datasource contract this edge exists
      # for is frozen since v1.0 (ADR-001, guarded by contracts/freeze_test.go).
      # We do not take that on faith: datasource_surface_frozen_across verifies
      # the contract surface is byte-identical at the lagged tag and the current
      # one. If it drifted, the exception is void and the pin must be bumped.
      #
      # Any OTHER module that pins the root has NO topological reason to lag, so
      # it falls through to the strict-equality FAIL below — as does a
      # root↔quarkdatasource pin that is ≥2 minors behind, a different major, or
      # ahead. This is the MAQ-3 narrowing.
      if datasource_surface_frozen_across "$ver" "$want"; then
        echo "ok   $gomod: $path $ver (root↔$DATASOURCE_EDGE_MODULE edge: lags $want by ≤1 minor, topologically forced; datasource contract frozen ADR-001, verified identical at both tags)"
      else
        echo "FAIL $gomod: $path pinned at $ver — the root↔$DATASOURCE_EDGE_MODULE ≤1-minor lag is only allowed while the datasource contract is frozen (ADR-001), but its surface differs from $want. Bump the pin and 'go mod tidy'." >&2
        status=1
      fi
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
