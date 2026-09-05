import { vi, type Mock } from 'vitest'

/**
 * Replaces `window.location.reload` with a mock for the current test and
 * returns it. jsdom marks Location unforgeable — every property is own,
 * non-writable and non-configurable — so neither `vi.spyOn` nor a Proxy can
 * swap `reload` alone. The stub is a plain object whose URL fields read the
 * real Location live (the router follows `history.pushState` through them)
 * and whose `reload` is the mock. Undo it with `vi.unstubAllGlobals()`.
 */
export function stubLocationReload(): Mock<() => void> {
  const reload = vi.fn<() => void>()
  const real = window.location
  const stub: Record<string, unknown> = {
    reload,
    assign: (url: string) => real.assign(url),
    replace: (url: string) => real.replace(url),
    toString: () => real.toString(),
  }
  for (const key of ['href', 'origin', 'protocol', 'host', 'hostname', 'port', 'pathname', 'search', 'hash'] as const) {
    Object.defineProperty(stub, key, { get: () => real[key], enumerable: true })
  }
  vi.stubGlobal('location', stub)
  return reload
}
