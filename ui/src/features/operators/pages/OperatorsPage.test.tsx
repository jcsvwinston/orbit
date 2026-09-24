import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'

vi.mock('@/services/api', async () => {
  const actual = await vi.importActual<typeof import('@/services/api')>('@/services/api')
  return {
    ...actual,
    getOperators: vi.fn(async () => ({
      total: 2,
      operators: [
        {
          id: 'u_1', username: 'ana', email: 'ana@example.com',
          is_superuser: true, is_active: true, roles: ['editors'],
        },
        {
          id: 'u_2', username: 'leaver', email: 'leaver@example.com',
          is_superuser: false, is_active: false, roles: [],
        },
      ],
    })),
    setOperatorActive: vi.fn(async () => {}),
  }
})

import OperatorsPage, { parseRoles } from './OperatorsPage'

describe('OperatorsPage', () => {
  it('separates a deactivated operator from an active one', async () => {
    render(<OperatorsPage />)

    const rows = await screen.findAllByTestId('operator-row')
    const ana = rows.find((row) => within(row).queryByText('ana'))
    const leaver = rows.find((row) => within(row).queryByText('leaver'))
    expect(ana).toBeDefined()
    expect(leaver).toBeDefined()

    expect(within(ana!).getByTestId('operator-status')).toHaveTextContent('active')
    expect(within(leaver!).getByTestId('operator-status')).toHaveTextContent('deactivated')

    // A deactivated account is still listed — the panel deactivates people
    // rather than erasing them — and the superuser badge is on the one who
    // holds it.
    expect(within(ana!).getByTestId('operator-superuser')).toBeInTheDocument()
    expect(within(leaver!).queryByTestId('operator-superuser')).toBeNull()
  })

  it('offers reactivation for the deactivated one and deactivation for the active one', async () => {
    render(<OperatorsPage />)
    expect(await screen.findByRole('button', { name: /deactivate ana/i })).toBeInTheDocument()
    expect(await screen.findByRole('button', { name: /reactivate leaver/i })).toBeInTheDocument()
  })

  it('asks before deleting, and says deactivating is the alternative', async () => {
    render(<OperatorsPage />)
    const button = await screen.findByRole('button', { name: /delete ana/i })
    button.click()
    expect(await screen.findByRole('dialog')).toHaveTextContent(/deactivate it\s+instead/i)
  })
})

describe('parseRoles', () => {
  it('drops the empty entries a trailing comma leaves behind', () => {
    expect(parseRoles('editors, viewers,')).toEqual(['editors', 'viewers'])
    expect(parseRoles('   ')).toEqual([])
  })
})
