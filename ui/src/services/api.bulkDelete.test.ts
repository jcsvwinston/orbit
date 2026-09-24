import { afterEach, describe, expect, it, vi } from 'vitest'
import { bulkDelete } from './api'

// The bulk endpoint takes ids as strings (the datasource contract's boundary
// type); the SPA must send them so whatever the grid decoded — a number, a
// UUID, a composite-ish string.
describe('bulkDelete', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sends every id as a string in the request body', async () => {
    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify({ deleted: 2, failed: 1, errors: [{ id: 'abc', error: 'invalid id' }] }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await bulkDelete('Doc', [7, '0b1c2d3e-0000-4000-8000-000000000001', 'abc'])

    expect(fetchMock).toHaveBeenCalledTimes(1)
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/admin/api/models/Doc/bulk')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({
      action: 'delete',
      ids: ['7', '0b1c2d3e-0000-4000-8000-000000000001', 'abc'],
    })
    expect(result.errors?.[0].id).toBe('abc')
  })
})
