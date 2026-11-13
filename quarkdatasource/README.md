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
searchable, labels are humanized field names — and a column whose name looks
like a credential is left out (below). What the defaults get wrong for a
model is declared at `Register`.

## Declaring what the panel shows

`Register[T]` takes options that say what Data Studio does with the model —
the same things an application on `pkg/model` says in `model.ModelConfig`
and the `admin` struct tag, in the same words, so a model moved to Quark
keeps the same panel (nucleus `MA-2026-024`). Quark itself reads none of
them: a struct tag that said "list" would be a UI decision in the data layer.

```go
quarkdatasource.Register[User](ds,
    quarkdatasource.ListFields("email", "role", "created_at"),
    quarkdatasource.SearchFields("email", "display_name"),
    quarkdatasource.Filters("role"),
    quarkdatasource.Exclude("notes"),
    quarkdatasource.ReadOnlyFields("email"),
    quarkdatasource.Label("display_name", "Name"),
    quarkdatasource.Choices("role",
        datasource.Choice{Value: "admin", Label: "Administrator"},
        datasource.Choice{Value: "member", Label: "Member"}),
    quarkdatasource.Icon("users"),
    quarkdatasource.OrderBy("created_at desc"),
    quarkdatasource.PageSize(50),
)
quarkdatasource.Register[AuditEntry](ds, quarkdatasource.ReadOnly())
```

| Option | `model.ModelConfig` / `admin` tag | What it does |
|---|---|---|
| `ListFields(cols...)` | `ListFields` / `admin:"list"` | the columns the list shows; none named keeps every column |
| `SearchFields(cols...)` | `SearchFields` / `admin:"search"` | the text columns `?search=` looks in; none named keeps every string column |
| `Filters(cols...)` | `Filters` / `admin:"filter"` | the columns offered as filters; none named keeps every scalar column |
| `Exclude(cols...)` | `ExcludeFields` / `admin:"exclude"` | never shown: not in the schema, the rows, a form, an export or the audit trail; a filter, sort, search or write naming one is refused as a column the model does not have |
| `ReadOnly()` | `ReadOnly` | the model is listed and every write is refused |
| `ReadOnlyFields(cols...)` | `admin:"readonly"` | shown, not edited; a write naming one leaves it to the database, as the key is |
| `Label(col, label)` | `FieldLabels` / `admin:"label:…"` | the label the panel shows |
| `Choices(col, choices...)` | `admin:"choices:…"` | a closed set of values; the form draws a select |
| `Icon(name)` | `Icon` | the sidebar icon |
| `OrderBy("col [asc\|desc], …")` | `OrderBy` | the order of a list the request did not sort |
| `PageSize(n)` | `PageSize` | the page of a list the request did not size (default 25) |
| `ShowSensitive(cols...)` | — | keeps a column the name rule below would hide |

A field is named by its storage column or its Go name, case-insensitively.
An option that names no field of the model, names a column of the wrong
kind (`SearchFields` on a number, `Filters` on a timestamp), contradicts
another (`Exclude` and `ListFields` on the same column, `OrderBy` on an
excluded one) or would silently overturn a default is an **error from
`Register`**, and the model is not registered: a misspelled column in
`ListFields` would otherwise list every column and look like the default.
The primary key and the tenant column cannot be excluded.

**A column whose name looks like a credential is excluded unless you say
otherwise.** `IsSensitiveColumnName` is the rule — the name contains
`password`, `secret`, `token`, `credential`, `api_key`, `apikey` or
`private_key`, or ends in `hash` or `salt` — and it is the same rule the
panel's audit trail redacts by. `Register` says what it hid in the adapter's
log (`WithLogger`; `slog.Default` otherwise), naming the option that shows
it: `ShowSensitive("password_hash")`. Naming such a column in `ListFields`,
`SearchFields`, `Filters`, `ReadOnlyFields`, `Label` or `Choices` without
`ShowSensitive` is an error rather than a column that is both asked for and
hidden. A required column that is hidden is stored as its Go zero value on a
create, as on the Nucleus adapter: hide the columns the application fills
itself.

`Register` grew its variadic options in a minor: `Register[T](ds)` is the
same call. Code that assigned `Register[T]` to a `func(*Adapter) error`
value names the new signature.

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

Released with its own tag (`quarkdatasource/vX.Y.Z`; the set certified by
the suite pins it in `versions.yaml` of
[quantum](https://github.com/jcsvwinston/quantum)). The datasource contract
it implements has been frozen since Orbit v1.0 (QADR-0005); the module's
own API — `New`, its options, `Register` and its options — is not part of
that freeze and grows by minor. At Orbit 2.0 the root module requires this
one (decision D11 of the 2.0 contract), and the Nucleus adapter it replaces
leaves (DEP-2026-002).
