// A saved view stores the query string the grid was showing, so what the grid
// holds in state has to survive a round trip through text.
//
// It is text on purpose: the panel does not parse a view against today's
// schema, so a filter on a column that was renamed fails on the list endpoint
// with that endpoint's message, instead of being silently dropped here.
export interface GridQuery {
  search: string
  filters: { [column: string]: string }
  orderBy: string
  pageSize?: number
}

const RESERVED = new Set(['search', 'order_by', 'page_size', 'page'])

export function gridQueryToString(query: GridQuery): string {
  const params = new URLSearchParams()
  if (query.search.trim()) params.set('search', query.search.trim())
  if (query.orderBy) params.set('order_by', query.orderBy)
  if (query.pageSize) params.set('page_size', String(query.pageSize))
  for (const [column, value] of Object.entries(query.filters)) {
    if (!value.trim() || RESERVED.has(column)) continue
    params.set(column, value.trim())
  }
  return params.toString()
}

export function gridQueryFromString(raw: string): GridQuery {
  const params = new URLSearchParams(raw.startsWith('?') ? raw.slice(1) : raw)
  const filters: { [column: string]: string } = {}
  let search = ''
  let orderBy = ''
  let pageSize: number | undefined
  for (const [key, value] of params.entries()) {
    if (key === 'search') { search = value; continue }
    if (key === 'order_by') { orderBy = value; continue }
    if (key === 'page_size') {
      const n = Number(value)
      if (Number.isFinite(n) && n > 0) pageSize = n
      continue
    }
    if (key === 'page') continue
    filters[key] = value
  }
  return { search, filters, orderBy, pageSize }
}
