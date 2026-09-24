import { useCallback, useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import * as api from '@/services/api'
import type { InlineSpec, ModelSchema, SchemaField, Record as AppRecord } from '@/types'
import { fieldToInput } from '../lib/fieldValues'
import { Loader2, Plus, Trash2, Undo2 } from 'lucide-react'

// The children of a record, edited where the record is.
//
// The panel could always edit both models; what it could not do is edit them
// together, so "an album and its tracks" meant two screens and an id carried
// across by hand. The backend takes the children in the parent's own payload
// (internal/admin/inlines.go); this is the form half of it.
//
// Two rules it holds to, because both are easy to get wrong and expensive
// when they are: a row is deleted only when it is MARKED, never by being
// absent — and the key that points at the parent is not editable here, since
// the backend stamps it from the record being edited anyway.

export interface InlineRow {
  // id is empty for a row that does not exist yet.
  id: string
  values: { [column: string]: string }
  deleted: boolean
}

interface Props {
  spec: InlineSpec
  parentId: string | null
  onChange: (key: string, rows: InlineRow[]) => void
}

const inputClass = 'flex w-full rounded-md border border-input bg-background px-2 py-1 text-sm'

function editableChildFields(schema: ModelSchema, parentColumn: string): SchemaField[] {
  return schema.fields.filter((f) => {
    if (f.is_excluded || f.is_pk || f.is_readonly || f.is_tenant_field) return false
    if (f.can_edit === false) return false
    if (f.column === parentColumn || f.name === parentColumn) return false
    if (f.name === 'CreatedAt' || f.name === 'UpdatedAt') return false
    return true
  })
}

export default function InlineEditor({ spec, parentId, onChange }: Props) {
  const [schema, setSchema] = useState<ModelSchema | null>(null)
  const [rows, setRows] = useState<InlineRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const publish = useCallback((next: InlineRow[]) => {
    setRows(next)
    onChange(spec.key, next)
  }, [onChange, spec.key])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    const load = async () => {
      try {
        const childSchema = await api.getModelSchema(spec.model)
        if (cancelled) return
        setSchema(childSchema)

        if (!parentId) {
          setRows([])
          return
        }
        const page = await api.getRecordsPaginated(spec.model, {
          page: 1,
          page_size: 100,
          filters: { [spec.column]: parentId },
        })
        if (cancelled) return
        const existing = page.items.map((item: AppRecord) => ({
          id: String(item[childSchema.primary_key] ?? item.id ?? ''),
          values: Object.fromEntries(
            editableChildFields(childSchema, spec.column).map((f) => [f.column, fieldToInput(item, f)]),
          ),
          deleted: false,
        }))
        setRows(existing)
      } catch (err) {
        if (!cancelled) setError(api.errorMessage(err))
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => { cancelled = true }
  }, [spec.model, spec.column, parentId])

  if (loading) {
    return (
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="h-3.5 w-3.5 animate-spin" /> Loading {spec.label}…
      </div>
    )
  }
  if (error || !schema) {
    return <p className="text-xs text-muted-foreground">{spec.label} cannot be edited here: {error}</p>
  }

  const fields = editableChildFields(schema, spec.column)

  const updateCell = (index: number, column: string, value: string) => {
    publish(rows.map((row, i) => (i === index ? { ...row, values: { ...row.values, [column]: value } } : row)))
  }

  const toggleDeleted = (index: number) => {
    const row = rows[index]
    if (!row.id) {
      // A row that was never saved is simply dropped.
      publish(rows.filter((_, i) => i !== index))
      return
    }
    publish(rows.map((r, i) => (i === index ? { ...r, deleted: !r.deleted } : r)))
  }

  const addRow = () => {
    publish([...rows, { id: '', values: Object.fromEntries(fields.map((f) => [f.column, ''])), deleted: false }])
  }

  return (
    <div className="space-y-2 rounded-md border p-3">
      <div className="flex items-center justify-between">
        <Label className="text-sm font-medium">{spec.label}</Label>
        <Button type="button" size="sm" variant="outline" className="h-7 gap-1 text-xs" onClick={addRow}>
          <Plus className="h-3 w-3" /> Add
        </Button>
      </div>

      {rows.length === 0 ? (
        <p className="text-xs text-muted-foreground">No {spec.label.toLowerCase()} yet.</p>
      ) : (
        <div className="space-y-1.5">
          {rows.map((row, index) => (
            <div
              key={row.id || `new-${index}`}
              className={`flex flex-wrap items-end gap-2 ${row.deleted ? 'opacity-50' : ''}`}
            >
              {fields.map((f) => (
                <div key={f.column} className="flex-1 min-w-[8rem] space-y-1">
                  <span className="text-xs text-muted-foreground">{f.label}</span>
                  <Input
                    value={row.values[f.column] ?? ''}
                    disabled={row.deleted}
                    onChange={(e) => updateCell(index, f.column, e.target.value)}
                    className={inputClass}
                    aria-label={`${spec.label} ${index + 1} ${f.label}`}
                  />
                </div>
              ))}
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="h-8"
                onClick={() => toggleDeleted(index)}
                aria-label={row.deleted ? `Keep ${spec.label} ${index + 1}` : `Remove ${spec.label} ${index + 1}`}
              >
                {row.deleted ? <Undo2 className="h-3.5 w-3.5" /> : <Trash2 className="h-3.5 w-3.5" />}
              </Button>
            </div>
          ))}
        </div>
      )}
      {rows.some((r) => r.deleted) && (
        <p className="text-xs text-muted-foreground">
          Rows marked for removal are deleted when you save.
        </p>
      )}
    </div>
  )
}
