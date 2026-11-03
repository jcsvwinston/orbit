# quarkdatasource

`github.com/jcsvwinston/orbit/quarkdatasource`

An opt-in implementation of Orbit's [datasource contract](../datasource)
(`github.com/jcsvwinston/orbit/datasource`, a module of its own) over a
[Quark](https://github.com/jcsvwinston/quark) ORM client,
so **Data Studio browses and edits Quark-managed models**
([QADR-0006](https://github.com/jcsvwinston/quantum/blob/main/docs/adr/QADR-0006-integracion-quark-orbit.md),
Caso 2).

It is the second implementation of the contract — the one that proves the
abstraction is not Nucleus-shaped. It lives in its own module so Quark never
enters the orbit core's dependency graph, and it requires the contract
module rather than the panel: a Quark data source pulls in neither the
in-process panel nor its SPA.

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

### In the fleet

The fleet's Data Studio speaks the same contract. An application that runs
the Orbit agent passes the same adapter to the agent's extension, and the
admin server's Data Studio browses and edits the Quark models through it,
as the operator the server sends (subject, role, read-only, tenant):

```go
import (
    "github.com/jcsvwinston/orbit/agent"
    "github.com/jcsvwinston/orbit/quarkdatasource"
)

ds := quarkdatasource.New(client, quarkdatasource.WithTenantColumn("tenant_id"))
quarkdatasource.Register[User](ds)

app, err := nucleus.New().
    Mount(orbit.Module(orbit.Config{Prefix: "/admin", DataSource: ds})).
    WithExtensions(agent.NewExtension(agent.ExtensionConfig{
        Endpoints:  []string{"https://admin.internal:9090"},
        Token:      os.Getenv("NUCLEUS_ADMIN_TOKEN"),
        DataSource: ds, // the fleet's Data Studio runs on the same Quark models
    }, stateDir, appVersion)).
    Build()
```

Nil leaves the agent on its Nucleus adapter, so an application that never
sets it serves what it always served. The tenant column named at
construction is what confines an operator with a tenant on both planes.

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
- **Records are the model's JSON object** (D2), with every schema field
  re-keyed to its **storage column**: Quark models normally carry no json tags,
  so the raw object would use Go-case keys (`CustomerID`) while Data Studio
  reads cells by column (`customer_id`). Values (including `quark.Nullable`)
  and keys outside the schema — relations, extra JSON — pass through unchanged.
- **Search** matches any searchable column via a single OR-group built with
  Quark's expression AST (`WhereExpr`), AND-composed with exact filters —
  column names go through Quark's `SQLGuard` like any builder input.
- **Update** uses `UpdateMap`, so zero values are written (unlike a
  full-entity save). **Delete** follows Quark's semantics: soft delete when the
  model has a `deleted_at` column, hard otherwise.
- **A missing row is a not-found** (`404`): a `Get`, an `Update` that touches
  no row and a `Delete` that removes none. The update and the delete used to
  answer success, which the panel reported as done and recorded in its audit
  trail.
- **A record is validated before it is written**, and a record the store
  cannot write faithfully is refused whole with the Nucleus adapter's answer:
  a `422` (`VALIDATION_FAILED`) with one message per key, in the same words —
  a key that names no field (`unknown field`), a value that does not fit the
  field's Go type (`must be a string`, `must be an integer`, out of the
  type's range) or the column's declared `size` (`must be at most 20
  characters`), a field named twice, and a required field left out or empty
  (`this field is required`). Required means `quark:"not_null"`, except a
  `created_at`/`updated_at` timestamp, which Quark stamps itself. A number
  may arrive as its text and a timestamp as RFC 3339 or a bare date, as on
  the Nucleus adapter; an empty string for a field that is not text is no
  value. An update names a field by its column or Go name; the struct's JSON
  key is refused there rather than ignored.
- **What the database refuses is classified**, and no driver text reaches the
  response: a value a unique key already holds is a `409` naming the field
  (recognised by `quark.IsUniqueViolation`, which reads the driver's error
  code, not its message), another constraint a `409` that says so. A model
  hook that refuses a write with a domain error is answered with that error.
- **Operator filters** (`Query.Where`: `gt`, `contains`, `in`, `isnull` and
  the rest of the twelve) are applied, and the store says so through
  `datasource.OperatorFilterSource` — which is what the panel asks before it
  sends any, because a source that ignored them would answer every row while
  looking like it filtered. Two refusals rather than a wrong answer: an
  operator this store cannot express is refused by name instead of falling
  back to equality, and a `contains`/`startswith`/`endswith` whose value holds
  `%` or `_` is refused on SQLite and Oracle, whose `LIKE` has no default
  escape character and whose builder emits no `ESCAPE` clause — there, the
  wildcard would widen the match instead of matching itself. An `in` with no
  values matches nothing, never everything.
- **Totals are real counts** over the same filters (`IsEstimated` is always
  false).
- **Tenancy**: pass a `*quark.TenantRouter` as the provider and every query
  runs under Quark's own scoping (WHERE-injection or native RLS). Additionally,
  `WithTenantColumn("tenant_id")` marks the tenant field so Data Studio's own
  tenant filter applies.
- **One database alias per adapter**: a Quark client is bound to one
  database, so `Store` serves the adapter's alias (`WithDatabaseAlias`,
  default `"default"`) and refuses any other with a `400` naming both.

## Status

Pre-1.0, alongside Orbit. The datasource contract freezes at Orbit v1.0
(QADR-0005).
