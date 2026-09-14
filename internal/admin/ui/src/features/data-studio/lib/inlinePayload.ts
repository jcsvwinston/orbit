// The shape the backend takes for a child collection.
//
// Three rules, and each of them is a decision rather than a detail: a row
// with an id is an EDIT, one without is an INSERT, and a row that should go
// says so with `_delete` — deletion is never implied by a row being absent,
// because a form that loaded two of the five lines would otherwise remove
// the three it never showed.
export interface InlineRowInput {
  id: string
  values: { [column: string]: string }
  deleted: boolean
}

export function inlinePayload(rows: InlineRowInput[]): Array<{ [key: string]: unknown }> {
  return rows.map((row) => {
    const child: { [key: string]: unknown } = { ...row.values }
    if (row.id) child.id = row.id
    if (row.deleted) child._delete = true
    return child
  })
}
