#!/usr/bin/env bash
# fuzz.sh — run Orbit's native Go fuzz targets, either as a seed-corpus test
# (no argument) or as a timed fuzzing session (one argument, a Go duration).
#
#   bash scripts/ci/fuzz.sh          # every seed, deterministic, ~1s
#   bash scripts/ci/fuzz.sh 45s      # 45s of mutation per target
#
# The seed form is what CI runs on every pull request. The timed form is what
# the weekly Fuzz workflow runs, and what to run locally before touching one of
# these parsing surfaces — it is deliberately NOT on the PR lane: `go test
# -fuzz` rebuilds the package AND every non-stdlib dependency with coverage
# instrumentation, which for internal/admin alone measured 20s wall (102s CPU)
# on a 12-core machine with the ordinary build already cached. Four packages of
# that is minutes of rebuilding before the first mutation.
#
# A new target belongs in TARGETS below, or nothing runs it.
#
# If a timed run dies with "open …/go-build/fuzz/…: no such file or directory",
# the corpus cache has index entries whose files are gone: `go clean -fuzzcache`
# and run again. It is an infrastructure failure, not a finding.
set -euo pipefail

cd "$(dirname "$0")/../.."

# module<TAB>package<TAB>target. The module is the go.mod directory (the
# workspace resolves the rest); the package is relative to it.
TARGETS=$(
  cat <<'EOF'
.	./internal/admin	FuzzDataStudioQuery
.	./internal/admin	FuzzRecordIDBoundary
.	./internal/admin	FuzzImportValidation
quarkdatasource	.	FuzzEscapeLike
server	./routing	FuzzNodeIDMatches
agent	./datastudio	FuzzParseID
EOF
)

# The list above is the whole list. A fuzz target that is not in it is a
# target the weekly timed lane never runs — it would only ever see its own
# seeds, which is a unit test wearing a fuzzing hat.
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

FUZZTIME="${1:-}"
count=0

while IFS=$'\t' read -r module pkg target; do
  [ -n "$module" ] || continue
  count=$((count + 1))
  if [ -z "$FUZZTIME" ]; then
    echo "==> seed corpus: $target ($module/$pkg)"
    ( cd "$module" && go test -run "^${target}\$" -count=1 "$pkg" )
  else
    echo "==> fuzzing $target for $FUZZTIME ($module/$pkg)"
    ( cd "$module" && go test -run '^$' -fuzz="^${target}\$" -fuzztime="$FUZZTIME" "$pkg" )
  fi
done <<< "$TARGETS"

echo "OK: $count fuzz targets"
