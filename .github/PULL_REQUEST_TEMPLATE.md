<!--
The PR title becomes the squash commit on main: it MUST be a valid
Conventional Commit (`fix(server): …`, `feat(agent): …`, `docs: …`).
A `feat`/`fix` scoped to a module's path cuts a release of that module.
-->

## What and why

One paragraph: the behaviour before, the behaviour after, and why.

## Which modules

- [ ] root (`./`, `internal/admin`, `datasource`)
- [ ] `proto/`
- [ ] `agent/`
- [ ] `server/`
- [ ] `quarkbridge/`
- [ ] `quarkdatasource/`
- [ ] `internal/admin/ui` (dist rebuilt and committed)
- [ ] `ui/` (fleet SPA)
- [ ] `website/docs/`

## Checklist

- [ ] Tests added or updated; a bug fix starts with a test that failed before.
- [ ] Docs updated in this PR for any public behaviour change (same-PR rule).
- [ ] `GOWORK=off go build ./... && go vet ./... && go test ./...` in every touched module.
- [ ] `go mod tidy` is a no-op in every touched module.
- [ ] Frozen surfaces (`contracts/freeze_test.go`: root + `datasource`) untouched, or the change is a deliberate, reviewed expansion.
- [ ] No marketing superlatives in code, commit or docs.
- [ ] If a `.proto` changed: `make proto-lint`, `make proto-breaking`, `make proto` run and stubs committed.

## Breaking changes

None — or describe them, and add `!` to the title plus a `BREAKING CHANGE:` footer.
