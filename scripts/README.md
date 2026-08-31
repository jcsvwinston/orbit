# scripts/

Maintainer tooling. Every script carries its full usage in its own header
comment; this file is just the map. All of them run from any directory (they
`cd` to the repo root themselves).

## release/ — the release train

- **`align_set.sh`** — closes orbit's set-alignment train in ONE commit.
  When the umbrella certifies a new quark/nucleus pair, all six module
  `go.mod` files (root included — the pin that got forgotten in the 1.24.0
  train and cost an extra root tag) must move to the new tags, internal
  sibling pins must match their latest published tags, and each touched
  module needs a tidy. This script does all of it and commits once, so
  release-please sees one alignment commit instead of a trail of partial ones.

  ```bash
  git fetch --tags   # sibling pins are checked against published tags
  bash scripts/release/align_set.sh --manifest ../quantum/versions.yaml            # write + tidy + ONE commit
  bash scripts/release/align_set.sh --nucleus v1.21.0 --quark v1.7.1 --dry-run     # exact go.mod diff, touches nothing
  bash scripts/release/align_set.sh --manifest ../quantum/versions.yaml --check    # exit 1 if any pin is misaligned (local CI)
  ```

  The root↔quarkdatasource pin may sit a patch behind (topologically forced —
  see `scripts/ci/check_internal_pins.sh`); this writer tolerates that lag
  within the same minor and closes anything wider.

- **`cut_docs_snapshot.sh`** — cuts the versioned docs snapshot for a minor
  (byte-for-byte copy of `website/docs`, versioned sidebar, `versions.json`).
  Cut it before the release and after every prose fix of the round.

## ci/ — the guards

Read-only checks wired into `.github/workflows/ci.yml`; each one's contract
is documented in its header. `check_internal_pins.sh` is the judge that
`release/align_set.sh` is the writer for.
