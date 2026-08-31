# Architecture Decision Records — Orbit (ADR)

> Decisiones **internas de producto** de Orbit, una por archivo, formato MADR
> (misma convención que los ADRs de Nucleus, `nucleus/docs/adrs/`). Orbit se
> extrajo del core de Nucleus por su `ADR-019`. Las decisiones de **coordinación
> de la suite** viven en los QADR del paraguas (`quantum/docs/adr/`), no aquí.

## Índice

| ID | Título | Estado | Relacionado |
|---|---|---|---|
| [ADR-001](ADR-001-datastudio-agnostic-datasource.md) | Data Studio agnóstico del origen de datos (contrato `datasource`) | Accepted | nucleus ADR-019 · quantum QADR-0006 |
| [ADR-003](ADR-003-plano-fleet-agente-servidor.md) | Plano fleet como subsistema opt-in (agent/server/proto, stream bidi) | Accepted · retroactivo | ADR-001 · quantum QADR-0006 |
| [ADR-004](ADR-004-frontera-authn-delegada-authz-panel.md) | Autenticación delegada, autorización del panel propia | Accepted · retroactivo | nucleus ADR-019 |
| [ADR-005](ADR-005-confinamiento-storage-browse.md) | El navegador de storage del panel se confina a un root fijo | Accepted · retroactivo | ADR-004 |

El número **ADR-002 está reservado**: es el borrador sobre el rumbo del Data
Studio del plano fleet (identidad por el stream vs telemetría+lectura), en
revisión en su propia rama. Cuando aterrice hay que añadir aquí su fila — el
guard `scripts/ci/check_adr_index.sh` fallará hasta que se haga, y eso es
deliberado.

> El estado de esta tabla es un **resumen**; la verdad vive en el frontmatter
> de cada ADR. «Retroactivo» marca actas escritas después de ejecutar la
> decisión, para dejar constancia — no decisiones nuevas.

## Cómo añadir un ADR nuevo

1. Copia la plantilla de uno existente (frontmatter + Contexto / Decisión /
   Consecuencias / Preguntas abiertas / Plan).
2. Numera secuencialmente (`ADR-NNN-titulo-corto-en-kebab.md`).
3. Estado inicial `proposed`; tras discusión, `accepted`/`rejected`.

## Para Code

Lee el ADR antes de tocar la superficie que cubre. **No reabras decisiones
aceptadas sin un ADR sucesor.** ADR-001 es la hoja de ruta del desacople de Data
Studio; su contrato `datasource` es API pública de Orbit y se congela en el gate
de v1.0 (ver `quantum/docs/adr/QADR-0005`).
