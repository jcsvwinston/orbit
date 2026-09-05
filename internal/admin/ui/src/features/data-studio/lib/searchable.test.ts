import { describe, expect, it } from 'vitest'
import type { ModelSchema, SchemaField } from '@/types'
import { isSearchable } from './searchable'

const field = (over: Partial<SchemaField>): SchemaField => ({
  name: 'Name', column: 'name', label: 'Name', type: 'string', html_type: 'text',
  is_pk: false, is_required: false, is_readonly: false, is_list: true, is_search: false,
  is_filter: false, is_excluded: false, is_fk: false, is_tenant_field: false,
  ...over,
})

const schema = (fields: SchemaField[]): ModelSchema => ({
  name: 'Gadget', plural: 'Gadgets', table: 'gadgets', primary_key: 'ID', icon: '', read_only: false,
  fields, foreign_keys: [], tenant_field: '',
})

describe('isSearchable', () => {
  it('is false for a model with no is_search field', () => {
    expect(isSearchable(schema([field({ column: 'qty', type: 'int' })]))).toBe(false)
  })

  it('ignores excluded fields', () => {
    expect(isSearchable(schema([field({ column: 'secret', is_search: true, is_excluded: true })]))).toBe(false)
  })

  it('is true once a visible field is searchable', () => {
    expect(isSearchable(schema([field({ column: 'qty' }), field({ column: 'label', is_search: true })]))).toBe(true)
  })
})
