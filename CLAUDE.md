# Orbit — instrucciones para Claude Code

> Se carga al inicio de sesión en el repo **orbit**. Mantenlo conciso. Orbit es
> uno de los tres productos de la suite **Quantum** (paraguas `quantum/`), pero
> tiene su repo, su release y su cadencia propios.

## Qué es Orbit

Producto de administración que monta **in-process** sobre una app **Nucleus**
(framework web) vía su API de extensión/módulos. Sirve un panel embebido (Data
Studio, feed live de HTTP/SQL, sesiones, RBAC, métricas, audit). Se extrajo del
core de Nucleus por su `ADR-019`; Nucleus ya no lleva código de admin.

## Estado real

- La raíz está en **v1.4.3** <!-- x-release-please-version --> (release-please
  reescribe esta línea en cada release del root y
  `scripts/ci/check_docs_version_claims.sh` falla si deriva — este fichero
  llegó a llevar tres minors de retraso y dio contexto falso de arranque). El
  gate v1.0 vive en `docs/V1_GATE.md`; las superficies congeladas (raíz +
  `datasource`) las guarda `contracts/freeze_test.go` contra su baseline.
  Cambios incompatibles en esas superficies requieren un major.
- **Fija Nucleus y Quark por tag en cada go.mod** (no por go.work): las
  versiones certificadas del set y CUALQUIER lag declarado viven en
  `versions.yaml` del paraguas (`quantum/`) — consúltalo ahí en vez de fiarte
  de números escritos aquí, que es exactamente como este fichero se fosilizó.
- **Aguas abajo de Nucleus**: consume ~15 de sus paquetes y se ata a
  `nucleus.Runtime` (`Models()`, `Session()`, `Authorizer()`, `Storage()`,
  `Observability()`, `DatabaseHandle(s)`) en `orbit.go`. Nunca toca internals.

## Estructura (multi-módulo, `go.work`)

- **raíz `./` + `internal/admin/`** — el panel in-process. Es el producto real.
- **`agent/` · `server/` · `proto/`** — observabilidad de clúster (opcional).
  Es un plano fleet real de punta a punta (registro de nodo, RBAC real, stream
  SQL/HTTP, listener fail-closed) con miles de líneas y tests de integración;
  la mayoría de apps solo montan el panel raíz, pero el plano fleet NO es
  esqueleto. Tags de componente propios (`agent/vX`, `server/vX`, `proto/vX`).

## Reglas (heredadas de la suite)

1. **Anti-hype**: sin superlativos de marketing (afirmaciones exageradas de
   madurez o rendimiento) en commits, README, ADRs ni docs.
2. **Docs en el mismo PR que la API** (cultura Quark/Nucleus, QADR-0003 de la suite).
3. **Conventional Commits**; trabaja en rama y abre PR, no commitees a `main`.
4. **No rompas el uso in-process**: Orbit lee del `Runtime`, no de internals de Nucleus.

## Decisiones arquitectónicas (`docs/adrs/`)

Primer ADR de Orbit. Léelo antes de tocar la superficie que cubre; no reabras uno
aceptado sin sucesor.

- **[ADR-001](docs/adrs/ADR-001-datastudio-agnostic-datasource.md)** — Data Studio
  agnóstico del origen de datos: contrato neutral `datasource`
  (`ModelSource`/`RecordStore`/`DataSource`) para que el panel deje de hablar tipos
  de Nucleus y pueda operar también sobre Quark. Trae el mapa de superficie
  (archivo:línea), las decisiones D1–D3 y las preguntas abiertas O1–O3. **Su
  contrato es API pública → se congela en el v1.0 de Orbit.**

## Contexto de suite

- Secuenciación y esa integración: `../docs/adr/QADR-0005` (Nucleus→v1.0 primero,
  Orbit en lockstep) y `../docs/adr/QADR-0006` (feed SQL Quark→bus de Nucleus +
  `orbit/quarkbridge`; Data Studio sobre Quark). Coordinación de la suite: el
  `/next-session` del repo `quantum`.
- **Tooling**: `release-please` (config multi-módulo con tags de componente) y
  **CI propio completo en PRs** (`.github/workflows/ci.yml`, instalado en la
  4ª–5ª ronda tras los fallos OR-1/OR5-1): build+vet+**test** standalone
  (`GOWORK=off`) por cada uno de los 6 módulos, tests en workspace, guard de
  pins internos (`check_internal_pins.sh`), Data Studio contra PG+MySQL
  reales, govulncheck, linter de voz de docs y coherencia de versión
  (`check_docs_version_claims.sh`), y build de la UI. El lane `orbit-lockstep`
  de la suite (`quantum/.github/workflows/integration.yml`) prueba además
  orbit contra el nucleus pinado del workspace.
