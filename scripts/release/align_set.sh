#!/usr/bin/env bash
# align_set.sh — the WRITER that closes orbit's alignment train in ONE commit.
#
# Orbit is six modules in one repo (root, proto, agent, server, quarkbridge,
# quarkdatasource), each pinning nucleus/quark by tag in its own go.mod.
# When the umbrella certifies a new set, every one of those pins — the ROOT's
# included, which is the one that got forgotten in the 1.24.0 train and cost
# an extra root tag — has to move to the new nucleus/quark tags, the internal
# sibling pins (proto in agent/server, agent in server) have to match their
# latest published tags, and each touched module needs a `GOWORK=off go mod
# tidy`. Done by hand that is up to a dozen edits spread over several commits,
# and release-please turns every stray commit into another tag. This script
# writes ALL of it as a single conventional commit.
#
# What it aligns, per module go.mod (direct requires only):
#   - github.com/jcsvwinston/nucleus  → the target tag (flag or manifest)
#   - github.com/jcsvwinston/quark    → the target tag (flag or manifest)
#   - github.com/jcsvwinston/orbit/*  → the sibling's latest PUBLISHED tag
#     (same rule check_internal_pins.sh enforces; run `git fetch --tags` first)
#   - github.com/jcsvwinston/orbit    → the root's latest published tag, with
#     the one topologically forced exception: quarkdatasource may keep a pin
#     in the SAME minor as the latest root tag (its tag is cut before the
#     root's certification tag, so a patch-level lag there is structural —
#     see check_internal_pins.sh). A pin a full minor behind IS rewritten:
#     the guard only tolerates that lag with a warning, and letting it stand
#     is how it rotted to two minors in OR5-3.
#
# Usage:
#   bash scripts/release/align_set.sh --nucleus vX.Y.Z --quark vX.Y.Z
#   bash scripts/release/align_set.sh --manifest ../quantum/versions.yaml
#     # reads modules.quark / modules.nucleus from the umbrella's versions.yaml
#
# Modes (combine with either target form above):
#   (default)   rewrite pins, tidy each touched module, commit ONCE as
#               `fix(deps): align <modules> to the set (nucleus vX, quark vY)`
#               — `fix` on purpose: the alignment must be releasable so the
#               release-please cascade can cut the re-pinned modules.
#   --no-commit rewrite + tidy, leave the commit to the caller
#   --dry-run   print the exact go.mod diff (go.sum/tidy effects excluded),
#               touch nothing, exit 0
#   --check     like --dry-run but exit 1 if any pin is misaligned — the
#               local-CI form: `align_set.sh --manifest ... --check`
#
# Needs the full tag list for the sibling pins (git fetch --tags) and network
# for `go mod tidy` in write mode (the target tags must be published).
#
# One MVS note: `go mod tidy` may raise a pin ABOVE the target when a sibling
# tag already requires a newer version (e.g. server cannot hold nucleus below
# what the published agent tag requires). That only bites when aligning
# DOWNWARD, which the release train never does; aligning to a new set the
# tidied result equals the target.
set -euo pipefail

cd "$(dirname "$0")/../.."

OWNER="jcsvwinston"
MODULE_ROOT="github.com/$OWNER/orbit"
MODULES=(. proto agent server quarkbridge quarkdatasource)
# The one module whose root pin may sit a patch behind (see header).
DATASOURCE_EDGE_MODULE="quarkdatasource"

NUCLEUS=""
QUARK=""
MANIFEST=""
MODE="write"   # write | no-commit | dry-run | check

# The whole header comment above IS the manual; print it verbatim.
usage() { sed -n '2,/^set -euo/p' "$0" | grep '^#' | sed -e 's/^# \{0,1\}//'; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --nucleus)  NUCLEUS="${2:?--nucleus needs a version}"; shift 2 ;;
    --quark)    QUARK="${2:?--quark needs a version}"; shift 2 ;;
    --manifest) MANIFEST="${2:?--manifest needs a path}"; shift 2 ;;
    --dry-run)  MODE="dry-run"; shift ;;
    --check)    MODE="check"; shift ;;
    --no-commit) MODE="no-commit"; shift ;;
    -h|--help)  usage; exit 0 ;;
    *) echo "FAIL: unknown argument '$1' (see --help)" >&2; exit 2 ;;
  esac
done

# --- Targets: flags win; the manifest fills whatever the flags left empty. ---
manifest_module_version() {
  # Prints the quoted value of modules.<key> from the umbrella's versions.yaml.
  # Deliberately the same shape the manifest-guard reads: a two-space-indented
  # `<key>: "vX.Y.Z"` line inside the top-level `modules:` block.
  local key="$1"
  awk -v key="$key:" '
    /^modules:/          { inb = 1; next }
    inb && /^[^[:space:]]/ { inb = 0 }
    inb && $1 == key     { gsub(/"/, "", $2); print $2; exit }
  ' "$MANIFEST"
}

if [[ -n "$MANIFEST" ]]; then
  [[ -f "$MANIFEST" ]] || { echo "FAIL: manifest not found: $MANIFEST" >&2; exit 2; }
  [[ -n "$NUCLEUS" ]] || NUCLEUS=$(manifest_module_version nucleus)
  [[ -n "$QUARK"   ]] || QUARK=$(manifest_module_version quark)
fi

if [[ -z "$NUCLEUS" || -z "$QUARK" ]]; then
  echo "FAIL: need target versions for nucleus AND quark (--nucleus/--quark or --manifest)" >&2
  exit 2
fi
for v in "$NUCLEUS" "$QUARK"; do
  [[ "$v" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] \
    || { echo "FAIL: '$v' is not a vX.Y.Z version" >&2; exit 2; }
done

# --- Sibling targets: latest published tag per module dir (as the guard). ---
latest_tag() {
  local dir="$1" prefix=""
  [[ "$dir" != "." ]] && prefix="$dir/"
  git tag -l "${prefix}v*" \
    | grep -E "^${prefix}v[0-9]+\.[0-9]+\.[0-9]+$" \
    | sed "s|^${prefix}||" \
    | sort -V \
    | tail -1
}

# "path tag" per line; bash 3.2 on macOS has no associative arrays, so the
# lookup is the same awk-over-a-list check_internal_pins.sh uses.
sibling_list=""
for dir in "${MODULES[@]}"; do
  path="$MODULE_ROOT"; [[ "$dir" != "." ]] && path="$MODULE_ROOT/$dir"
  tag=$(latest_tag "$dir")
  if [[ -z "$tag" ]]; then
    echo "FAIL: no published tag found for $path — run 'git fetch --tags' first" >&2
    exit 2
  fi
  sibling_list+="$path $tag"$'\n'
done

sibling_latest() { awk -v p="$1" '$1 == p {print $2}' <<<"$sibling_list"; }

# same_minor <ver> <want> — true when both are the same major.minor.
same_minor() {
  local a="${1#v}" b="${2#v}"
  [[ "${a%.*}" == "${b%.*}" ]]
}

# want_for <consumer-dir> <require-path> <current-ver> — prints the version the
# pin should hold, or nothing when the path is not one this script manages.
want_for() {
  local dir="$1" path="$2" ver="$3"
  case "$path" in
    "github.com/$OWNER/nucleus") echo "$NUCLEUS" ;;
    "github.com/$OWNER/quark")   echo "$QUARK" ;;
    "$MODULE_ROOT")
      local want
      want=$(sibling_latest "$MODULE_ROOT")
      if [[ "$dir" == "$DATASOURCE_EDGE_MODULE" ]] && same_minor "$ver" "$want"; then
        echo "$ver"   # structural patch-lag on the root↔quarkdatasource edge
      else
        echo "$want"
      fi ;;
    "$MODULE_ROOT"/*)            sibling_latest "$path" ;;
  esac
}

# --- Walk the six go.mod files and collect / apply the pin moves. ---
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

changed_files=()
changed_mods=()
changed_any=0

for dir in "${MODULES[@]}"; do
  gomod="$dir/go.mod"
  shadow="$tmpdir/${dir//\//_}.go.mod"
  cp "$gomod" "$shadow"
  changed_here=0

  while read -r path ver; do
    [[ "$path" == github.com/$OWNER/* ]] || continue
    want=$(want_for "$dir" "$path" "$ver")
    [[ -n "$want" && "$want" != "$ver" ]] || continue
    echo "align: $gomod: $path $ver -> $want"
    changed_here=1
    changed_any=1
    go mod edit -require="$path@$want" "$shadow"
  done < <(go mod edit -json "$gomod" \
    | python3 -c 'import json,sys; [print(r["Path"], r["Version"]) for r in json.load(sys.stdin).get("Require") or []]')

  if [[ $changed_here -eq 1 ]]; then
    if [[ "$MODE" == "dry-run" || "$MODE" == "check" ]]; then
      diff -u --label "a/$gomod" --label "b/$gomod" "$gomod" "$shadow" || true
    else
      cp "$shadow" "$gomod"
      echo "tidy: $dir (GOWORK=off go mod tidy)"
      (cd "$dir" && GOWORK=off go mod tidy)
      changed_files+=("$gomod")
      [[ -f "$dir/go.sum" ]] && changed_files+=("$dir/go.sum")
      changed_mods+=("$([[ "$dir" == "." ]] && echo root || echo "$dir")")
    fi
  fi
done

if [[ $changed_any -eq 0 ]]; then
  echo "OK: nothing to do — every pin already matches the set (nucleus $NUCLEUS, quark $QUARK)"
  exit 0
fi

case "$MODE" in
  check)
    echo "FAIL: pins above are misaligned with the set (nucleus $NUCLEUS, quark $QUARK)" >&2
    exit 1 ;;
  dry-run)
    echo "dry-run: no files written, nothing committed (tidy/go.sum effects not shown)"
    exit 0 ;;
  no-commit)
    echo "Done (no commit). Touched: ${changed_mods[*]}. Review the diff and commit as:"
    echo "  fix(deps): align ${changed_mods[*]} to the set (nucleus $NUCLEUS, quark $QUARK)"
    exit 0 ;;
esac

# ONE commit, only the files this script touched — a dirty tree or a staged
# index elsewhere is left exactly as it was.
mods_list=$(IFS=,; echo "${changed_mods[*]}" | sed 's/,/, /g')
msg="fix(deps): align $mods_list to the set (nucleus $NUCLEUS, quark $QUARK)"
git commit -m "$msg" -- "${changed_files[@]}"
echo "OK: committed — $msg"
