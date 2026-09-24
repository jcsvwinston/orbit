import { describe, expect, it } from 'vitest'
import { BATCH_SIZE_OPTIONS, DEFAULT_PAGE_SIZE, MAX_PAGE_SIZE } from './constants'

describe('batch size options', () => {
  it('never offers a page size the list endpoint rejects (page_size <= 200)', () => {
    expect(MAX_PAGE_SIZE).toBe(200)
    for (const size of BATCH_SIZE_OPTIONS) {
      expect(size).toBeLessThanOrEqual(MAX_PAGE_SIZE)
      expect(size).toBeGreaterThan(0)
    }
  })

  it('includes the default page size', () => {
    expect(BATCH_SIZE_OPTIONS).toContain(DEFAULT_PAGE_SIZE)
  })
})
