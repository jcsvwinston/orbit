import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

import { ApiError } from '@/services/api'

const getDashboard = vi.hoisted(() => vi.fn())

vi.mock('@/services/api', async () => {
  const actual = await vi.importActual<typeof import('@/services/api')>('@/services/api')
  return { ...actual, getDashboard }
})

import DashboardPage from './DashboardPage'

function open(id: string) {
  return render(
    <MemoryRouter initialEntries={[`/dashboards/${id}`]}>
      <Routes>
        <Route path="/dashboards/:id" element={<DashboardPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('DashboardPage', () => {
  it('draws the dashboard the route names, under its title', async () => {
    getDashboard.mockResolvedValueOnce({
      id: 'finance', title: 'Finance', description: 'The money', columns: 2,
      widgets: [{ id: 'revenue', title: 'Revenue', value: '$1.2M' }],
    })
    open('finance')
    expect(await screen.findByRole('heading', { level: 1, name: 'Finance' })).toBeInTheDocument()
    expect(await screen.findByText('$1.2M')).toBeInTheDocument()
    expect(getDashboard).toHaveBeenLastCalledWith('finance')
  })

  // The navigation hides a dashboard an operator may not open; reached by
  // its address, the API's 403 is drawn as a permission, not a breakage.
  it('draws a refusal as a missing permission', async () => {
    getDashboard.mockRejectedValueOnce(new ApiError(403, 'not authorized to view on dashboard:finance', null))
    open('finance')
    expect(await screen.findByText('You do not have permission to view this')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /try again/i })).toBeNull()
  })
})
