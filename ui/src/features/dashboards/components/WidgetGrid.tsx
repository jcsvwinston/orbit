import { lazy, Suspense, type ReactNode } from 'react'
import { ArrowDownRight, ArrowRight, ArrowUpRight } from 'lucide-react'

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { cn } from '@/lib/utils'
import type { DashboardWidget } from '@/services/api'
import { useTranslate } from '@/stores/messagesStore'
import { gridColumnsClass, isChart, isTabular, seriesColour, spanClass } from '../lib/widgets'

/**
 * The cards an application declared, drawn by kind (A11 O5): the value card
 * A6 shipped, a figure and its change, a line or bar chart, a table, and the
 * newest rows of a model. The application writes no frontend code for any of
 * them — the payload says what to draw.
 *
 * This module is a chunk of its own, loaded by the overview only when the
 * application declared cards and by the dashboard route; the chart library is
 * a further chunk, loaded only for a screen with a chart.
 */

const SeriesChart = lazy(() => import('./SeriesChart'))

interface Props {
  widgets: DashboardWidget[]
  /** columns is the grid's width on a wide screen; the overview's is 4. */
  columns?: number
}

export default function WidgetGrid({ widgets, columns = 4 }: Props) {
  return (
    <div className={cn('grid gap-4', gridColumnsClass(columns))}>
      {widgets.map((widget) => (
        <WidgetCard key={widget.id} widget={widget} />
      ))}
    </div>
  )
}

function WidgetCard({ widget }: { widget: DashboardWidget }) {
  const t = useTranslate()
  const span = spanClass(widget.span)
  const drawsData = isChart(widget) || isTabular(widget)

  // A value or a stat is a glance, and the whole card is the link, as it
  // was in A6. A chart or a table has content worth reading on its own —
  // a link around a table would make a screen reader read every cell as
  // its name — so there only the title is the link.
  const title = drawsData && widget.link ? (
    <a href={widget.link} className="hover:underline">{widget.title}</a>
  ) : (
    widget.title
  )

  const body = (
    <>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium">{title}</CardTitle>
        {widget.description && <CardDescription>{widget.description}</CardDescription>}
      </CardHeader>
      <CardContent>
        {widget.error ? (
          <p className="text-sm text-muted-foreground">
            {t('dashboard.unavailable', 'This card could not be read')}
          </p>
        ) : (
          <WidgetBody widget={widget} />
        )}
      </CardContent>
    </>
  )

  if (!drawsData && widget.link) {
    return (
      <a href={widget.link} className={cn('block', span)} data-widget={widget.id}>
        <Card className="h-full transition-colors hover:border-primary">{body}</Card>
      </a>
    )
  }
  return (
    <Card className={span || undefined} data-widget={widget.id}>
      {body}
    </Card>
  )
}

function WidgetBody({ widget }: { widget: DashboardWidget }) {
  switch (widget.kind) {
    case 'stat':
      return <StatBody widget={widget} />
    case 'line':
    case 'bar':
      return <ChartBody widget={widget} kind={widget.kind} />
    case 'table':
    case 'records':
      return <WidgetTable widget={widget} />
  }
  if (widget.items && widget.items.length > 0) {
    return (
      <ul className="space-y-1 text-sm">
        {widget.items.map((item, index) => (
          <li key={`${widget.id}-${index}`} className="flex items-center justify-between gap-3">
            <span className="truncate">{item.label}</span>
            {item.value && <span className="text-muted-foreground">{item.value}</span>}
          </li>
        ))}
      </ul>
    )
  }
  return (
    <>
      <div className="text-3xl font-semibold">{widget.value}</div>
      {widget.detail && <p className="mt-1 text-xs text-muted-foreground">{widget.detail}</p>}
    </>
  )
}

// The colours of a change that is good or bad news, each at 4.5:1 or more on
// the light and the dark surface. The arrow and the word beside it carry the
// direction too, so the colour is never the only signal.
const SENTIMENT: Record<string, string> = {
  good: 'text-emerald-700 dark:text-emerald-400',
  bad: 'text-red-700 dark:text-red-400',
}

function StatBody({ widget }: { widget: DashboardWidget }) {
  const t = useTranslate()
  const Arrow = widget.trend === 'up' ? ArrowUpRight : widget.trend === 'down' ? ArrowDownRight : widget.trend === 'flat' ? ArrowRight : null
  const word =
    widget.trend === 'up'
      ? t('dashboard.trend_up', 'up')
      : widget.trend === 'down'
        ? t('dashboard.trend_down', 'down')
        : widget.trend === 'flat'
          ? t('dashboard.trend_flat', 'no change')
          : ''
  return (
    <>
      <div className="text-3xl font-semibold">{widget.value}</div>
      {widget.delta && (
        <p
          className={cn(
            'mt-1 flex items-center gap-1 text-sm font-medium',
            (widget.sentiment && SENTIMENT[widget.sentiment]) || 'text-muted-foreground',
          )}
          data-trend={widget.trend}
        >
          {Arrow && <Arrow className="h-4 w-4 shrink-0" aria-hidden="true" />}
          {word && <span className="sr-only">{word}</span>}
          <span>{widget.delta}</span>
        </p>
      )}
      {widget.detail && <p className="mt-1 text-xs text-muted-foreground">{widget.detail}</p>}
    </>
  )
}

function ChartBody({ widget, kind }: { widget: DashboardWidget; kind: 'line' | 'bar' }) {
  const t = useTranslate()
  const labels = widget.labels ?? []
  const series = widget.series ?? []
  if (labels.length === 0 || series.length === 0) {
    return <p className="text-sm text-muted-foreground">{t('state.empty', 'Nothing to show')}</p>
  }
  return (
    <>
      {/* The picture is an image to assistive technology, named by the card;
          the numbers it draws are the table below it, which only a screen
          reader is given. */}
      <div role="img" aria-label={`${widget.title}, ${t('dashboard.chart', 'chart')}`} className="h-56 w-full" data-chart={kind}>
        <Suspense fallback={null}>
          <SeriesChart kind={kind} labels={labels} series={series} />
        </Suspense>
      </div>
      {series.length > 1 && (
        <ul className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground" aria-hidden="true">
          {series.map((s, i) => (
            <li key={i} className="flex items-center gap-1.5">
              <span className="inline-block h-2.5 w-2.5 rounded-sm" style={{ backgroundColor: seriesColour(i) }} />
              {s.name}
            </li>
          ))}
        </ul>
      )}
      <table className="sr-only">
        <caption>{widget.title}</caption>
        <thead>
          <tr>
            <td />
            {series.map((s, i) => (
              <th key={i} scope="col">{s.name || widget.title}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {labels.map((label, i) => (
            <tr key={i}>
              <th scope="row">{label}</th>
              {series.map((s, j) => (
                <td key={j}>{s.values[i]}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {widget.detail && <p className="mt-2 text-xs text-muted-foreground">{widget.detail}</p>}
    </>
  )
}

function WidgetTable({ widget }: { widget: DashboardWidget }) {
  const t = useTranslate()
  const columns = widget.columns ?? []
  const rows = widget.rows ?? []
  let content: ReactNode
  if (rows.length === 0) {
    content = <p className="text-sm text-muted-foreground">{t('state.empty', 'Nothing to show')}</p>
  } else {
    content = (
      <Table>
        <TableHeader>
          <TableRow>
            {columns.map((column, i) => (
              <TableHead key={i} scope="col" className="h-9 px-2">
                {column}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row, i) => (
            <TableRow key={i}>
              {row.map((cell, j) => (
                <TableCell key={j} className="max-w-[16rem] truncate px-2 py-2" title={cell}>
                  {cell}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    )
  }
  return (
    <>
      {content}
      {widget.detail && <p className="mt-2 text-xs text-muted-foreground">{widget.detail}</p>}
    </>
  )
}
