import { describe, expect, it } from 'vitest'
import type { ModelSchema, SchemaField } from '@/types'
import { primaryKeyColumn, recordId, toApiId } from './recordIds'

const field = (over: Partial<SchemaField>): SchemaField => ({
  name: 'Name', column: 'name', label: 'Name', type: 'string', html_type: 'text',
  is_pk: false, is_required: false, is_readonly: false, is_list: true, is_search: false,
  is_filter: false, is_excluded: false, is_fk: false, is_tenant_field: false,
  ...over,
})

const schema = (fields: SchemaField[], primary_key = ''): ModelSchema => ({
  name: 'Doc', plural: 'Docs', table: 'docs', primary_key, icon: '', read_only: false,
  fields, foreign_keys: [], tenant_field: '',
})

describe('primaryKeyColumn', () => {
  it('prefers the declared primary key by Go name, then by column', () => {
    const fields = [field({ name: 'ID', column: 'id', is_pk: true }), field({ name: 'Code', column: 'code' })]
    expect(primaryKeyColumn(schema(fields, 'ID'))).toBe('id')
    expect(primaryKeyColumn(schema(fields, 'code'))).toBe('code')
  })

  it('falls back to the is_pk field, then to "id"', () => {
    expect(primaryKeyColumn(schema([field({ name: 'Key', column: 'key', is_pk: true })]))).toBe('key')
    expect(primaryKeyColumn(schema([field({})]))).toBe('id')
  })
})

describe('recordId', () => {
  it('reads integer and string keys, including UUIDs', () => {
    expect(recordId({ id: 7 }, 'id')).toBe(7)
    expect(recordId({ key: '0b1c2d3e-0000-4000-8000-000000000001' }, 'key')).toBe('0b1c2d3e-0000-4000-8000-000000000001')
  })

  it('falls back to "id" and refuses rows without a usable key', () => {
    expect(recordId({ id: 3 }, 'key')).toBe(3)
    expect(recordId({ key: null }, 'key')).toBeNull()
    expect(recordId({ key: { nested: true } }, 'key')).toBeNull()
  })
})

describe('toApiId', () => {
  it('sends strings unchanged and numbers in decimal form', () => {
    expect(toApiId('abc')).toBe('abc')
    expect(toApiId(3)).toBe('3')
    expect(toApiId(0)).toBe('0')
  })
})
