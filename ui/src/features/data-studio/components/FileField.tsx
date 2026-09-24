import { useState } from 'react'
import { Button } from '@/components/ui/button'
import * as api from '@/services/api'
import { Loader2, Upload, X } from 'lucide-react'

interface Props {
  id: string
  modelName: string
  field: string
  value: string
  image: boolean
  inputClass: string
  onChange: (value: string) => void
}

// A file field holds a storage KEY, and the only honest way to produce one is
// to send the file: the panel uploads it to the application's own storage and
// writes back what the record should hold. The key stays visible and editable
// as text, because an operator who knows the key of an existing object should
// be able to point at it without uploading it again.
export default function FileField({ id, modelName, field, value, image, inputClass, onChange }: Props) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const upload = async (file: File | undefined) => {
    if (!file) return
    setBusy(true)
    setError(null)
    try {
      const uploaded = await api.uploadFieldFile(modelName, field, file)
      onChange(uploaded.key)
    } catch (err) {
      setError(api.errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <input
          id={id}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className={inputClass}
          placeholder={image ? 'image key' : 'file key'}
        />
        {value && (
          <Button type="button" variant="ghost" size="sm" onClick={() => onChange('')} aria-label="Clear the file">
            <X className="h-4 w-4" />
          </Button>
        )}
      </div>
      <label className="inline-flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
        {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
        <span>{busy ? 'Uploading…' : 'Upload a file'}</span>
        <input
          type="file"
          className="hidden"
          accept={image ? 'image/*' : undefined}
          disabled={busy}
          onChange={(e) => { void upload(e.target.files?.[0]); e.target.value = '' }}
        />
      </label>
      {error && <p className="text-xs text-destructive">{error}</p>}
    </div>
  )
}
