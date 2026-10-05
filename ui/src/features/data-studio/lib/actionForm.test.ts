import { describe, expect, it } from 'vitest'
import type { ActionFieldSpec } from '@/types'
import { actionInput, initialActionForm, strayFieldErrors } from './actionForm'

const fields: ActionFieldSpec[] = [
  { name: 'reason', label: 'Reason', type: 'text', required: true },
  { name: 'priority', label: 'Priority', type: 'number', required: false },
  { name: 'notify', label: 'Notify', type: 'boolean', required: false },
  { name: 'channel', label: 'Channel', type: 'select', required: true, options: [{ value: 'web', label: 'Website' }] },
  { name: 'publish_on', label: 'Publish on', type: 'date', required: false },
]

describe('initialActionForm', () => {
  it('opens with every input empty and every box unticked', () => {
    expect(initialActionForm(fields)).toEqual({
      reason: '', priority: '', notify: false, channel: '', publish_on: '',
    })
  })
})

describe('actionInput', () => {
  it('posts what was entered, typed', () => {
    expect(actionInput(fields, {
      reason: 'never arrived', priority: '2.5', notify: true, channel: 'web', publish_on: '2026-10-05',
    })).toEqual({ reason: 'never arrived', priority: 2.5, notify: true, channel: 'web', publish_on: '2026-10-05' })
  })

  // The server decides what is required: an empty form posts nothing for
  // the inputs, and the refusal names them.
  it('leaves empty inputs out and always sends the boxes', () => {
    expect(actionInput(fields, initialActionForm(fields))).toEqual({ notify: false })
    expect(actionInput(fields, { ...initialActionForm(fields), reason: '   ' })).toEqual({ notify: false })
  })

  it('sends a number it cannot read as the text, for the server to refuse', () => {
    expect(actionInput(fields, { ...initialActionForm(fields), priority: 'high' })).toEqual({ priority: 'high', notify: false })
  })
})

describe('strayFieldErrors', () => {
  it('keeps the problems no input of the form can show', () => {
    expect(strayFieldErrors(fields, { reason: 'is required', force: 'is not a field of this action' }))
      .toEqual(['force is not a field of this action'])
  })
})
