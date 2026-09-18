import { afterEach, describe, expect, it, vi } from 'vitest'
import { getUIExtensions, runModelAction } from './api'

// What an application adds to the panel, from the SPA's side: a verb it
// posts to the same bulk endpoint the built-in ones use, and the navigation
// entries for the screens the application serves itself.
describe('runModelAction', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('posts the declared verb with the selection as strings', async () => {
    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify({ action: 'publish', ran: true, requested: 2, affected: 2, failed: 0, message: '2 published' }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await runModelAction('Post', 'publish', [7, 'b1c2d3e4'])

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/admin/api/models/Post/bulk')
    expect(JSON.parse(init.body as string)).toEqual({ action: 'publish', ids: ['7', 'b1c2d3e4'] })
    expect(result.message).toBe('2 published')
  })
})

describe('getUIExtensions', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns the pages the panel listed', async () => {
    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify({ pages: [{ id: 'reports', title: 'Reports', url: '/admin/x/reports/' }] }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }))
    vi.stubGlobal('fetch', fetchMock)

    expect(await getUIExtensions()).toEqual([{ id: 'reports', title: 'Reports', url: '/admin/x/reports/' }])
  })

  // An application with no screens of its own is the common case: the
  // payload carries an empty list and the navigation shows nothing extra.
  it('treats a payload with no pages as none', async () => {
    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify({}), { status: 200, headers: { 'content-type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    expect(await getUIExtensions()).toEqual([])
  })
})
