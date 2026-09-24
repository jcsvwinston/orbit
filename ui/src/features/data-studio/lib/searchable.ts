import type { ModelSchema } from '@/types'

// isSearchable reports whether ?search= has a column to look in: a field the
// backend marks searchable (Nucleus: admin:"search", ModelConfig.SearchFields
// or the Field settings editor; Quark: every string column) that is not
// excluded from the panel. The list endpoint answers 400 to a search on a
// model without one, so the grid disables the box instead of sending it.
export function isSearchable(schema: ModelSchema): boolean {
  return schema.fields.some((f) => f.is_search && !f.is_excluded)
}
