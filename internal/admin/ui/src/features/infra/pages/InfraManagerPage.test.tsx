import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'

vi.mock('@/services/api', async () => {
  const actual = await vi.importActual<typeof import('@/services/api')>('@/services/api')
  return {
    ...actual,
    getSessions: vi.fn(async () => ({
      enabled: true,
      currentActive: 2,
      truncatedByLimit: false,
      sessions: [
        {
          id: 'h1', tokenShort: 'abc...1234', user: 'ana', current: true,
          device: 'Firefox on Linux', userAgent: 'Mozilla/5.0 (X11; Linux x86_64) Firefox/130.0',
          remoteIp: '198.51.100.10', host: '', pod: '', instance: '',
          firstSeenAt: '', lastSeenAt: '', expiresAt: '',
        },
        {
          id: 'h2', tokenShort: 'def...5678', user: 'bob', current: false,
          device: 'Safari on iOS', userAgent: 'Mozilla/5.0 (iPhone) Safari/604.1',
          remoteIp: '', host: '', pod: '', instance: '',
          firstSeenAt: '', lastSeenAt: '', expiresAt: '',
        },
      ],
    })),
    revokeUserSessions: vi.fn(async (user: string) => ({ user, revoked: 1, keptCurrent: user === 'ana' })),
  }
})

import InfraManagerPage from './InfraManagerPage'

describe('InfraManagerPage', () => {
  it('says whose each session is and from what device', async () => {
    render(<InfraManagerPage />)

    const rows = await screen.findAllByTestId('session-row')
    const ana = rows.find((row) => within(row).queryByText('ana'))
    const bob = rows.find((row) => within(row).queryByText('bob'))
    expect(ana).toBeDefined()
    expect(bob).toBeDefined()

    expect(within(ana!).getByTestId('session-device')).toHaveTextContent('Firefox on Linux')
    expect(within(bob!).getByTestId('session-device')).toHaveTextContent('Safari on iOS')

    // The operator's own session is marked, so "which one is me" is read,
    // not worked out from a token prefix.
    expect(within(ana!).getByTestId('session-current')).toBeInTheDocument()
    expect(within(bob!).queryByTestId('session-current')).toBeNull()
  })

  it('offers to revoke every session of a user, and says the caller keeps theirs', async () => {
    render(<InfraManagerPage />)
    const button = await screen.findByRole('button', { name: /revoke all sessions of bob/i })
    button.click()
    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveTextContent(/signed in as/)
    expect(dialog).toHaveTextContent('bob')
    expect(dialog).toHaveTextContent(/this browser is kept/i)
  })
})
