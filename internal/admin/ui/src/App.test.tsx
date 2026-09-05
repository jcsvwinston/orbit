import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import type { ComponentType } from 'react'
import { stubLocationReload } from './test/stub-reload'

type PageModule = { default: ComponentType }

// The feature routes are mocked so each test decides how a chunk loads:
// deferred, rejected, or a page of its own. App.tsx builds the React.lazy
// components at module load and lazy remembers a settled loader for good, so
// every test imports a fresh App (vi.resetModules in beforeEach).
const routes = vi.hoisted(() => ({
  load: {} as Record<string, () => Promise<PageModule>>,
}))

vi.mock('@/routes', () => ({
  lazyRoutes: [
    { path: 'rbac', load: () => routes.load.rbac() },
    { path: 'audit', load: () => routes.load.audit() },
  ],
}))

vi.mock('@/services/api', async () => {
  const actual = await vi.importActual<typeof import('@/services/api')>('@/services/api')
  return {
    ...actual,
    checkSession: vi.fn(async () => true),
    getRBACPolicies: vi.fn(async () => ({ enabled: true, policies: [] })),
    getAuditLogs: vi.fn(async () => ({ enabled: true, entries: [], total: 0, page: 1, pageSize: 50, totalPages: 0 })),
  }
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

const realRBACPage = () => import('@/features/rbac/pages/RBACPage')
const realAuditPage = () => import('@/features/audit/pages/AuditLogPage')

// What Chrome rejects a dynamic import with when the chunk answers 404.
function missingChunkError(name: string) {
  return new TypeError(`Failed to fetch dynamically imported module: https://example.test/nucleus-admin/assets/${name}`)
}

function setPrefix(prefix: string) {
  const meta = document.createElement('meta')
  meta.name = 'nucleus-admin-prefix'
  meta.content = prefix
  document.head.appendChild(meta)
  return meta
}

const nav = () => screen.findByRole('navigation', { name: /main navigation/i })
// The content area exists once the session check has committed the layout.
const main = async () => within(await screen.findByRole('main'))
const contentSpinner = () => within(screen.getByRole('main')).queryByRole('status', { name: /loading/i })
const rbacHeading = () => screen.findByRole('heading', { level: 1, name: 'Access Control' })
const auditHeading = () => screen.findByRole('heading', { level: 1, name: 'Audit Log' })

async function loadApp() {
  vi.resetModules()
  return (await import('./App')).default
}

describe('App', () => {
  let meta: HTMLMetaElement
  let reload: Mock<() => void>
  let App: ComponentType

  beforeEach(async () => {
    meta = setPrefix('/nucleus-admin')
    sessionStorage.clear()
    reload = stubLocationReload()
    // Before reloading, the recovery module asks the server for the document
    // (a HEAD request) to be sure the reload can fetch it; here it answers.
    vi.stubGlobal('fetch', vi.fn(async () => ({ status: 200 }) as Response))
    routes.load = { rbac: realRBACPage, audit: realAuditPage }
    App = await loadApp()
  })

  afterEach(() => {
    meta.remove()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    window.history.pushState({}, '', '/')
  })

  it('keeps the sidebar while a page chunk loads and swaps the spinner for the page when it lands', async () => {
    const chunk = deferred<PageModule>()
    routes.load.rbac = () => chunk.promise
    window.history.pushState({}, '', '/nucleus-admin/rbac')

    render(<App />)

    // Both at once: the layout (sidebar) is committed and the spinner sits in
    // its content area. Without the Suspense boundary in DashboardLayout the
    // lazy page suspends up to the root and nothing renders until the chunk
    // lands, so the navigation never appears while the chunk is pending.
    expect(await nav()).toBeInTheDocument()
    expect(contentSpinner()).toBeInTheDocument()
    expect(screen.queryByRole('heading', { level: 1, name: 'Access Control' })).not.toBeInTheDocument()

    chunk.resolve(await realRBACPage())

    expect(await rbacHeading()).toBeInTheDocument()
    expect(await nav()).toBeInTheDocument()
    expect(contentSpinner()).not.toBeInTheDocument()
    expect(reload).not.toHaveBeenCalled()
  })

  it('shows the spinner in the content area as soon as the sidebar navigates to a page whose chunk is pending', async () => {
    const chunk = deferred<PageModule>()
    routes.load.rbac = () => chunk.promise
    window.history.pushState({}, '', '/nucleus-admin/audit')

    render(<App />)
    expect(await auditHeading()).toBeInTheDocument()

    fireEvent.click(screen.getByRole('link', { name: 'Access Control' }))

    // The router wraps navigations in a transition; the keyed boundary in the
    // layout mounts a new Suspense, whose fallback is allowed to show, so the
    // click gets feedback instead of the Audit page lingering until the chunk lands.
    await waitFor(() => expect(contentSpinner()).toBeInTheDocument())
    expect(screen.queryByRole('heading', { level: 1, name: 'Audit Log' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Access Control' })).toHaveAttribute('aria-current', 'page')

    chunk.resolve(await realRBACPage())
    expect(await rbacHeading()).toBeInTheDocument()
  })

  describe('when a page chunk fails to load', () => {
    beforeEach(() => {
      // React reports the caught error through console.error; keep the run quiet.
      vi.spyOn(console, 'error').mockImplementation(() => {})
    })

    it('reloads the panel once instead of unmounting it (stale index.html after an upgrade)', async () => {
      routes.load.rbac = () => Promise.reject(missingChunkError('RBACPage-old.js'))
      window.history.pushState({}, '', '/nucleus-admin/rbac')

      render(<App />)

      expect(await nav()).toBeInTheDocument()
      await waitFor(() => expect(reload).toHaveBeenCalledTimes(1))
      // The spinner stays up until the new document lands: no error flash,
      // and the sidebar is still there.
      expect(contentSpinner()).toBeInTheDocument()
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
      expect(await nav()).toBeInTheDocument()
    })

    it('shows the error with a Reload button, sidebar in place, when the chunk fails while the server is restarting', async () => {
      // The redeploy window itself: the process serving the panel is not
      // answering yet, so the probe that precedes the reload fails. Reloading
      // now would replace the whole panel with the browser's connection-error
      // page; the boundary's error state, with the sidebar still there, is
      // the outcome instead.
      const probe = deferred<Response>()
      const fetchMock = vi.fn(() => probe.promise)
      vi.stubGlobal('fetch', fetchMock)
      routes.load.rbac = () => Promise.reject(missingChunkError('RBACPage-old.js'))
      window.history.pushState({}, '', '/nucleus-admin/audit')

      render(<App />)
      expect(await auditHeading()).toBeInTheDocument()

      fireEvent.click(screen.getByRole('link', { name: 'Access Control' }))

      // The chunk fails and the probe goes out; while it is out the spinner
      // stays, the usual outcome being the reload.
      await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
      expect(contentSpinner()).toBeInTheDocument()
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
      expect(reload).not.toHaveBeenCalled()

      probe.reject(new TypeError('Failed to fetch'))

      const alert = await (await main()).findByRole('alert')
      expect(alert).toHaveTextContent('This page could not be loaded')
      expect(contentSpinner()).not.toBeInTheDocument()
      expect(reload).not.toHaveBeenCalled()
      // Nothing was spent: the tab keeps its automatic reload for when the server is back.
      expect(sessionStorage.getItem('orbit-admin:chunk-reload')).toBeNull()
      expect(await nav()).toBeInTheDocument()

      // Pages already loaded still open from the sidebar...
      fireEvent.click(screen.getByRole('link', { name: 'Audit Log' }))
      expect(await auditHeading()).toBeInTheDocument()
      expect(reload).not.toHaveBeenCalled()

      // ...and the failed page shows the same error again, whose Reload button is the way back.
      fireEvent.click(screen.getByRole('link', { name: 'Access Control' }))
      const again = await (await main()).findByRole('alert')
      fireEvent.click(within(again).getByRole('button', { name: 'Reload' }))
      expect(reload).toHaveBeenCalledTimes(1)
    })

    it('shows an error with a Reload button when the chunk fails again in the reloaded document', async () => {
      routes.load.rbac = () => Promise.reject(missingChunkError('RBACPage-old.js'))
      window.history.pushState({}, '', '/nucleus-admin/rbac')

      // First document: the automatic reload.
      const first = render(<App />)
      await waitFor(() => expect(reload).toHaveBeenCalledTimes(1))
      first.unmount()

      // The document the reload produced: fresh modules, same sessionStorage,
      // and the chunk is still missing (the server really lacks it now).
      App = await loadApp()
      render(<App />)

      expect(await nav()).toBeInTheDocument()
      const alert = await (await main()).findByRole('alert')
      expect(alert).toHaveTextContent('This page could not be loaded')
      expect(alert).toHaveTextContent('RBACPage-old.js')
      expect(contentSpinner()).not.toBeInTheDocument()
      expect(reload).toHaveBeenCalledTimes(1)

      fireEvent.click(within(alert).getByRole('button', { name: 'Reload' }))
      expect(reload).toHaveBeenCalledTimes(2)
    })

    it('gives the next upgrade its own automatic reload once a chunk has loaded', async () => {
      routes.load.rbac = () => Promise.reject(missingChunkError('RBACPage-old.js'))
      window.history.pushState({}, '', '/nucleus-admin/rbac')

      const first = render(<App />)
      await waitFor(() => expect(reload).toHaveBeenCalledTimes(1))
      first.unmount()

      // Reloaded document: the chunk exists again and loads, which clears the flag.
      routes.load.rbac = realRBACPage
      App = await loadApp()
      const second = render(<App />)
      expect(await rbacHeading()).toBeInTheDocument()
      second.unmount()

      // Much later, another upgrade with this tab still open.
      routes.load.audit = () => Promise.reject(missingChunkError('AuditLogPage-old.js'))
      window.history.pushState({}, '', '/nucleus-admin/audit')
      App = await loadApp()
      render(<App />)

      expect(await nav()).toBeInTheDocument()
      await waitFor(() => expect(reload).toHaveBeenCalledTimes(2))
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })

    it('leaves the error behind when the sidebar navigates to another page', async () => {
      sessionStorage.setItem('orbit-admin:chunk-reload', String(Date.now())) // the automatic reload is spent
      routes.load.rbac = () => Promise.reject(missingChunkError('RBACPage-old.js'))
      window.history.pushState({}, '', '/nucleus-admin/rbac')

      render(<App />)
      expect(await (await main()).findByRole('alert')).toBeInTheDocument()

      fireEvent.click(screen.getByRole('link', { name: 'Audit Log' }))

      expect(await auditHeading()).toBeInTheDocument()
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
      expect(reload).not.toHaveBeenCalled()
    })
  })

  it('contains a page that throws while rendering and renders it afresh on Try again', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    let broken = true
    function Page() {
      if (broken) throw new Error('render exploded')
      return <h1>Recovered</h1>
    }
    routes.load.rbac = () => Promise.resolve({ default: Page })
    window.history.pushState({}, '', '/nucleus-admin/rbac')

    render(<App />)

    expect(await nav()).toBeInTheDocument()
    const alert = await (await main()).findByRole('alert')
    expect(alert).toHaveTextContent('This page failed to render')
    expect(alert).toHaveTextContent('render exploded')
    expect(reload).not.toHaveBeenCalled()

    broken = false
    fireEvent.click(within(alert).getByRole('button', { name: 'Try again' }))

    expect(await screen.findByRole('heading', { level: 1, name: 'Recovered' })).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(await nav()).toBeInTheDocument()
  })
})
