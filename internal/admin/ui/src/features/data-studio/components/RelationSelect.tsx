import { useEffect, useState } from 'react'
import * as api from '@/services/api'
import type { RelationOption } from '@/services/api'

interface Props {
  id: string
  modelName: string
  field: string
  value: string
  inputClass: string
  onChange: (value: string) => void
}

// The widget a foreign key deserves: what it may point at, by name.
//
// The lookup is the TARGET model's read permission, so an operator who may
// edit this record and not browse what it points at gets a 403 — and then the
// field falls back to the plain id input it always was, rather than to a
// select with nothing in it.
export default function RelationSelect({ id, modelName, field, value, inputClass, onChange }: Props) {
  const [options, setOptions] = useState<RelationOption[] | null>(null)
  const [truncated, setTruncated] = useState(false)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let cancelled = false
    api.getFieldOptions(modelName, field)
      .then((res) => {
        if (cancelled) return
        setOptions(res.options)
        setTruncated(res.truncated)
      })
      .catch(() => { if (!cancelled) setFailed(true) })
    return () => { cancelled = true }
  }, [modelName, field])

  if (failed) {
    return (
      <input
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={inputClass}
        placeholder="id"
      />
    )
  }

  // A value the list does not carry is kept as its own option, so opening a
  // record whose target is past the lookup's limit does not silently clear
  // the field on save.
  const known = (options ?? []).some((o) => o.value === value)

  return (
    <div className="space-y-1">
      <select
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={`${inputClass} h-10`}
        disabled={options === null}
      >
        <option value="">— None —</option>
        {value && !known && <option value={value}>{value}</option>}
        {(options ?? []).map((o) => (
          <option key={o.value} value={o.value}>{o.label}</option>
        ))}
      </select>
      {truncated && (
        <p className="text-xs text-muted-foreground">
          Showing the first matches only — more exist than are listed here.
        </p>
      )}
    </div>
  )
}
