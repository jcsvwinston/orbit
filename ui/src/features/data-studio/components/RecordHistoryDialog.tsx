import { useEffect, useState } from 'react'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { ErrorState } from '@/components/ui/error-state'
import * as api from '@/services/api'
import type { AuditLogPage } from '@/types'
import { Loader2 } from 'lucide-react'
import { changedFields, renderHistoryValue } from '../lib/recordHistory'

interface Props {
  open: boolean
  onClose: () => void
  modelName: string
  recordId: string | number | null
}

export default function RecordHistoryDialog({ open, onClose, modelName, recordId }: Props) {
  const [result, setResult] = useState<AuditLogPage | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(null)

  useEffect(() => {
    if (!open || recordId === null) return
    let cancelled = false
    setLoading(true)
    setError(null)
    api.getRecordHistory(modelName, recordId)
      .then((data) => { if (!cancelled) setResult(data) })
      .catch((err) => { if (!cancelled) setError(err) })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [open, modelName, recordId])

  const entries = result?.entries ?? []

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) onClose() }}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>History of {modelName} {recordId}</DialogTitle>
          <DialogDescription>
            {/* How far back this can possibly go, so a short history is read
                as what it is rather than as "nothing happened". */}
            {result?.persistent === false
              ? 'The audit trail is kept in memory, so this history starts when the process did.'
              : result?.retentionDays
                ? `From the audit trail, which is kept for ${result.retentionDays} day${result.retentionDays === 1 ? '' : 's'}.`
                : 'From the audit trail.'}
          </DialogDescription>
        </DialogHeader>

        {loading ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : error ? (
          <ErrorState error={error} title="Failed to load the history" />
        ) : entries.length === 0 ? (
          <p className="py-6 text-sm text-muted-foreground">
            No recorded change for this record.
          </p>
        ) : (
          <div className="max-h-[60vh] space-y-3 overflow-y-auto">
            {entries.map((entry) => {
              const changes = changedFields(entry)
              return (
                <div key={entry.id} className="rounded-md border p-3 space-y-2">
                  <div className="flex flex-wrap items-center gap-2 text-xs">
                    <Badge variant="outline">{entry.action}</Badge>
                    <span className="font-medium">{entry.username || entry.userId || 'unknown'}</span>
                    <span className="text-muted-foreground">{entry.timestamp}</span>
                  </div>
                  {changes.length === 0 ? (
                    <p className="text-xs text-muted-foreground">No field values recorded for this entry.</p>
                  ) : (
                    <table className="w-full text-xs">
                      <thead className="text-muted-foreground">
                        <tr>
                          <th className="text-left font-normal py-1">Field</th>
                          <th className="text-left font-normal py-1">Before</th>
                          <th className="text-left font-normal py-1">After</th>
                        </tr>
                      </thead>
                      <tbody>
                        {changes.map((c) => (
                          <tr key={c.field} className="border-t">
                            <td className="py-1 pr-2 font-mono">{c.field}</td>
                            <td className="py-1 pr-2 break-all text-muted-foreground">{renderHistoryValue(c.before)}</td>
                            <td className="py-1 break-all">{renderHistoryValue(c.after)}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
