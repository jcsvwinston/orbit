#!/usr/bin/env bash
# fuzz.sh — run Orbit's native Go fuzz targets, either as a seed-corpus test
# (no duration) or as a timed fuzzing session (a Go duration).
#
#   bash scripts/ci/fuzz.sh                        # every seed, deterministic, ~1s
#   bash scripts/ci/fuzz.sh 45s                    # 45s of mutation per target
#   bash scripts/ci/fuzz.sh --only FuzzEscapeLike 45s
#   bash scripts/ci/fuzz.sh --list                 # the target names, as JSON
#
# The seed form is what CI runs on every pull request. The timed form is what
# the weekly Fuzz workflow runs (one target per matrix job), and what to run
# locally before touching one of these parsing surfaces — it is deliberately
# NOT on the PR lane: `go test -fuzz` rebuilds the package AND every non-stdlib
# dependency with coverage instrumentation, which for internal/admin alone
# measured 20s wall (102s CPU) on a 12-core machine with the ordinary build
# already cached. Four packages of that is minutes of rebuilding before the
# first mutation.
#
# A new target belongs in TARGETS below, or nothing runs it.
#
# ONE TARGET'S FAILURE DOES NOT SKIP THE REST. This script used to run under
# `set -e`, so the first non-zero `go test` ended the run and every later
# target went unfuzzed — and the failure that ends a run is not always a
# finding: a timed run can die with
#
#   open …/go-build/fuzz/…: no such file or directory
#
# which is the fuzz corpus cache having index entries whose files are gone.
# That one is retried once after `go clean -fuzzcache`; anything else is
# recorded and the list carries on, with a summary and a non-zero exit at the
# end. The failure was seen twice by a reviewer on darwin/arm64 (go1.26.6) at
# targets 2 and 4 of a full list, including on a run that started from an
# empty cache; three full-list runs here (5s, 20s, 45s per target, same
# toolchain and platform) did not reproduce it, so what is verified is the
# recovery and not the absence — the retry and the carry-on were exercised
# against an injected failure of each kind. The weekly workflow gives every
# target its own job for the same reason.
set -uo pipefail

cd "$(dirname "$0")/../.."

# module<TAB>package<TAB>target. The module is the go.mod directory (the
# workspace resolves the rest); the package is relative to it.
TARGETS=$(
  cat <<'EOF'
.	./internal/admin	FuzzDataStudioQuery
.	./internal/admin	FuzzRecordIDBoundary
.	./internal/admin	FuzzImportValidation
quarkdatasource	.	FuzzEscapeLike
server	./routing	FuzzEventFilterMatches
agent	./datastudio	FuzzParseID
EOF
)

# The list above is the whole list, in both directions: a target in the tree
# that is missing from it is a target the weekly lane never runs (it would
# only ever see its own seeds, which is a unit test wearing a fuzzing hat),
# and a target listed here that is no longer in the tree is worse — `go test
# -fuzz=^Gone$` exits 0 without fuzzing anything, so a rename would quietly
# empty the lane.
declared=$(printf '%s\n' "$TARGETS" | awk -F'\t' 'NF{print $3}' | sort -u)
found=$(git ls-files --cached --others --exclude-standard '*_test.go' |
  xargs grep -hoE 'func Fuzz[A-Za-z0-9_]+\(f \*testing\.F\)' 2>/dev/null |
  sed -E 's/^func //; s/\(.*$//' | sort -u || true)
if [ "$declared" != "$found" ]; then
  echo "scripts/ci/fuzz.sh: the TARGETS list and the tree disagree." >&2
  echo "  listed here : $(echo "$declared" | tr '\n' ' ')" >&2
  echo "  in the tree : $(echo "$found" | tr '\n' ' ')" >&2
  echo "Add the missing target to TARGETS (module, package, name) and try again." >&2
  exit 1
fi

ONLY=""
if [ "${1:-}" = "--list" ]; then
  # The weekly workflow builds its matrix from this, so the list lives in one
  # place. One JSON array of target names, on one line.
  printf '%s\n' "$TARGETS" | awk -F'\t' 'NF{printf "%s\"%s\"", (n++ ? "," : "["), $3} END{print "]"}'
  exit 0
fi
if [ "${1:-}" = "--only" ]; then
  ONLY="${2:-}"
  if [ -z "$ONLY" ]; then
    echo "scripts/ci/fuzz.sh: --only needs a target name" >&2
    exit 2
  fi
  shift 2
fi

FUZZTIME="${1:-}"
count=0
failed=()
retried=()

# run_one runs a target once, streaming its output and returning go test's
# status through the tee.
run_one() {
  local module=$1 pkg=$2 target=$3 log=$4
  if [ -z "$FUZZTIME" ]; then
    ( cd "$module" && go test -run "^${target}\$" -count=1 "$pkg" ) 2>&1 | tee "$log"
  else
    ( cd "$module" && go test -run '^$' -fuzz="^${target}\$" -fuzztime="$FUZZTIME" "$pkg" ) 2>&1 | tee "$log"
  fi
  return "${PIPESTATUS[0]}"
}

# is_corpus_cache_error reports whether a run died on the fuzz corpus cache
# rather than on a property.
is_corpus_cache_error() {
  grep -qE 'open .*[/\\]fuzz[/\\].*: no such file or directory' "$1"
}

# An explicit template: GNU mktemp refuses "-t name" without the X's, BSD
# mktemp does not, and this lane runs on both.
log=$(mktemp "${TMPDIR:-/tmp}/orbit-fuzz.XXXXXX")
trap 'rm -f "$log"' EXIT

while IFS=$'\t' read -r module pkg target; do
  [ -n "$module" ] || continue
  if [ -n "$ONLY" ] && [ "$ONLY" != "$target" ]; then
    continue
  fi
  count=$((count + 1))
  if [ -z "$FUZZTIME" ]; then
    echo "==> seed corpus: $target ($module/$pkg)"
  else
    echo "==> fuzzing $target for $FUZZTIME ($module/$pkg)"
  fi

  if run_one "$module" "$pkg" "$target" "$log"; then
    continue
  fi

  if is_corpus_cache_error "$log"; then
    echo "--> $target died on the fuzz corpus cache, not on a property; clearing it and running the target again." >&2
    go clean -fuzzcache || true
    if run_one "$module" "$pkg" "$target" "$log"; then
      retried+=("$target")
      continue
    fi
    if is_corpus_cache_error "$log"; then
      echo "--> $target hit the corpus cache twice; recording it as infrastructure, not as a finding." >&2
    fi
  fi

  failed+=("$target")
done <<< "$TARGETS"

if [ "$count" -eq 0 ]; then
  echo "scripts/ci/fuzz.sh: no target matched --only ${ONLY}" >&2
  exit 2
fi

if [ ${#retried[@]} -gt 0 ]; then
  echo "NOTE: cleared the fuzz corpus cache and re-ran: ${retried[*]}"
fi

if [ ${#failed[@]} -gt 0 ]; then
  echo "FAIL: ${#failed[@]} of $count fuzz targets: ${failed[*]}" >&2
  exit 1
fi

if [ "$count" -eq 1 ]; then
  echo "OK: 1 fuzz target"
else
  echo "OK: $count fuzz targets"
fi
