import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'

// The chart library is not what these tests are about, and a jsdom without
// layout gives it no size to draw in: the chart module is replaced by one
// that writes down what it was asked to draw.
vi.mock('./SeriesChart', () => ({
  default: ({ kind, labels, series }: { kind: string; labels: string[]; series: Array<{ values: number[] }> }) => (
    <div data-testid="series-chart" data-kind={kind} data-points={labels.length} data-series={series.length} />
  ),
}))

import WidgetGrid from './WidgetGrid'

describe('WidgetGrid', () => {
  // The card A6 shipped is drawn as it was: the whole card is the link.
  it('draws a value card as the overview always did', () => {
    const { container } = render(
      <WidgetGrid widgets={[{ id: 'orders', title: 'Orders', value: '12', detail: 'oldest: 3 days', link: '/data-studio' }]} />,
    )
    const link = screen.getByRole('link')
    expect(link).toHaveAttribute('href', '/data-studio')
    expect(link).toHaveClass('block')
    expect(within(link).getByText('12')).toBeInTheDocument()
    expect(container.firstElementChild).toHaveClass('grid', 'gap-4', 'md:grid-cols-2', 'xl:grid-cols-4')
  })

  it('draws a stat with its change, and says which way it went in words', () => {
    render(<WidgetGrid widgets={[{ id: 'rev', kind: 'stat', title: 'Revenue', value: '$1.2M', delta: '+12%', trend: 'up', sentiment: 'good' }]} />)
    expect(screen.getByText('$1.2M')).toBeInTheDocument()
    const change = screen.getByText('+12%').closest('p')!
    expect(change).toHaveAttribute('data-trend', 'up')
    expect(change).toHaveClass('text-emerald-700')
    expect(within(change).getByText('up')).toHaveClass('sr-only')
  })

  it('draws a series as a chart, with its numbers as a table for a screen reader', async () => {
    render(
      <WidgetGrid
        widgets={[{
          id: 'signups', kind: 'line', title: 'Signups', span: 2,
          labels: ['Mon', 'Tue', 'Wed'], series: [{ name: 'Web', values: [1, 2, 3] }, { name: 'Mobile', values: [0, 1, 5] }],
        }]}
      />,
    )
    const chart = await screen.findByTestId('series-chart')
    expect(chart).toHaveAttribute('data-kind', 'line')
    expect(chart).toHaveAttribute('data-points', '3')
    expect(screen.getByRole('img', { name: 'Signups, chart' })).toBeInTheDocument()
    const table = screen.getByRole('table', { name: 'Signups' })
    expect(table).toHaveClass('sr-only')
    expect(within(table).getAllByRole('row')).toHaveLength(4)
    expect(screen.getByText('Mobile', { selector: 'li' })).toBeInTheDocument()
    expect(document.querySelector('[data-widget="signups"]')).toHaveClass('md:col-span-2')
  })

  it('draws a table, and the newest rows of a model, as tables', () => {
    render(
      <WidgetGrid
        widgets={[
          { id: 'queue', kind: 'table', title: 'Queues', columns: ['Queue', 'Depth'], rows: [['mail', '3']] },
          { id: 'recent', kind: 'records', title: 'Recent notes', model: 'Note', columns: ['Title'], rows: [] },
        ]}
      />,
    )
    const queue = document.querySelector('[data-widget="queue"]') as HTMLElement
    expect(within(queue).getByRole('columnheader', { name: 'Depth' })).toBeInTheDocument()
    expect(within(queue).getByRole('cell', { name: 'mail' })).toBeInTheDocument()
    const recent = document.querySelector('[data-widget="recent"]') as HTMLElement
    expect(within(recent).getByText('Nothing to show')).toBeInTheDocument()
  })

  // A card that could not be read is drawn saying so, whatever its kind:
  // dropping it would report a broken query as "nothing to see".
  it('draws a failed card as a failure, not as an empty one', () => {
    render(<WidgetGrid widgets={[{ id: 'bad', kind: 'line', title: 'Broken', error: 'series "x" has 1 values for 3 labels' }]} />)
    expect(screen.getByText('This card could not be read')).toBeInTheDocument()
    expect(screen.queryByTestId('series-chart')).toBeNull()
  })
})
