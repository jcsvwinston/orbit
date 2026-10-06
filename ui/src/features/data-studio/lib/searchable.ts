import type { ModelSchema } from '@/types'

// isSearchable reports whether ?search= has a column to look in: a field the
// backend marks searchable (Nucleus: admin:"search", ModelConfig.SearchFields
// or the Field settings editor; Quark: every string column) that is not
// excluded from the panel. The list endpoint answers 400 to a search on a
// model without one, so the grid disables the box instead of sending it.
//
// The schema's own answer wins when it gives one: a search also looks in the
// searchable fields kept from this operator, which the schema does not list,
// and the server refuses a search that would reach one.
export function isSearchable(schema: ModelSchema): boolean {
  if (typeof schema.searchable === 'boolean') return schema.searchable
  return hasSearchableField(schema)
}

// searchUnavailableReason is what the disabled box says when isSearchable is
// false: the model has nothing to search in, or the search would also look in
// fields this operator may not read.
export function searchUnavailableReason(schema: ModelSchema): string {
  if (hasSearchableField(schema)) {
    return 'Search would also look in fields you may not read, so it is not available to you on this model.'
  }
  return 'No field of this model is searchable. Enable is_search in Field settings.'
}

function hasSearchableField(schema: ModelSchema): boolean {
  return schema.fields.some((f) => f.is_search && !f.is_excluded)
}
