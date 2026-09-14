import { describe, it, expect } from 'vitest'
import { changedFields, renderHistoryValue } from './recordHistory'
import type { AuditLog } from '@/types'

function entry(partial: Partial<AuditLog>): AuditLog {
  return {
    id: 1, timestamp: '2026-09-14T00:00:00Z', userId: 'u1', username: 'root',
    action: 'update', modelName: 'Note', recordId: '1', ip: '127.0.0.1',
    ...partial,
  }
}

describe('changedFields', () => {
  it('lists only the fields that actually moved', () => {
    const got = changedFields(entry({
      oldValue: { title: 'before', status: 'draft' },
      newValue: { title: 'after', status: 'draft' },
    }))
    expect(got).toEqual([{ field: 'title', before: 'before', after: 'after' }])
  })

  it('reads a create as every field arriving', () => {
    const got = changedFields(entry({ action: 'create', oldValue: null, newValue: { title: 'new' } }))
    expect(got).toEqual([{ field: 'title', before: undefined, after: 'new' }])
  })

  it('reads a delete as every field leaving', () => {
    const got = changedFields(entry({ action: 'delete', oldValue: { title: 'gone' }, newValue: null }))
    expect(got).toEqual([{ field: 'title', before: 'gone', after: undefined }])
  })

  it('does not report a nested value that is equal as a change', () => {
    const got = changedFields(entry({
      oldValue: { meta: { a: 1 }, title: 'x' },
      newValue: { meta: { a: 1 }, title: 'y' },
    }))
    expect(got.map((c) => c.field)).toEqual(['title'])
  })

  it('is empty for an entry with no values recorded', () => {
    expect(changedFields(entry({ oldValue: null, newValue: null }))).toEqual([])
  })
})

describe('renderHistoryValue', () => {
  it('shows an em dash for what is not there', () => {
    expect(renderHistoryValue(null)).toBe('—')
    expect(renderHistoryValue(undefined)).toBe('—')
    expect(renderHistoryValue('')).toBe('—')
  })

  it('shows a string as itself and anything else as JSON', () => {
    expect(renderHistoryValue('hello')).toBe('hello')
    expect(renderHistoryValue(42)).toBe('42')
    expect(renderHistoryValue({ a: 1 })).toBe('{"a":1}')
  })
})
