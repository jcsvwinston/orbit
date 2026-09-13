import { describe, it, expect } from 'vitest'
import { screenCapabilities, isFieldEditable } from './capabilities'
import type { ModelSchema, SchemaField } from '@/types'

function schema(partial: Partial<ModelSchema>): ModelSchema {
  return {
    name: 'Note', plural: 'Notes', table: 'notes', primary_key: 'id', icon: '',
    read_only: false, fields: [], foreign_keys: [], tenant_field: '',
    ...partial,
  } as ModelSchema
}

function field(partial: Partial<SchemaField>): SchemaField {
  return {
    name: 'Title', column: 'title', label: 'Title', type: 'string', html_type: 'text',
    is_pk: false, is_required: false, is_readonly: false, is_list: true,
    is_search: false, is_filter: false, is_excluded: false, is_fk: false,
    is_tenant_field: false,
    ...partial,
  } as SchemaField
}

describe('screenCapabilities', () => {
  it('treats a schema with no hints as fully allowed, the way it behaved before hints existed', () => {
    const caps = screenCapabilities(schema({}))
    expect(caps).toEqual({ canCreate: true, canUpdate: true, canDelete: true, rowScoped: false })
  })

  it('honours the hints the backend sends', () => {
    const caps = screenCapabilities(schema({ can_create: false, can_update: true, can_delete: false }))
    expect(caps.canCreate).toBe(false)
    expect(caps.canUpdate).toBe(true)
    expect(caps.canDelete).toBe(false)
  })

  it('keeps read_only winning over any hint', () => {
    const caps = screenCapabilities(schema({ read_only: true, can_create: true, can_update: true, can_delete: true }))
    expect(caps.canCreate).toBe(false)
    expect(caps.canUpdate).toBe(false)
    expect(caps.canDelete).toBe(false)
  })

  it('reports a row-scoped grant so the screen can say the list is not the whole table', () => {
    expect(screenCapabilities(schema({ row_scope: ['list', 'update'] })).rowScoped).toBe(true)
    expect(screenCapabilities(schema({ row_scope: [] })).rowScoped).toBe(false)
  })
})

describe('isFieldEditable', () => {
  it('is true for a field with no hint', () => {
    expect(isFieldEditable(field({}))).toBe(true)
  })

  it('is false only when the backend says the field is not writable', () => {
    expect(isFieldEditable(field({ can_edit: false }))).toBe(false)
    expect(isFieldEditable(field({ can_edit: true }))).toBe(true)
  })
})
