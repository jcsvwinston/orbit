# Deprecation Notice: the Nucleus data-source adapter leaves at v2.0.0

- ID: `DEP-2026-002`
- Status: `active`
- Announced in: the next minor after `v1.19.3` (A12 `M0-O`; decision D5b of
  the owner, 2026-10-16)
- Earliest removal: `v2.0.0`, no earlier than 2027-01-17 — major-only per
  the suite's deprecation policy (QADR-0010).
- Scope: `api`
- Affected lifecycle tag: `stable`
- Owner: `@jcsvwinston`

## Summary

Data Studio speaks Orbit's neutral `datasource` contract. The panel and the
fleet agent fill it, by default, with the Nucleus adapter
(`github.com/jcsvwinston/orbit/datasource/nucleus`): the one place in Orbit
that imports `nucleus/pkg/model` and `pkg/db`, wrapping the application's
model registry, its database handles and `model.NewCRUD`. An application
that runs the Quark ORM hands the panel a `quarkdatasource` adapter instead,
through `orbit.Config.DataSource` (and `agent.Config.DataSource` or
`ExtensionConfig.DataSource` on a fleet node).

Nucleus retires `pkg/model` as a public data layer at its 2.0 (Nucleus
`DEP-2026-024`): Quark is the data layer of the suite, and the framework
stops carrying a second one. The owner decided on 2026-10-16 that the Orbit
adapter built on it leaves with it, rather than being kept on a demoted
substrate: at 2.0 the panel reads Quark models only, through
`quarkdatasource`.

What changes and when:

- **This release:** the package, `Config`, `Adapter` and `New` carry
  `// Deprecated:`; everything keeps working. An application that mounts the
  panel without `orbit.Config.DataSource`, and a fleet node that runs the
  agent with a registry and no `DataSource`, log one WARN at start naming
  this notice and the way to stay.
- **v2.0.0:** the package is removed. `orbit.Config.DataSource` is required:
  a nil value is a configuration error, and the module refuses to start
  naming it, the way it refuses an action on a model that does not exist. An
  agent with no `DataSource` serves no Data Studio on that node (as a node
  with no registry does today). The runtime field-metadata editor
  (`PUT /api/models/{name}/schema/fields`, `PanelConfig.SchemaRegistry`)
  leaves with the adapter: it mutates the Nucleus registry, has no
  equivalent in the contract, and a panel on a custom source already
  answers it with `501`. The `*signals.Bus` the adapter hands to
  `model.NewCRUD` (`Config.Bus`, the panel's `SetSignalBus`) leaves with it
  too — Nucleus `DEP-2026-025` names Orbit as the holder of that bus through
  the untyped door, and nothing else in Orbit uses that door.

## Affected surfaces

- `github.com/jcsvwinston/orbit/datasource/nucleus`: `Config`, `Adapter`,
  `New`, and the package itself (a `datasource` module package: its tag is
  `datasource/vX.Y.Z`).
- The nil default of `orbit.Config.DataSource`, of `agent.Config.DataSource`
  and of `agent.ExtensionConfig.DataSource`.
- The field-metadata editor of the panel (`PUT /api/models/{name}/schema/fields`),
  which only the default path serves.
- Not affected: the `datasource` contract (`ModelSource`, `RecordStore`,
  `DataSource`, `OperatorFilterSource`…), `quarkdatasource`, and every Data
  Studio screen, which speaks the contract and not the adapter.

## Migration path

- **Now (v1):** define the models with Quark and mount the panel on the
  Quark adapter. The panel's own tables (operators, sessions, audit) are not
  models of the source and need nothing.

  ```go
  // before: the default — Data Studio over the application's pkg/model registry
  app := nucleus.New().
      Use("orbit", orbit.Module(orbit.Config{Prefix: "/admin"}))

  // after: Data Studio over Quark models, through the adapter that stays
  src := quarkdatasource.New(provider) // the application's quark.ClientProvider
  if err := quarkdatasource.Register[Article](src); err != nil {
      log.Fatal(err)
  }
  app := nucleus.New().
      Use("orbit", orbit.Module(orbit.Config{Prefix: "/admin", DataSource: src}))

  // a fleet node: the same adapter, on the agent
  agent.NewExtension(agent.ExtensionConfig{Endpoints: endpoints, DataSource: src}, stateDir, version)
  ```

  See *Data Studio over Quark* in the features page for the options
  `quarkdatasource` takes (searchable columns, per-model read-only, the
  filter operators it answers).
- **At 2.0:** an application that still passes no `DataSource` does not
  start; the error names the field. There is no Nucleus adapter to fall back
  to, because there is no `pkg/model` registry to read.
- **Behaviour differences:** Quark counts exactly where the Nucleus adapter
  may estimate (`CountResult.IsEstimated` is false), applies its own version
  checks and tenant scoping, and the field-metadata editor is not available
  (it mutates the Nucleus registry, which a Quark source does not have; the
  panel answers `501` for it today on any custom source).
- **Required app changes:** the model definitions (Quark instead of
  `pkg/model`, which Nucleus's own notice covers) and one field on
  `orbit.Config` — or on the agent's configuration on a fleet node.

## Detection

- At start, the panel logs
  `orbit: Data Studio reads the application's pkg/model registry through the Nucleus adapter, which leaves at Orbit 2.0 (DEP-2026-002)`,
  and a fleet node logs the same with the `admin agent:` prefix. An
  application that does not log it is already on its own source.
- Source scan: `grep -rn 'orbit/datasource/nucleus"' --include='*.go' .` for
  a direct use of the adapter; a composition root with `orbit.Module(` and no
  `DataSource:` for the default; staticcheck SA1019 on the markers.
- Suggested rewrite: manual — the model layer moves to Quark first.

## What breaks if this is ignored

At `v2.0.0`, an application that mounts the panel with a nil
`orbit.Config.DataSource` does not start, and a fleet node with a registry
and no `DataSource` serves no Data Studio. Code that imports
`orbit/datasource/nucleus` does not compile. Nothing changes for an
application already on `quarkdatasource`.

## Change window

- **Why `v2.0.0`:** removing a published package is a breaking change, and
  the suite groups those into the one major (QADR-0010). It cannot outlive
  the Nucleus package it wraps, which leaves at the same major.
- **Why `2027-01-17`:** the suite's support policy sets a minimum of 90
  calendar days of notice, counted from the set that publishes the notice.
  The owner decided on 2026-10-16; this notice is dated 2026-10-18; the first
  set in cadence that can pin it is the one of 2026-10-19, and 90 days later
  is 2027-01-17. If a later set is the first to pin it, the date moves with
  that set, and the suite's list of the major
  (`quantum/docs/planes/quantum-2.0.md` §3.0, row 30) carries the date that
  governs.

## Validation

- **The v1 behaviour is pinned**: the adapter's own tests
  (`datasource/nucleus/adapter_test.go`, `validation_test.go`,
  `parse_id_fuzz_test.go`) and the panel tests that mount on it
  (`internal/admin/panel_test.go` and the scope tests beside it) keep
  passing until the package is removed; the 2.0 PR deletes them with it.
- **The WARN is pinned**: `TestNew_DefaultDataSourceIsAnnouncedAsLeaving`
  (`agent/datasource_deprecation_test.go`) on the agent, and
  `TestModule_DefaultDataSourceIsAnnouncedAsLeaving`
  (`datasource_deprecation_test.go`, root) on the panel: the default is
  announced, an own source and a node without a registry are not.
- **The exit is exercised**: `TestQuarkDataSourceInTheFleet`
  (`internal/fleettest/fleetbench/quark_datasource_test.go`) runs a fleet
  node on `quarkdatasource`, and the `quarkdatasource` module's tests cover
  the panel's side of the contract (`quarkdatasource_test.go`,
  `operator_filters_test.go`).

  ```sh
  cd agent && go test -count=1 -run 'TestNew_DefaultDataSourceIsAnnouncedAsLeaving' .
  go test -count=1 -run 'TestModule_DefaultDataSourceIsAnnouncedAsLeaving' .
  cd internal/fleettest && go test -count=1 -run 'TestQuarkDataSourceInTheFleet' ./fleetbench/
  ```
- **Release note:** the release that carries this file announces it.
- **Rollback plan:** not applicable in v1 — nothing changes.

## Timeline

- Adapter shipped: `v1.13.0` (ADR-001, the neutral contract and its first
  implementation; before it the panel spoke `pkg/model` directly).
- Decided: `2026-10-16` (D5b of the owner, against the measurement's
  recommendation to keep it on a demoted `pkg/model` until a 3.0).
- Written: `2026-10-18` (A12 `M0-O`).
- Review checkpoint: `2027-01-17`, when the window opens.
- Removal: at `v2.0.0` planning (`M1`), in one PR that deletes the package,
  makes `orbit.Config.DataSource` required and removes the field-metadata
  editor and the signal-bus plumbing.

## Notes

The umbrella guard reads the markers (they cite this notice, the version and
the date) and the suite's list of the major
(`quantum/docs/planes/quantum-2.0.md` §3.0, row 30). The Nucleus side of the
same decision is Nucleus `DEP-2026-024`.
