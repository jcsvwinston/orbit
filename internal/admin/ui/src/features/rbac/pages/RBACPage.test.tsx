import { describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'

vi.mock('@/services/api', async () => {
  const actual = await vi.importActual<typeof import('@/services/api')>('@/services/api')
  return {
    ...actual,
    getRBACPolicies: vi.fn(async () => ({
      enabled: true,
      policies: [
        { sub: 'viewer', obj: 'Note', act: 'list', eft: 'allow' as const },
        { sub: 'viewer', obj: 'Note', act: 'delete', eft: 'deny' as const },
      ],
    })),
  }
})

import RBACPage from './RBACPage'

describe('RBACPage', () => {
  it('shows a deny policy as deny, not as allow', async () => {
    render(<RBACPage />)

    const rows = await screen.findAllByRole('row')
    const denyRow = rows.find((row) => within(row).queryByText('delete'))
    const allowRow = rows.find((row) => within(row).queryByText('list'))
    expect(denyRow).toBeDefined()
    expect(allowRow).toBeDefined()

    expect(within(denyRow!).getByTestId('policy-effect')).toHaveTextContent('deny')
    expect(within(allowRow!).getByTestId('policy-effect')).toHaveTextContent('allow')
  })

  it('asks for confirmation before deleting a policy', async () => {
    render(<RBACPage />)
    const button = await screen.findByRole('button', { name: /delete policy viewer note delete/i })
    button.click()
    expect(await screen.findByRole('dialog')).toHaveTextContent(/remove the deny rule/i)
  })
})
