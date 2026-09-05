import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'

vi.mock('@/services/api', async () => {
  const actual = await vi.importActual<typeof import('@/services/api')>('@/services/api')
  return {
    ...actual,
    checkSession: vi.fn(async () => true),
    getRBACPolicies: vi.fn(async () => ({ enabled: true, policies: [] })),
  }
})

import App from './App'

function setPrefix(prefix: string) {
  const meta = document.createElement('meta')
  meta.name = 'nucleus-admin-prefix'
  meta.content = prefix
  document.head.appendChild(meta)
  return meta
}

describe('App', () => {
  let meta: HTMLMetaElement

  beforeEach(() => {
    meta = setPrefix('/nucleus-admin')
  })

  afterEach(() => {
    meta.remove()
    window.history.pushState({}, '', '/')
  })

  it('renders a lazy feature page inside the layout once its chunk resolves', async () => {
    window.history.pushState({}, '', '/nucleus-admin/rbac')

    render(<App />)

    // The layout is eager: the sidebar is there as soon as the session check
    // passes, before the page chunk has landed.
    expect(await screen.findByRole('navigation', { name: /main navigation/i })).toBeInTheDocument()
    // The page arrives through React.lazy + the Suspense boundary in the layout.
    expect(await screen.findByRole('heading', { level: 1, name: 'Access Control' })).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('status', { name: /loading/i })).not.toBeInTheDocument())
  })
})
