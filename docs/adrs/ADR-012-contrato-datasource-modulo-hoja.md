---
id: ADR-012
title: El contrato `datasource` es un módulo hoja — la segunda arista que ADR-006 permite
status: accepted
date: 2026-09-21
deciders: jcsvwinston
related: [ADR-001, ADR-002, ADR-006, quantum/QADR-0002, quantum/QADR-0010]
supersedes: null
tags: [orbit, fleet, data-studio, módulos, tren]
---

# ADR-012 — El contrato `datasource` es un módulo hoja

## Contexto

ADR-002 decidió que el plano fleet consuma el contrato `datasource` (ADR-001)
con identidad propagada: el agente debe ejecutar Data Studio a través de un
`datasource.DataSource`, el mismo que el panel in-process, para que el RBAC
por modelo, el filtrado de tenant y los orígenes alternativos
(`quarkdatasource`) valgan en los dos planos. Y ADR-006 decidió que ningún
módulo hermano requiera a otro por tag salvo el contrato del protocolo
(`proto`), porque cada arista de tag entre hermanos costaba un corte de raíz
por converger pines.

La `S0` de A9 midió que las dos decisiones, tal como están escritas, no
pueden cumplirse a la vez: **el agente no puede importar el contrato.** El
paquete `datasource` vive en el módulo raíz (`github.com/jcsvwinston/orbit`),
`agent/go.mod` no lo requiere, y requerirlo abriría la arista `agent → raíz`
que ADR-006 prohíbe — con motivo: la raíz es el panel entero, con sus
dependencias de `nucleus/pkg/*` y su SPA embebida, y se corta la última, así
que un pin del agente a la raíz iría siempre un tag por detrás y arrastraría
la cascada que ADR-006 cerró.

Tres hechos medidos que cambian el problema:

1. **`datasource/datasource.go` no importa nada.** 269 líneas, cero imports:
   tipos, interfaces y un `ParseFilterOp`. Es un contrato puro, como `proto`
   — y es la razón de que ADR-001 lo congelara como API pública en el gate
   de v1.0 (`contracts/freeze_test.go`).
2. **`quarkdatasource` sólo importa `datasource` de la raíz.** Sus cuatro
   imports de `github.com/jcsvwinston/orbit/...` son todos ese paquete. La
   excepción topológica de `check_internal_pins.sh` (`DATASOURCE_EDGE_MODULE`,
   ≤1 minor de desfase con la raíz mientras el contrato esté congelado)
   existe únicamente para esta arista.
3. **El adaptador que convierte el runtime de una aplicación en un
   `DataSource` es `internal/datasource/nucleus`** (1 486 líneas sobre
   `nucleus/pkg/{db,model,observe,signals,validate}`). Es interno a la raíz:
   el agente no podría reutilizarlo ni aunque requiriera la raíz. Esa es una
   segunda decisión, de `S4`, y este ADR no la toma: aquí se decide dónde
   vive el contrato, no quién lo implementa.

## Decisión

1. **`datasource` pasa a ser un módulo Go propio**,
   `github.com/jcsvwinston/orbit/datasource`, con la misma ruta de import que
   hoy (ningún consumidor cambia una línea de import) y **cero dependencias**.
   Es la **segunda arista** que ADR-006 permite entre hermanos, por el mismo
   motivo que la primera: es un contrato, cambia poco y a propósito, y está
   congelado. ADR-006 queda enmendado en ese punto y en nada más.
2. **Quien necesite el contrato lo requiere a él, no a la raíz**: la raíz
   (el panel), `quarkdatasource` y —desde `S4`— `agent`. La arista
   `quarkdatasource → raíz` desaparece con su excepción: `check_internal_pins.sh`
   deja de necesitar `DATASOURCE_EDGE_MODULE` y pasa a exigir igualdad exacta
   en `datasource` como en `proto`.
3. **La primera etiqueta es `datasource/v1.0.0`.** El contrato es API pública
   congelada desde el gate de v1.0 (ADR-001); un `v0` afirmaría lo contrario.
   Los majors siguen en lockstep con la suite (QADR-0002): romper el contrato
   es un major de Orbit y de Quantum, que QADR-0010 acumula en el cierre de
   A12.
4. **La extracción se ejecuta en `S4`**, en el mismo PR que hace al agente
   consumidor, y **no antes**: un módulo que sale de la raíz no puede
   convivir con ningún tag de raíz que aún lo contenga (Go se planta con
   `ambiguous import`), así que el suelo de cada consumidor debe nombrar el
   tag que el corte crea, y el tren aprendió en Quantum 1.30.0 cómo se hace.

## Mecánica que la decisión arrastra (para `S4` y el tren)

- `datasource/go.mod` (`go 1.26`, sin `require`). `contracts/freeze_test.go`
  sigue leyendo el directorio: el freeze no cambia.
- `release-please-config.json`: paquete `datasource` con `component:
  datasource`, `include-component-in-tag: true` e `initial-version: 1.0.0`;
  la raíz lo añade a `exclude-paths`; `.release-please-manifest.json` no
  lleva entrada hasta el primer corte.
- `go.mod` de la raíz, de `quarkdatasource` y de `agent`: `require
  github.com/jcsvwinston/orbit/datasource v1.0.0` — el tag que ese corte
  crea. `quarkdatasource` deja de requerir la raíz.
- CI: la lane standalone (`GOWORK=off`) no puede resolver un tag que aún no
  existe. Hasta el corte, esos tres módulos se comprueban con un `go.work`
  generado que lleva `replace module@version => ./datasource` **versionado**
  (el patrón de `quark/scripts/ci/link_workspace.sh`); tras el corte la lane
  vuelve a `GOWORK=off`. Un `replace` sin versión no sirve: Go rechaza
  reemplazar un módulo del workspace a todas las versiones.
- `scripts/ci/check_internal_pins.sh`: `datasource` entra en `MODULES`, y la
  excepción `DATASOURCE_EDGE_MODULE` se retira.
- Paraguas: `versions.yaml` gana `orbit_modules.datasource`; `manifest-guard`
  descubre los módulos del árbol y **fallará** en cuanto el set pine un árbol
  con `datasource/go.mod` sin esa entrada — es el aviso, no un defecto.
- La doc del contrato (`website/docs/...` y el `README` de `quarkdatasource`)
  nombra el módulo nuevo; el `go get` de un origen alternativo es
  `github.com/jcsvwinston/orbit/datasource`, sin arrastrar el panel.

## Lo que NO decide este ADR

- **Quién implementa el contrato en el agente.** `internal/datasource/nucleus`
  es interno a la raíz. `S4` decide entre hacerlo público (bajo `datasource/`
  o como módulo propio con dependencia de nucleus) o que la aplicación
  entregue su `DataSource` al agente y el módulo raíz lo cablee cuando panel y
  agente van juntos. Las dos formas son aditivas; se elige con la medición
  de `S4` delante.
- **La forma en el cable.** Los operadores de filtro, la identidad del
  operador y el total exacto entran en `admin.proto` por adición
  (`proto/EVOLUTION.md`), en el PR que acepta este ADR: `RecordFilter` y
  `ListRecordsRequest.where`, `OperatorIdentity` y
  `DataStudioRequest.operator`. El servidor rellena la identidad en `S5`.

## Alternativas descartadas

- **Enmendar ADR-006 para permitir `agent → raíz`.** Devuelve la cascada
  que ADR-006 cerró y mete el panel entero en cada aplicación que monte sólo
  el agente.
- **Duplicar las interfaces en `agent`.** Go las casaría por estructura,
  pero `quarkdatasource` implementa las de la raíz y el agente aceptaría las
  suyas: dos contratos con el mismo nombre que divergen en silencio, la
  clase de defecto que la `S0` de A4 registró en el paraguas.
- **Meter el contrato en `proto`.** `proto` es código generado de un
  contrato de cable; mezclarle interfaces Go a mano confunde qué regenera
  `make proto` y qué no.
- **Fusionar `proto`, `agent`, `server` y `datasource` en un módulo
  `fleet`.** Cambia rutas de import y el `go install` del binario: major de
  la suite. Sigue en la lista de A12, como ADR-006 lo dejó.

## Consecuencias

- Un consumidor de `datasource` (un origen alternativo, el agente) no
  arrastra el panel, su SPA ni `nucleus/pkg/*`.
- Una arista de tag más entre hermanos, del mismo tipo que `proto`: un
  cambio en `datasource` cuesta dos cortes (el suyo y el de sus
  consumidores), y el driver del tren ya sabe converger (`orbit-converge`).
  Como el contrato está congelado, ese caso es raro por construcción.
- Una excepción menos en `check_internal_pins.sh`, que era la única razón
  de tolerar un desfase con la raíz.
- El corte que ejecute la extracción es el que más trampas conocidas
  concentra (suelo que nombra el tag, `replace` versionado, entrada del
  paraguas): `S4` lo trocea con esas trampas escritas delante, no las
  redescubre.
