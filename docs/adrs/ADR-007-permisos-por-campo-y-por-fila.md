---
id: ADR-007
title: Los permisos llegan al campo y a la fila por adición a la gramática de políticas
status: accepted
date: 2026-09-13
deciders: jcsvwinston
related: [ADR-001, ADR-004, quantum/QADR-0010]
supersedes: null
tags: [orbit, rbac, permisos, datastudio, contrato]
---

# ADR-007 — Los permisos llegan al campo y a la fila por adición

## Contexto

La autorización del panel es propia (ADR-004) y su vocabulario era
`(sujeto, objeto, acción)` con el objeto nombrando **un modelo**:
`(editores, admin:Post, update)`. Dos permisos que cualquier admin editorial
necesita no cabían en esa gramática:

- **por campo** — «este editor corrige el título, no toca el precio»;
- **por fila** — «cada autor edita lo suyo».

La medición de la sesión `S0` del arco A6 lo registró como los controles
`PERM-06` y `PERM-07` del banco de admin (`internal/adminbench`), ausentes
los dos, y añadió un tercero: lo que una pantalla carga **no llevaba ninguna
pista de capacidad** (`PERM-09`), así que la UI sólo descubría un permiso
**siendo rechazada** — dibujaba todos los botones y la persona aprendía
cuáles no eran suyos pulsándolos.

La restricción que condiciona la solución es el **QADR-0010** de la suite
(`quantum/docs/adr/`): lo rompiente se acumula en un único major al cierre de A12. Hasta
entonces lo nuevo entra **junto** a lo viejo. `datasource.Query` y
`orbit.Config` están congeladas por `contracts/freeze_test.go`.

## Decisión

**El objeto de una política admite dos formas más, y nada de lo que ya
funcionaba cambia.**

| Objeto | Significa |
|---|---|
| `admin:Post` | el modelo entero (lo de siempre) |
| `admin:Post#own` | el mismo verbo, confinado a las filas del operador |
| `admin:Post.title` | un campo del modelo |

1. **Por fila.** Un `#own` filtra la lista por la columna de propiedad,
   contesta `404` en los endpoints de registro para una fila ajena (la misma
   respuesta que una fila inexistente: no se revela el espacio de ids),
   estampa al operador al crear e impide que un update entregue la fila a
   otro. Cubre `list`, `retrieve`, `create`, `update`, `delete`, `export_csv`
   y `bulk_delete` — las superficies que nombran un modelo.

2. **Qué columna dice de quién es una fila lo declara la aplicación**, no una
   convención: `PanelConfig.RowOwnerFields` (`row_owner_fields` en el yaml),
   por modelo y con `"*"` como defecto, y `RowOwnerSubject` para elegir si la
   columna guarda el `username` (defecto) o el `id`.

3. **Un `#own` que el panel no puede honrar se RECHAZA** con un 403 que dice
   por qué; nunca se ensancha a todas las filas. Una regla de propiedad que
   degrada en silencio a «todo» es exactamente el fallo que este mecanismo
   existe para impedir, y sería invisible.

4. **Por campo, en dos formas**, porque un admin necesita las dos: `deny`
   nombra la excepción («todo menos el precio») y una acción (`read`,
   `create`, `update`, `write`) nombra el conjunto permitido entero («el
   título, y nada más»). Una allow-list sólo se aplica al sujeto que tiene al
   menos una política de campo de esa acción para ese modelo; quien no tiene
   ninguna conserva el permiso de modelo de siempre.

5. **Una escritura sobre un campo prohibido se rechaza con 403 NOMBRANDO el
   campo**, no se descarta en silencio: un formulario que cree haber guardado
   lo que no guardó es peor que uno al que se le dice que no puede. Un campo
   que el operador no puede leer no sale del registro, ni de la lista, ni del
   CSV, ni del esquema.

6. **Las políticas de campo estrechan, nunca ensanchan.** Quien no puede
   actualizar el modelo es rechazado antes de mirar ningún campo.

7. **Lo que una pantalla carga dice lo que el operador puede hacer**:
   `permissions`, `can_create`, `can_update`, `can_delete` y `row_scope` en
   `/api/models` y en el esquema, y `can_edit` por campo. Son ayuda de
   render: todo se vuelve a comprobar en la petición siguiente, y un cliente
   que las ignore es rechazado igual que antes.

8. **El superusuario sigue saltándoselo todo**, como en `authorizeAction`.

## Consecuencias

- **Expansión del contrato congelado** (`contracts/baseline`): dos campos
  nuevos en `orbit.Config` (`RowOwnerFields`, `RowOwnerSubject`). Son
  adiciones —no se renombra ni se quita nada—, así que caben bajo QADR-0010;
  la baseline se regenera en el mismo cambio.
- **Nada cambia para quien no escriba una política nueva.** Un panel sin
  `#own` ni políticas de campo se comporta exactamente igual, y hay un test
  que lo fija (`TestFieldPerms_NoFieldPolicyChangesNothing`,
  `TestRowScope_FullGrantIsUnconfined`).
- **El confinamiento por fila comparte mecanismo con el de tenant**
  (`columnScopeOwns`): las dos son «la columna X vale Y», incluida la parte
  difícil — un registro que no trae la columna se confirma contra el almacén.

### Lo que NO cubre, dicho por escrito

- **Las superficies globales.** `export_data`, `import_data` y las fixtures se
  autorizan sobre `admin:*`, no sobre un modelo: no llevan alcance de fila ni
  de campo. Conceder esos verbos a un operador confinado es darle el rodeo, y
  quien los concede lo está decidiendo.
- **El rastro de auditoría.** Guarda los valores antes y después de una
  escritura con sus propias reglas de redacción; un campo prohibido por
  política puede seguir siendo legible ahí para quien tenga `audit_view`.
- **El feed en vivo** no filtra por estas políticas.
- **La allow-list de creación se refleja en `can_edit` como la de update.** Un
  formulario de alta con allow-lists distintas para `create` y `update`
  ofrece los campos de `update`; el servidor rechaza lo que sobre.

## Preguntas abiertas

- Un operador de filtro con más gramática (rango, contiene, en) llega en la
  sesión `S5` del arco; el filtro de propiedad usa igualdad porque
  `datasource.Query.Filters` es columna→valor y está congelado.
