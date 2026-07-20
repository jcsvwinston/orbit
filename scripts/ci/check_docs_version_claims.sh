#!/usr/bin/env bash
# check_docs_version_claims.sh — the version the website claims must be the
# version actually released.
#
# Orbit was the only repo in the suite without this guard, and it showed: the
# published docs said "the current tagged release is v1.2.1" while the real
# tag was v1.4.1 — three minors of drift, updated by hand exactly never
# (QM5-3, 5ª auditoría).
#
# Contract: the released root version lives in .release-please-manifest.json
# under ".". Every line in the files below carrying an
# `x-release-please-version` marker must claim exactly that version, and each
# file must contain at least one marker (losing the annotation silently would
# disarm the updater AND this check). release-please's generic updater
# rewrites the marker lines on each root release PR (extra-files in
# release-please-config.json).
set -euo pipefail

cd "$(dirname "$0")/../.."

manifest_version=$(sed -n 's/.*"\.": *"\([^"]*\)".*/\1/p' .release-please-manifest.json)
if [[ -z "$manifest_version" ]]; then
  echo "FAIL: could not read version for '.' from .release-please-manifest.json" >&2
  exit 1
fi

files=(README.md CLAUDE.md website/docs/intro.md website/docs/quick-start.md website/docs/reference/release-notes.md)
status=0

for f in "${files[@]}"; do
  if ! grep -q "x-release-please-version" "$f"; then
    echo "FAIL: $f has no x-release-please-version marker — the version claim lost its updater annotation" >&2
    status=1
    continue
  fi
  while IFS= read -r line; do
    claimed=$(printf '%s\n' "$line" | grep -o 'v[0-9][0-9.]*[0-9]' | head -1 || true)
    if [[ -z "$claimed" ]]; then
      echo "FAIL: $f: marker line carries no version string: $line" >&2
      status=1
    elif [[ "$claimed" != "v$manifest_version" ]]; then
      echo "FAIL: $f claims $claimed but the released version is v$manifest_version: $line" >&2
      status=1
    fi
  done < <(grep "x-release-please-version" "$f")
done

# Content, not just the claim: the release notes must DOCUMENT the released
# version, not merely name it. "The current release is vX" sitting above a
# changelog whose first section is vX-1 is the same drift with better
# camouflage — the marker line auto-updates on every release, the sections
# do not (v1.4.3 shipped with release-notes.md starting at v1.4.2, OR7-1).
notes="website/docs/reference/release-notes.md"
esc_version=$(printf '%s' "$manifest_version" | sed 's/\./\\./g')
if ! grep -qE "^## v${esc_version}([^0-9]|$)" "$notes"; then
  echo "FAIL: $notes claims v$manifest_version but has no '## v$manifest_version' section documenting it" >&2
  status=1
fi

if [[ $status -eq 0 ]]; then
  echo "OK: version claims in ${files[*]} match v$manifest_version, and $notes documents it"
fi
exit $status
