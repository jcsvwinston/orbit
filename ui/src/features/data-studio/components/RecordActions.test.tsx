import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import type { ModelActionSpec, ModelSchema } from '@/types'
import RecordForm from './RecordForm'
import RecordActionsMenu from './RecordActionsMenu'

const duplicate: ModelActionSpec = {
  name: 'duplicate', label: 'Duplicate', description: 'Copy this note', destructive: false,
  requires_selection: true, placement: 'record',
}

const schema: ModelSchema = {
  name: 'Note', plural: 'Notes', table: 'notes', primary_key: 'id', icon: '', read_only: false,
  foreign_keys: [], tenant_field: '',
  fields: [
    { name: 'ID', column: 'id', label: 'ID', type: 'uint', html_type: 'number', is_pk: true, is_required: false, is_readonly: true, is_list: true, is_search: false, is_filter: false, is_excluded: false, is_fk: false, is_tenant_field: false },
    { name: 'Title', column: 'title', label: 'Title', type: 'string', html_type: 'text', is_pk: false, is_required: true, is_readonly: false, is_list: true, is_search: true, is_filter: false, is_excluded: false, is_fk: false, is_tenant_field: false },
  ],
}

// The record view (EXT-02, UIX-08): the actions offered on one record are
// drawn where the record is, and only on a record that exists.
describe('RecordForm actions', () => {
  it('draws the record actions on an existing record and runs the one pressed', () => {
    const onAction = vi.fn()
    render(<RecordForm open onClose={() => {}} schema={schema} record={{ id: 7, title: 'Seven' }} onSave={async () => {}} actions={[duplicate]} onAction={onAction} />)
    const group = screen.getByRole('group', { name: 'Actions on this record' })
    fireEvent.click(group.querySelector('button') as HTMLButtonElement)
    expect(onAction).toHaveBeenCalledWith(duplicate)
  })

  it('draws none on a record being created', () => {
    render(<RecordForm open onClose={() => {}} schema={schema} record={null} onSave={async () => {}} actions={[duplicate]} onAction={() => {}} />)
    expect(screen.queryByRole('group', { name: 'Actions on this record' })).toBeNull()
  })

  // An operator who may not update the record still reaches it — through a
  // link or an action's redirect — and is shown its values, not a form
  // whose save would be refused.
  it('shows the record read-only to an operator who may not update it', () => {
    render(<RecordForm open readOnly onClose={() => {}} schema={schema} record={{ id: 7, title: 'Seven' }} onSave={async () => {}} />)
    expect(screen.getByRole('dialog')).toHaveTextContent('Seven')
    expect(screen.queryByRole('textbox')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Update' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Done' })).toBeInTheDocument()
  })
})

describe('RecordActionsMenu', () => {
  it('names the record it acts on and offers its actions', async () => {
    const onSelect = vi.fn()
    render(<RecordActionsMenu actions={[duplicate]} recordLabel="7" onSelect={onSelect} />)
    const trigger = screen.getByRole('button', { name: 'Actions for record 7' })
    fireEvent.click(trigger)
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Duplicate' }))
    expect(onSelect).toHaveBeenCalledWith(duplicate)
  })
})
