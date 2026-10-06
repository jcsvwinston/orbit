import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import type { InlineSpec, ModelSchema } from '@/types'

vi.mock('@/services/api', () => ({
  getModelSchema: vi.fn(),
  getRecordsPaginated: vi.fn(),
  errorMessage: (err: unknown) => String(err),
}))

import * as api from '@/services/api'
import InlineEditor, { type InlineRow } from './InlineEditor'
import { inlinePayload } from '../lib/inlinePayload'

const spec: InlineSpec = { model: 'Comment', key: 'comments', column: 'note_id', field: 'NoteID', label: 'Comments' }

function childSchema(permissions: { [verb: string]: boolean }): ModelSchema {
  return {
    name: 'Comment', plural: 'Comments', table: 'comments', primary_key: 'id', icon: '', read_only: false,
    foreign_keys: [], tenant_field: '',
    permissions,
    can_create: permissions.create, can_update: permissions.update, can_delete: permissions.delete,
    fields: [
      { name: 'ID', column: 'id', label: 'ID', type: 'uint', html_type: 'number', is_pk: true, is_required: false, is_readonly: true, is_list: true, is_search: false, is_filter: false, is_excluded: false, is_fk: false, is_tenant_field: false },
      { name: 'NoteID', column: 'note_id', label: 'Note', type: 'uint', html_type: 'number', is_pk: false, is_required: true, is_readonly: false, is_list: true, is_search: false, is_filter: false, is_excluded: false, is_fk: true, is_tenant_field: false },
      { name: 'Body', column: 'body', label: 'Body', type: 'string', html_type: 'text', is_pk: false, is_required: false, is_readonly: false, is_list: true, is_search: false, is_filter: false, is_excluded: false, is_fk: false, is_tenant_field: false },
    ],
  }
}

// The children's verbs are the child model's own, and the server checks each
// one before the parent is written (OR-65): what the editor offers has to be
// what the child's schema says this operator holds, or the save is lost.
async function open(permissions: { [verb: string]: boolean }) {
  vi.mocked(api.getModelSchema).mockResolvedValue(childSchema(permissions))
  vi.mocked(api.getRecordsPaginated).mockResolvedValue({
    items: [{ id: 3, note_id: 7, body: 'saved line' }],
  } as unknown as Awaited<ReturnType<typeof api.getRecordsPaginated>>)
  const published: InlineRow[][] = []
  render(<InlineEditor spec={spec} parentId="7" onChange={(_, rows) => published.push(rows)} />)
  const saved = await screen.findByRole('textbox', { name: 'Comments 1 Body' })
  return { saved, published }
}

describe('InlineEditor', () => {
  beforeEach(() => vi.clearAllMocks())

  it('offers every change to an operator who holds the child verbs', async () => {
    const { saved } = await open({ create: true, update: true, delete: true })
    expect(screen.getByRole('button', { name: /Add/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Remove Comments 1' })).toBeInTheDocument()
    expect(saved).toBeEnabled()
  })

  it('offers neither an Add nor a Remove to one who may only edit the lines', async () => {
    const { saved, published } = await open({ create: false, update: true, delete: false })
    expect(screen.queryByRole('button', { name: /Add/ })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Remove Comments 1' })).toBeNull()
    fireEvent.change(saved, { target: { value: 'edited line' } })
    expect(inlinePayload(published[published.length - 1])).toEqual([{ id: '3', body: 'edited line' }])
  })

  it('lets one who may only add a line add it, and sends no update for the lines they only saw', async () => {
    const { saved, published } = await open({ create: true, update: false, delete: false })
    expect(saved).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Remove Comments 1' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /Add/ }))
    const added = screen.getByRole('textbox', { name: 'Comments 2 Body' })
    expect(added).toBeEnabled()
    // A new line can always be dropped again: it was never saved.
    expect(screen.getByRole('button', { name: 'Remove Comments 2' })).toBeInTheDocument()
    fireEvent.change(added, { target: { value: 'new line' } })
    expect(inlinePayload(published[published.length - 1])).toEqual([{ body: 'new line' }])
  })

  it('shows the lines read-only to one who may change none of them', async () => {
    const { saved } = await open({ create: false, update: false, delete: false })
    expect(saved).toBeDisabled()
    expect(screen.queryByRole('button', { name: /Add/ })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Remove Comments 1' })).toBeNull()
    expect(screen.getByText('You may view these, not change them.')).toBeInTheDocument()
  })
})
