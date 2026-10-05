import type { ActionFieldSpec, ActionInputValues } from '@/types'

// The state of an action's form: what each input holds, as the input holds
// it. A checkbox holds a boolean; every other input holds the text it shows.
export type ActionFormState = { [field: string]: string | boolean }

// initialActionForm is the form as it opens: every box unticked and every
// input empty. Nothing is pre-chosen for the operator — a select that
// opened on its first option would post a choice nobody made.
export function initialActionForm(fields: ActionFieldSpec[]): ActionFormState {
  const state: ActionFormState = {}
  for (const field of fields) {
    state[field.name] = field.type === 'boolean' ? false : ''
  }
  return state
}

// actionInput turns the form into what is posted. An empty input is left
// out rather than sent as "", so "the operator entered nothing" and "the
// operator entered an empty string" are the same thing on the wire, and the
// server — which decides what is required — says so. A number goes as a
// number when it reads as one and as the text otherwise, so a value the
// browser let through and the server refuses is refused by the server,
// naming the field, instead of vanishing here.
export function actionInput(fields: ActionFieldSpec[], state: ActionFormState): ActionInputValues {
  const input: ActionInputValues = {}
  for (const field of fields) {
    const value = state[field.name]
    if (field.type === 'boolean') {
      input[field.name] = value === true
      continue
    }
    const text = typeof value === 'string' ? value : ''
    if (text.trim() === '') continue
    if (field.type === 'number') {
      const n = Number(text)
      input[field.name] = Number.isFinite(n) ? n : text
      continue
    }
    input[field.name] = text
  }
  return input
}

// strayFieldErrors are the problems a refusal names for keys the form does
// not have — a field the server knows and this screen does not, which can
// only be shown as a message.
export function strayFieldErrors(fields: ActionFieldSpec[], errors: { [field: string]: string }): string[] {
  const known = new Set(fields.map((f) => f.name))
  return Object.entries(errors)
    .filter(([name]) => !known.has(name))
    .map(([name, problem]) => `${name} ${problem}`)
}
