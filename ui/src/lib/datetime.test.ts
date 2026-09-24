import { describe, expect, it } from 'vitest'
import { isoToLocalInput, localInputToISO } from './datetime'

describe('datetime-local <-> ISO', () => {
  it('keeps the instant when converting to the input and back', () => {
    const iso = '2026-09-03T08:15:00Z'
    const local = isoToLocalInput(iso)
    expect(local).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/)
    const back = localInputToISO(local)
    expect(back).toMatch(/[+-]\d{2}:\d{2}$/)
    expect(new Date(back).getTime()).toBe(new Date(iso).getTime())
  })

  it('leaves unparseable values untouched', () => {
    expect(isoToLocalInput('not a date')).toBe('not a date')
    expect(localInputToISO('')).toBe('')
  })
})
