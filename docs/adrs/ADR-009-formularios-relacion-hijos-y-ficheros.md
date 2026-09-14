---
id: ADR-009
title: Un formulario resuelve la relación, edita los hijos y acepta un fichero — sin transacción y sin fingirla
status: accepted
date: 2026-09-14
deciders: jcsvwinston
related: [ADR-001, ADR-007]
supersedes: null
tags: [orbit, datastudio, formularios, relaciones, storage]
---

# ADR-009 — Relación, hijos y ficheros en un formulario

## Contexto

El panel editaba tablas de escalares. Tres cosas que cualquier admin de
producto necesita no existían, y la medición `S0` del arco A6 las registró
como `DS-10`, `DS-11` y `DS-12`:

- **La relación**: el esquema marcaba la clave foránea y nada decía a QUÉ
  podía apuntar, así que el formulario enseñaba un id crudo y la persona
  tecleaba un número que tenía que saberse.
- **Los hijos**: «un pedido con sus líneas» eran dos pantallas y un id que el
  operador cargaba en la cabeza. Un payload anidado no se rechazaba: **se
  descartaba**, que es peor, porque el formulario parecía haber guardado.
- **Los tipos**: un documento JSON, un texto rico o un fichero se editaban con
  un input de texto, o no se editaban.

## Decisión

1. **La relación se resuelve con dos endpoints de sólo lectura**
   (`/options` de un modelo y `/fields/{field}/options` de un campo), que
   devuelven `value` + `label` legible y pasan por la MISMA maquinaria de
   lista: búsqueda, confinamiento por tenant y alcance por fila.

2. **El permiso del lookup es el del DESTINO.** Resolver qué significa un id
   de Author es leer Authors. Quien puede editar el registro pero no navegar
   el destino recibe 403 y el formulario cae al id crudo: el panel **no
   ensancha una concesión** para dibujar un widget más bonito.

3. **Los hijos viajan en el payload del padre**, con la colección nombrada por
   el plural del hijo. Un hijo con id es edición, sin id es alta, y el que
   debe irse **lo dice** (`_delete`). **La ausencia nunca borra**: un
   formulario que cargó dos de cinco líneas borraría las tres que no enseñó.

4. **La clave al padre la estampa el panel**, nunca el payload: un hijo que
   nombre otro padre sería una escritura en un registro que el operador no
   estaba editando.

5. **Escribir un hijo exige los permisos del MODELO hijo**, y se comprueban
   **antes** de escribir el padre. El orden importa: sin transacción, un
   rechazo descubierto después dejaría el padre guardado y al formulario un
   «prohibido» sobre el que nadie puede actuar.

6. **No es transaccional, y no se finge.** El contrato `datasource` (ADR-001)
   escribe una fila; el padre va primero y cada hijo se reporta por separado
   en la respuesta (`inlines`). Quien necesite todo-o-nada necesita antes un
   origen de datos transaccional. Está dicho en la doc pública, no supuesto.

7. **El vocabulario de widgets crece** con `json`, `richtext`, `file` e
   `image`. El documento se **infiere** del tipo (un mapa o un slice es un
   documento); «esta cadena es HTML» y «esta cadena es una clave de storage»
   son afirmaciones de intención que ningún tipo lleva, así que las **declara
   la aplicación** (`field_widgets`), igual que declara la columna de
   propiedad de fila (ADR-007).

8. **Un campo de fichero guarda una CLAVE**, y la única forma honesta de
   producirla es enviar el fichero: `POST /api/models/{model}/upload` lo
   guarda en el storage de la aplicación —el panel no se convierte en
   servidor de ficheros— y responde con la clave. Rechaza un campo que no sea
   de fichero, acota a 32 MB, se queda sólo con el nombre base de lo que el
   cliente dijo, y **queda auditado**.

## Consecuencias

- **Una excepción más en el gate de content-type**: `multipart/form-data` ya
  sólo se aceptaba en la subida de importación; ahora también en la subida por
  campo, reconocida por FORMA de ruta (`/api/models/{modelo}/upload`) y no por
  sufijo, para que un `/upload` en otro sitio no herede la exención.
- **Un update cuyo payload sólo trae hijos es un update legítimo**: el padre
  no se escribe (ni se audita un cambio que no ocurrió), pero se comprueba que
  la fila existe y que el operador la alcanza.
- **Adición al contrato congelado**: `orbit.Config.FieldWidgets`.
- **Lo que NO hace**: no hay editor WYSIWYG. El texto rico se edita como
  marcado — un editor que reescribe lo que no entiende es peor que un área de
  texto que no lo toca. Y la relación many-to-many pura (tabla intermedia sin
  modelo) sigue fuera: lo que hay es la relación uno-a-muchos declarada por
  una clave foránea.
