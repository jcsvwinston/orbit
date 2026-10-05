import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import type { ModelSchema } from '@/types'
import { forgetFieldRenderers, registerFieldRenderer } from '@/lib/clientExtensions'
import RenderedField from './RenderedField'
import RecordForm from './RecordForm'

afterEach(() => forgetFieldRenderers())

function badge(value: unknown): Node {
  if (value !== 'draft') throw new Error(`no badge for ${JSON.stringify(value)}`)
  const el = document.createElement('span')
  el.setAttribute('data-badge', String(value))
  el.textContent = 'Draft'
  return el
}

const field = { model: 'Note', field: 'Status', column: 'status', where: 'list' as const }

// EXT-07: the application's renderer draws the value; when it fails, the
// value is drawn the panel's way and the failure is said in the same place.
describe('RenderedField', () => {
  it('draws what the renderer returns', () => {
    registerFieldRenderer('badge', badge)
    const { container } = render(<RenderedField renderer="badge" value="draft" record={{ status: 'draft' }} fallback="draft" {...field} />)
    expect(container.querySelector('[data-badge="draft"]')).toHaveTextContent('Draft')
    expect(container.querySelector('[data-renderer-state]')).toHaveAttribute('data-renderer-state', 'drawn')
  })

  it('falls back to the panel\'s drawing and says the renderer failed, when it throws', () => {
    registerFieldRenderer('badge', badge)
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    render(<RenderedField renderer="badge" value="archived" record={{ status: 'archived' }} fallback="archived" {...field} />)
    expect(screen.getByText('archived')).toBeInTheDocument()
    expect(screen.getByText('badge failed: no badge for "archived"')).toBeInTheDocument()
    expect(error).toHaveBeenCalled()
    error.mockRestore()
  })

  it('draws the panel\'s way until the renderer is registered, then draws again', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { container } = render(<RenderedField renderer="badge" value="draft" record={null} fallback="draft" {...field} />)
    expect(screen.getByText('draft')).toBeInTheDocument()
    expect(container.querySelector('[data-badge]')).toBeNull()
    act(() => registerFieldRenderer('badge', badge))
    expect(container.querySelector('[data-badge="draft"]')).toHaveTextContent('Draft')
    warn.mockRestore()
  })
})

const schema: ModelSchema = {
  name: 'Note', plural: 'Notes', table: 'notes', primary_key: 'id', icon: '', read_only: false,
  foreign_keys: [], tenant_field: '',
  fields: [
    { name: 'ID', column: 'id', label: 'ID', type: 'uint', html_type: 'number', is_pk: true, is_required: false, is_readonly: true, is_list: true, is_search: false, is_filter: false, is_excluded: false, is_fk: false, is_tenant_field: false },
    { name: 'Status', column: 'status', label: 'Status', type: 'string', html_type: 'text', is_pk: false, is_required: false, is_readonly: false, is_list: true, is_search: false, is_filter: true, is_excluded: false, is_fk: false, is_tenant_field: false, renderer: 'badge' },
  ],
}

// The record view draws the stored value with the renderer, and keeps the
// input that edits it; a renderer that throws costs the drawing, not the form.
describe('RecordForm with a field renderer', () => {
  it('draws the stored value above the input that edits it', () => {
    registerFieldRenderer('badge', badge)
    const { container } = render(<RecordForm open onClose={() => {}} schema={schema} record={{ id: 7, status: 'draft' }} onSave={async () => {}} />)
    expect(container.ownerDocument.querySelector('[role="dialog"] [data-badge="draft"]')).toHaveTextContent('Draft')
    expect(screen.getByLabelText('Status')).toHaveValue('draft')
  })

  it('keeps the form when the renderer throws', () => {
    registerFieldRenderer('badge', badge)
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    render(<RecordForm open onClose={() => {}} schema={schema} record={{ id: 8, status: 'archived' }} onSave={async () => {}} />)
    expect(screen.getByText('badge failed: no badge for "archived"')).toBeInTheDocument()
    expect(screen.getByLabelText('Status')).toHaveValue('archived')
    expect(screen.getByRole('button', { name: 'Update' })).toBeEnabled()
    error.mockRestore()
  })

  it('draws the renderer on a record shown read-only', () => {
    registerFieldRenderer('badge', badge)
    render(<RecordForm open readOnly onClose={() => {}} schema={schema} record={{ id: 9, status: 'draft' }} onSave={async () => {}} />)
    expect(document.querySelector('[role="dialog"] [data-badge="draft"]')).toHaveTextContent('Draft')
  })
})
