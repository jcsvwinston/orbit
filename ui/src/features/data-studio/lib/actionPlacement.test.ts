import { describe, expect, it } from 'vitest'
import type { ModelActionSpec } from '@/types'
import { offeredOnRecord, offeredOnSelection } from './actionPlacement'

const action = (placement?: ModelActionSpec['placement']): ModelActionSpec => ({
  name: 'a', label: 'A', destructive: false, requires_selection: true, placement,
})

describe('action placement', () => {
  // A server older than placements offered every action on the selection,
  // and that is what an absent placement still means.
  it('reads an absent placement as the selection', () => {
    expect(offeredOnSelection(action())).toBe(true)
    expect(offeredOnRecord(action())).toBe(false)
  })

  it('offers each action where it was declared', () => {
    expect([offeredOnSelection(action('selection')), offeredOnRecord(action('selection'))]).toEqual([true, false])
    expect([offeredOnSelection(action('record')), offeredOnRecord(action('record'))]).toEqual([false, true])
    expect([offeredOnSelection(action('selection_and_record')), offeredOnRecord(action('selection_and_record'))]).toEqual([true, true])
  })
})
