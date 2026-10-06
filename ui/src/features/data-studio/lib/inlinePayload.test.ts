import { describe, it, expect } from 'vitest'
import { inlinePayload } from './inlinePayload'

describe('inlinePayload', () => {
  it('sends a row without an id as an insert', () => {
    expect(inlinePayload([{ id: '', values: { title: 'One' }, deleted: false }]))
      .toEqual([{ title: 'One' }])
  })

  it('sends a row with an id as an edit', () => {
    expect(inlinePayload([{ id: '7', values: { title: 'One' }, deleted: false }]))
      .toEqual([{ id: '7', title: 'One' }])
  })

  it('marks a removal explicitly, so absence never deletes', () => {
    expect(inlinePayload([{ id: '7', values: { title: 'One' }, deleted: true }]))
      .toEqual([{ id: '7', title: 'One', _delete: true }])
  })

  it('leaves out a saved row nobody changed, which the server would ask update for', () => {
    const got = inlinePayload([
      { id: '1', values: { title: 'looked at' }, deleted: false, changed: false },
      { id: '2', values: { title: 'edited' }, deleted: false, changed: true },
      { id: '3', values: { title: 'removed' }, deleted: true, changed: false },
      { id: '', values: { title: 'added' }, deleted: false, changed: false },
    ])
    expect(got).toEqual([
      { id: '2', title: 'edited' },
      { id: '3', title: 'removed', _delete: true },
      { title: 'added' },
    ])
  })

  it('keeps every row it is given, in order', () => {
    const got = inlinePayload([
      { id: '1', values: { title: 'a' }, deleted: false },
      { id: '', values: { title: 'b' }, deleted: false },
      { id: '2', values: { title: 'c' }, deleted: true },
    ])
    expect(got).toHaveLength(3)
    expect(got.map((r) => r.title)).toEqual(['a', 'b', 'c'])
  })
})
