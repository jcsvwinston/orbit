import type { AuditLog } from '@/types'

// What an operator came to see in a record's history: the fields whose value
// is not the same on both sides of the change, with what they said before and
// after. A create has no "before" and a delete has no "after"; both read
// correctly as one column being empty, which is why this compares the union
// of the keys instead of iterating one side.
export interface FieldChange {
  field: string
  before: unknown
  after: unknown
}

export function changedFields(entry: AuditLog): FieldChange[] {
  const before = (entry.oldValue ?? {}) as Record<string, unknown>
  const after = (entry.newValue ?? {}) as Record<string, unknown>
  const keys = new Set([...Object.keys(before), ...Object.keys(after)])
  const out: FieldChange[] = []
  for (const key of Array.from(keys).sort()) {
    // Compared as JSON so a nested value that is equal does not show up as a
    // change: an update that rewrote the whole row records every field, and
    // a diff that listed all of them would hide the one that moved.
    if (JSON.stringify(before[key]) === JSON.stringify(after[key])) continue
    out.push({ field: key, before: before[key], after: after[key] })
  }
  return out
}

export function renderHistoryValue(value: unknown): string {
  if (value === undefined || value === null || value === '') return '—'
  if (typeof value === 'string') return value
  return JSON.stringify(value)
}
