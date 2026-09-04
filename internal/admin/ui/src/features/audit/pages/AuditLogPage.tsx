import { useCallback, useEffect, useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ErrorState } from '@/components/ui/error-state'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import * as api from '@/services/api'
import type { AuditLogPage as AuditPage } from '@/types'
import { FileText, RefreshCw, Loader2, ChevronLeft, ChevronRight } from 'lucide-react'

const PAGE_SIZE = 50
const FILTER_DEBOUNCE_MS = 300

interface Filters {
  user_id: string
  model: string
  action: string
}

const emptyFilters: Filters = { user_id: '', model: '', action: '' }

function getActionColor(action: string): string {
  switch (action.toLowerCase()) {
    case 'create':
      return 'bg-green-500/10 text-green-700 border-green-700/20 dark:text-green-400 dark:border-green-400/20'
    case 'update':
      return 'bg-blue-500/10 text-blue-700 border-blue-700/20 dark:text-blue-400 dark:border-blue-400/20'
    case 'delete':
      return 'bg-red-500/10 text-red-700 border-red-700/20 dark:text-red-400 dark:border-red-400/20'
    case 'login':
      return 'bg-purple-500/10 text-purple-700 border-purple-700/20 dark:text-purple-400 dark:border-purple-400/20'
    case 'logout':
      return 'bg-yellow-500/10 text-yellow-700 border-yellow-700/20 dark:text-yellow-400 dark:border-yellow-400/20'
    default:
      return 'bg-gray-500/10 text-gray-700 border-gray-700/20 dark:text-gray-400 dark:border-gray-400/20'
  }
}

export default function AuditLogPage() {
  const [result, setResult] = useState<AuditPage | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)
  const [filterInput, setFilterInput] = useState<Filters>(emptyFilters)
  const [filters, setFilters] = useState<Filters>(emptyFilters)
  const [page, setPage] = useState(1)

  // Debounce the text filters into the query the backend actually
  // understands (user_id / model / action); typing resets to page 1.
  useEffect(() => {
    const timeout = setTimeout(() => {
      setFilters(filterInput)
      setPage(1)
    }, FILTER_DEBOUNCE_MS)
    return () => clearTimeout(timeout)
  }, [filterInput])

  const fetchLogs = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const data = await api.getAuditLogs({
        user_id: filters.user_id.trim() || undefined,
        model: filters.model.trim() || undefined,
        action: filters.action.trim() || undefined,
        page,
        page_size: PAGE_SIZE,
      })
      setResult(data)
    } catch (err) {
      setError(err)
    } finally {
      setLoading(false)
    }
  }, [filters, page])

  useEffect(() => {
    fetchLogs()
  }, [fetchLogs])

  const entries = result?.entries ?? []
  const total = result?.total ?? 0
  const totalPages = result?.totalPages ?? 1
  const hasActiveFilter = Object.values(filters).some((v) => v.trim())

  const updateFilter = (key: keyof Filters, value: string) => {
    setFilterInput((prev) => ({ ...prev, [key]: value }))
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Audit Log</h1>
        <p className="text-muted-foreground">Track administrative actions</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <FileText className="h-5 w-5" />
              Audit Trail
            </div>
            <Button onClick={fetchLogs} disabled={loading} size="sm" aria-label="Refresh audit log">
              {loading ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <RefreshCw className="h-4 w-4" />
              )}
            </Button>
          </CardTitle>
          <CardDescription>
            {total} log entr{total === 1 ? 'y' : 'ies'}
            {hasActiveFilter && ' (filtered)'}
          </CardDescription>
          <div className="flex flex-wrap items-end gap-3 pt-2">
            <div className="space-y-1">
              <Label htmlFor="audit-user" className="text-xs text-muted-foreground">User ID</Label>
              <Input
                id="audit-user"
                value={filterInput.user_id}
                onChange={(e) => updateFilter('user_id', e.target.value)}
                placeholder="Any user"
                className="h-8 w-40 text-xs"
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="audit-model" className="text-xs text-muted-foreground">Model</Label>
              <Input
                id="audit-model"
                value={filterInput.model}
                onChange={(e) => updateFilter('model', e.target.value)}
                placeholder="Any model"
                className="h-8 w-40 text-xs"
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="audit-action" className="text-xs text-muted-foreground">Action</Label>
              <select
                id="audit-action"
                value={filterInput.action}
                onChange={(e) => updateFilter('action', e.target.value)}
                className="flex h-8 rounded-md border border-input bg-background px-2 text-xs ring-offset-background focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              >
                <option value="">Any action</option>
                <option value="create">create</option>
                <option value="update">update</option>
                <option value="delete">delete</option>
                <option value="login">login</option>
                <option value="logout">logout</option>
              </select>
            </div>
            {hasActiveFilter && (
              <Button variant="ghost" size="sm" className="h-8 text-xs" onClick={() => setFilterInput(emptyFilters)}>
                Clear filters
              </Button>
            )}
          </div>
        </CardHeader>
        <CardContent>
          {loading && !result ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-8 w-8 animate-spin" aria-label="Loading" />
            </div>
          ) : error ? (
            <ErrorState error={error} title="Failed to load the audit log" onRetry={fetchLogs} />
          ) : result && !result.enabled ? (
            <div className="text-center py-12">
              <FileText className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
              <p className="text-muted-foreground">Audit logging is not enabled</p>
              {result.reason && <p className="text-sm text-muted-foreground mt-2">{result.reason}</p>}
            </div>
          ) : entries.length === 0 ? (
            <div className="text-center py-12">
              <FileText className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
              <p className="text-muted-foreground">No audit logs found</p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Timestamp</TableHead>
                    <TableHead>User</TableHead>
                    <TableHead>Action</TableHead>
                    <TableHead>Model</TableHead>
                    <TableHead>Record</TableHead>
                    <TableHead className="hidden lg:table-cell">IP</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {entries.map((log) => (
                    <TableRow key={log.id}>
                      <TableCell className="text-sm whitespace-nowrap">
                        {new Date(log.timestamp).toLocaleString()}
                      </TableCell>
                      <TableCell>
                        <p className="font-medium">{log.username || '—'}</p>
                        {log.userId && log.userId !== log.username && (
                          <p className="text-xs text-muted-foreground font-mono">{log.userId}</p>
                        )}
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" className={getActionColor(log.action)}>
                          {log.action}
                        </Badge>
                      </TableCell>
                      <TableCell className="font-mono text-sm">{log.modelName || '—'}</TableCell>
                      <TableCell className="font-mono text-sm">{log.recordId || '—'}</TableCell>
                      <TableCell className="hidden lg:table-cell text-sm text-muted-foreground">{log.ip || '—'}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}

          {result && result.enabled && total > 0 && (
            <div className="flex items-center justify-between pt-4 text-sm text-muted-foreground">
              <span>
                Page {result.page} of {totalPages} · {total.toLocaleString()} total
              </span>
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  aria-label="Previous page"
                  disabled={loading || page <= 1}
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                >
                  <ChevronLeft className="h-4 w-4" />
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  aria-label="Next page"
                  disabled={loading || page >= totalPages}
                  onClick={() => setPage((p) => p + 1)}
                >
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
