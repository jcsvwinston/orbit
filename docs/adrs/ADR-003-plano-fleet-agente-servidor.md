---
id: ADR-003
title: Plano fleet como subsistema opt-in (agent/server/proto, stream bidi)
status: accepted
date: 2026-08-31
deciders: jcsvwinston
related: [ADR-001, quantum/QADR-0006]
supersedes: null
tags: [orbit, fleet, agente, servidor, observabilidad]
---

# ADR-003 — Plano fleet como subsistema opt-in (agent/server/proto)

> **ADR retroactivo.** Documenta una decisión ya ejecutada y en producción
> desde hace varios ciclos; se escribe ahora porque es una de las primeras
> preguntas de cualquier auditor o adoptante y no tenía acta.

## Contexto

El panel in-process (raíz) muestra **un** proceso. Para observar una flota
de nodos hacía falta un plano aparte, y había dos caminos: engordar el
panel raíz con modo multi-nodo, o construir un subsistema separado.

## Decisión

Un plano fleet **separado y opt-in**, en tres módulos Go con tags de
componente propios (`proto/vX`, `agent/vX`, `server/vX`):

1. **`proto/`** define el contrato Connect-RPC; los stubs generados (Go y
   TS) se commitean para que un checkout limpio compile sin `buf`.
2. **`agent/`** es una extensión de Nucleus (`agent.NewExtension`) que abre
   un **stream bidireccional** hacia el servidor y publica por él la
   telemetría HTTP/SQL/sesiones del proceso anfitrión. Es estrictamente
   **opt-in y fail-open**: sin endpoint configurado no se activa, y un
   fallo del stream nunca toca el hot path del framework.
3. **`server/`** es un binario standalone (`admin-server`) con dos
   listeners: el de agentes y el de la UI. **No tiene acceso a ninguna base
   de datos de las apps** — todo lo que muestra (y toda mutación de Data
   Studio del fleet) viaja por el stream y la ejecuta el agente en su
   proceso. El listener de agentes es **fail-closed**: sin token, sin TLS y
   fuera de loopback, el servidor se niega a arrancar (el override
   explícito `--insecure-agent-listener` existe para redes ya controladas).

El panel raíz y el plano fleet no comparten proceso ni almacenamiento; la
única pieza común es el vocabulario de eventos.

## Consecuencias

- La mayoría de las apps montan solo el panel raíz; el fleet no les cuesta
  nada (ni dependencia ni superficie).
- El servidor no puede filtrar datos que nunca tiene: el radio de
  compromiso del binario fleet queda acotado a lo que los agentes le
  envían.
- El precio: por el stream **no viaja identidad de operador**, así que el
  RBAC por-modelo y el contexto multi-tenant de la app no corren en las
  operaciones del Data Studio del fleet. Ese hueco se mitiga hoy con
  puertas del lado servidor (allowlist de modelos mutables, deny por
  defecto) y su rumbo definitivo está pendiente de decisión en un
  borrador de ADR aparte (el rumbo del Data Studio del fleet).
- Tres módulos más en el `go.work` significan tres `go.mod` que pueden
  mentir; la lane standalone `GOWORK=off` del CI existe por esto.
