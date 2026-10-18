import { afterEach, describe, expect, it, vi } from 'vitest'
import { getModelSchema, isApiError } from './api'

// The backend's error shape is the application's choice: the framework's
// envelope today, RFC 9457 problem details when the application opted in —
// and by default from Nucleus 2.0 on. The panel must read whichever arrives
// and show the backend's own message, not a generic one.
describe('ApiError from either error shape', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  async function failWith(status: number, body: string, contentType = 'application/json') {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(body, { status, headers: { 'content-type': contentType } })))
    try {
      await getModelSchema('Note')
    } catch (err) {
      if (!isApiError(err)) throw err
      return err
    }
    throw new Error('the call did not fail')
  }

  it('reads a domain error from the envelope', async () => {
    const err = await failWith(403, JSON.stringify({ error: { code: 'FORBIDDEN', message: 'operator may not retrieve Note' } }))
    expect(err.status).toBe(403)
    expect(err.isForbidden).toBe(true)
    expect(err.message).toBe('operator may not retrieve Note')
    expect(err.code).toBe('FORBIDDEN')
  })

  it('reads a plain error from the envelope', async () => {
    const err = await failWith(500, JSON.stringify({ error: 'something broke' }))
    expect(err.message).toBe('something broke')
    expect(err.code).toBeUndefined()
  })

  it('reads problem details, preferring detail over title', async () => {
    const err = await failWith(
      404,
      JSON.stringify({
        type: 'about:blank',
        title: 'Not Found',
        status: 404,
        detail: 'model Nope is not registered',
        instance: '/admin/api/models/Nope/schema',
        code: 'NOT_FOUND',
      }),
      'application/problem+json',
    )
    expect(err.status).toBe(404)
    expect(err.message).toBe('model Nope is not registered')
    expect(err.code).toBe('NOT_FOUND')
  })

  it('falls back to the problem title when there is no detail', async () => {
    const err = await failWith(403, JSON.stringify({ type: 'about:blank', title: 'Forbidden', status: 403 }), 'application/problem+json')
    expect(err.isForbidden).toBe(true)
    expect(err.message).toBe('Forbidden')
    expect(err.code).toBeUndefined()
  })

  it('falls back to the raw text, then to the status line', async () => {
    const text = await failWith(502, 'upstream gone', 'text/plain')
    expect(text.message).toBe('upstream gone')
    const empty = await failWith(503, '', 'text/plain')
    expect(empty.message).toMatch(/^503/)
  })
})
