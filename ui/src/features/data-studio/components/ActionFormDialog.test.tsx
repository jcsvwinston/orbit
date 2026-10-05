import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { ApiError } from '@/services/api'
import type { ModelActionSpec } from '@/types'
import ActionFormDialog from './ActionFormDialog'

const schedule: ModelActionSpec = {
  name: 'schedule',
  label: 'Schedule',
  description: 'Schedule the selected notes',
  destructive: false,
  requires_selection: true,
  fields: [
    { name: 'reason', label: 'Reason', type: 'text', required: true, help: 'Recorded with the audit entry' },
    { name: 'notify', label: 'Notify subscribers', type: 'boolean', required: false },
    { name: 'channel', label: 'Channel', type: 'select', required: true, options: [{ value: 'web', label: 'Website' }] },
  ],
}

describe('ActionFormDialog', () => {
  it('draws one labelled input per declared field', () => {
    render(<ActionFormDialog action={schedule} selectedCount={2} onCancel={() => {}} onSubmit={async () => {}} />)
    expect(screen.getByRole('dialog')).toHaveTextContent('2 records selected.')
    expect(screen.getByLabelText(/^Reason/)).toHaveAttribute('type', 'text')
    expect(screen.getByLabelText(/^Notify subscribers/)).toHaveAttribute('type', 'checkbox')
    expect(screen.getByLabelText(/^Channel/).tagName).toBe('SELECT')
    expect(screen.getByLabelText(/^Reason/)).toHaveAccessibleDescription('Recorded with the audit entry')
  })

  // The server decides: the form posts what it has, and a refusal lands on
  // the field it names, as that input's description.
  it('puts the server refusal on the field it names', async () => {
    const onSubmit = vi.fn(async () => {
      throw new ApiError(422, 'Schedule: reason is required', {
        error: { code: 'VALIDATION_FAILED', message: 'Schedule: reason is required', details: { reason: 'is required' } },
      })
    })
    render(<ActionFormDialog action={schedule} selectedCount={1} onCancel={() => {}} onSubmit={onSubmit} />)
    fireEvent.click(screen.getByRole('button', { name: 'Schedule' }))

    await waitFor(() => expect(screen.getByLabelText(/^Reason/)).toHaveAttribute('aria-invalid', 'true'))
    expect(onSubmit).toHaveBeenCalledWith({ notify: false })
    expect(screen.getByLabelText(/^Reason/)).toHaveAccessibleDescription(/Reason is required/)
    expect(screen.getByRole('alert')).toHaveTextContent('Fix the highlighted fields.')

    // Typing in the field clears its error.
    fireEvent.change(screen.getByLabelText(/^Reason/), { target: { value: 'late' } })
    expect(screen.getByLabelText(/^Reason/)).not.toHaveAttribute('aria-invalid')
  })

  it('shows any other refusal as a message', async () => {
    const onSubmit = vi.fn(async () => {
      throw new ApiError(403, 'forbidden', { error: { code: 'FORBIDDEN', message: 'forbidden' } })
    })
    render(<ActionFormDialog action={schedule} selectedCount={1} onCancel={() => {}} onSubmit={onSubmit} />)
    fireEvent.click(screen.getByRole('button', { name: 'Schedule' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('forbidden')
  })
})
