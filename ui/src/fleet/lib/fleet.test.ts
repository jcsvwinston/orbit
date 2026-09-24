import { describe, expect, it } from 'vitest'

import { NodeInfo } from '@/fleet/gen/nucleus/admin/v1/admin_pb'
import { fleetMainVersion } from './fleet'

describe('fleetMainVersion', () => {
  it('is the version most nodes run, and empty for an empty fleet', () => {
    const nodes = [
      new NodeInfo({ nodeId: 'a', version: '1.2.0' }),
      new NodeInfo({ nodeId: 'b', version: '1.3.0' }),
      new NodeInfo({ nodeId: 'c', version: '1.3.0' }),
    ]
    expect(fleetMainVersion(nodes)).toBe('1.3.0')
    expect(fleetMainVersion([])).toBe('')
  })
})
