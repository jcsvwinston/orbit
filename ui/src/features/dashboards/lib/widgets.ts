import type { DashboardWidget } from '@/services/api'

/**
 * What the cards of a dashboard need that is not drawing: the grid's classes,
 * the rows a chart is fed, and the colour of each series. Pure, so the
 * shapes the server sends are tested without a browser laying anything out.
 */

// Tailwind only ships the classes it finds written out, so each width is
// spelled here rather than built from a number. Four columns is the
// overview's grid, as it was before dashboards existed.
const GRID_COLUMNS: Record<number, string> = {
  1: 'grid-cols-1',
  2: 'md:grid-cols-2',
  3: 'md:grid-cols-2 xl:grid-cols-3',
  4: 'md:grid-cols-2 xl:grid-cols-4',
}

const SPANS: Record<number, string> = {
  2: 'md:col-span-2',
  3: 'md:col-span-2 xl:col-span-3',
  4: 'md:col-span-2 xl:col-span-4',
}

/** gridColumnsClass is the grid of a screen with that many columns. */
export function gridColumnsClass(columns: number | undefined): string {
  return GRID_COLUMNS[columns ?? 4] ?? GRID_COLUMNS[4]
}

/** spanClass is what makes a card take that many columns; one takes none. */
export function spanClass(span: number | undefined): string {
  return (span && SPANS[span]) || ''
}

/** isChart: the kinds drawn from series. */
export function isChart(widget: DashboardWidget): boolean {
  return widget.kind === 'line' || widget.kind === 'bar'
}

/** isTabular: the kinds drawn as a table. */
export function isTabular(widget: DashboardWidget): boolean {
  return widget.kind === 'table' || widget.kind === 'records'
}

/** seriesKey names series i in a chart row. Positional, so a series called
 * "label" cannot overwrite the axis. */
export function seriesKey(index: number): string {
  return `s${index}`
}

export type ChartRow = { label: string } & Record<string, string | number>

/** chartRows turns the payload's columns of values into the rows a chart
 * library reads: one per label, with each series' value under its key. */
export function chartRows(labels: string[] | undefined, series: DashboardWidget['series']): ChartRow[] {
  return (labels ?? []).map((label, i) => {
    const row: ChartRow = { label }
    ;(series ?? []).forEach((s, j) => {
      row[seriesKey(j)] = s.values[i]
    })
    return row
  })
}

// The first series wears the panel's accent, which the configured palette is
// checked against at startup; the rest are mid-tones that keep 3:1 against
// both the light and the dark surface, which is what a line or a bar needs to
// be seen (WCAG 1.4.11). The server draws at most eight series.
const SERIES_COLOURS = [
  'hsl(var(--primary))',
  '#d97706',
  '#059669',
  '#dc2626',
  '#7c3aed',
  '#0891b2',
  '#db2777',
  '#64748b',
]

export function seriesColour(index: number): string {
  return SERIES_COLOURS[index % SERIES_COLOURS.length]
}

/** maxDrawnPoints is where a line stops marking each point: past it the
 * marks are a smear, and the line says the same thing. */
export const maxDrawnPoints = 60
