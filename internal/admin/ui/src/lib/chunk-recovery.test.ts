import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from 'vitest'
import { stubLocationReload } from '../test/stub-reload'

type Recovery = typeof import('./chunk-recovery')

const FLAG = 'orbit-admin:chunk-reload'

const chrome = () => new TypeError('Failed to fetch dynamically imported module: https://h/admin/assets/RBACPage-x.js')
const firefox = () => new TypeError('error loading dynamically imported module: https://h/admin/assets/RBACPage-x.js')
const safari = () => new TypeError('Importing a module script failed.')
const viteCss = () => new Error('Unable to preload CSS for /admin/assets/DataStudioPage-x.css')

describe('chunk recovery', () => {
  let recovery: Recovery
  let reload: Mock<() => void>

  beforeEach(async () => {
    sessionStorage.clear()
    reload = stubLocationReload()
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
    it('reloads on the first chunk failure of the session and remembers it', () => {
      expect(recovery.reloadPending(chrome())).toBe(true)
      expect(recovery.reloadOnce(chrome())).toBe(true)
      expect(reload).toHaveBeenCalledTimes(1)
      expect(sessionStorage.getItem(FLAG)).not.toBeNull()
      // A second failure while that reload is under way is the same reload.
      expect(recovery.reloadOnce(safari())).toBe(true)
      expect(recovery.reloadPending(new Error('anything'))).toBe(true)
      expect(reload).toHaveBeenCalledTimes(1)
    })

    it('does not reload again in the document the reload produced', () => {
      sessionStorage.setItem(FLAG, '1')
      expect(recovery.reloadPending(chrome())).toBe(false)
      expect(recovery.reloadOnce(chrome())).toBe(false)
      expect(reload).not.toHaveBeenCalled()
    })

    it('reloads again once a chunk has loaded in between', () => {
      sessionStorage.setItem(FLAG, '1')
      recovery.markChunkLoaded()
      expect(recovery.reloadOnce(firefox())).toBe(true)
      expect(reload).toHaveBeenCalledTimes(1)
    })

    it('never reloads for an error that is not a chunk failure', () => {
      expect(recovery.reloadPending(new Error('render exploded'))).toBe(false)
      expect(recovery.reloadOnce(new Error('render exploded'))).toBe(false)
      expect(reload).not.toHaveBeenCalled()
      expect(sessionStorage.getItem(FLAG)).toBeNull()
    })

    // vitest exposes the runtime's own Web Storage here: its prototype is not
    // the global Storage.prototype, and an own property set on the instance
    // stores an item instead of overriding the method, so spy on the prototype.
    const storageProto = () => Object.getPrototypeOf(sessionStorage) as Storage

    it('shows the error instead of reloading when storage cannot bound the reloads', () => {
      vi.spyOn(storageProto(), 'getItem').mockImplementation(() => {
        throw new Error('SecurityError')
      })
      expect(recovery.reloadPending(chrome())).toBe(false)
      expect(recovery.reloadOnce(chrome())).toBe(false)
      expect(reload).not.toHaveBeenCalled()
    })

    it('does not reload when the flag cannot be written', () => {
      vi.spyOn(storageProto(), 'setItem').mockImplementation(() => {
        throw new Error('QuotaExceededError')
      })
      expect(recovery.reloadOnce(chrome())).toBe(false)
      expect(reload).not.toHaveBeenCalled()
    })
  })

  describe('installPreloadErrorReload', () => {
    function preloadError(payload: unknown) {
      return Object.assign(new Event('vite:preloadError', { cancelable: true }), { payload })
    }

    it("applies the one-reload rule to Vite's preload failures and leaves the import rejecting", () => {
      const remove = recovery.installPreloadErrorReload()
      try {
        const event = preloadError(viteCss())
        window.dispatchEvent(event)
        expect(reload).toHaveBeenCalledTimes(1)
        // Not prevented: Vite still throws, so the boundary sees the failure too.
        expect(event.defaultPrevented).toBe(false)

        window.dispatchEvent(preloadError(chrome()))
        expect(reload).toHaveBeenCalledTimes(1)
      } finally {
        remove()
      }
    })

    it('ignores payloads that are not chunk failures and stops listening once removed', () => {
      const remove = recovery.installPreloadErrorReload()
      window.dispatchEvent(preloadError(new Error('something else')))
      expect(reload).not.toHaveBeenCalled()

      remove()
      window.dispatchEvent(preloadError(chrome()))
      expect(reload).not.toHaveBeenCalled()
    })
  })
})
