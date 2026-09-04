import { describe, expect, it } from 'vitest'
import type { SchemaField } from '@/types'
import { fieldToInput, formatCellValue, inputToPayload, isJsonField } from './fieldValues'

function field(overrides: Partial<SchemaField>): SchemaField {
  return {
    name: 'Meta',
    column: 'meta',
    label: 'Meta',
    type: 'map[string]interface {}',
    html_type: 'text',
    is_pk: false,
    is_required: false,
    is_readonly: false,
    is_list: true,
    is_search: false,
    is_filter: false,
    is_excluded: false,
    is_fk: false,
    is_tenant_field: false,
    ...overrides,
  }
}

describe('JSON fields', () => {
  it('round-trips an object through the editor without corrupting it', () => {
    const f = field({})
    const record = { meta: { tags: ['a', 'b'], nested: { n: 1 } } }

    const shown = fieldToInput(record, f)
    expect(shown).not.toContain('[object Object]')
    expect(JSON.parse(shown)).toEqual(record.meta)

    const saved = inputToPayload(f, shown)
    expect(saved.error).toBeUndefined()
    expect(saved.value).toEqual(record.meta)
  })

  it('reports a per-field error instead of saving unparseable JSON', () => {
    const f = field({})
    const result = inputToPayload(f, '{not json')
    expect(result.error).toMatch(/Invalid JSON/)
    expect(result.value).toBeUndefined()
  })

  it('detects JSON by html_type, Go type or runtime value', () => {
    expect(isJsonField(field({ type: 'string', html_type: 'json' }))).toBe(true)
    expect(isJsonField(field({ type: '[]string', html_type: 'text' }))).toBe(true)
    expect(isJsonField(field({ type: 'datatypes.JSON', html_type: 'text' }))).toBe(true)
    expect(isJsonField(field({ type: 'string', html_type: 'text' }), 'plain')).toBe(false)
    expect(isJsonField(field({ type: 'string', html_type: 'text' }), { a: 1 })).toBe(true)
  })

  it('renders compact JSON in the grid', () => {
    const f = field({})
    expect(formatCellValue(f, { a: 1, b: [2] })).toBe('{"a":1,"b":[2]}')
    expect(formatCellValue(f, null)).toBe('—')
  })
})
