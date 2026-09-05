import type { ModelSchema, Record as AppRecord } from '@/types'

// Primary keys are whatever the driver decoded: integer ids, UUID strings,
// composite-ish strings. The SPA never assumes a number: every id crosses
// the API as a string — the boundary type of the backend's datasource
// contract — and the backend narrows it to the model's key type.
export type RecordId = string | number

// primaryKeyColumn resolves the column that carries the record id: the
// schema's declared primary key (a Go field name), else the field flagged
// is_pk, else "id".
export function primaryKeyColumn(schema: ModelSchema): string {
  const declared = schema.primary_key
    ? schema.fields.find((f) => f.name === schema.primary_key || f.column === schema.primary_key)
    : undefined
  return declared?.column ?? schema.fields.find((f) => f.is_pk)?.column ?? 'id'
}

// recordId reads a row's key from the primary-key column, falling back to
// the conventional "id"; null when the row carries no usable key.
export function recordId(row: AppRecord, pkColumn: string): RecordId | null {
  const v = row[pkColumn] ?? row.id
  if (typeof v === 'number' || typeof v === 'string') return v
  return null
}

// toApiId renders an id for the API path or a bulk body: strings as they
// are, numbers in their canonical decimal form (3 → "3"), which is also how
// the backend canonicalises keys on its side.
export function toApiId(id: RecordId): string {
  return String(id)
}
