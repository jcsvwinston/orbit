#!/usr/bin/env bash
# check_versioned_docs_markers.sh — a snapshot announces ITS OWN version.
#
# Orbit versions its docs the same way nucleus does (website/versioned_docs/
# version-X.Y.Z), and had the same defect with no guard to catch it: the
# snapshots for v1.7.0 and v1.8.0 announced "the current release is v1.6.7"
# and "v1.7.4" respectively — the version marker was copied forward stale at
# cut time and never corrected, and the pages still said so in production
# (/orbit/1.7.0/, /orbit/1.8.0/). check_docs_version_claims.sh only inspects
# website/docs/ (the current tree), never versioned_docs/. This is that
# missing guard, ported from nucleus's, adapted to orbit's HTML marker
# (`<!-- x-release-please-version -->`).
#
# Two teeth:
#  1. every line carrying the marker must announce the snapshot's version;
#  2. every "current [tagged] release is vX.Y.Z" DECLARATION must too, with
#     or without the marker — the present tense ("is") separates a state
#     declaration from the release-notes prose that recounts history in the
#     past tense and must not match.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

[ -d website/versioned_docs ] || { echo "OK: this site does not version documentation."; exit 0; }

status=0
for dir in website/versioned_docs/version-*; do
  version="${dir##*/version-}"

  # Tooth 1: marker lines.
  while IFS= read -r file; do
    while IFS= read -r line; do
      claimed=$(printf '%s\n' "$line" | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)
      if [ -z "$claimed" ]; then
        echo "FAIL: $file: marker line carries no version: $line" >&2
        status=1
      elif [ "$claimed" != "v$version" ]; then
        echo "FAIL: $file claims $claimed, but it is the v$version snapshot — an archived version's docs cannot announce another" >&2
        status=1
      fi
    done < <(grep "x-release-please-version" "$file")
  done < <(grep -rl "x-release-please-version" "$dir" 2>/dev/null || true)

  # Tooth 2: "current [tagged] release is vX.Y.Z" declarations, marker or not.
  while IFS= read -r file; do
    while IFS= read -r line; do
      claimed=$(printf '%s\n' "$line" | grep -oiE 'current( tagged)? release is[^v]*v[0-9]+\.[0-9]+\.[0-9]+' | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)
      [ -z "$claimed" ] && continue
      if [ "$claimed" != "v$version" ]; then
        echo "FAIL: $file declares «current release is $claimed» in the v$version snapshot — a snapshot cannot announce another version as current (marker or not)" >&2
        status=1
      fi
    done < <(grep -riE 'current( tagged)? release is' "$file" 2>/dev/null || true)
  done < <(grep -rilE 'current( tagged)? release is' "$dir" 2>/dev/null || true)
done

[ $status -eq 0 ] && echo "OK: every documentation snapshot announces its own version ($(ls -d website/versioned_docs/version-* | wc -l | tr -d ' ') snapshots)"
exit $status
