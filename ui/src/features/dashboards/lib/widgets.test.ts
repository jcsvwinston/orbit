import { describe, expect, it } from 'vitest'
import { chartRows, gridColumnsClass, isChart, isTabular, seriesColour, spanClass } from './widgets'

describe('the grid', () => {
  // The overview's grid is the one it had before dashboards existed.
  it('draws four columns the way the overview always did', () => {
    expect(gridColumnsClass(4)).toBe('md:grid-cols-2 xl:grid-cols-4')
    expect(gridColumnsClass(undefined)).toBe('md:grid-cols-2 xl:grid-cols-4')
  })

  it('narrows to the columns a dashboard declares, and ignores what it cannot draw', () => {
    expect(gridColumnsClass(1)).toBe('grid-cols-1')
    expect(gridColumnsClass(2)).toBe('md:grid-cols-2')
    expect(gridColumnsClass(9)).toBe('md:grid-cols-2 xl:grid-cols-4')
  })

  // A card of one column carries no class, so a value card of A6 is drawn
  // with the markup it had.
  it('spans a card only when it is wider than one column', () => {
    expect(spanClass(undefined)).toBe('')
    expect(spanClass(1)).toBe('')
    expect(spanClass(2)).toBe('md:col-span-2')
    expect(spanClass(4)).toBe('md:col-span-2 xl:col-span-4')
  })
})

describe('chartRows', () => {
  it('turns columns of values into one row per label, a key per series', () => {
    expect(
      chartRows(['Mon', 'Tue'], [{ name: 'Web', values: [1, 2] }, { name: 'label', values: [3, 4] }]),
    ).toEqual([
      { label: 'Mon', s0: 1, s1: 3 },
      { label: 'Tue', s0: 2, s1: 4 },
    ])
  })

  it('is empty for a chart with nothing to draw', () => {
    expect(chartRows(undefined, undefined)).toEqual([])
  })
})

describe('kinds and colours', () => {
  it('tells charts and tables from the rest', () => {
    expect(isChart({ id: 'a', title: 'a', kind: 'line' })).toBe(true)
    expect(isChart({ id: 'a', title: 'a', kind: 'bar' })).toBe(true)
    expect(isChart({ id: 'a', title: 'a' })).toBe(false)
    expect(isTabular({ id: 'a', title: 'a', kind: 'records' })).toBe(true)
    expect(isTabular({ id: 'a', title: 'a', kind: 'stat' })).toBe(false)
  })

  it('gives the first series the accent and a colour of its own to each of eight', () => {
    expect(seriesColour(0)).toBe('hsl(var(--primary))')
    expect(new Set(Array.from({ length: 8 }, (_, i) => seriesColour(i))).size).toBe(8)
  })
})
