import { useState } from 'react'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from '@/components/ui/dialog'
import type { ActionFieldSpec, ActionInputValues, ModelActionSpec } from '@/types'
import { errorMessage, fieldErrors as fieldErrorsOf } from '@/services/api'
import { actionInput, initialActionForm, strayFieldErrors, type ActionFormState } from '../lib/actionForm'
import { Loader2 } from 'lucide-react'

interface Props {
  action: ModelActionSpec
  // selectedCount is how many rows the action runs over, said in the
  // dialog so the operator knows the subject before they fill anything in.
  selectedCount: number
  onCancel: () => void
  // onSubmit runs the action with what the form collected. It throws when
  // the server refuses, and the dialog puts the refusal on the fields.
  onSubmit: (input: ActionInputValues) => Promise<void>
}

const selectClass = 'flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'

// ActionFormDialog asks for what an action declared before it runs (EXT-01).
//
// The form does not decide what is valid: it posts what was entered and the
// server, which checks every call against the same declaration, answers
// with the field at fault. A check here would be a second copy of the rule
// that could disagree with the first, and a client that skipped it would
// reach the server anyway — so the browser's own constraint checking is off
// (noValidate) and the one answer the operator reads is the server's.
export default function ActionFormDialog({ action, selectedCount, onCancel, onSubmit }: Props) {
  const fields = action.fields ?? []
  const [values, setValues] = useState<ActionFormState>(() => initialActionForm(fields))
  const [errors, setErrors] = useState<{ [field: string]: string }>({})
  const [formError, setFormError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  const update = (name: string, value: string | boolean) => {
    setValues((prev) => ({ ...prev, [name]: value }))
    if (errors[name]) {
      setErrors((prev) => {
        const next = { ...prev }
        delete next[name]
        return next
      })
    }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setFormError(null)
    setSubmitting(true)
    try {
      await onSubmit(actionInput(fields, values))
    } catch (err) {
      const byField = fieldErrorsOf(err)
      if (Object.keys(byField).length > 0) {
        setErrors(byField)
        const stray = strayFieldErrors(fields, byField)
        setFormError(stray.length > 0 ? stray.join('; ') : 'Fix the highlighted fields.')
      } else {
        setFormError(errorMessage(err, `${action.label} failed`))
      }
    } finally {
      setSubmitting(false)
    }
  }

  const subject = action.requires_selection
    ? `${selectedCount} record${selectedCount === 1 ? '' : 's'} selected.`
    : ''
  const lead = action.confirm || action.description || ''

  return (
    <Dialog open={true} onOpenChange={(open: boolean) => !open && !submitting && onCancel()}>
      <DialogContent className="max-w-md max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{action.label}</DialogTitle>
          <DialogDescription>
            {[lead, subject].filter(Boolean).join(' ')}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4 py-2" noValidate>
          {fields.map((field) => (
            <ActionFieldInput
              key={field.name}
              id={`action-${action.name}-${field.name}`}
              field={field}
              value={values[field.name]}
              error={errors[field.name]}
              onChange={(value) => update(field.name, value)}
            />
          ))}

          {formError && (
            <div role="alert" className="rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2 text-sm text-destructive-text">
              {formError}
            </div>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={onCancel} disabled={submitting}>
              Cancel
            </Button>
            <Button type="submit" variant={action.destructive ? 'destructive' : 'default'} disabled={submitting}>
              {submitting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
              {action.label}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

// ActionFieldInput draws one declared field with its label, its help line
// and, after a refusal, the server's message — tied to the input with
// aria-describedby, so a screen reader reads the error where it applies.
function ActionFieldInput({
  id,
  field,
  value,
  error,
  onChange,
}: {
  id: string
  field: ActionFieldSpec
  value: string | boolean | undefined
  error?: string
  onChange: (value: string | boolean) => void
}) {
  const helpId = field.help ? `${id}-help` : undefined
  const errorId = error ? `${id}-error` : undefined
  const describedBy = [helpId, errorId].filter(Boolean).join(' ') || undefined
  const invalid = error ? true : undefined
  const text = typeof value === 'string' ? value : ''

  const label = (
    <>
      {field.label}
      {field.required && <span className="text-destructive-text text-xs" aria-hidden="true">*</span>}
    </>
  )

  let control: React.ReactNode
  if (field.type === 'boolean') {
    return (
      <div className="space-y-1.5">
        <div className="flex items-center gap-2">
          <input
            id={id}
            type="checkbox"
            checked={value === true}
            onChange={(e) => onChange(e.target.checked)}
            required={field.required}
            aria-invalid={invalid}
            aria-describedby={describedBy}
            className="h-4 w-4 rounded border-input"
          />
          <Label htmlFor={id} className="flex items-center gap-1.5">{label}</Label>
        </div>
        {field.help && <p id={helpId} className="text-xs text-muted-foreground">{field.help}</p>}
        {error && <p id={errorId} className="text-xs text-destructive-text">{field.label} {error}</p>}
      </div>
    )
  }
  if (field.type === 'select') {
    control = (
      <select
        id={id}
        value={text}
        onChange={(e) => onChange(e.target.value)}
        required={field.required}
        aria-invalid={invalid}
        aria-describedby={describedBy}
        className={`${selectClass} ${error ? 'border-destructive' : ''}`}
      >
        <option value="">— Select —</option>
        {(field.options ?? []).map((option) => (
          <option key={option.value} value={option.value}>{option.label}</option>
        ))}
      </select>
    )
  } else {
    control = (
      <Input
        id={id}
        type={field.type === 'number' ? 'number' : field.type === 'date' ? 'date' : 'text'}
        step={field.type === 'number' ? 'any' : undefined}
        value={text}
        onChange={(e) => onChange(e.target.value)}
        required={field.required}
        aria-invalid={invalid}
        aria-describedby={describedBy}
        className={error ? 'border-destructive' : undefined}
      />
    )
  }

  return (
    <div className="space-y-1.5">
      <Label htmlFor={id} className="flex items-center gap-1.5">{label}</Label>
      {control}
      {field.help && <p id={helpId} className="text-xs text-muted-foreground">{field.help}</p>}
      {error && <p id={errorId} className="text-xs text-destructive-text">{field.label} {error}</p>}
    </div>
  )
}
