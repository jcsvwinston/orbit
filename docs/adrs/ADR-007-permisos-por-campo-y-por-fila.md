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
  quien los concede lo está decidiendo. *(Enmendado para `export_data` y el
  volcado de fixtures por OR-66, abajo: llevan el alcance de `list`; y para
  la importación y la carga de fixtures por OR-67, más abajo: escriben con el
  alcance de `create` y `update`. Ninguna de las cuatro sigue ya como dice
  esta viñeta.)*
- **El rastro de auditoría.** Guarda los valores antes y después de una
  escritura con sus propias reglas de redacción; un campo prohibido por
  política puede seguir siendo legible ahí para quien tenga `audit_view`.
  *(Enmendado por OR-73, más abajo: el historial de un registro y el rastro
  enseñan los valores de una fila con el alcance del `retrieve` de su
  modelo.)*
- **El feed en vivo** no filtra por estas políticas.
- **La allow-list de creación se refleja en `can_edit` como la de update.** Un
  formulario de alta con allow-lists distintas para `create` y `update`
  ofrece los campos de `update`; el servidor rechaza lo que sobre.

## Enmienda de OR-66 (2026-10-06): la exportación lleva el alcance de `list`

La primera viñeta de «Lo que NO cubre» dejaba `export_data` sin alcance de
fila ni de campo, y el resultado era peor que un rodeo decidido: el botón
Export de Data Studio usa esa exportación, así que un operador confinado a
sus filas, o sin un campo, exportaba todas las filas y todos los campos del
modelo que estaba mirando — y de cualquier modelo que no podía abrir — en
CSV, JSON y SQL. Nadie que concediera `export_data` para «que exporte lo que
ve» estaba decidiendo eso.

Se enmienda así:

- **`export_data` sigue concediéndose sobre `admin:*`**, pero dice quién
  exporta, no qué. La exportación del panel (`POST /api/exports`) y el
  volcado de fixtures (`dumpdata`, que va con el mismo permiso) llevan, por
  modelo, lo que el `list` de ese operador mostraría: el tenant, las filas
  propias bajo `#own` y sólo los campos legibles, en todos los formatos. Las
  cinco superficies que leen filas en nombre de un operador (la lista,
  `export_csv`, la exportación del panel, el volcado y la tarjeta de
  registros de un dashboard) toman ese alcance de una sola función
  (`requestReadScope`).
- **Un modelo que el operador no puede listar** se rechaza con el `403` de
  `list` si la petición lo nombra (también el de un `#own` sin columna de
  propiedad: el punto 3 vale aquí), y se omite si la petición exporta todos
  los modelos.
- **Una exportación ya cortada es una copia de lo que su autor podía leer**:
  la lista de trabajos, su estado y su descarga se entregan a ese operador y
  a un superusuario, y la clave lleva un sufijo aleatorio (dos exportaciones
  en el mismo segundo compartían clave y la segunda pisaba la primera).
- La pista `export_data` del esquema es falsa en un modelo que el operador
  no puede listar, porque es la respuesta que da el handler.

**Lo que esta enmienda NO cambia**: `import_data` y la carga de fixtures
siguen autorizándose sobre `admin:*` sin el `create`/`update` de cada
modelo, sin alcance de fila y sin políticas de campo de escritura, tal como
dice «Lo que NO cubre»; si eso se mantiene es una decisión pendiente del
dueño (OR-67), no de esta enmienda. Lo único que ganan es lo que cualquier
escritura del panel ya tenía: un modelo de sólo lectura rechaza la carga de
fixtures entera, antes de escribir ninguna fila (OR-68), como desde OR-65
rechaza la importación. *(El dueño lo decidió el mismo día: ver la enmienda
de OR-67.)*

## Enmienda de OR-67 (2026-10-06): quien importa escribe lo que escribiría a mano

La enmienda de OR-66 dejó a la importación y a la carga de fixtures donde
las ponía «Lo que NO cubre»: autorizadas sobre `admin:*` sin el `create` ni
el `update` de cada modelo, sin alcance de fila y sin políticas de campo. Es
el gemelo de escritura de OR-66: quien tenía `import_data` escribía cualquier
modelo, creando o actualizando, en filas ajenas y en campos que no podía
escribir — lo que el formulario del registro le negaba. El dueño lo decidió
el 2026-10-06 como arreglo de seguridad en una minor, no aplazado al 2.0:
**quien puede importar ya no escribe lo que no podría escribir a mano.**

Se enmienda así:

- **`import_data` sigue concediéndose sobre `admin:*`**, y dice quién
  importa, no qué. La importación (subida, validación y ejecución) y la
  carga de fixtures (`loaddata`, la misma operación en otro formato) piden,
  fila a fila, lo que pide el formulario: el `create` del modelo para una
  fila nueva y su `update` para una existente; el tenant de la petición (una
  fila de otro tenant se rechaza, nunca se re-tenantiza, y la que no nombra
  ninguno lo recibe estampado, como en el alta); bajo `#own`, sólo las filas
  propias para actualizar y el operador estampado como propietario al crear;
  y ningún campo que el operador no pueda escribir, que se rechaza por
  nombre y no se descarta (punto 5). El formulario y las dos superficies
  toman ese alcance de una sola función (`requestWriteScope`, gemela de
  `requestReadScope`), así que lo que se escribe a mano y lo que se importa
  no pueden separarse.
- **Qué fila es existente.** En la importación, sólo cuando `on_conflict`
  pide buscarla (`skip` o `update`): su clave primaria —bajo el nombre de la
  pk, su columna o su nombre Go— nombra una fila que el almacén tiene, o, si
  no, todas las columnas de un índice único están en la fila y coinciden con
  una fila del tenant de la importación. En la carga de fixtures, cuando su
  `pk` nombra una fila que el almacén tiene. Una fila existente de otro
  tenant no se encuentra, diga lo que diga `on_conflict` —un `skip`
  confirmaría que existe—, y una fuera de las filas propias del operador no
  se encuentra para actualizarla. Saltar una fila existente no escribe nada
  y no pide permiso. Cualquier otra fila es un alta.
- **Todo o nada.** El fichero se planifica entero antes de escribir ninguna
  fila, y una fila que el operador no puede escribir lo rechaza con un `403`
  que nombra la fila (contada desde 1), el modelo y el permiso, el campo o el
  alcance que falta — como desde OR-65 y OR-68 lo rechaza un modelo de sólo
  lectura. Lo que no es una cuestión de quién escribe sigue como estaba: una
  celda inválida, una `pk` inutilizable de una fixture o una escritura que el
  almacén rechaza son filas del informe.
- **La validación rechaza lo que rechazaría la ejecución**: recorre el mismo
  plan sin escribir.
- **El superusuario no cambia**: no le aplican el permiso del modelo, el
  `#own` ni las políticas de campo (punto 8). Sí le aplica, como en el
  formulario, el tenant de su petición: una fila de otro tenant rechaza ahora
  el fichero entero en vez de fallar sola.
- La pista `import_data` del esquema es falsa en un modelo del que el
  operador no puede crear ni actualizar filas, y Data Studio ofrece su
  importación —que crea cada fila que lee— a quien tiene además `create`.

**Cambio de comportamiento**: un operador con `import_data` y sin permisos
sobre un modelo importaba en él; ahora necesita su `create`, y su `update`
para un fichero que actualiza filas existentes. Un fichero con una fila de
otro tenant, que antes fallaba esa fila y escribía las demás, se rechaza
entero.

## Enmienda de OR-69 (2026-10-06): un campo que no se lee no se pregunta

El punto 5 decía que un campo que el operador no puede leer no sale del
registro, de la lista, del CSV ni del esquema. Salía de otra forma: la lista
filtraba, ordenaba y buscaba por él en nombre del operador. Con un `deny`
sobre `owner`, `?owner=operator` contestaba una fila y `?owner=nobody`
ninguna — el valor, una conjetura cada vez —, y un `order_by` paginaba las
filas en el orden del campo oculto. La exportación rechazaba ese filtro desde
OR-66; las demás superficies no.

Se enmienda así:

- **Un campo que el operador no lee no se puede nombrar en una consulta**, en
  ninguna superficie que lea filas por él: un filtro (con operador o sin él) o
  un `order_by` de la lista contestan el `400` que recibe un campo que el
  modelo no tiene — la negativa no dice que el campo existe —; la exportación
  rechaza el filtro con el mismo `400`; una búsqueda de relación etiqueta con
  un campo legible; una vista guardada que filtra u ordena por él no se le
  lista; y una tarjeta de registros ordenada por él no se le muestra, como no
  se muestra la de un modelo que no puede listar. Todas preguntan lo mismo
  que elige las columnas de una exportación (`fieldRules.readsField`), así
  que lo que se lee y lo que se pregunta no pueden separarse.
- **Una búsqueda que miraría en un campo oculto se rechaza, no se estrecha.**
  El backend busca en todos los campos que marca buscables y
  `datasource.Query` no tiene forma de decirle cuáles, así que el panel no
  puede limitar la búsqueda a los legibles: contesta `400` y el esquema lleva
  `searchable` en falso para que la rejilla no ofrezca la caja. Estrecharla
  pide un añadido al contrato congelado (QADR-0010), que es otro cambio.
- **Un campo excluido cuenta para todos**, superusuario incluido: nadie lo lee
  en el panel, y Nucleus busca en un campo buscable aunque esté excluido. El
  superusuario no cambia en lo demás: las políticas de campo no le aplican.

## Enmienda de OR-72 (2026-10-06): un hijo se escribe con el alcance de su propio formulario

El formulario de un registro lleva a sus hijos en el mismo payload
(ADR-009), y cada hijo se autorizaba sólo con el verbo del modelo hijo: no
con el tenant de la petición, ni con un `#own`, ni con las políticas de
campo. Y un hijo nombrado por id era cualquier fila del modelo hijo, no una
del registro que se editaba. El formulario de un registro era así un rodeo a
lo que el formulario del hijo rechaza.

Se enmienda así:

- **Un hijo pide lo que pediría su propio formulario**: el `create`, el
  `update` o el `delete` del modelo hijo; el tenant de la petición (uno que
  nombra otro se rechaza, el que no nombra ninguno lo recibe estampado); bajo
  `#own`, sólo las filas propias y el operador estampado al crear; y ningún
  campo que el operador no pueda escribir, rechazado por nombre (punto 5). Lo
  toma de la función del formulario, la importación y la carga de fixtures
  (`requestWriteScope`).
- **Un hijo nombrado por id es un hijo del registro que se edita**: su clave
  al padre apunta a él. Uno que no existe, de otro tenant, de otro
  propietario o de otro registro recibe el mismo `404`, y un registro que se
  está creando no tiene hijos todavía. Es integridad, no autorización:
  también aplica al superusuario.
- **Un hijo no cambia de registro**: un payload que nombra otro en la clave
  al padre —con cualquier grafía que el backend acepte, o dos veces— se
  rechaza con `400` en vez de re-estamparse en silencio (ADR-009, punto 4).
  En un alta, la clave estampada cuenta como campo escrito, igual que el
  tenant y el propietario estampados.
- **Un rechazo es de todo el guardado**: los hijos se planifican antes de
  escribir el padre, y uno rechazado rechaza el guardado entero con el estado
  de su rechazo y nombrándolo (`tracks[1]: …`). Lo que el almacén falle
  después sigue siendo, como dice ADR-009, un error por hijo en la respuesta.

**Cambio de comportamiento**: un hijo que nombraba otro registro en la clave
al padre se guardaba bajo el registro editado; ahora rechaza el guardado. El
formulario de Data Studio nunca envía esa clave.

## Enmienda de OR-73 (2026-10-06): el rastro enseña de una fila lo que enseña la fila

El historial de un registro (`GET /api/models/{modelo}/{id}/history`) lee
del rastro los valores de cada escritura de la fila, y preguntaba el `#own`
de `retrieve` pero no el tenant: una fila de otro tenant contestaba `404` en
el registro y `200`, con sus valores, en su historial. Y el rastro entero
(`GET /api/audit` y su copia CSV), concedido por `audit_view` sobre
`admin:*`, enseñaba los valores de cualquier fila a quien lo leyera, como
dejaba dicho «Lo que NO cubre».

Se enmienda así:

- **El historial lee con el alcance del registro** (`requestReadScope` con
  `retrieve`): una fila de otro tenant o, bajo `#own`, ajena recibe el mismo
  `404` que la fila —también el superusuario mientras la petición esté en un
  tenant, que deja con `?tenant=` como en todo registro—, y los campos que el
  operador no puede leer se enmascaran en cada entrada.
- **`audit_view` dice quién lee el rastro, no de quién son las filas.** Una
  entrada que guarda los valores de una fila (`create`, `update`, `delete`)
  los enseña sólo como lo haría el historial de esa fila: con el `retrieve`
  del modelo, dentro del tenant de la petición y, bajo `#own`, en las filas
  del operador, con los campos que no puede leer enmascarados. La fila puede
  no existir ya, así que de quién era se lee de los valores que guardó la
  entrada, cada lado por separado; unos valores que no lo dicen no se enseñan
  a quien está confinado. Lo que no se enseña queda vacío (`null`), y la
  entrada sigue listada: quién hizo qué, a qué registro y cuándo es lo que
  concede `audit_view`.
- El superusuario no cambia salvo por el tenant (punto 8).

**Lo que esta enmienda NO cambia**: las entradas no guardan el tenant, así
que el rastro sigue listando a un operador confinado las de otros tenants
—sin los valores de sus filas—, con su usuario, su IP y su agente. Las
entradas que no guardan una fila (acciones de la aplicación, cambios de
esquema, de RBAC…) siguen como estaban.

**Cambio de comportamiento**: un operador con `audit_view` y sin el
`retrieve` de un modelo leía en el rastro los valores de sus filas; ahora ve
esas entradas sin ellos.

## Enmienda de OR-77 (2026-10-07): un campo se conoce por todas sus claves

Una política por campo (`admin:Modelo.campo`) resolvía el campo que nombra,
y las claves de los registros que enmascara y de las cargas que guarda, por
la columna y el nombre Go del campo. El adaptador de Nucleus emite cada
registro con la clave JSON de cada campo (su etiqueta `json`) y acepta una
carga bajo esa misma clave, así que un campo cuya clave JSON es una tercera
grafía —ni la columna ni el nombre Go— era conocido por el backend y
desconocido por la política: un `deny` no lo enmascaraba en el registro, la
lista, el historial ni el rastro, y una escritura que lo nombraba por esa
clave se escribía donde la misma escritura por la columna se rechazaba. El
tenant y el propietario de una fila ya se resolvían por su clave JSON
(OR-23, `fieldJSONKey`); las políticas por campo no.

Se enmienda así:

- **Las reglas por campo resuelven una clave como lo hace el adaptador**
  (`fieldRules.resolve`): primero la columna y el nombre Go, después la clave
  JSON del campo, en cualquier caja. Vale para lo que enmascaran —registro,
  lista, exportación, historial y rastro— y para lo que guardan —formulario,
  importación, hijos en línea y carga de fixtures—, porque todas esas
  superficies pasan por las mismas tres funciones (`mask`, `maskValues`,
  `guardPayload`).
- **Una política puede nombrar el campo por cualquiera de las tres grafías**:
  `admin:Nota.secret_note`, `admin:Nota.Secret` y `admin:Nota.hidden` son la
  misma regla. El `403` sigue nombrando la columna.
- Un modelo sin registro de esquemas (una `DataSource` propia, Quark) no
  tiene clave JSON que resolver: sus registros van por columna, que las
  reglas ya conocían.

**Cambio de comportamiento**: una escritura que nombra por su clave JSON un
campo que el operador no puede escribir responde ahora `403` en vez de
escribirse, y un registro que lleva ese campo bajo su clave JSON lo pierde
en las lecturas de ese operador. Un modelo cuyas claves JSON coinciden con
sus columnas o sus nombres Go —la mayoría— no cambia.

## Preguntas abiertas

- Un operador de filtro con más gramática (rango, contiene, en) llega en la
  sesión `S5` del arco; el filtro de propiedad usa igualdad porque
  `datasource.Query.Filters` es columna→valor y está congelado.
