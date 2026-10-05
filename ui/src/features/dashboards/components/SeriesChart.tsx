import {
  Bar,
  BarChart,
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'

import type { DashboardWidget } from '@/services/api'
import { chartRows, maxDrawnPoints, seriesColour, seriesKey } from '../lib/widgets'

/**
 * The drawing of a "line" or "bar" card. It is its own module so the chart
 * library is fetched only when a screen has a chart on it: the overview of an
 * application with no series never loads it, and the panel's first load
 * never does (ui/embed_test.go keeps that budget).
 */

interface Props {
  kind: 'line' | 'bar'
  labels: string[]
  series: NonNullable<DashboardWidget['series']>
}

// The axis and grid read the panel's own tokens, so the chart follows the
// theme and the configured palette instead of the library's greys.
const tick = { fill: 'hsl(var(--muted-foreground))', fontSize: 12 }
const tooltipStyle = {
  backgroundColor: 'hsl(var(--popover))',
  border: '1px solid hsl(var(--border))',
  borderRadius: '0.5rem',
  color: 'hsl(var(--popover-foreground))',
}

export default function SeriesChart({ kind, labels, series }: Props) {
  const rows = chartRows(labels, series)
  const markPoints = labels.length <= maxDrawnPoints
  const name = (i: number) => series[i]?.name || undefined

  const axes = [
    <CartesianGrid key="grid" strokeDasharray="3 3" stroke="hsl(var(--border))" vertical={false} />,
    <XAxis key="x" dataKey="label" tick={tick} stroke="hsl(var(--border))" tickLine={false} minTickGap={12} />,
    <YAxis key="y" tick={tick} stroke="hsl(var(--border))" tickLine={false} width={44} allowDecimals />,
    <Tooltip key="tooltip" contentStyle={tooltipStyle} labelStyle={{ color: 'hsl(var(--popover-foreground))' }} />,
  ]

  return (
    <ResponsiveContainer width="100%" height="100%">
      {kind === 'bar' ? (
        <BarChart data={rows}>
          {axes}
          {series.map((_, i) => (
            <Bar
              key={seriesKey(i)}
              dataKey={seriesKey(i)}
              name={name(i)}
              fill={seriesColour(i)}
              radius={[3, 3, 0, 0]}
              isAnimationActive={false}
            />
          ))}
        </BarChart>
      ) : (
        <LineChart data={rows}>
          {axes}
          {series.map((_, i) => (
            <Line
              key={seriesKey(i)}
              type="monotone"
              dataKey={seriesKey(i)}
              name={name(i)}
              stroke={seriesColour(i)}
              strokeWidth={2}
              // Each reading is marked, so a point is a point and not a bend
              // in a curve; the marks carry data-series-point, which is what
              // the browser bench counts (UIX-11).
              dot={
                markPoints
                  ? ({ cx, cy, index }: { cx?: number; cy?: number; index?: number }) => (
                      <circle
                        key={`${seriesKey(i)}-${index}`}
                        data-series-point={seriesKey(i)}
                        cx={cx}
                        cy={cy}
                        r={3}
                        fill={seriesColour(i)}
                        stroke="hsl(var(--card))"
                        strokeWidth={1}
                      />
                    )
                  : false
              }
              isAnimationActive={false}
            />
          ))}
        </LineChart>
      )}
    </ResponsiveContainer>
  )
}
