import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from 'vitest'
import { stubLocationReload } from '../test/stub-reload'

type Recovery = typeof import('./chunk-recovery')

const FLAG = 'orbit-admin:chunk-reload'
// What the probe that precedes a reload asks the server for.
const PROBE = { method: 'HEAD', cache: 'no-store', credentials: 'same-origin' }

const chrome = () => new TypeError('Failed to fetch dynamically imported module: https://h/admin/assets/RBACPage-x.js')
const firefox = () => new TypeError('error loading dynamically imported module: https://h/admin/assets/RBACPage-x.js')
const safari = () => new TypeError('Importing a module script failed.')
const viteCss = () => new Error('Unable to preload CSS for /admin/assets/DataStudioPage-x.css')

// Stand-ins for the server the probe asks: one that answers with a status,
// and one that cannot be reached (the process is restarting, or the network
// is gone), which is what fetch rejects with.
const answering = (status = 200) => vi.fn(async () => ({ status }) as Response)
const unreachable = () =>
  vi.fn(async (): Promise<Response> => {
    throw new TypeError('Failed to fetch')
  })

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

// Lets a fire-and-forget decision (the preload listener's) settle.
const settled = () => new Promise<void>((resolve) => setTimeout(resolve, 0))

describe('chunk recovery', () => {
  let recovery: Recovery
  let reload: Mock<() => void>
  let probe: Mock<() => Promise<Response>>

  function serverThat(mock: Mock<() => Promise<Response>>) {
    probe = mock
    vi.stubGlobal('fetch', probe)
  }

  beforeEach(async () => {
    sessionStorage.clear()
    reload = stubLocationReload()
    serverThat(answering())
    // The module remembers a reload it started; every test gets a fresh document.
    vi.resetModules()
    recovery = await import('./chunk-recovery')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  describe('isChunkLoadError', () => {
    it.each([
      ['Chrome', chrome()],
      ['Firefox', firefox()],
      ['Safari', safari()],
      ["Vite's stylesheet preload", viteCss()],
    ])('recognises the error %s rejects a dynamic import with', (_browser, error) => {
      expect(recovery.isChunkLoadError(error)).toBe(true)
    })

    it.each([
      ['a render error', new Error('Cannot read properties of undefined')],
      ['a TypeError of another kind', new TypeError('x is not a function')],
      ['a string', 'Failed to fetch dynamically imported module'],
      ['undefined', undefined],
    ])('does not match %s', (_what, error) => {
      expect(recovery.isChunkLoadError(error)).toBe(false)
    })
  })

  describe('reloadOnce', () => {
    it('reloads on the first chunk failure of the session once the server answers, and remembers it', async () => {
      expect(recovery.reloadPending(chrome())).toBe(true)
      const decision = recovery.reloadOnce(chrome())
      // The reload waits for the probe, which asks for the document the
      // reload will fetch; nothing is reloaded or spent before it answers.
      expect(probe).toHaveBeenCalledWith(window.location.href, PROBE)
      expect(reload).not.toHaveBeenCalled()
      expect(sessionStorage.getItem(FLAG)).toBeNull()

      await expect(decision).resolves.toBe(true)
      expect(reload).toHaveBeenCalledTimes(1)
      expect(sessionStorage.getItem(FLAG)).not.toBeNull()

      // A second failure while that reload is under way is the same reload.
      await expect(recovery.reloadOnce(safari())).resolves.toBe(true)
      expect(recovery.reloadPending(new Error('anything'))).toBe(true)
      expect(probe).toHaveBeenCalledTimes(1)
      expect(reload).toHaveBeenCalledTimes(1)
    })

    it('shows the error instead of reloading when the server does not answer (the process is restarting)', async () => {
      serverThat(unreachable())
      // Read synchronously the failure still warrants a reload; the probe decides.
      expect(recovery.reloadPending(chrome())).toBe(true)

      await expect(recovery.reloadOnce(chrome())).resolves.toBe(false)
      expect(probe).toHaveBeenCalledTimes(1)
      expect(reload).not.toHaveBeenCalled()
      // Nothing was spent and nothing is in flight: the next failure probes
      // again, and reloads once the server is back.
      expect(sessionStorage.getItem(FLAG)).toBeNull()
      expect(recovery.reloadPending(chrome())).toBe(true)

      serverThat(answering())
      await expect(recovery.reloadOnce(chrome())).resolves.toBe(true)
      expect(reload).toHaveBeenCalledTimes(1)
      expect(sessionStorage.getItem(FLAG)).not.toBeNull()
    })

    it.each([500, 502, 503, 504])(
      'shows the error when the server answers %i (a proxy whose upstream is restarting)',
      async (status) => {
        serverThat(answering(status))
        await expect(recovery.reloadOnce(chrome())).resolves.toBe(false)
        expect(reload).not.toHaveBeenCalled()
        expect(sessionStorage.getItem(FLAG)).toBeNull()
      },
    )

    it.each([200, 401, 404])('reloads when the server answers %i: it is up, whatever it makes of the request', async (status) => {
      serverThat(answering(status))
      await expect(recovery.reloadOnce(chrome())).resolves.toBe(true)
      expect(reload).toHaveBeenCalledTimes(1)
    })

    it('shares one probe between failures reported while it is out', async () => {
      const held = deferred<Response>()
      serverThat(vi.fn(() => held.promise))

      const first = recovery.reloadOnce(chrome())
      const second = recovery.reloadOnce(viteCss())
      expect(probe).toHaveBeenCalledTimes(1)
      expect(recovery.reloadPending(new Error('anything'))).toBe(true)
      expect(reload).not.toHaveBeenCalled()

      held.resolve({ status: 200 } as Response)
      await expect(first).resolves.toBe(true)
      await expect(second).resolves.toBe(true)
      expect(reload).toHaveBeenCalledTimes(1)
    })

    it('does not reload again in the document the reload produced', async () => {
      sessionStorage.setItem(FLAG, String(Date.now() - 5_000))
      expect(recovery.reloadPending(chrome())).toBe(false)
      await expect(recovery.reloadOnce(chrome())).resolves.toBe(false)
      expect(probe).not.toHaveBeenCalled()
      expect(reload).not.toHaveBeenCalled()
      // The spent flag is left as it was, not refreshed.
      expect(Number(sessionStorage.getItem(FLAG))).toBeLessThan(Date.now() - 4_000)
    })

    it('reloads again once the previous reload is older than the window', async () => {
      // The reloaded document landed on a static page (session invalidated by
      // the upgrade: /rbac -> /login -> Overview), so no chunk cleared the
      // flag; a later upgrade with the tab still open must get its reload.
      sessionStorage.setItem(FLAG, String(Date.now() - recovery.RELOAD_SPENT_MS - 1_000))
      expect(recovery.reloadPending(chrome())).toBe(true)
      await expect(recovery.reloadOnce(chrome())).resolves.toBe(true)
      expect(reload).toHaveBeenCalledTimes(1)
      // The flag is fresh again, so the reloaded document sees it as spent.
      expect(Number(sessionStorage.getItem(FLAG))).toBeGreaterThan(Date.now() - 1_000)
    })

    it('treats a flag it cannot read as a date as not spent', async () => {
      sessionStorage.setItem(FLAG, 'yes')
      expect(recovery.reloadPending(chrome())).toBe(true)
      await expect(recovery.reloadOnce(chrome())).resolves.toBe(true)
      expect(reload).toHaveBeenCalledTimes(1)
    })

    it('reloads again once a chunk has loaded in between', async () => {
      sessionStorage.setItem(FLAG, String(Date.now()))
      recovery.markChunkLoaded()
      await expect(recovery.reloadOnce(firefox())).resolves.toBe(true)
      expect(reload).toHaveBeenCalledTimes(1)
    })

    it('shows the error instead of reloading while the browser is offline, without asking the server', async () => {
      vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false)
      expect(recovery.reloadPending(chrome())).toBe(false)
      await expect(recovery.reloadOnce(safari())).resolves.toBe(false)
      expect(probe).not.toHaveBeenCalled()
      expect(reload).not.toHaveBeenCalled()
      // Nothing was spent: once back online the next failure reloads.
      expect(sessionStorage.getItem(FLAG)).toBeNull()
      vi.restoreAllMocks()
      await expect(recovery.reloadOnce(chrome())).resolves.toBe(true)
      expect(reload).toHaveBeenCalledTimes(1)
    })

    it('never reloads for an error that is not a chunk failure', async () => {
      expect(recovery.reloadPending(new Error('render exploded'))).toBe(false)
      await expect(recovery.reloadOnce(new Error('render exploded'))).resolves.toBe(false)
      expect(probe).not.toHaveBeenCalled()
      expect(reload).not.toHaveBeenCalled()
      expect(sessionStorage.getItem(FLAG)).toBeNull()
    })

    // vitest exposes the runtime's own Web Storage here: its prototype is not
    // the global Storage.prototype, and an own property set on the instance
    // stores an item instead of overriding the method, so spy on the prototype.
    const storageProto = () => Object.getPrototypeOf(sessionStorage) as Storage

    it('shows the error instead of reloading when storage cannot bound the reloads', async () => {
      vi.spyOn(storageProto(), 'getItem').mockImplementation(() => {
        throw new Error('SecurityError')
      })
      expect(recovery.reloadPending(chrome())).toBe(false)
      await expect(recovery.reloadOnce(chrome())).resolves.toBe(false)
      expect(probe).not.toHaveBeenCalled()
      expect(reload).not.toHaveBeenCalled()
    })

    it('does not reload when the flag cannot be written', async () => {
      vi.spyOn(storageProto(), 'setItem').mockImplementation(() => {
        throw new Error('QuotaExceededError')
      })
      await expect(recovery.reloadOnce(chrome())).resolves.toBe(false)
      expect(reload).not.toHaveBeenCalled()
    })
  })

  describe('installPreloadErrorReload', () => {
    function preloadError(payload: unknown) {
      return Object.assign(new Event('vite:preloadError', { cancelable: true }), { payload })
    }

    it("applies the one-reload rule to Vite's preload failures and leaves the import rejecting", async () => {
      const remove = recovery.installPreloadErrorReload()
      try {
        const event = preloadError(viteCss())
        window.dispatchEvent(event)
        // Not prevented: Vite still throws, so the boundary sees the failure too.
        expect(event.defaultPrevented).toBe(false)
        await vi.waitFor(() => expect(reload).toHaveBeenCalledTimes(1))

        window.dispatchEvent(preloadError(chrome()))
        await settled()
        expect(probe).toHaveBeenCalledTimes(1)
        expect(reload).toHaveBeenCalledTimes(1)
      } finally {
        remove()
      }
    })

    it('does not reload for a preload failure while offline', async () => {
      vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false)
      const remove = recovery.installPreloadErrorReload()
      try {
        window.dispatchEvent(preloadError(viteCss()))
        await settled()
        expect(probe).not.toHaveBeenCalled()
        expect(reload).not.toHaveBeenCalled()
      } finally {
        remove()
      }
    })

    it('does not reload for a preload failure when the server does not answer', async () => {
      serverThat(unreachable())
      const remove = recovery.installPreloadErrorReload()
      try {
        window.dispatchEvent(preloadError(viteCss()))
        await settled()
        expect(probe).toHaveBeenCalledTimes(1)
        expect(reload).not.toHaveBeenCalled()
        expect(sessionStorage.getItem(FLAG)).toBeNull()
      } finally {
        remove()
      }
    })

    it('ignores payloads that are not chunk failures and stops listening once removed', async () => {
      const remove = recovery.installPreloadErrorReload()
      window.dispatchEvent(preloadError(new Error('something else')))
      await settled()
      expect(reload).not.toHaveBeenCalled()

      remove()
      window.dispatchEvent(preloadError(chrome()))
      await settled()
      expect(probe).not.toHaveBeenCalled()
      expect(reload).not.toHaveBeenCalled()
    })
  })
})
