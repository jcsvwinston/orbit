import type { SchemaField, Record as AppRecord } from '@/types'
import { isoToLocalInput, localInputToISO } from '@/lib/datetime'

// A JSON-shaped field is one the operator marked as json, one whose Go type
// is a map / slice / json blob, or one whose current value is an object:
// those must round-trip through JSON.stringify / JSON.parse. String()-ing
// them yields "[object Object]" and, on save, persists that string.
export function isJsonField(field: SchemaField, value?: unknown): boolean {
  if (field.html_type === 'json') return true
  if (/^(map\[|\[\])/.test(field.type) && field.type !== '[]byte') return true
  if (/json/i.test(field.type)) return true
  return value !== null && typeof value === 'object'
}

export function readField(record: AppRecord, field: SchemaField): unknown {
  if (field.column in record) return record[field.column]
  if (field.name in record) return record[field.name]
  const lc = field.column.toLowerCase()
  for (const key of Object.keys(record)) {
    if (key.toLowerCase() === lc) return record[key]
  }
  return undefined
}

// fieldToInput renders a stored value as the string an editor shows.
export function fieldToInput(record: AppRecord | null, field: SchemaField): string {
  if (!record) return ''
  const val = readField(record, field)
  if (val === null || val === undefined) return ''
  if (field.html_type === 'checkbox') return val ? 'true' : 'false'
  if (isJsonField(field, val)) {
    if (typeof val === 'string') return val
    return JSON.stringify(val, null, 2)
  }
  if (field.html_type === 'datetime-local' && typeof val === 'string') {
    return isoToLocalInput(val)
  }
  return String(val)
}

export interface PayloadValue {
  value?: unknown
  skip?: boolean
  error?: string
}

// inputToPayload converts what the editor holds back into what the API
// stores. A JSON field that does not parse is a per-field error, never a
// silently saved string.
export function inputToPayload(field: SchemaField, raw: string | undefined): PayloadValue {
  if (raw === undefined || raw === '') {
    if (field.html_type === 'checkbox') return { value: false }
    return { skip: true }
  }
  if (field.html_type === 'number') {
    const n = field.type.includes('int') ? parseInt(raw, 10) : parseFloat(raw)
    if (Number.isNaN(n)) return { error: 'Must be a number' }
    return { value: n }
  }
  if (field.html_type === 'checkbox') {
    return { value: raw === 'true' || raw === '1' }
  }
  if (field.html_type === 'datetime-local') {
    return { value: localInputToISO(raw) }
  }
  if (isJsonField(field)) {
    try {
      return { value: JSON.parse(raw) }
    } catch (err) {
      return { error: `Invalid JSON: ${err instanceof Error ? err.message : String(err)}` }
    }
  }
  return { value: raw }
}

// formatCellValue is the grid's one-line rendering of a value.
export function formatCellValue(field: SchemaField, value: unknown, maxLength = 80): string {
  if (value === null || value === undefined) return '—'
  if (field.html_type === 'datetime-local' || field.type === 'time.Time') {
    if (typeof value === 'string' && value.length > 0) {
      const d = new Date(value)
      return Number.isNaN(d.getTime()) ? value : d.toLocaleString()
    }
  }
  const s = typeof value === 'object' ? JSON.stringify(value) : String(value)
  return s.length > maxLength ? s.slice(0, maxLength) + '…' : s
}
