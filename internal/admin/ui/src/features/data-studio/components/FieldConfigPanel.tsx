import { useState, useEffect } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from '@/components/ui/dialog'
import type { ModelSchema, SchemaField } from '@/types'
import * as api from '@/services/api'
import { Loader2, Save, RotateCcw } from 'lucide-react'

interface Props {
  open: boolean
  onClose: () => void
  schema: ModelSchema
  onSaved: () => void
}

interface FieldState {
  is_list: boolean
  is_search: boolean
  is_filter: boolean
  is_excluded: boolean
  is_readonly: boolean
  label: string
  html_type: string
}

const HTML_TYPE_OPTIONS = [
  'text', 'number', 'email', 'password', 'url', 'tel',
  'textarea', 'checkbox', 'datetime-local', 'color',
]

function fieldToState(f: SchemaField): FieldState {
  return {
    is_list: f.is_list,
    is_search: f.is_search,
    is_filter: f.is_filter,
    is_excluded: f.is_excluded,
    is_readonly: f.is_readonly,
    label: f.label,
    html_type: f.html_type || 'text',
  }
}

function hasChanges(original: FieldState, current: FieldState): boolean {
  return (
    original.is_list !== current.is_list ||
    original.is_search !== current.is_search ||
    original.is_filter !== current.is_filter ||
    original.is_excluded !== current.is_excluded ||
    original.is_readonly !== current.is_readonly ||
    original.label !== current.label ||
    original.html_type !== current.html_type
  )
}

export default function FieldConfigPanel({ open, onClose, schema, onSaved }: Props) {
  const [fields, setFields] = useState<Record<string, FieldState>>({})
  const [original, setOriginal] = useState<Record<string, FieldState>>({})
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    const state: Record<string, FieldState> = {}
    const orig: Record<string, FieldState> = {}
    for (const f of schema.fields) {
      state[f.name] = fieldToState(f)
      orig[f.name] = fieldToState(f)
    }
    setFields(state)
    setOriginal(orig)
    setError(null)
  }, [open, schema])

  const toggleField = (fieldName: string, prop: keyof FieldState) => {
    setFields((prev) => ({
      ...prev,
      [fieldName]: {
        ...prev[fieldName],
        [prop]: !prev[fieldName][prop],
      },
    }))
  }

  const updateFieldProp = (fieldName: string, prop: keyof FieldState, value: string) => {
    setFields((prev) => ({
      ...prev,
      [fieldName]: {
        ...prev[fieldName],
        [prop]: value,
      },
    }))
  }

  const resetAll = () => {
    setFields({ ...original })
  }

  const changedFields = Object.keys(fields).filter(
    (name) => original[name] && hasChanges(original[name], fields[name]),
  )

  const handleSave = async () => {
    if (changedFields.length === 0) return
    setSaving(true)
    setError(null)

    try {
      const updates: { [name: string]: api.FieldMetaUpdate } = {}
      for (const name of changedFields) {
        const cur = fields[name]
        const orig = original[name]
        const upd: api.FieldMetaUpdate = {}
        if (cur.is_list !== orig.is_list) upd.is_list = cur.is_list
        if (cur.is_search !== orig.is_search) upd.is_search = cur.is_search
        if (cur.is_filter !== orig.is_filter) upd.is_filter = cur.is_filter
        if (cur.is_excluded !== orig.is_excluded) upd.is_excluded = cur.is_excluded
        if (cur.is_readonly !== orig.is_readonly) upd.is_readonly = cur.is_readonly
        if (cur.label !== orig.label) upd.label = cur.label
        if (cur.html_type !== orig.html_type) upd.html_type = cur.html_type
        updates[name] = upd
      }

      await api.updateFieldsMeta(schema.name, updates)
      onSaved()
      onClose()
    } catch (err: any) {
      setError(err.message || 'Failed to update field configuration')
    } finally {
      setSaving(false)
    }
  }

  const editableSchemaFields = schema.fields.filter((f) => !f.is_pk)
  const pkFields = schema.fields.filter((f) => f.is_pk)

  return (
    <Dialog open={open} onOpenChange={(val) => !val && onClose()}>
      <DialogContent className="max-w-4xl max-h-[85vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle>Field Configuration — {schema.name}</DialogTitle>
          <DialogDescription>
            Configure which fields appear in lists, searches, and filters. Changes take effect immediately.
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-auto">
          <table className="w-full text-sm">
            <thead className="sticky top-0 bg-background z-10">
              <tr className="border-b">
                <th className="text-left py-2 px-2 font-medium text-muted-foreground">Field</th>
                <th className="text-left py-2 px-2 font-medium text-muted-foreground">Column</th>
                <th className="text-left py-2 px-2 font-medium text-muted-foreground w-36">Label</th>
                <th className="text-left py-2 px-2 font-medium text-muted-foreground w-32">HTML Type</th>
                <th className="text-center py-2 px-1 font-medium text-muted-foreground" title="Show in list view">List</th>
                <th className="text-center py-2 px-1 font-medium text-muted-foreground" title="Include in search">Search</th>
                <th className="text-center py-2 px-1 font-medium text-muted-foreground" title="Show as filter">Filter</th>
                <th className="text-center py-2 px-1 font-medium text-muted-foreground" title="Read-only field">RO</th>
                <th className="text-center py-2 px-1 font-medium text-muted-foreground" title="Exclude from admin">Excl.</th>
              </tr>
            </thead>
            <tbody>
              {/* PK fields (display only) */}
              {pkFields.map((f) => (
                <tr key={f.name} className="border-b bg-muted/30">
                  <td className="py-1.5 px-2 font-mono text-xs text-muted-foreground">{f.name}</td>
                  <td className="py-1.5 px-2 font-mono text-xs text-muted-foreground">{f.column}</td>
                  <td className="py-1.5 px-2 text-xs text-muted-foreground">{f.label}</td>
                  <td className="py-1.5 px-2 text-xs text-muted-foreground">{f.html_type}</td>
                  <td colSpan={5} className="py-1.5 px-2 text-center text-[10px] text-muted-foreground italic">
                    Primary Key
                  </td>
                </tr>
              ))}

              {/* Editable fields */}
              {editableSchemaFields.map((f) => {
                const state = fields[f.name]
                if (!state) return null
                const changed = original[f.name] && hasChanges(original[f.name], state)
                return (
                  <tr key={f.name} className={`border-b ${changed ? 'bg-yellow-50 dark:bg-yellow-950/20' : ''}`}>
                    <td className="py-1.5 px-2 font-mono text-xs">{f.name}</td>
                    <td className="py-1.5 px-2 font-mono text-xs text-muted-foreground">{f.column}</td>
                    <td className="py-1.5 px-2">
                      <Input
                        value={state.label}
                        onChange={(e) => updateFieldProp(f.name, 'label', e.target.value)}
                        className="h-7 text-xs"
                      />
                    </td>
                    <td className="py-1.5 px-2">
                      <select
                        value={state.html_type}
                        onChange={(e) => updateFieldProp(f.name, 'html_type', e.target.value)}
                        className="h-7 w-full rounded-md border border-input bg-background px-1.5 text-xs"
                      >
                        {HTML_TYPE_OPTIONS.map((t) => (
                          <option key={t} value={t}>{t}</option>
                        ))}
                      </select>
                    </td>
                    <td className="text-center py-1.5 px-1">
                      <input
                        type="checkbox"
                        checked={state.is_list}
                        onChange={() => toggleField(f.name, 'is_list')}
                        className="h-4 w-4 rounded border-input cursor-pointer"
                      />
                    </td>
                    <td className="text-center py-1.5 px-1">
                      <input
                        type="checkbox"
                        checked={state.is_search}
                        onChange={() => toggleField(f.name, 'is_search')}
                        className="h-4 w-4 rounded border-input cursor-pointer"
                      />
                    </td>
                    <td className="text-center py-1.5 px-1">
                      <input
                        type="checkbox"
                        checked={state.is_filter}
                        onChange={() => toggleField(f.name, 'is_filter')}
                        className="h-4 w-4 rounded border-input cursor-pointer"
                      />
                    </td>
                    <td className="text-center py-1.5 px-1">
                      <input
                        type="checkbox"
                        checked={state.is_readonly}
                        onChange={() => toggleField(f.name, 'is_readonly')}
                        className="h-4 w-4 rounded border-input cursor-pointer"
                      />
                    </td>
                    <td className="text-center py-1.5 px-1">
                      <input
                        type="checkbox"
                        checked={state.is_excluded}
                        onChange={() => toggleField(f.name, 'is_excluded')}
                        className="h-4 w-4 rounded border-input cursor-pointer"
                      />
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>

        {error && (
          <div className="rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2 text-sm text-destructive">
            {error}
          </div>
        )}

        <DialogFooter className="flex items-center justify-between gap-2 pt-2 border-t">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            {changedFields.length > 0 ? (
              <span>{changedFields.length} field(s) modified</span>
            ) : (
              <span>No changes</span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Button type="button" variant="ghost" size="sm" onClick={resetAll} disabled={saving || changedFields.length === 0}>
              <RotateCcw className="mr-1.5 h-3.5 w-3.5" />
              Reset
            </Button>
            <Button type="button" variant="outline" onClick={onClose} disabled={saving}>
              Cancel
            </Button>
            <Button type="button" onClick={handleSave} disabled={saving || changedFields.length === 0}>
              {saving ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Saving...
                </>
              ) : (
                <>
                  <Save className="mr-1.5 h-3.5 w-3.5" />
                  Save Changes
                </>
              )}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
