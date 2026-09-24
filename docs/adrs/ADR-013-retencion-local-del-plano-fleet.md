---
id: ADR-013
title: El servidor del fleet retiene localmente — eventos, audit y métricas con ventana, en un fichero
status: accepted
date: 2026-09-24
deciders: jcsvwinston
related: [ADR-003, ADR-006, ADR-008, quantum/QADR-0008]
supersedes: null
tags: [orbit, fleet, retención, servidor, agente]
---

# ADR-013 — El servidor del fleet retiene localmente

> **Estado: aceptado e implementado (2026-09-24, arco A9 de la suite,
> sesión `S6`).** Sustituye al invariante «el servidor NO es persistencia»
> que `server/doc.go` declaraba desde el ADR-003, sin invertirlo: sin
> directorio de datos el servidor sigue siendo el de siempre.

## Contexto

El servidor de administración del plano fleet nació como un enrutador sin
estado duradero (ADR-003): los eventos que reenvía a la UI viven en anillos
acotados por tipo, las métricas de host se reducen a la última muestra por
nodo, y el rastro de auditoría del fleet es un anillo de 2048 entradas. Un
reinicio lo vacía todo. `server/doc.go` lo decía por escrito: «The server
is NOT persistence».

La medición del arco A9 (`internal/fleettest/fleetbench`, familia
`retention`, siete controles) lo confirmó y le puso números: `RET-01`,
`RET-02`, `RET-04`, `RET-05` y `RET-06` ausentes; sólo el anillo acotado
(`RET-07`) presente. Y la sesión `S5` acababa de hacer que el audit del
fleet dijera qué cambió (antes y después de cada mutación): un rastro que
se pierde al reiniciar el proceso que lo escribió es una promesa a medias.

Del lado del agente, la misma medición encontró que el buffer por tipo
«no puentea desconexiones»: el agente se suscribía al bus sólo con un
stream abierto, así que lo emitido entre una caída y la siguiente conexión
aceptada no se capturaba en ningún sitio.

## Opciones consideradas

1. **Seguir sin persistir** y documentarlo como límite del producto. Honesto
   con el ADR-003, pero deja el rastro de auditoría y el historial de
   métricas como cosas que el fleet no puede ofrecer, y el plano fleet ya
   existe para que un operador no tenga que entrar nodo a nodo.
2. **Delegar en una base externa** (PostgreSQL, un bus). Añade una
   dependencia operativa a un binario que hoy se arranca con dos flags, y
   la misma decisión de esquema y ventana habría que tomarla igual.
3. **Un almacén local embebido, opcional, con ventana.** Un fichero bajo un
   directorio de datos, abierto sólo si el operador lo pide; lo que no cabe
   en la ventana se deja de servir y se borra.

## Decisión

**Opción 3.** El servidor retiene localmente cuando se le da un directorio
de datos (`server.Config.DataDir`, `--data-dir`), en un solo fichero
SQLite (`server/store`, `fleet.db`) con el driver puro en Go que la
distribución del binario ya arrastra por el driver de Nucleus: sin cgo,
sin dependencia nueva. Retiene tres cosas:

- **los eventos** que reenvía a la UI: cada evento recibido de un agente se
  escribe además de entrar en el anillo, y al arrancar el servidor calienta
  el anillo con lo más reciente del fichero, así que una UI que se abre
  tras un reinicio ve lo que vio el proceso anterior;
- **el rastro de auditoría del fleet**: cada entrada se escribe, y
  `ListAudit` y la descarga leen del fichero;
- **las muestras de métricas de host por nodo**, una por heartbeat, la
  serie que la UI podrá pedir cuando el protocolo declare el RPC.

**La ventana manda** (`server.Config.Retention`, `--retention`, siete días
por defecto): ninguna lectura devuelve una fila más antigua que la ventana,
aunque el purgador no haya pasado todavía, y un purgador borra las filas
antiguas cada minuto (o diez veces por ventana si es más corta). Un valor
negativo conserva todo.

**El rastro se descarga** en `GET /api/audit/export?format=csv|json` del
listener de la UI, detrás de la misma cadena de autenticación que los RPC,
las mismas filas que `ListAudit` sirve, hasta diez mil.

**El agente aparca lo que ocurre sin stream**: cuando el stream termina, el
agente recuerda los filtros a los que el servidor estaba suscrito y sigue
escuchando el bus bajo ellos, empujando los eventos al buffer por tipo que
ya tenía; el siguiente stream vacía ese buffer nada más registrarse. Está
acotado como el buffer: una caída más larga que él conserva lo más
reciente por tipo y cuenta lo que se fue.

**Sin directorio de datos, nada de esto corre**: los anillos en memoria,
el reinicio que vacía, el mismo servidor. La retención es opt-in porque
escribir en disco donde antes no se escribía es un cambio que el operador
debe pedir, no descubrir.

## Consecuencias

- El escritor es uno solo, detrás de un canal con lotes: recibir un evento
  cuesta un envío a canal, no una transacción. Si el escritor se retrasa
  más de lo que el canal admite, la recepción espera; la retención que
  descarta bajo carga no es retención.
- Una lectura de audit vacía el canal antes de leer (`Flush`): quien
  escribe lee lo que escribió.
- El fichero crece con la ventana y el tráfico; no hay tope de tamaño
  distinto de la ventana. Si hiciera falta, es una decisión sucesora.
- El historial de métricas se retiene ya, pero la UI no puede pedirlo hasta
  que `admin.proto` declare el RPC (`RET-03`); ese cambio de proto se
  agrupa con los de las sesiones siguientes para pagar un solo corte.
- `server/doc.go` deja de afirmar que el servidor no es persistencia y
  dice lo que es: persistencia opcional, local y con ventana.

## Lo que NO decide

- Estado compartido entre servidores (familia `ha`, `S8`): el fichero es
  de un proceso. Dos servidores con el mismo directorio de datos no están
  contemplados y no se protege contra ello más allá de lo que SQLite hace.
- Exportar eventos o métricas: sólo el rastro de auditoría se descarga.
- Cifrado del fichero en reposo: el directorio de datos hereda los
  permisos del sistema; quien pueda leerlo lee lo que las UIs vieron.
