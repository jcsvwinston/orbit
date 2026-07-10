# orbit/proto

Connect-RPC v1 contract between the agent, the admin server, and the
admin UI. Single source of truth: `nucleus/admin/v1/admin.proto`.

## Day-to-day commands

```bash
# Regenerate Go + TypeScript stubs (committed). Run this after any
# change to a .proto file.
make proto

# Lint the proto with buf's STANDARD ruleset (plus our documented
# exceptions for shared Frame/Event/Snapshot types).
make proto-lint

# Verify nothing in this PR is a wire-incompatible change vs main.
make proto-breaking
```

## Generated outputs

* `gen/go/nucleus/admin/v1/` — Go message structs and Connect-RPC
  service stubs. Imported by `orbit/agent` and `orbit/server`.
* `../ui/src/gen/nucleus/admin/v1/` — TypeScript stubs consumed by the
  Connect-Web client in the UI.

Both are committed so a fresh checkout compiles without `buf`
installed. Regeneration is a manual discipline (`make proto` after any
`.proto` change) — there is no CI lane verifying a clean diff today.

## Evolution rules

Read `EVOLUTION.md` before changing this proto. Short version: never
remove fields, never reorder oneof tags, never reuse field numbers.
The reasoning and the agent/server/UI rolling-update implications are
all there.

## Why version pins

`buf.gen.yaml` pins `bufbuild/es:v1.10.0` and `connectrpc/es:v1.6.1`
because the connectrpc/es buf-registry plugin has no `v2` published
yet. The npm packages in `../ui/package.json` track the same line
(`@bufbuild/protobuf@^1.10`, `@connectrpc/connect@^1.6.1`).

When the v2 plugin lands on the registry, this is a 3-line change here
plus a single `npm install` in `../ui`.

## Distribution

Released as its own Go module with component tags (`proto/vX.Y.Z`,
via release-please) so the agent and the server can depend on the
generated bindings by tag instead of intra-repo `replace` directives.
Wire-compatibility rules live in `EVOLUTION.md`.
