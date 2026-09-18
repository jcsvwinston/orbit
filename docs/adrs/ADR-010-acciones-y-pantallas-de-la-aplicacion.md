---
id: ADR-010
title: La aplicación añade sus verbos y sus pantallas al panel, por adición y bajo sus mismas reglas
status: accepted
date: 2026-09-18
deciders: jcsvwinston
related: [ADR-001, ADR-004, ADR-007]
supersedes: null
tags: [orbit, extensibilidad, acciones, pantallas, rbac]
---

# ADR-010 — Acciones y pantallas que declara la aplicación

## Contexto

El panel sabe hacer lo que toda tabla hace —crear, editar, borrar, exportar—
y enseña las pantallas que toda aplicación tiene. Lo que un PRODUCTO necesita
encima es suyo: «publica estos tres borradores», «reintenta estos pagos», una
pantalla de conciliación que no se parece a ninguna otra.

Hasta aquí el único punto de extensión era `DataSource` (ADR-001), que
sustituye el BACKEND y no toca la interfaz. La medición `S0` del arco A6 lo
registró como `DS-09` («acciones que una aplicación define para sus propios
modelos», la lista de verbos de bulk estaba cerrada en `delete`/`export`) y
`CUST-04` («una aplicación puede añadir su propia pantalla o control»).

El panel **no puede descubrir** ninguna de las dos cosas: que una columna se
llame `status` no dice que «publicar» signifique algo, y una pantalla de
conciliación no se deduce de un esquema. Las declara la aplicación, como ya
declara la columna de propiedad de fila (ADR-007), los widgets de campo
(ADR-009) y su caché (OR-49).

## Decisión

1. **Dos campos nuevos en la superficie de montaje**, ambos sólo desde Go
   (llevan funciones; no hay nada que `nucleus.yml` pueda enlazar):
   `orbit.Config.Actions []ModelAction` y `orbit.Config.Pages []Page`.

2. **El verbo ES el permiso.** Una acción llamada `publish` se autoriza como
   `publish` sobre `admin:<Modelo>`, con la misma máquina que `delete`. No se
   inventa un espacio de permisos paralelo: quien escribe políticas ya sabe
   escribir ésta.

3. **La acción corre POR el panel, y por eso hereda su confinamiento.** Los
   ids que recibe la función de la aplicación son los que ese operador puede
   tocar: el alcance por tenant y el `#own` de ADR-007 se aplican ANTES de
   llamarla, y las filas de fuera vuelven al cliente como fallos por id, como
   en un borrado masivo. Una acción que los saltara sería el rodeo de todas
   las políticas de fila que el panel defiende — que es justamente lo que se
   quiere evitar al ofrecer el punto de extensión.

4. **Una selección rechazada por completo NO llega a la función**
   (`ran: false`). Una acción a la que se le entrega una lista vacía sería
   indistinguible de una invocada sobre la tabla entera, que es lo que
   `AllowEmptySelection` declara aparte.

5. **La llamada queda auditada como `action.<nombre>`**, con el modelo, los
   ids y lo que la acción reportó, **haya terminado o no**. Una acción que
   falla a medias ya tocó filas; sólo el rastro distingue eso de una que no
   empezó.

6. **Una declaración que el panel no puede honrar impide arrancar**: modelo
   inexistente, verbo duplicado, verbo del propio panel, `Run` nulo, página
   sin handler, id de página con barra. Todas serían, si no, un control que
   nunca aparece sin que nada diga por qué — la degradación silenciosa que
   este arco lleva encontrando.

7. **Una pantalla es un `http.Handler` montado DENTRO del panel**
   (`<prefijo>/x/<id>/`): bajo su sesión, autorizada como `view` sobre
   `admin:page:<id>` —espacio de nombres propio, para que una concesión sobre
   un modelo no pueda abrir una pantalla— y listada en su navegación. La
   aplicación escribe la página; el panel pone alrededor lo que no debería
   tener que reconstruir. El operador viaja en el contexto
   (`orbit.OperatorFromContext`).

8. **La pantalla es un ENLACE, no un marco.** El panel manda
   `X-Frame-Options: DENY` y `frame-ancestors 'none'` en toda respuesta: una
   pantalla embebida en la SPA la bloquearía el navegador mientras cada test
   en Go seguiría leyendo un 200, y relajar esa cabecera en todo el panel para
   embeber una pantalla cambiaría una defensa contra clickjacking por una
   maquetación. Su CSP también se aplica a la página: los scripts vienen de
   ficheros, no de `<script>` en línea.

9. **Lo que el operador no puede ejecutar no se le ofrece**: el esquema sólo
   lleva las acciones que tiene, y la navegación sólo las páginas que puede
   abrir. Las pistas de permiso siguen llevando el verbo en `false`, como
   cualquier verbo de registro que no tenga.

## Consecuencias

- **Adición al contrato congelado**: `orbit.Config.Actions`,
  `orbit.Config.Pages`, los tipos `ModelAction`, `ActionRequest`,
  `ActionResult`, `Page`, `Operator` y la función `OperatorFromContext`.
  Ninguna firma existente cambia (QADR-0010: lo rompiente espera al major).
- **Los verbos de registro quedan reservados** como nombres de acción
  (`delete`, `export`, `create`, `update`, `list`, `retrieve`, `export_csv`,
  `bulk_delete`, `bulk_export`, `get_schema`): una acción llamada `update`
  la concedería una política sobre editar.
- **Una ruta sin método junto al catch-all de la SPA no arranca**: el
  fallback es `GET /{path...}` y `ServeMux` considera ambiguo un patrón para
  todos los métodos al lado. Las páginas registran sus métodos uno a uno.
- **El panel no dibuja la pantalla dentro de su layout**: es navegación, no
  composición. Un lienzo de widgets dentro de la SPA es otro problema
  (`CUST-03`, sesión `S8`), y resolverlo con un `iframe` habría sido resolver
  la cabecera, no el problema.
- **La acción no recibe la petición HTTP**, sólo su contexto y lo que el
  panel resolvió (modelo, ids, operador, tenant, alias de base). Pasarle el
  `*http.Request` invitaría a releer cabeceras que el panel ya interpretó, y
  el confinamiento dejaría de ser la única lectura posible.
