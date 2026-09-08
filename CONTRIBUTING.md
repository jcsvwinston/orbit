# Contributing to Orbit

Thank you for considering a contribution. This document explains how the
repository is laid out, how to build and test it the way CI does, and what
conventions a pull request is expected to follow.

---

## Table of Contents

- [Repository layout](#repository-layout)
- [Development setup](#development-setup)
- [Building and testing like CI](#building-and-testing-like-ci)
- [Fuzzing the parsing surfaces](#fuzzing-the-parsing-surfaces)
- [The two web UIs](#the-two-web-uis)
- [Protobuf changes](#protobuf-changes)
- [Local guards](#local-guards)
- [Commit conventions](#commit-conventions)
- [Documentation rules](#documentation-rules)
- [How to submit a pull request](#how-to-submit-a-pull-request)
- [Reporting security issues](#reporting-security-issues)

---

## Repository layout

Orbit is **six Go modules** tied together by a `go.work` workspace, plus two
React SPAs:

| Path | What it is |
|---|---|
| `./` (root) + `internal/admin/` | The in-process admin panel — the product most apps mount. |
| `proto/` | Connect-RPC contract + committed generated stubs (Go and TS). |
| `agent/` | In-process agent that ships observability to an admin server. |
| `server/` | Standalone fleet admin-server binary. |
| `quarkbridge/` | Opt-in Quark ORM → live SQL feed bridge. |
| `quarkdatasource/` | Opt-in Quark backend for Data Studio. |
| `internal/admin/ui/` | SPA of the in-process panel (**built `dist/` is committed**). |
| `ui/` | SPA of the fleet admin server (embedded via `server/ui/embed.go`). |

Each module under `proto/`, `agent/` and `server/` releases with its own
component tag (`proto/vX`, `agent/vX`, `server/vX`).

## Development setup

You need **Go 1.26+**; **Node 22+** only if you touch one of the UIs, and
[`buf`](https://buf.build/docs/installation/) only if you touch the protobuf.

```bash
git clone https://github.com/jcsvwinston/orbit.git
cd orbit
go work sync
make build   # go build ./... in every module
make test    # go test ./... in every module
```

## Building and testing like CI

The workspace is convenient and it lies: `go.work` resolves every
inter-module import from your checkout, so a module can compile locally
while its `go.mod` still pins an old tag of its sibling — which breaks
`go install` for everyone outside this repo. CI therefore builds, vets and
tests **each module standalone**, with the workspace disabled:

```bash
# Repeat for: . proto agent server quarkbridge quarkdatasource
cd server
GOWORK=off go build ./...
GOWORK=off go vet ./...
GOWORK=off go test ./... -count=1
```

Run the standalone lane for every module whose code — or whose sibling
pins — your change touches. Workspace-wide `make build` / `make vet` /
`make test` are still the quick inner loop; the `GOWORK=off` lane is what
proves your `go.mod` is honest.

If you bump a sibling-module `require`, `scripts/ci/check_internal_pins.sh`
verifies every internal pin matches the latest sibling tag.

## Fuzzing the parsing surfaces

Six native Go fuzz targets sit on parsing surfaces that read untrusted input:
the Data Studio query string (`order_by`, filters, search, pagination), record
ids and tenant values at the API boundary, the CSV import validator, the LIKE
escaping of the Quark datasource, the event-stream filter shared by the live
bus and the replay buffer, and the agent's record-id narrowing. They are not a
survey of every such surface, and the list is meant to grow.

Each target asserts a property — a round trip, an allow-list, an invariance,
a differential between two implementations — not merely that the code does not
panic. Write the property against the **rule the surface means to enforce**,
not against what the code happens to return: a property fitted to the code's
own behaviour is green by construction and can never report the thing the
surface gets wrong. Four of the six were rewritten for exactly that reason
during review, and each rewrite found a defect (a hidden column was sortable,
a status class that is not a digit string matched a status, an id past the
width of its key was accepted, a CSV cell past the width of its column was
imported). The comment above each target says which property it states and
where that property has a history.

The tell of a property fitted to the code is that it is written in the code's
own vocabulary: the same library calls, in the same order, with the same
constants. `FuzzImportValidation` survived one round of review with a type
check that called `strconv` the way the validator calls it, at the same 64-bit
widths — so it could not see that an int8 column accepted `300` — and its
`time.Time` branch was missing altogether, which made the datetime check
untested rather than tested. State the rule in another vocabulary (a width
from the language spec, an explicit sign policy, `math/big` instead of
`strconv`), and where a full reading would mean writing a second parser, state
a **necessary** condition and say so: it may stay silent, it may not refuse
something the surface accepts.

```bash
make fuzz-seeds          # every seed corpus, deterministic, seconds — what CI runs
make fuzz                # 20s of mutation per target
make fuzz FUZZTIME=5m    # before changing one of those surfaces
```

Both forms go through `scripts/ci/fuzz.sh`, which holds the list of targets and
fails when the list and the tree disagree in either direction — an unlisted
target is one the weekly `Fuzz` workflow never runs, and a listed target that
was renamed away is worse, because `go test -fuzz=^Gone$` exits 0 without
fuzzing anything. The script also runs the whole list even when one target
fails, so a single red (or a fuzz-corpus-cache fault, which it retries once)
never leaves the rest of the list unfuzzed.

When fuzzing finds an input that breaks a property, Go writes it to
`<package>/testdata/fuzz/<Target>/<hash>`. Commit that file: it becomes a seed,
and every later pull request re-checks it in seconds. Give it a name that says
what it is (`f12_folded_and_padded_node_id`), and keep it even when the
finding turns out to be in the property rather than in the product — that
distinction is the thing you will want back.

## The two web UIs

There are two SPAs and they follow **different rules**:

- **`internal/admin/ui/` (in-process panel).** The built `dist/` is
  **committed** and embedded, so consumers need no Node toolchain. If you
  touch its source you must rebuild and commit the dist in the same PR:

  ```bash
  cd internal/admin/ui
  npm ci && npm run build
  git add dist
  ```

  CI rebuilds it and fails if the committed dist does not match the source.

- **`ui/` (fleet admin server).** `npm run build` writes straight into
  `server/ui/dist/`, which `server/ui/embed.go` embeds into the
  `admin-server` binary. See [`ui/README.md`](ui/README.md) for the dev
  server, typecheck and lint commands; CI runs `npm run typecheck`,
  `npm run lint` and `npm run build` on it.

## Protobuf changes

Generated stubs (`proto/gen/go`, `ui/src/gen`) are committed. After editing
a `.proto` file:

```bash
make proto-lint      # buf lint
make proto-breaking  # no breaking change vs origin/main
make proto           # regenerate Go + TS stubs — commit the diff
```

## Local guards

CI runs a set of shell guards you can (and should) run locally before
pushing — each one exists because the drift it checks for actually shipped:

```bash
bash scripts/ci/check_docs_product_voice.sh      # no internal vocabulary in website/docs
bash scripts/ci/check_docs_version_claims.sh     # docs claim the released version
bash scripts/ci/check_versioned_docs_markers.sh  # archived snapshots claim their own version
bash scripts/ci/check_docs_archive_freshness.sh  # snapshot archive not behind the published minor
bash scripts/ci/check_adr_index.sh               # every ADR is listed in docs/adrs/README.md
bash scripts/ci/check_internal_pins.sh           # sibling go.mod pins == latest sibling tags
```

`govulncheck ./...` also runs in CI on every module.

## Commit conventions

Orbit uses **Conventional Commits**, and the release pipeline reads them:
a `feat`/`fix` scoped to a module's path bumps that module's version and
lands in its changelog. Use `docs`, `test`, `ci` or `chore` for changes
that must not cut a release.

Breaking changes require `!` after the type and a `BREAKING CHANGE:`
footer. The public surfaces of the root module and `datasource` are frozen
(`contracts/freeze_test.go`): an incompatible change there is a major, not
a clever workaround.

One more house rule, inherited from the suite: **no marketing superlatives**
in commits, README, or docs. Say what the thing does.

## Documentation rules

- **Docs ship in the same PR as the API change.** A public behavior change
  without its documentation is an incomplete PR.
- `website/docs/` is the published product documentation: it is written in
  **English** and must pass `check_docs_product_voice.sh` (no internal
  ticket vocabulary, no references to internal files).
- `website/versioned_docs/` is a byte-for-byte archive of released
  documentation. **Never edit it by hand**; snapshots are cut by
  `scripts/release/cut_docs_snapshot.sh` in the PR that precedes a minor
  release.
- Architecture decisions live in `docs/adrs/` (see its README for the
  format). Do not reopen an accepted decision without a successor record.

## How to submit a pull request

1. For any non-trivial change, open an issue first so direction can be
   discussed before you invest time.
2. Create a branch (`feat/...`, `fix/...`, `docs/...`) — never commit to
   `main`.
3. Add or update tests. Bug fixes start with a failing test that the fix
   turns green.
4. Run the relevant lanes above (workspace tests + standalone for touched
   modules + guards).
5. Open the PR against `main`. PRs are squash-merged: the **PR title must
   itself be a valid conventional commit**, because it becomes the commit
   on `main`.

## Reporting security issues

Never open a public issue for a vulnerability — Orbit is an admin panel,
and its vulnerabilities are exactly the kind attackers scan for. Follow
[SECURITY.md](SECURITY.md).
