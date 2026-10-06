// The shape the backend takes for a child collection.
//
// Three rules, and each of them is a decision rather than a detail: a row
// with an id is an EDIT, one without is an INSERT, and a row that should go
// says so with `_delete` — deletion is never implied by a row being absent,
// because a form that loaded two of the five lines would otherwise remove
// the three it never showed.
//
// And because absence never deletes, a saved row nobody touched is not sent
// at all: the server asks every row it is sent for its verb (an id is an
// update), so an operator who may add a line and not edit one would be
// refused the line for the rows they only looked at.
export interface InlineRowInput {
  id: string
  values: { [column: string]: string }
  deleted: boolean
  // changed is false for a saved row whose values are still the ones it
  // was loaded with. Absent, the row is sent as it always was.
  changed?: boolean
}

export function inlinePayload(rows: InlineRowInput[]): Array<{ [key: string]: unknown }> {
  return rows.filter((row) => !row.id || row.deleted || row.changed !== false).map((row) => {
    const child: { [key: string]: unknown } = { ...row.values }
    if (row.id) child.id = row.id
    if (row.deleted) child._delete = true
    return child
  })
}
