import {
  ClientSideRowModelApiModule,
  ClientSideRowModelModule,
  ColumnApiModule,
  ColumnAutoSizeModule,
  ModuleRegistry,
  RowSelectionModule,
  ValidationModule,
  colorSchemeDarkBlue,
  themeQuartz,
} from 'ag-grid-community'

/**
 * The grid Data Studio draws, set up once for the chunk that carries it.
 *
 * AG Grid ships its features as modules and leaves out of the bundle every
 * one nobody registers. The list is what AGGridTable uses and nothing else:
 * the client-side row model and its API (`refreshClientSideRowModel` puts the
 * server's order back after a header click), row selection with checkboxes,
 * sizing the columns to the grid's width, and reading the column state (the
 * sort the header asked for, which becomes the server's `order_by`). Filters,
 * pagination, editors, CSV export and the infinite row model are not here:
 * Data Studio filters, pages and exports on the server. A feature used
 * without its module does nothing in a production build, so the development
 * build registers AG Grid's validation module, which names the missing one
 * in the console; the production bundle leaves it out.
 *
 * The grid draws with AG Grid's Theming API: its styles are injected by the
 * chunk itself (the policy's `style-src` already allows inline styles for the
 * panel's own), and its icons are SVG images, so no stylesheet or font
 * travels with Data Studio — the quartz stylesheet used to carry an icon font
 * as a `data:` URL that the panel's own `font-src 'self'` refused.
 */
ModuleRegistry.registerModules([
  ClientSideRowModelModule,
  ClientSideRowModelApiModule,
  RowSelectionModule,
  ColumnAutoSizeModule,
  ColumnApiModule,
])
if (import.meta.env.DEV) {
  ModuleRegistry.registerModules([ValidationModule])
}

/** The light grid: quartz as AG Grid draws it. */
export const gridThemeLight = themeQuartz

/** The dark grid: quartz on the dark-blue scheme, the one the legacy
 * `ag-theme-quartz-dark` stylesheet drew. */
export const gridThemeDark = themeQuartz.withPart(colorSchemeDarkBlue)
