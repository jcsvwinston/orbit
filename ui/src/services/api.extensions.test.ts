import { afterEach, describe, expect, it, vi } from 'vitest'
import { checkSession, getDashboard, getUIExtensions, runModelAction } from './api'

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

    expect(await getUIExtensions()).toEqual({
      pages: [{ id: 'reports', title: 'Reports', url: '/admin/x/reports/' }],
      dashboards: [],
    })
  })

  // An application with no screens of its own is the common case: the
  // payload carries an empty list and the navigation shows nothing extra.
  it('treats a payload with no pages as none', async () => {
    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify({}), { status: 200, headers: { 'content-type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    expect(await getUIExtensions()).toEqual({ pages: [], dashboards: [] })
  })

  it('returns the dashboards the panel listed beside the pages', async () => {
    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify({ pages: [], dashboards: [{ id: 'finance', title: 'Finance', url: '/admin/dashboards/finance' }] }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }))
    vi.stubGlobal('fetch', fetchMock)

    expect((await getUIExtensions()).dashboards).toEqual([{ id: 'finance', title: 'Finance', url: '/admin/dashboards/finance' }])
  })
})

describe('getDashboard', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('asks for the dashboard by id and fills what the payload left out', async () => {
    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify({ id: 'finance', title: 'Finance' }), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      }))
    vi.stubGlobal('fetch', fetchMock)

    expect(await getDashboard('finance')).toEqual({ id: 'finance', title: 'Finance', description: undefined, columns: 4, widgets: [] })
    expect((fetchMock.mock.calls[0] as unknown as [string])[0]).toBe('/admin/api/ui/dashboards/finance')
  })

  // A 403 travels as the ApiError the page draws as "no permission".
  it('rejects with the status of a refusal', async () => {
    vi.stubGlobal('fetch', vi.fn(async () =>
      new Response(JSON.stringify({ error: { code: 'FORBIDDEN', message: 'not authorized to view on dashboard:finance' } }), {
        status: 403,
        headers: { 'content-type': 'application/json' },
      })))

    await expect(getDashboard('finance')).rejects.toMatchObject({ status: 403 })
  })
})

describe('checkSession', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  const answering = (status: number) =>
    vi.fn(async () => new Response('{}', { status, headers: { 'content-type': 'application/json' } }))

  it('is signed in when the model list answers', async () => {
    vi.stubGlobal('fetch', answering(200))
    expect(await checkSession()).toBe(true)
  })

  // An operator granted a dashboard and not the model list is signed in:
  // sending them to the login screen made signing in a loop.
  it('is signed in when the model list refuses this operator', async () => {
    vi.stubGlobal('fetch', answering(403))
    expect(await checkSession()).toBe(true)
  })

  it('is not signed in when the session is not accepted', async () => {
    vi.stubGlobal('fetch', answering(401))
    expect(await checkSession()).toBe(false)
  })
})
