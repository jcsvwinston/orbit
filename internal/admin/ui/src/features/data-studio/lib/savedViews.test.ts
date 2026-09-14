import { describe, it, expect } from 'vitest'
import { gridQueryToString, gridQueryFromString } from './savedViews'

describe('saved view query round trip', () => {
  it('carries search, order and filters', () => {
    const query = { search: 'blue', filters: { status: 'open' }, orderBy: 'title asc', pageSize: 25 }
    const back = gridQueryFromString(gridQueryToString(query))
    expect(back).toEqual(query)
  })

  it('drops empty filters rather than storing a filter on nothing', () => {
    const text = gridQueryToString({ search: '', filters: { status: '  ', tag: 'x' }, orderBy: '' })
    expect(text).toBe('tag=x')
  })

  it('never lets a filter shadow the reserved parameters', () => {
    const text = gridQueryToString({
      search: 'real',
      filters: { search: 'fake', page: '9', tag: 'x' },
      orderBy: '',
    })
    const back = gridQueryFromString(text)
    expect(back.search).toBe('real')
    expect(back.filters).toEqual({ tag: 'x' })
  })

  it('accepts a stored value with or without the leading question mark', () => {
    expect(gridQueryFromString('?status=open').filters).toEqual({ status: 'open' })
    expect(gridQueryFromString('status=open').filters).toEqual({ status: 'open' })
  })

  it('ignores a page size that is not a positive number', () => {
    expect(gridQueryFromString('page_size=0').pageSize).toBeUndefined()
    expect(gridQueryFromString('page_size=abc').pageSize).toBeUndefined()
    expect(gridQueryFromString('page_size=30').pageSize).toBe(30)
  })
})
