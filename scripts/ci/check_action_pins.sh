#!/usr/bin/env bash
# check_action_pins.sh — every GitHub Action the workflows under
# .github/workflows/ run is pinned to a commit SHA, with its tag written next
# to it.
#
# A tag is a moving pointer owned by someone else: `actions/checkout@v7` runs
# whatever the owner of that tag decides to publish under it, in a job that
# has this repository checked out. Orbit's jobs are not all cheap targets:
# release.yml signs artifacts with cosign and attests build provenance,
# codeql.yml and scorecard.yml upload SARIF with `security-events: write`,
# pages.yml deploys the documentation site, and release-please.yml opens
# release pull requests. A 40-hex SHA is immutable, which is what OpenSSF
# Scorecard's Pinned-Dependencies check asks for and, more to the point, what
# keeps "which code ran in that release" answerable after the fact.
#
# The trailing `# vX.Y.Z` comment is not decoration: it is what makes the pin
# readable and what Dependabot rewrites when it proposes the next SHA (the
# github-actions ecosystem is declared in .github/dependabot.yml, whose own
# comment spells out why deleting it fossilises every pin here). A pin with no
# bot behind it is worse than a tag — it freezes the action on the day it was
# written, security fixes included — so the comment and the Dependabot entry
# are part of the rule, not a nicety.
#
# Orbit's pins were placed by hand and, unlike its two siblings, had no guard:
# the convention held by review alone, and the next hand-written
# `uses: actions/checkout@v7` is what this check exists to catch.
#
# Usage: bash scripts/ci/check_action_pins.sh
set -uo pipefail
# Fail loudly rather than checking whatever tree the caller happened to be
# standing in: a guard that silently inspects the wrong directory reports
# a pass it never verified.
cd "$(dirname "${BASH_SOURCE[0]}")/../.." || exit 1

status=0
found=0

while IFS= read -r line; do
  file=${line%%:*}
  rest=${line#*:}
  lineno=${rest%%:*}
  text=${rest#*:}

  # `uses: ./path` (an action living in this tree) and `uses: docker://…` are
  # not tag references and have nothing to pin. A reusable workflow
  # (`uses: owner/repo/.github/workflows/x.yml@ref`) IS checked: it is third
  # party code running in this repository's context exactly like an action,
  # and Dependabot bumps it through the same github-actions ecosystem. Orbit
  # has neither shape today; the rules are here so the first one arrives
  # already pinned.
  ref=$(printf '%s' "$text" | sed -nE 's/.*uses:[[:space:]]*([^[:space:]#]+).*/\1/p')
  case "$ref" in
    ./*|docker://*|'') continue ;;
  esac

  found=$((found + 1))

  version=${ref##*@}
  action=${ref%@*}

  if [ "$version" = "$ref" ]; then
    echo "FAIL: $file:$lineno: $ref has no version at all — pin it to a commit SHA" >&2
    status=1
    continue
  fi

  if ! printf '%s' "$version" | grep -qE '^[0-9a-f]{40}$'; then
    echo "FAIL: $file:$lineno: $action is pinned to '$version', which is a moving tag — use the commit SHA it points at today, with the tag in a trailing comment" >&2
    status=1
    continue
  fi

  if ! printf '%s' "$text" | grep -qE '#[[:space:]]*v?[0-9]'; then
    echo "FAIL: $file:$lineno: $action@$version carries no '# <tag>' comment — without it nobody (Dependabot included) can tell which release this SHA is" >&2
    status=1
  fi
# Anchored: a `uses:` KEY, not the word inside a comment. Several of these
# workflows open with a header comment explaining the pinning rule, and an
# unanchored grep flags those headers.
done < <(grep -rnE '^[[:space:]]*(-[[:space:]]+)?uses:[[:space:]]' .github/workflows/ 2>/dev/null)

if [ "$found" -eq 0 ]; then
  echo "FAIL: no 'uses:' reference found under .github/workflows/ — did the layout change?" >&2
  exit 1
fi

if [ "$status" -ne 0 ]; then
  echo >&2
  echo "Resolve the tag with: gh api repos/<owner>/<action>/commits/<tag> --jq .sha" >&2
  echo "and write: uses: <owner>/<action>@<40-hex-sha> # <tag>" >&2
  exit 1
fi

echo "action pins OK: $found 'uses:' references, all pinned by commit SHA with their tag written next to them"
