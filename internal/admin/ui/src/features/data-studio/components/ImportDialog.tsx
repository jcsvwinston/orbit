import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from '@/components/ui/dialog'
import { useToast } from '@/components/ui/use-toast'
import * as api from '@/services/api'
import { FileText, Loader2, Upload, CheckCircle2, AlertTriangle } from 'lucide-react'

interface Props {
  open: boolean
  onClose: () => void
  modelName: string
  onImported: () => void
}

type Step = 'pick' | 'validating' | 'validated' | 'importing'

// ImportDialog drives the backend's three-step import: upload the file,
// validate it against the model (a dry run that reports per-row errors and
// whether the import may proceed), then execute. The success toast only
// fires after execute returns, with the counts it reports.
export default function ImportDialog({ open, onClose, modelName, onImported }: Props) {
  const { toast } = useToast()
  const [file, setFile] = useState<File | null>(null)
  const [step, setStep] = useState<Step>('pick')
  const [upload, setUpload] = useState<api.ImportUpload | null>(null)
  const [validation, setValidation] = useState<api.ImportValidation | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setFile(null)
    setStep('pick')
    setUpload(null)
    setValidation(null)
    setError(null)
  }, [open, modelName])

  const busy = step === 'validating' || step === 'importing'

  const handleValidate = async () => {
    if (!file) return
    setStep('validating')
    setError(null)
    try {
      const uploaded = await api.uploadImportFile(file)
      setUpload(uploaded)
      const result = await api.validateImport(uploaded.key, { model: modelName, format: uploaded.format })
      setValidation(result)
      setStep('validated')
    } catch (err) {
      setError(api.errorMessage(err))
      setStep('pick')
    }
  }

  const handleExecute = async () => {
    if (!upload || !validation?.can_proceed) return
    setStep('importing')
    setError(null)
    try {
      const report = await api.executeImport(upload.key, { model: modelName, format: upload.format })
      const parts = [`${report.imported} imported`]
      if (report.updated > 0) parts.push(`${report.updated} updated`)
      if (report.skipped > 0) parts.push(`${report.skipped} skipped`)
      if (report.failed > 0) parts.push(`${report.failed} failed`)
      toast({
        variant: report.failed > 0 ? 'destructive' : 'default',
        title: report.failed > 0 ? 'Import finished with errors' : 'Import complete',
        description: `${modelName}: ${parts.join(', ')} of ${report.total} rows`,
      })
      onImported()
      onClose()
    } catch (err) {
      setError(api.errorMessage(err))
      setStep('validated')
    }
  }

  const pickFile = (next: File | null) => {
    setFile(next)
    setUpload(null)
    setValidation(null)
    setError(null)
    setStep('pick')
  }

  if (!open) return null

  return (
    <Dialog open={true} onOpenChange={(val: boolean) => !val && !busy && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Import into {modelName}</DialogTitle>
          <DialogDescription>
            Upload a CSV or JSON file. Rows are validated first; nothing is written until you confirm.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <div className="space-y-1">
            <label htmlFor="import-file" className="text-xs text-muted-foreground">Import file</label>
            <label className="flex items-center gap-1.5 px-3 py-2 rounded border border-dashed text-sm cursor-pointer hover:bg-muted">
              <FileText className="h-4 w-4" aria-hidden="true" />
              {file ? `${file.name} (${(file.size / 1024).toFixed(1)} KB)` : 'Choose a .csv or .json file…'}
              <input
                id="import-file"
                type="file"
                accept=".csv,.json,text/csv,application/json"
                disabled={busy}
                onChange={(e) => pickFile(e.target.files?.[0] ?? null)}
                className="sr-only"
              />
            </label>
          </div>

          {validation && (
            <div className="rounded-md border px-3 py-2 text-sm space-y-2" aria-live="polite">
              <div className="flex items-center gap-2">
                {validation.can_proceed ? (
                  <CheckCircle2 className="h-4 w-4 text-green-700 dark:text-green-400" aria-hidden="true" />
                ) : (
                  <AlertTriangle className="h-4 w-4 text-red-700 dark:text-red-400" aria-hidden="true" />
                )}
                <span>
                  {validation.valid_records} of {validation.total_records} row{validation.total_records === 1 ? '' : 's'} valid
                  {validation.can_proceed ? ' — ready to import' : ' — fix the errors and upload again'}
                </span>
              </div>
              {validation.errors.length > 0 && (
                <ul className="max-h-40 overflow-y-auto space-y-1 text-xs font-mono text-destructive">
                  {validation.errors.slice(0, 50).map((e, i) => (
                    <li key={`${e.row}-${e.field ?? ''}-${i}`}>
                      row {e.row}{e.field ? ` · ${e.field}` : ''}: {e.message}
                    </li>
                  ))}
                  {validation.errors.length > 50 && (
                    <li>… and {validation.errors.length - 50} more</li>
                  )}
                </ul>
              )}
            </div>
          )}

          {error && (
            <div role="alert" className="rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          {(step === 'validated' || step === 'importing') && validation?.can_proceed ? (
            <Button type="button" onClick={handleExecute} disabled={busy} className="gap-1.5">
              {step === 'importing' ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
              Import {validation.valid_records} row{validation.valid_records === 1 ? '' : 's'}
            </Button>
          ) : (
            <Button type="button" onClick={handleValidate} disabled={busy || !file} className="gap-1.5">
              {step === 'validating' ? <Loader2 className="h-4 w-4 animate-spin" /> : <CheckCircle2 className="h-4 w-4" />}
              Validate
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
