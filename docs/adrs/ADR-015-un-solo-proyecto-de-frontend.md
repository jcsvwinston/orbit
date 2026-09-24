---
id: ADR-015
title: Un solo proyecto de frontend — el panel y el fleet, dos entradas sobre un módulo que embebe un dist
status: accepted
date: 2026-09-24
deciders: jcsvwinston
related: [ADR-006, ADR-011, ADR-012, ADR-014]
supersedes: null
tags: [orbit, ui, panel, fleet, módulos]
---

# ADR-015 — Un solo proyecto de frontend

> **Estado: aceptado e implementado (2026-09-24, arco A9 de la suite,
> sesión `S9`).**

## Contexto

Orbit tenía dos SPAs sin una línea en común. La del panel in-process
(`internal/admin/ui`) y la del plano fleet (`ui/`), cada una con su
`package.json`, su Vite, su Tailwind, su sistema de tokens, su ESLint y su
`dist` embebido por un `//go:embed` distinto. La medición del arco A9
(`internal/fleettest/fleetbench`, familia `ui`) lo puso en cifras y fue lo
que hizo la decisión en vez de la preferencia:

| | panel (`internal/admin/ui`) | fleet (`ui/`) |
|---|---|---|
| fuentes (sin stubs generados) | 79 ficheros, 10 318 líneas | 37 ficheros, 5 306 líneas |
| tests | vitest, 21 ficheros de test, 116 casos | ninguno |
| enrutado | react-router, páginas en chunks propios | hash a mano, un chunk |
| componentes | base-ui, AG Grid, Recharts, lucide | propios (7 ficheros) |
| tokens | HSL semánticos (`--background`, `--primary`…) | numerados `--t0…--t53` |
| lint | ESLint 10, config plana | ESLint 9, reglas con tipos |
| dist | 1,9 MB en 19 ficheros, carga inicial acotada por test | 444 KB en 3 ficheros, sin presupuesto |
| frescura del dist en CI | sí (diff tras build) | no |
| toolchain | Vite 8, plugin-react 6 | Vite 6, plugin-react 4 |

El plan del arco decía «el stack del fleet como base»; los datos dicen lo
contrario: el proyecto fuerte es el del panel, y lo que le falta al fleet
(tests, presupuesto, frescura, división en chunks) el panel ya lo tiene.

Había además un límite del lenguaje que la primera versión del control
`UI-01` no sabía: un `//go:embed` no puede salir de su módulo, y el panel
(módulo raíz) y el servidor (`server/`) son módulos distintos por ADR-006.
«El servidor y el panel embeben el mismo dist» sólo es posible si el dist lo
embebe un tercer módulo que los dos requieren.

## Decisión

**Un proyecto, dos entradas, un módulo.** El proyecto del panel es la base
y se mueve a `ui/`, que pasa a ser también un módulo Go
(`github.com/jcsvwinston/orbit/ui`) sin dependencias, como el contrato
`datasource` (ADR-012): la tercera arista que ADR-006 permite, porque es una
hoja.

- **Dos entradas del mismo proyecto.** El panel conserva sus fuentes en
  `src/` con `index.html`; el fleet pasa a `src/fleet/` con su entrada en
  `fleet/index.html` y su propia configuración de Vite y de Tailwind
  (`vite.fleet.config.ts`, `tailwind.fleet.config.js`), porque sus pantallas
  siguen leyendo su paleta numerada. Un `npm run build` construye las dos en
  `dist/panel/` y `dist/fleet/`, cada una con base `./` y autocontenida.
- **Un dist, embebido por el módulo.** `ui/embed.go` embebe `dist/` y expone
  `Panel()`, `Fleet()` y `Dist()`. La raíz sirve `Panel()` bajo su prefijo;
  el servidor sirve `Fleet()` en la raíz de su listener de UI. Ninguno de los
  dos embebe ya un dist propio; `server/ui` desaparece.
- **Compartido por construcción**: `src/shared/tokens.css` es la fuente de
  tokens del sistema de diseño y las dos hojas de estilos la importan; un
  `tsconfig`, un ESLint, un Vitest (`npm test` corre la suite del panel y los
  specs del fleet), un `package-lock`, una lane de CI que tipa, linta, prueba,
  construye y falla si el `dist` commiteado difiere de lo construido, y un
  test Go en el módulo (`embed_test.go`) con un presupuesto de carga inicial
  por entrada.
- **La mecánica del nacimiento es la del ADR-012**: la raíz y el servidor
  requieren `ui v1.0.0`, el tag que el corte crea (`initial-version` en
  release-please); hasta entonces, `replace` versionado en el `go.work` para
  el desarrollo y `link_unpublished_siblings.sh` en las lanes standalone; el
  corte quita el `replace`, y el paraguas añade `orbit_modules.ui` y
  `./orbit/ui` en su `go.work`.

## Consecuencias

- El fleet pierde su toolchain propia (Vite 6, ESLint con reglas con tipos)
  y adopta la del panel. Lo que se rompió al mover fue un comentario de una
  regla de lint que ya no existe; nada del código.
- Las pantallas del fleet **no están re-skineadas**: importan los tokens
  compartidos y siguen usando su paleta. Re-skinearlas sobre los nombres del
  sistema de diseño es trabajo de la familia `ui` que queda (`S10`) o
  posterior, y `tailwind.fleet.config.js` existe para que ese trabajo no
  necesite tocar la configuración.
- El dist del fleet es un solo chunk de 424 KB; su presupuesto (512 KB de JS
  inicial) es un techo para el re-skin, no un objetivo.
- Mover el proyecto rompe las rutas que la documentación y las herramientas
  nombraban (`internal/admin/ui`, `server/ui`, `ui/src/gen`); están
  actualizadas en este mismo cambio. Dependabot vigila un solo proyecto npm.
- Un consumidor del panel arrastra en su binario también el dist del fleet
  (440 KB) y viceversa: el precio de embeber un dist, no dos. Si pesara, la
  decisión sucesora es un módulo por entrada, no volver a dos proyectos.

## Lo que NO decide

- La migración a connect-es 2 y protobuf-es 2 (`S10`, desde `proto/`).
- El instrumento de navegador sobre el fleet y el tenant en la UI (`S10`).
- Si el fleet y el panel llegan a ser una sola aplicación con un solo
  enrutado: hoy son dos entradas porque hablan con dos backends distintos
  (REST del panel, Connect del servidor).
