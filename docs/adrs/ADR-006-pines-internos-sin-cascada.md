---
id: ADR-006
title: Ningún módulo hermano requiere a otro hermano por tag salvo el contrato del protocolo
status: accepted
date: 2026-09-05
deciders: jcsvwinston
related: [ADR-003, quantum/QADR-0002, quantum/QADR-0008]
supersedes: null
tags: [orbit, fleet, release, módulos, tren]
---

# ADR-006 — Ningún módulo hermano requiere a otro hermano por tag salvo el contrato del protocolo

## Contexto

Orbit son seis módulos Go en un repositorio, cortados por release-please
desde **un solo PR de release**: la raíz, `proto`, `agent`, `server`,
`quarkbridge` y `quarkdatasource` reciben su tag del mismo commit. Tres de
ellos se requerían entre sí por tag:

- `agent` requiere `proto` (código generado del contrato Connect-RPC).
- `server` requiere `proto` (lo mismo) **y `agent`**.
- `quarkdatasource` requiere la raíz (el contrato `datasource`, ADR-001).

Cortar todos del mismo commit tiene una consecuencia mecánica: cuando el
tag de `proto` sale, `agent` y `server` ya están etiquetados requiriendo el
tag **anterior** de `proto`. Y el guard `check_internal_pins.sh` tiene razón
al rechazarlo: `go install github.com/jcsvwinston/orbit/server/cmd/admin-server@server/vX`
resuelve `agent` y `proto` por el suelo que declara `server/go.mod` —nadie
más eleva la versión en ese grafo—, así que el binario publicado se lleva
el código viejo de sus hermanos. Converger exige un corte por nivel de la
cadena: `proto` → `agent` → `server`.

Lo que costó, medido en los dos últimos trenes:

| Tren | Raíces de orbit cortadas | De ellas, solo por converger pines internos |
|---|---|---|
| Quantum 1.26.1 (2026-09-04) | 3 (v1.8.18 → v1.8.20) | 2 |
| Quantum 1.26.2 (2026-09-05) | 5 (v1.8.21 → v1.8.25) | 2 (+1 por liberar bumps de Dependabot en `proto`/`agent`/`server`, +1 por `quarkdatasource`) |

Cinco de ocho cortes de raíz no publicaron ningún cambio de producto. Cada
corte son unos quince minutos de CI, un release PR, sus notas y una
oportunidad de que release-please se auto-bloquee al etiquetar (pasó dos
veces).

Dos hechos que leídos en el código cambian el problema:

1. **`server` importa `agent` solo desde tests.** Los tres ficheros que lo
   hacen (`agent_token_stream_test.go`, `datastudio_integration_test.go`,
   `manage_integration_test.go`, 860 líneas) arrancan un `agent.Agent` real
   contra el servidor. Ningún fichero de producción de `server` importa
   `agent`. El `require` que dispara el tercer corte existe para poder
   compilar esos tests.
2. **Los cambios recientes de `proto` no eran de protocolo.** Fueron bumps
   de Dependabot de `connectrpc.com/connect`, en commits `chore` que
   release-please no libera, y que el manifiesto del paraguas rechaza como
   «código de módulo sin tag».

## Decisión

1. **`server` deja de requerir `agent`.** Los tests de integración que
   arrancan un agente real se mudan a un módulo de solo test,
   `internal/fleettest` (`github.com/jcsvwinston/orbit/internal/fleettest`),
   que requiere `server`, `agent` y `proto` y se resuelve por el `go.work`
   del repo. Es un módulo **no publicable por construcción**: la regla
   `internal` de Go impide que nadie fuera de `github.com/jcsvwinston/orbit/...`
   lo importe, no tiene entrada en release-please ni en el manifiesto del
   paraguas, y sus pines a los hermanos no pasan por `check_internal_pins.sh`
   (en el workspace resuelven al árbol; fuera del workspace no se usa).
2. **`proto` es un módulo hoja que solo cambia a propósito.** Dependabot no
   toca sus dependencias Go: `connect-go` y `protobuf-go` se suben a mano
   junto con la regeneración de los stubs (`make proto`), que es cuando
   cambiar la versión tiene sentido. Un cambio real en `proto` asume **dos**
   cortes (el suyo y el de `agent`/`server` re-pinados), y ese segundo corte
   lo hace el driver del tren, no una persona.
3. **Los bumps de Dependabot en `agent`, `server`, `quarkbridge` y
   `quarkdatasource` van con prefijo `fix(deps)`**, para que release-please
   los libere en el mismo tren en que se fusionan y el manifiesto del
   paraguas no encuentre código de módulo sin tag. La raíz sigue con `chore`:
   se corta con cualquier `fix` de sus hermanos.

La única arista de tag que queda entre hermanos es `agent`/`server` →
`proto`, y es legítima: es el contrato del protocolo, que cambia poco y a
propósito. La de `quarkdatasource` → raíz ya tiene su excepción topológica
(`check_internal_pins.sh`, ≤1 minor mientras el contrato esté congelado).

## Alternativas descartadas

- **Fusionar `proto`, `agent` y `server` en un módulo `fleet`.** Elimina
  todas las aristas, pero cambia las rutas de import de los tres y el
  `go install` de `admin-server`: es un cambio mayor de la suite (QADR-0002,
  lockstep de majors). Queda como candidato para el 2.0 que el plan agrupa en
  A9 (fleet unificado), no para un patch.
- **Duplicar el código generado del protocolo en `agent` y en `server`.**
  Dos registros del mismo tipo protobuf en un binario que enlace ambos
  (cualquier test end-to-end) entran en pánico al inicializar.
- **Relajar `check_internal_pins.sh`** para tolerar un patch de desfase.
  Rompe exactamente lo que el guard protege: el binario instalado por tag
  se llevaría el hermano anterior.
- **Mover los tests a la raíz.** La raíz pasaría a requerir `server` y
  `agent` por tag, y como se corta la última, heredaría la misma cascada.

## Consecuencias

- Un cambio solo en `agent` o solo en `server` cuesta **un** corte de raíz
  (antes, hasta tres). Un cambio en `proto`, dos (antes, tres). Una semana
  de Dependabot, uno (antes, tres o cuatro).
- El CI de orbit ejecuta `internal/fleettest` en el job de tests con
  workspace y en las lanes con motor real (Data Studio contra PostgreSQL y
  MySQL); las matrices standalone y de `go mod tidy` no lo incluyen porque
  no se publica.
- El paraguas excluye `internal/*` del descubrimiento de módulos de
  `manifest-guard` y lo añade a su `go.work` (los patrones `./orbit/...`
  del CI de integración lo atraviesan).
- El driver del tren gana el paso de convergencia (`orbit-converge`): tras
  un corte que mueva `proto`, alinea `agent` y `server` y corta de nuevo sin
  intervención.
- Deuda que este ADR **no** cierra: el auto-bloqueo de release-please al
  etiquetar un release PR con paquetes sin cambios. Se trata en el arco del
  tren del plan de la suite.
