import { useState, useEffect } from 'react'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from '@/components/ui/dialog'
import type { SchemaField, ModelSchema, Record as AppRecord } from '@/types'
import { errorMessage } from '@/services/api'
import { fieldToInput, inputToPayload, isJsonField, readField } from '../lib/fieldValues'
import { Loader2 } from 'lucide-react'

interface Props {
  open: boolean
  onClose: () => void
  schema: ModelSchema
  record: AppRecord | null
  onSave: (data: AppRecord) => Promise<void>
}

function editableFields(schema: ModelSchema, isEdit: boolean): SchemaField[] {
  return schema.fields.filter((f) => {
    if (f.is_excluded) return false
    if (f.is_pk) return false
    if (f.is_readonly) return false
    if (f.is_tenant_field) return false
    if (isEdit && f.name === 'CreatedAt') return false
    return true
  })
}

function displayFields(schema: ModelSchema): SchemaField[] {
  return schema.fields.filter((f) => {
    if (f.is_excluded) return false
    if (f.is_pk && f.is_readonly) return true
    if (f.is_readonly) return true
    return false
  })
}

const inputClass = 'flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'

function FieldInput({
  field,
  id,
  value,
  json,
  invalid,
  onChange,
}: {
  field: SchemaField
  id: string
  value: string
  json: boolean
  invalid: boolean
  onChange: (val: string) => void
}) {
  const htmlType = field.html_type || 'text'
  const errorId = invalid ? `${id}-error` : undefined

  if (field.choices && field.choices.length > 0) {
    return (
      <select
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={`${inputClass} h-10`}
      >
        <option value="">— Select —</option>
        {field.choices.map((c) => (
          <option key={c.value} value={c.value}>
            {c.label || c.value}
          </option>
        ))}
      </select>
    )
  }

  if (json) {
    return (
      <textarea
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        rows={6}
        spellCheck={false}
        aria-invalid={invalid || undefined}
        aria-describedby={errorId}
        className={`${inputClass} font-mono text-xs resize-y min-h-[120px] ${invalid ? 'border-destructive' : ''}`}
      />
    )
  }

  if (htmlType === 'textarea') {
    return (
      <textarea
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        rows={4}
        className={`${inputClass} resize-y min-h-[80px]`}
      />
    )
  }

  if (htmlType === 'checkbox') {
    const checked = value === 'true' || value === '1'
    return (
      <div className="flex items-center gap-2 h-10">
        <input
          id={id}
          type="checkbox"
          checked={checked}
          onChange={(e) => onChange(e.target.checked ? 'true' : 'false')}
          className="h-4 w-4 rounded border-input"
        />
        <span className="text-sm text-muted-foreground">{checked ? 'Yes' : 'No'}</span>
      </div>
    )
  }

  return (
    <Input
      id={id}
      type={htmlType === 'number' ? 'number' : htmlType === 'email' ? 'email' : htmlType === 'password' ? 'password' : htmlType === 'datetime-local' ? 'datetime-local' : 'text'}
      step={htmlType === 'number' && !field.type.includes('int') ? 'any' : undefined}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      required={field.is_required}
      placeholder={field.label}
      aria-invalid={invalid || undefined}
      aria-describedby={errorId}
    />
  )
}

export default function RecordForm({ open, onClose, schema, record, onSave }: Props) {
  const isEdit = record !== null
  const fields = editableFields(schema, isEdit)
  const readonlyFields = displayFields(schema)
  const [formData, setFormData] = useState<{ [column: string]: string }>({})
  const [jsonColumns, setJsonColumns] = useState<Set<string>>(new Set())
  const [fieldErrors, setFieldErrors] = useState<{ [column: string]: string }>({})
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    const data: { [column: string]: string } = {}
    const json = new Set<string>()
    for (const f of editableFields(schema, record !== null)) {
      data[f.column] = fieldToInput(record, f)
      if (isJsonField(f, record ? readField(record, f) : undefined)) json.add(f.column)
    }
    setFormData(data)
    setJsonColumns(json)
    setFieldErrors({})
    setError(null)
  }, [open, record, schema])

  const updateField = (column: string, value: string) => {
    setFormData((prev) => ({ ...prev, [column]: value }))
    if (fieldErrors[column]) {
      setFieldErrors((prev) => {
        const next = { ...prev }
        delete next[column]
        return next
      })
    }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)

    const payload: AppRecord = {}
    const errors: { [column: string]: string } = {}
    for (const f of fields) {
      const field = jsonColumns.has(f.column) ? { ...f, html_type: 'json' } : f
      const result = inputToPayload(field, formData[f.column])
      if (result.error) {
        errors[f.column] = result.error
        continue
      }
      if (result.skip) continue
      payload[f.column] = result.value
    }
    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors)
      setError('Fix the highlighted fields before saving.')
      return
    }

    setSaving(true)
    try {
      await onSave(payload)
      onClose()
    } catch (err) {
      setError(errorMessage(err, 'Failed to save record'))
    } finally {
      setSaving(false)
    }
  }

  if (!open) return null

  return (
    <Dialog open={true} onOpenChange={(val: boolean) => !val && onClose()}>
      <DialogContent className="max-w-xl max-h-[80vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{isEdit ? 'Edit' : 'Create'} {schema.name}</DialogTitle>
          <DialogDescription>
            {isEdit ? 'Update the record details below.' : 'Fill in the details to create a new record.'}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4 py-2" noValidate>
          {isEdit && readonlyFields.length > 0 && (
            <div className="space-y-2 pb-3 border-b">
              {readonlyFields.map((f) => (
                <div key={f.name} className="flex items-start gap-2 text-sm">
                  <span className="text-muted-foreground w-24 flex-shrink-0">{f.label}:</span>
                  <span className="font-mono text-xs whitespace-pre-wrap break-all">{fieldToInput(record, f) || '—'}</span>
                </div>
              ))}
            </div>
          )}

          {fields.map((f) => {
            const id = `field-${f.column}`
            const json = jsonColumns.has(f.column)
            const fieldError = fieldErrors[f.column]
            return (
              <div key={f.column} className="space-y-1.5">
                <Label htmlFor={id} className="flex items-center gap-1.5">
                  {f.label}
                  {f.is_required && <span className="text-destructive text-xs" aria-hidden="true">*</span>}
                  {json && <span className="text-xs text-muted-foreground">JSON</span>}
                  {f.is_fk && f.fk_model && (
                    <span className="text-xs text-muted-foreground">FK → {f.fk_model}</span>
                  )}
                </Label>
                <FieldInput
                  field={f}
                  id={id}
                  value={formData[f.column] ?? ''}
                  json={json}
                  invalid={Boolean(fieldError)}
                  onChange={(val) => updateField(f.column, val)}
                />
                {fieldError && (
                  <p id={`${id}-error`} className="text-xs text-destructive">{fieldError}</p>
                )}
              </div>
            )
          })}

          {error && (
            <div role="alert" className="rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose} disabled={saving}>
              Cancel
            </Button>
            <Button type="submit" disabled={saving || schema.read_only}>
              {saving ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Saving...
                </>
              ) : isEdit ? (
                'Update'
              ) : (
                'Create'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
