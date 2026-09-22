---
id: ADR-002
title: Rumbo del Data Studio del plano fleet (identidad y autorización)
status: implemented
date: 2026-08-31
deciders: jcsvwinston
related: [ADR-001, ADR-003, quantum/QADR-0006]
supersedes: null
tags: [orbit, fleet, data-studio, seguridad]
---

# ADR-002 — Rumbo del Data Studio del plano fleet

> **Estado: implementado (2026-09-22, arco A9 de la suite, sesiones `S3` a
> `S5`).** La decisión es la D2 de la auditoría integral 2026-08-30,
> registrada en `quantum/docs/RUMBO.md`: el plano fleet consume el contrato
> `datasource`. La Opción B (declarar el fleet «telemetría + lectura») quedó
> descartada. Este acta conserva el análisis de ambas opciones tal como se
> escribió; la sección «Decisión» es la que manda y la sección «Ejecución»
> dice con qué se cumplió cada paso del plan.

## Contexto

El plano fleet (agent/ + server/) tiene dos Data Studios de facto:

1. **El del panel in-process** (raíz, `internal/admin`): opera dentro del
   proceso de la app, con la sesión del operador, su RBAC (`authorizeAction`)
   y el contexto multi-tenant del request. Desde ADR-001 habla el contrato
   neutral `datasource`.
2. **El del fleet** (`agent/datastudio` + `server/services.DataStudioService`):
   el servidor de administración no tiene acceso a base de datos; reenvía la
   operación por el stream bidi al agente, que la ejecuta con **su propio**
   acceso a la base. Por el stream **no viaja ninguna identidad de operador**,
   así que en el agente no corren ni el RBAC por-modelo de la app ni la
   resolución/filtrado multi-tenant, y el bus de señales es nil.

La auditoría (AO-3, P1) encontró que la documentación del paquete afirmaba lo
contrario («preserving signals, validation, multi-tenant resolution, RBAC»).
Esa doc ya se corrigió, y como mitigación mínima el servidor aplica hoy dos
puertas del lado servidor:

- allowlist de modelos mutables (`Config.DataStudioAllowedModels` /
  `--datastudio-allowed-models`), deny-by-default para toda mutación;
- roles de operador de solo lectura (`viewer` / `--ui-read-only`).

Eso acota el daño pero no resuelve el problema de fondo: la autorización del
plano fleet es por-servidor y por-modelo, no por-operador ni por-tenant. Las
lecturas siguen sin filtrado de tenant.

## Opción A — El fleet consume el contrato `datasource` con identidad propagada

Extender el proto del stream para que cada `DataStudioRequest` lleve la
identidad resuelta del operador (sujeto, rol, tenant), y que el agente ejecute
la operación a través del mismo camino autorizado que el panel in-process
(contrato `datasource` de ADR-001 + `Authorizer` + contexto de tenant), en vez
de un `model.CRUD` crudo.

- A favor: una sola semántica de autorización en toda la suite; el trabajo de
  ADR-001 se reutiliza; el fleet deja de ser una puerta trasera al RBAC; las
  lecturas quedan filtradas por tenant.
- En contra: es un arco entero, no un parche — cambia el proto (compatibilidad
  del stream con agentes viejos), exige mapear identidades del plano fleet a
  identidades de la app (¿el operador del fleet existe como usuario de cada
  app?, ¿con qué rol?), y complica el modelo de despliegue (hoy el agente no
  confía en nada de lo que diga el servidor más allá del transporte).
- Riesgo clave: una propagación a medias (identidad que el agente no puede
  verificar) daría apariencia de RBAC sin su garantía.

## Opción B — El fleet es telemetría + lectura; las mutaciones no son su trabajo

Declarar que el plano fleet es un plano de OBSERVABILIDAD: streams, topología,
snapshots, métricas, y a lo sumo lecturas de Data Studio explícitamente
habilitadas. Retirar (o dejar permanentemente deny-by-default sin wildcard) la
superficie de mutación; quien necesite editar datos usa el panel in-process de
cada app, donde la identidad, el RBAC y el tenant ya existen.

- A favor: honesto con la arquitectura actual (no hay identidad en el stream y
  no hay que inventarla); menor superficie de ataque; el panel in-process ya
  cubre la edición con garantías.
- En contra: pierde el caso de uso «arreglar un dato desde la UI del fleet sin
  entrar nodo a nodo»; deja dos UIs con capacidades distintas y hay que
  explicarlo; el trabajo ya hecho en la ruta de mutación del fleet queda como
  código a retirar o congelar.
- Riesgo clave: si la demanda real de mutación remota existe, reaparecerá como
  presión para reabrir la puerta «solo esta vez».

## Qué NO cubre este borrador

- El filtrado de tenant en LECTURAS del fleet: ninguna de las dos puertas
  actuales lo da. La opción A lo resolvería; con la opción B habría que
  decidir si las lecturas de Data Studio del fleet se quedan (documentadas
  como sin filtrar y detrás del allowlist) o se van con las mutaciones.
- La autorización por-operador del resto de superficies del fleet (snapshots,
  streams), que hoy es binaria (read-write/viewer).

## Decisión

**Opción A: el fleet consume el contrato `datasource` con identidad
propagada.** Orbit deja de ser bicéfalo: el Data Studio del plano fleet pasa
a operar a través del mismo `datasource.DataSource` (ADR-001) que el panel
in-process, de modo que el RBAC por-modelo, el filtrado de tenant y los
orígenes alternativos (`quarkdatasource`) valgan en los dos planos.

Consecuencias que se aceptan con la decisión:

- El proto del stream crece (adición compatible, reglas de `proto/EVOLUTION.md`)
  para que cada `DataStudioRequest` lleve la identidad resuelta del operador
  (sujeto, rol, tenant). Los agentes que no la entiendan siguen funcionando
  con la puerta actual (allowlist + solo-lectura), que se mantiene como
  mínimo hasta que la propagación esté completa.
- El agente ejecuta la operación por el camino autorizado del panel
  (contrato `datasource` + `Authorizer` + contexto de tenant), no por un
  `model.CRUD` crudo. `agent/datastudio` deja de importar `nucleus/pkg/model`
  directamente.
- Una propagación a medias no vale: hasta que el agente pueda verificar la
  identidad que recibe, las mutaciones del fleet siguen deny-by-default. El
  riesgo señalado en la Opción A («apariencia de RBAC sin su garantía») se
  mitiga no relajando ninguna puerta antes de tiempo.
- Las lecturas de Data Studio del fleet quedan filtradas por tenant al
  llegar la identidad; mientras tanto siguen documentadas como sin filtrar y
  detrás del allowlist.

## Plan

1. Extender el proto (identidad en `DataStudioRequest`) — módulo `proto/`.
2. Que el agente construya un `datasource.DataSource` sobre su runtime y
   enrute las operaciones por él, con la identidad recibida — módulo `agent/`.
3. Que el servidor rellene la identidad desde la cadena de auth de la UI y
   la envíe — módulo `server/`.
4. Retirar la doble implementación (`agent/datastudio` sobre `model.CRUD`) y
   registrar `quarkdatasource` también en el fleet.

Cada paso lleva sus tests y su doc en el mismo PR (regla de la suite). El
estado de ejecución del arco se sigue en `quantum/docs/RUMBO.md`, no aquí.

## Ejecución

Los cuatro pasos del plan, con el PR que los cumplió y el control del banco
`internal/fleettest/fleetbench` que lo mide:

1. **Proto**: `OperatorIdentity` y `DataStudioRequest.operator` (sujeto,
   correo, rol, solo lectura, tenant), `RecordFilter` y
   `ListRecordsRequest.where` — orbit#506, `proto/v0.5.0`. Después,
   `AuditEntry.before_json`/`after_json` y `DataStudioResponse.previous` —
   orbit#512, `proto/v0.6.0`.
2. **Agente**: el contrato pasa a módulo hoja con su adaptador Nucleus
   dentro (ADR-012, orbit#509, `datasource/v1.0.0`) y `agent/datastudio`
   se reescribe sobre `datasource.DataSource` bajo el operador recibido:
   claims del framework en el contexto, la política de la aplicación por
   modelo y verbo, confinamiento por tenant — orbit#511, `agent/v0.10.0`
   (`FDS-05`, `FDS-06`, `FDS-07`, `FDS-08`, `FDS-09`, `FDS-10`).
3. **Servidor**: rellena `operator` desde `auth.Identity` en cada petición,
   con el tenant de la cabecera del proxy de confianza — orbit#511,
   `server/v0.15.0`; y escribe en su audit los valores antes y después de
   cada mutación con lo que el agente devuelve — la parte 2 de `S5`
   (`FDS-11`).
4. **La doble implementación se retira** con orbit#511 (`agent/datastudio`
   ya no importa `nucleus/pkg/model`), y `quarkdatasource` entra en el
   fleet por `agent.ExtensionConfig.DataSource`: una aplicación Quark pasa el
   mismo adaptador al agente y al panel, y el banco lo prueba con un agente
   real sobre un cliente Quark (`TestQuarkDataSourceInTheFleet`).

La consecuencia que la decisión aceptaba como riesgo —una propagación a
medias— se evitó no relajando ninguna puerta: la allowlist de modelos
mutables y los operadores de solo lectura siguen vigentes en el servidor,
y el agente sin operador (un servidor anterior) se comporta como antes.
