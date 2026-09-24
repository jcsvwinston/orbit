# Architecture Decision Records — Orbit (ADR)

> Decisiones **internas de producto** de Orbit, una por archivo, formato MADR
> (misma convención que los ADRs de Nucleus, `nucleus/docs/adrs/`). Orbit se
> extrajo del core de Nucleus por su `ADR-019`. Las decisiones de **coordinación
> de la suite** viven en los QADR del paraguas (`quantum/docs/adr/`), no aquí.

## Índice

| ID | Título | Estado | Relacionado |
|---|---|---|---|
| [ADR-001](ADR-001-datastudio-agnostic-datasource.md) | Data Studio agnóstico del origen de datos (contrato `datasource`) | Accepted | nucleus ADR-019 · quantum QADR-0006 |
| [ADR-002](ADR-002-fleet-datastudio-identidad.md) | Rumbo del Data Studio del plano fleet: el fleet consume el contrato `datasource` | Implemented (2026-09-22, A9 `S3`–`S5`; decidido 2026-08-31, D2) | ADR-001 · ADR-003 · ADR-012 · quantum QADR-0006 |
| [ADR-003](ADR-003-plano-fleet-agente-servidor.md) | Plano fleet como subsistema opt-in (agent/server/proto, stream bidi) | Accepted · retroactivo | ADR-001 · quantum QADR-0006 |
| [ADR-004](ADR-004-frontera-authn-delegada-authz-panel.md) | Autenticación delegada, autorización del panel propia | Accepted · retroactivo | nucleus ADR-019 |
| [ADR-005](ADR-005-confinamiento-storage-browse.md) | El navegador de storage del panel se confina a un root fijo | Accepted · retroactivo | ADR-004 |
| [ADR-006](ADR-006-pines-internos-sin-cascada.md) | Ningún módulo hermano requiere a otro por tag salvo el contrato del protocolo (fin de la cascada de pines internos) | Accepted (2026-09-05) | ADR-003 · quantum QADR-0002 · QADR-0008 |
| [ADR-007](ADR-007-permisos-por-campo-y-por-fila.md) | Los permisos llegan al campo y a la fila por adición a la gramática de políticas | Accepted (2026-09-13) | ADR-001 · ADR-004 · quantum QADR-0010 |
| [ADR-008](ADR-008-rastro-de-auditoria-en-la-base.md) | El rastro de auditoría vive en la base de datos, y por defecto | Accepted (2026-09-14) | ADR-004 · ADR-007 |
| [ADR-009](ADR-009-formularios-relacion-hijos-y-ficheros.md) | Un formulario resuelve la relación, edita los hijos y acepta un fichero — sin transacción y sin fingirla | Accepted (2026-09-14) | ADR-001 · ADR-007 |
| [ADR-010](ADR-010-acciones-y-pantallas-de-la-aplicacion.md) | La aplicación añade sus verbos y sus pantallas al panel, por adición y bajo sus mismas reglas | Accepted (2026-09-18) | ADR-001 · ADR-004 · ADR-007 |
| [ADR-011](ADR-011-la-ropa-del-producto.md) | El panel lleva la ropa del producto — marca, tarjetas propias e idioma, sin traducir los datos | Accepted (2026-09-18) | ADR-004 · ADR-010 |
| [ADR-012](ADR-012-contrato-datasource-modulo-hoja.md) | El contrato `datasource` es un módulo hoja — la segunda arista que ADR-006 permite (extraído en A9 `S4`, con el adaptador Nucleus como subpaquete) | Accepted (2026-09-21) | ADR-001 · ADR-002 · ADR-006 · quantum QADR-0002 · QADR-0010 |
| [ADR-013](ADR-013-retencion-local-del-plano-fleet.md) | El servidor del fleet retiene localmente — eventos, audit y métricas con ventana, en un fichero opcional; el agente aparca lo que ocurre sin stream | Accepted · implemented (2026-09-24, A9 `S6`) | ADR-003 · ADR-006 · ADR-008 · quantum QADR-0008 |
| [ADR-014](ADR-014-una-flota-de-servidores.md) | Una flota de servidores — malla de un salto sobre el listener de agentes, registro compartido con nodos remotos, relay de eventos con demanda total, asignación por rendezvous hashing y `Command.redirect`; el stream reemplazado termina (OR-56) | Accepted · implemented (2026-09-24, A9 `S8`) | ADR-003 · ADR-006 · ADR-013 |

**ADR-002 está implementado** (2026-09-22): la D2 de la auditoría integral
2026-08-30 decidió (2026-08-31) que el Data Studio del plano fleet consuma el
contrato `datasource` con identidad propagada por el stream, y el arco A9 de
la suite lo ejecutó en tres sesiones (orbit#506, #509, #511, #512 y la parte
2 de `S5`). El acta conserva el análisis de la alternativa descartada y su
sección «Ejecución» dice con qué se cumplió cada paso. Las puertas del
servidor (allowlist de modelos mutables, operadores de solo lectura) siguen
vigentes: la propagación no relajó ninguna.

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
