import { describe, expect, it } from 'vitest'
import type { ModelSchema, SchemaField } from '@/types'
import { isSearchable, searchUnavailableReason } from './searchable'

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

  it('takes the schema\'s answer when it gives one', () => {
    // A searchable field kept from the operator is not in their schema, and
    // the search would still look in it: the server says no.
    const hidden = { ...schema([field({ column: 'label', is_search: true })]), searchable: false }
    expect(isSearchable(hidden)).toBe(false)
    expect(searchUnavailableReason(hidden)).toMatch(/fields you may not read/)
    expect(isSearchable({ ...schema([field({ column: 'label', is_search: true })]), searchable: true })).toBe(true)
  })

  it('says a model with nothing to search in has nothing', () => {
    expect(searchUnavailableReason(schema([field({ column: 'qty' })]))).toMatch(/Enable is_search/)
  })
})
