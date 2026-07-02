# Architecture Decision Records — Orbit (ADR)

> Decisiones **internas de producto** de Orbit, una por archivo, formato MADR
> (misma convención que los ADRs de Nucleus, `nucleus/docs/adrs/`). Orbit se
> extrajo del core de Nucleus por su `ADR-019`. Las decisiones de **coordinación
> de la suite** viven en los QADR del paraguas (`quantum/docs/adr/`), no aquí.

## Índice

| ID | Título | Estado | Relacionado |
|---|---|---|---|
| [ADR-001](ADR-001-datastudio-agnostic-datasource.md) | Data Studio agnóstico del origen de datos (contrato `datasource`) | Proposed | nucleus ADR-019 · quantum QADR-0006 |

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
