# quarkdatasource

`github.com/jcsvwinston/orbit/quarkdatasource`

An opt-in implementation of Orbit's [datasource contract](../datasource)
(orbit ADR-001) over a [Quark](https://github.com/jcsvwinston/quark) ORM client,
so **Data Studio browses and edits Quark-managed models**
([QADR-0006](https://github.com/jcsvwinston/quantum/blob/main/docs/adr/QADR-0006-integracion-quark-orbit.md),
Caso 2).

It is the second implementation of the contract — the one that proves the
abstraction is not Nucleus-shaped. It lives in its own module so Quark never
enters the orbit core's dependency graph.

## Usage

```go
import (
    "github.com/jcsvwinston/orbit"
    "github.com/jcsvwinston/orbit/quarkdatasource"
    "github.com/jcsvwinston/quark"
)

client, err := quark.New("pgx", dsn)
// ... client.RegisterModel / migrations as usual ...

ds := quarkdatasource.New(client)
quarkdatasource.Register[User](ds)
quarkdatasource.Register[Post](ds)

app, err := nucleus.New().
    Mount(orbit.Module(orbit.Config{
        Prefix:     "/admin",
        DataSource: ds, // Data Studio now runs on Quark models
    })).
    Build()
```

## Why registration is generic (`Register[T]`)

Quark's query API is typed — `quark.For[T](ctx, provider)` (its ADR-0002/0014
design) — so a model's CRUD path cannot be bound from a `reflect.Type` at
runtime. `Register[T]` monomorphizes the typed path once, at wiring time. The
model **metadata** (columns, PK, not-null, unique, relations) comes from the
struct's Quark tags via `quark.GetModelMetaByType`, the same source Quark's own
migrations use — not from table introspection, which would lose the Go-level
facts.

Presentation metadata Quark deliberately does not carry (labels, HTML input
types, list/search/filter flags) is derived from the Go type with permissive
defaults: every column is listed and filterable (scalars), string columns are
searchable, labels are humanized field names.

## Semantics

- **IDs are strings at the boundary** (ADR-001 D1), narrowed to the PK field's
  Go kind. Models with a **composite primary key** (or none) are catalogued
  **read-only**: List/Count work; Get/Create/Update/Delete return an error.
- **Records are the model's JSON object** (D2): entities round-trip through
  `encoding/json`, so Data Studio shows exactly what the struct marshals to
  (including `quark.Nullable` values).
- **Search** matches any searchable column via a single OR-group built with
  Quark's expression AST (`WhereExpr`), AND-composed with exact filters —
  column names go through Quark's `SQLGuard` like any builder input.
- **Update** uses `UpdateMap`, so zero values are written (unlike a
  full-entity save). **Delete** follows Quark's semantics: soft delete when the
  model has a `deleted_at` column, hard otherwise.
- **Totals are real counts** over the same filters (`IsEstimated` is always
  false).
- **Tenancy**: pass a `*quark.TenantRouter` as the provider and every query
  runs under Quark's own scoping (WHERE-injection or native RLS). Additionally,
  `WithTenantColumn("tenant_id")` marks the tenant field so Data Studio's own
  tenant filter applies.
- **`Store`'s dbAlias is ignored**: a Quark client is bound to one database.

## Status

Pre-1.0, alongside Orbit. The datasource contract freezes at Orbit v1.0
(QADR-0005).
