---
id: ADR-014
title: Una flota de servidores de administración — registro compartido, relay de eventos y asignación de nodos
status: accepted
date: 2026-09-24
deciders: jcsvwinston
related: [ADR-003, ADR-006, ADR-013]
supersedes: null
tags: [orbit, fleet, servidor, alta-disponibilidad]
---

# ADR-014 — Una flota de servidores de administración

> **Estado: aceptado e implementado (2026-09-24, arco A9 de la suite,
> sesión `S8`).** `server/doc.go` decía «single-instance by default» y que
> el activo-activo no estaba implementado; esta acta decide cómo lo está.

## Contexto

Un servidor de administración era un proceso solo (ADR-003): los agentes
llevan una lista de endpoints y hacen failover al siguiente que contesta,
pero dos servidores vivos no se conocen. La medición del arco A9 (familia
`ha` del banco `internal/fleettest/fleetbench`) lo dejó en cifras: un agente
conectado a A no aparece en B (`HA-02`), una UI en B no ve los eventos que A
recibe (`HA-03`), nada asigna agentes entre servidores (`HA-04`), y el
stream que un reconexión reemplaza no se termina, así que sus frames siguen
publicándose como el nodo (`HA-05`, hallazgo OR-56).

La sesión `S6` (ADR-013) dio al servidor un fichero propio con ventana; el
lote de proto que la siguió declaró `PeerService.Sync`, sus frames y
`Command.redirect`, para que esta sesión no pagara un corte propio.

## Decisión

**Una malla de servidores iguales sobre el listener de agentes.** Cada
servidor conoce a los demás por configuración (`--peers`: los endpoints de
sus listeners de agentes) y mantiene **un stream saliente hacia cada uno**,
autenticado como un agente (token o certificado de cliente). Por ese stream
empuja su estado en un solo sentido: `PeerHello`, sus nodos locales, y
después cada cambio de nodo, cada evento y cada muestra de métricas que sus
agentes le envían. El otro extremo, `PeerService`, recibe y no reenvía: la
malla es de **un salto**, y dos servidores sostienen dos streams, uno por
sentido, sin deduplicar nada.

**Registro compartido.** Un nodo que un peer anuncia entra en el registro
local como **remoto**: se lista (con el servidor de origen en la etiqueta
reservada `orbit.server`, porque el cable no tiene campo para ello), sus
métricas se actualizan, pero no tiene stream aquí: nada se le encola, y una
petición de Data Studio dirigida a él se rechaza nombrando al propietario.
Un nodo conectado a ESTE servidor nunca lo sustituye un anuncio remoto: el
stream aquí es la verdad sobre él. Cuando un peer se va, sus nodos se van
con él.

**Relay de eventos, y demanda total.** Un evento que llega de un agente
local se publica en el bus propio, entra en el replay, se retiene si hay
fichero (ADR-013) y se reenvía a cada peer, que lo publica a sus
suscriptores y a su replay — **no lo retiene**: el origen retiene. La malla
lleva eventos, no los filtros de las UIs de los peers, así que **mientras un
peer está conectado, los agentes de este servidor envían todo** y cada
servidor filtra para sus propias suscripciones. Es el precio de no tocar el
proto en esta sesión; el campo de demanda en `PeerFrame` es la adición
natural del corte siguiente.

**Asignación determinista y redirección.** Con `--assign-nodes`, cada nodo
tiene un propietario elegido por **rendezvous hashing** (máximo de
`hash(node_id, endpoint)`) sobre este servidor y los peers que alcanza en
ese momento: todos los servidores con el mismo conjunto eligen el mismo, y
un servidor que entra o sale mueve sólo los nodos que gana o pierde. Un
agente que se registra donde no le toca recibe `Command.redirect` con el
endpoint de su propietario; el stream sigue abierto hasta que el agente se
va, así que un nodo cuyo propietario no responde sigue servido aquí. El
agente **acepta la redirección sólo hacia un endpoint que el operador le
configuró**: el servidor manda sobre la flota, el operador sobre dónde puede
conectar su agente. Hace falta `--agent-advertise-addr` para recibir
asignaciones: la flota no puede asignar nodos a un servidor que no sabe
nombrarse.

**El stream reemplazado termina** (OR-56). El handler del stream espera a su
propio contexto además de a `Receive`; cuando un registro más nuevo lo
desaloja, devuelve `Aborted` al extremo viejo —que ve el error y su stream
cerrado— y descarta el frame que hubiera entrado en la carrera.

## Consecuencias

- Configurar peers cambia lo que los agentes envían: todo, mientras haya un
  peer conectado. Un despliegue con muchos agentes y UIs selectivas paga
  ancho de banda por la simetría de la malla.
- La malla es de un salto: con tres servidores, cada uno configura a los
  otros dos. No hay descubrimiento; la lista es configuración.
- Dos servidores no deben compartir directorio de datos (ADR-013 sigue
  valiendo): cada uno retiene lo que sus agentes le envían.
- Las alertas se evalúan donde está el agente: un peer no evalúa las
  métricas relayadas, así que las reglas viven en cada servidor y el
  propietario de un nodo es quien alerta sobre él.

## Lo que NO decide

- Descubrimiento de servidores ni consenso: no hay líder ni quórum; cada
  servidor decide la asignación con lo que ve, y dos servidores con vistas
  distintas de la malla pueden asignar distinto durante una partición.
- Relay de peticiones de Data Studio o de snapshots a otro servidor: la UI
  habla con el propietario, al que la etiqueta señala.
- Cifrado propio del tráfico entre peers: es el del listener de agentes.
