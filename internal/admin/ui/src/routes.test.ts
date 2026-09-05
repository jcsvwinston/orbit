import { describe, expect, it } from 'vitest'
import { lazyRoutes } from './routes'

describe('lazyRoutes', () => {
  it('lists every sidebar destination except the Overview landing', () => {
    expect(lazyRoutes.map((route) => route.path)).toEqual([
      'data-studio',
      'system',
      'live',
      'sessions',
      'health',
      'rbac',
      'audit',
    ])
  })

  // A mistyped dynamic import only fails when someone navigates to the page;
  // resolving each loader here catches it in CI instead.
  it.each(lazyRoutes)('resolves $path to a page component', async ({ load }) => {
    const mod = await load()
    expect(typeof mod.default).toBe('function')
  })
})
