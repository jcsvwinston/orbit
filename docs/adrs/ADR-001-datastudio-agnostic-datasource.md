---
id: ADR-001
title: Data Studio agnóstico del origen de datos (contrato datasource)
status: accepted
date: 2026-07-01
deciders: jcsvwinston
related: [nucleus/ADR-019, quantum/QADR-0006]
supersedes: null
tags: [orbit, data-studio, api, desacople]
---

# ADR-001 — Data Studio agnóstico del origen de datos

> Primer ADR de Orbit. Orbit se extrajo del core de Nucleus (nucleus
> `ADR-019-extract-admin-to-orbit-module`); sus decisiones internas de producto
> viven aquí, no en los QADR de la suite. Contexto de suite:
> `quantum/docs/adr/QADR-0006`.

## Contexto

Data Studio (el explorador CRUD del panel) está acoplado en profundidad a
`nucleus/pkg/model`, repartido por ~10 ficheros de `internal/admin`. "Desacoplar
del `NewPanel`" no es cambiar dos parámetros; es sacar `model.*` de toda esa
superficie. Mapa real (verificado, con archivo:línea):

| Dependencia de Nucleus | Dónde | Se abstrae con |
|---|---|---|
| `*model.Registry` (`.All`/`.Get`) | `panel.go:104,147`; `handlers.go:84`; ~15 `registry.Get` | `ModelSource` |
| `*model.ModelMeta` (Name, Plural, Table, PrimaryKey, Type, DatabaseAlias, Fields, Config) | `handlers.go:338-392,864,935,992`; exporters/fixtures/importers/audit | `ModelInfo` |
| `model.FieldMeta` (Column, Name, Label, GoType, HTMLType, IsPK, IsRequired, IsReadOnly, IsList, IsSearch, IsFilter, IsExcluded, IsForeignKey, IsTenantField, ForeignModel, Choices) | `handlers.go:346-378,1015,1318`; `importers.go:195-204` | `FieldInfo` |
| `model.CRUDOperator` + `model.NewCRUD(sqlDB, meta, bus)` | `panel.go:312-332` | `RecordStore` (factory `DataSource.Store`) |
| `crud.FindAll/FindByID/Create/Update/Delete` + `model.QueryOpts` | `handlers.go:500-706`; `importers.go:311-348` | métodos de `RecordStore` + `Query` |
| Reflexión sobre entidades (`payloadToEntity`, `entityToMap`, `extractPKValue`, `fieldForInput`) | `handlers.go:992-1013`; `audit.go:233`; `fixtures.go:399-518` | desaparece del panel; vive en el adaptador (usa `meta.Type`) |
| Conteo por dialecto + `tableExists` (SQL directo) | `handlers.go:864-954` | `RecordStore.Count` / `.TableExists` |
| `model.SanitizeOrderBy`, `collectFilters/normalizeFilter/resolveField` | `handlers.go:1233-1360` | se reimplementan en el panel sobre `ModelInfo.Fields` |

Hoy `NewPanel(database *db.DB, registry *model.Registry, …)` (`panel.go:147`)
habla el vocabulario de Nucleus. Motivación de suite: que Data Studio funcione
también sobre Quark (QADR-0006, Caso 2).

## Decisión

**El panel se construye contra un contrato neutral, propio de Orbit; deja de
hablar tipos de Nucleus.** Un único adaptador por backend traduce.

Contrato (paquete `internal/datasource`, sin importar `pkg/model`/`pkg/db`):

```go
type ModelSource interface {
    All() []ModelInfo
    Get(name string) (ModelInfo, bool)
}
type RecordStore interface { // por (modelo, alias)
    List(ctx, Query) (Page, error)
    Get(ctx, id string) (Record, error)
    Create(ctx, Record) (Record, error)
    Update(ctx, id string, Record) error
    Delete(ctx, id string) error
    Count(ctx) (CountResult, error)
    TableExists(ctx) bool
}
type DataSource interface { // lo que toma NewPanel
    ModelSource
    Store(modelName, dbAlias string) (RecordStore, error)
}
```

Con tipos neutrales `ModelInfo`/`FieldInfo`/`Query`/`Page`/`Record` (=`map[string]any`).
`NewPanel(src datasource.DataSource, logger, cfg)`. El adaptador **Nucleus**
(`internal/datasource/nucleus`) envuelve `registry`+`db.DB`+`NewCRUD` y se queda la
reflexión y el conteo por dialecto. El adaptador **Quark** (después, módulo aparte)
implementa el mismo contrato sobre `*quark.Client` + introspección.

**Decisiones de diseño:**
- **D1** — IDs `string` en el límite (no `uint`): Quark tiene PK uuid/string/
  compuesta. El adaptador Nucleus estrecha a `uint` internamente.
- **D2** — el panel habla `Record` (mapas), no entidades; la reflexión sale al
  adaptador. Simplifica el panel y habilita backends no-struct.
- **D3** — catálogo (`ModelSource`) y acceso (`RecordStore`) separados; `Store`
  resuelve por petición, igual que hoy `getCRUD(meta, alias)`.

## Consecuencias

- Es un refactor real (≈10 ficheros), no un cambio de firma. El límite queda
  limpio: el panel depende solo de `datasource`; un único adaptador toca Nucleus.
- **Estas interfaces son API pública de Orbit → deben congelarse en el gate de
  v1.0 de Orbit** (QADR-0005). Definirlas bien ahora evita un breaking-change en
  v2.0. Proyecto embrionario ⇒ se hace directo, sin doble registro transitorio.
- La prueba de que la abstracción no quedó con forma de Nucleus son **dos**
  implementaciones (Nucleus + Quark).

## Preguntas abiertas (confirmadas al implementar)

- **O1 — resuelto.** `model.NewCRUD(db *sql.DB, meta *ModelMeta, bus *signals.Bus)`
  (`nucleus/pkg/model/crud.go:92`). El adaptador toma un `*signals.Bus` (nil en el
  cableado de orbit, donde el feed live va por el bus de observabilidad).
- **O2 — resuelto.** `model.Choice{Value, Label string}` (con tags json
  `value`/`label`); `model.ForeignKey{FieldName, Column, ForeignModel,
  ForeignTable, ForeignColumn}` (`nucleus/pkg/model/fields.go`, `meta.go`).
  Reflejados en `datasource.Choice` y `datasource.ForeignKey`.
- **O3 — resuelto, sin cambio en la SPA.** `crud.FindAll` devuelve
  `*model.PaginatedResult` con JSON `items,total,page,page_size,total_pages,
  is_estimated,has_more`; la SPA (`ui/src/types/index.ts` `PaginatedResult`) lee
  exactamente esas claves, y los ítems por `field.column`/`field.name`.
  `datasource.Page` serializa con esas mismas claves, y el adaptador construye cada
  `Record` con un round-trip struct→JSON (`entityToRecord`), byte-idéntico a lo que
  el panel reenviaba antes. La SPA no se toca. Cubierto por un test del adaptador.

## Validación con la segunda implementación (quarkdatasource, 2026-07-02)

La prueba de que la abstracción no quedó con forma de Nucleus eran dos
implementaciones. La segunda (`orbit/quarkdatasource`, sobre `*quark.Client`)
arrojó **una corrección** y tres encajes sin forzar:

- **Corrección: el contrato sale de `internal/`** → `orbit/datasource`. Estas
  interfaces son API pública (se congelan en v1.0), y el Caso 2 exige que la app
  las nombre: construye el adaptador Quark y lo inyecta vía
  `orbit.Config.DataSource`. Un tipo `internal/` no es importable por la app. El
  adaptador Nucleus sigue en `internal/` (es cableado, no contrato).
- **Encaja (D1)**: PK string/uuid/int de Quark se estrechan desde el id string
  del límite; PK compuesta → el modelo se cataloga **read-only** (List/Count sí;
  Get/Create/Update/Delete devuelven error). El contrato no necesitó cambios.
- **Encaja (D2)**: `Record` como mapa JSON absorbe `quark.Nullable` y structs sin
  tags json sin tocar el contrato.
- **Encaja (D3)**: `Store(model, alias)` — el adaptador Quark ignora el alias
  (un cliente = una base), documentado; la firma no estorbó.
- **Matiz de implementación** (no de contrato): la API de consulta de Quark es
  tipada (`quark.For[T]`), sin binding de tipos en runtime, así que el registro
  del catálogo es genérico por modelo (`quarkdatasource.Register[T]`), que
  monomorfiza el camino tipado en el cableado.

## Plan de adopción

1. Añadir `internal/datasource` + `internal/datasource/nucleus`.
2. Cambiar la firma de `NewPanel` a `DataSource`; construir el adaptador Nucleus
   en `orbit.go` desde los mismos accessors del `Runtime`.
3. Migrar Data Studio fichero a fichero a `datasource.*`; mover reflexión y CRUD
   al adaptador; reescribir filtros/order-by/tenant sobre `ModelInfo`.
4. Verde en los tests de `internal/admin` (actualizar el constructor en los
   `*_test.go`, p. ej. `panel_test.go:820`).
5. Más tarde: adaptador Quark (módulo aparte) → Data Studio sobre modelos Quark.
