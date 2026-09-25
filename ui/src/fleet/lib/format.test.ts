import { describe, expect, it } from 'vitest'
import { create } from '@bufbuild/protobuf'
import { DurationSchema, TimestampSchema } from '@bufbuild/protobuf/wkt'

import { durationToMillis, formatDuration, streamRowKey, timestampToDate } from './format'

describe('fleet formatters', () => {
  it('turns a protobuf timestamp into a date, nanoseconds rounded down to milliseconds', () => {
    const ts = create(TimestampSchema, { seconds: BigInt(1_700_000_000), nanos: 123_999_999 })
    expect(timestampToDate(ts)?.getTime()).toBe(1_700_000_000_123)
    expect(timestampToDate(undefined)).toBeUndefined()
  })

  it('keys a stream row by node and wall clock, and by index only without a clock', () => {
    const ts = create(TimestampSchema, { seconds: BigInt(7), nanos: 42 })
    expect(streamRowKey('node-a', ts, 3)).toBe('node-a:7.42')
    expect(streamRowKey('node-a', undefined, 3)).toBe('node-a:idx3')
  })

  it('formats durations in the unit a person reads', () => {
    expect(durationToMillis(create(DurationSchema, { seconds: BigInt(1), nanos: 500_000_000 }))).toBe(1500)
    expect(durationToMillis(undefined)).toBe(0)
    expect(formatDuration(0.25)).toBe('250µs')
    expect(formatDuration(12.3456)).toBe('12.35ms')
    expect(formatDuration(2500)).toBe('2.50s')
  })
})
