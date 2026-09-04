import { useCallback, useEffect, useRef, useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import * as api from '@/services/api'
import type { LiveFeedEntry } from '@/types'
import { Network, Play, Pause, Trash, Database } from 'lucide-react'

const MAX_FEED_ENTRIES = 200
const RECONNECT_BASE_MS = 1000
const RECONNECT_MAX_MS = 30000

type LiveStatus = 'idle' | 'connecting' | 'live' | 'reconnecting'

function formatDuration(ms: number): string {
  if (ms < 1) return `${(ms * 1000).toFixed(0)}µs`
  if (ms < 1000) return `${ms.toFixed(2)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

function getStatusColor(status: number): string {
  if (status < 300) return 'bg-green-500/10 text-green-700 border-green-700/20 dark:text-green-400 dark:border-green-400/20'
  if (status < 400) return 'bg-yellow-500/10 text-yellow-700 border-yellow-700/20 dark:text-yellow-400 dark:border-yellow-400/20'
  return 'bg-red-500/10 text-red-700 border-red-700/20 dark:text-red-400 dark:border-red-400/20'
}

function getMethodColor(method: string): string {
  switch (method.toUpperCase()) {
    case 'GET': return 'bg-blue-500/10 text-blue-700 border-blue-700/20 dark:text-blue-400 dark:border-blue-400/20'
    case 'POST': return 'bg-green-500/10 text-green-700 border-green-700/20 dark:text-green-400 dark:border-green-400/20'
    case 'PUT': return 'bg-yellow-500/10 text-yellow-700 border-yellow-700/20 dark:text-yellow-400 dark:border-yellow-400/20'
    case 'DELETE': return 'bg-red-500/10 text-red-700 border-red-700/20 dark:text-red-400 dark:border-red-400/20'
    default: return 'bg-gray-500/10 text-gray-700 border-gray-700/20 dark:text-gray-400 dark:border-gray-400/20'
  }
}

function shortRequestId(requestId?: string): string | null {
  if (!requestId) return null
  return requestId.length > 8 ? requestId.slice(-8) : requestId
}

function statusLabel(status: LiveStatus, attempt: number): string {
  switch (status) {
    case 'live': return 'Live'
    case 'connecting': return 'Connecting…'
    case 'reconnecting': return `Reconnecting (attempt ${attempt})…`
    default: return ''
  }
}

export default function NetworkInspectorPage() {
  const [entries, setEntries] = useState<LiveFeedEntry[]>([])
  const [status, setStatus] = useState<LiveStatus>('idle')
  const [attempt, setAttempt] = useState(0)
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  // The operator's intent (Start/Stop) — read inside socket callbacks so a
  // close that the operator asked for does not trigger a reconnect.
  const activeRef = useRef(false)
  const attemptRef = useRef(0)

  const fetchSnapshot = async () => {
    try {
      const data = await api.getLiveFeed()
      setEntries(data.slice(0, MAX_FEED_ENTRIES))
    } catch (error) {
      console.error('Failed to fetch live feed:', error)
    }
  }

  const clearReconnectTimer = () => {
    if (reconnectTimer.current) {
      clearTimeout(reconnectTimer.current)
      reconnectTimer.current = null
    }
  }

  const connectWebSocket = useCallback(() => {
    const ws = api.getLiveWebSocket()
    if (!ws) {
      setStatus('idle')
      activeRef.current = false
      return
    }

    wsRef.current = ws
    setStatus(attemptRef.current > 0 ? 'reconnecting' : 'connecting')

    ws.onopen = () => {
      attemptRef.current = 0
      setAttempt(0)
      setStatus('live')
    }

    // The live stream publishes typed envelopes (see liveEventEnvelope on the
    // backend): `http.request` carries the request under `request`, `db.query`
    // carries the redacted SQL statement under `sql`. Anything else
    // (`session.activity`, future types) is ignored here.
    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        let entry: LiveFeedEntry | null = null
        if (data.type === 'http.request' && data.request) {
          entry = { kind: 'http', ...api.mapLiveRequest(data.request) }
        } else if (data.type === 'db.query' && data.sql) {
          entry = { kind: 'sql', ...api.mapLiveQuery(data.sql) }
        }
        if (entry) {
          const next = entry
          setEntries(prev => [next, ...prev.slice(0, MAX_FEED_ENTRIES - 1)])
        }
      } catch (error) {
        console.error('WebSocket message error:', error)
      }
    }

    ws.onerror = (error) => {
      console.error('WebSocket error:', error)
    }

    // A close the operator did not ask for (server restart, proxy idle
    // timeout, network blip) schedules a reconnect with exponential
    // backoff; Stop flips activeRef first so its close ends here.
    ws.onclose = () => {
      if (wsRef.current === ws) wsRef.current = null
      if (!activeRef.current) {
        setStatus('idle')
        return
      }
      attemptRef.current += 1
      setAttempt(attemptRef.current)
      setStatus('reconnecting')
      const delay = Math.min(RECONNECT_BASE_MS * 2 ** (attemptRef.current - 1), RECONNECT_MAX_MS)
      clearReconnectTimer()
      reconnectTimer.current = setTimeout(() => {
        reconnectTimer.current = null
        if (activeRef.current) connectWebSocket()
      }, delay)
    }
  }, [])

  const startMonitoring = () => {
    activeRef.current = true
    attemptRef.current = 0
    setAttempt(0)
    connectWebSocket()
  }

  const stopMonitoring = () => {
    activeRef.current = false
    clearReconnectTimer()
    attemptRef.current = 0
    setAttempt(0)
    setStatus('idle')
    wsRef.current?.close()
    wsRef.current = null
  }

  const clearEntries = () => {
    setEntries([])
  }

  useEffect(() => {
    fetchSnapshot()
    return () => {
      activeRef.current = false
      clearReconnectTimer()
      wsRef.current?.close()
      wsRef.current = null
    }
  }, [])

  const isMonitoring = status !== 'idle'
  const requestCount = entries.filter(e => e.kind === 'http').length
  const queryCount = entries.length - requestCount

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Network Inspector</h1>
          <p className="text-muted-foreground">Live HTTP and SQL traffic monitoring</p>
        </div>
        <div className="flex gap-2">
          {isMonitoring ? (
            <button
              type="button"
              onClick={stopMonitoring}
              className="flex items-center gap-2 px-3 py-2 rounded-md bg-destructive text-destructive-foreground hover:bg-destructive/90 transition-colors"
            >
              <Pause className="h-4 w-4" />
              Stop
            </button>
          ) : (
            <button
              type="button"
              onClick={startMonitoring}
              className="flex items-center gap-2 px-3 py-2 rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors"
            >
              <Play className="h-4 w-4" />
              Start Live
            </button>
          )}
          <button
            type="button"
            onClick={clearEntries}
            className="flex items-center gap-2 px-3 py-2 rounded-md border border-border hover:bg-accent transition-colors"
          >
            <Trash className="h-4 w-4" />
            Clear
          </button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Network className="h-5 w-5" />
            Live Feed
            {isMonitoring && (
              <Badge
                variant="outline"
                aria-live="polite"
                className={status === 'live'
                  ? 'text-green-700 border-green-700/20 dark:text-green-400 dark:border-green-400/20'
                  : 'text-yellow-700 border-yellow-700/20 dark:text-yellow-400 dark:border-yellow-400/20'}
              >
                {statusLabel(status, attempt)}
              </Badge>
            )}
          </CardTitle>
          <CardDescription>
            {requestCount} requests · {queryCount} SQL statements
          </CardDescription>
        </CardHeader>
        <CardContent>
          {entries.length === 0 ? (
            <div className="text-center py-12">
              <Network className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
              <p className="text-muted-foreground">No traffic recorded</p>
              <p className="text-sm text-muted-foreground mt-2">
                Click "Start Live" to monitor HTTP requests and SQL statements in real-time
              </p>
            </div>
          ) : (
            <div className="space-y-2 max-h-96 overflow-y-auto">
              {entries.map((entry) => (
                entry.kind === 'http' ? (
                  <div
                    key={entry.id}
                    className="flex items-center gap-3 p-3 rounded-lg border border-border hover:bg-accent/50 transition-colors"
                  >
                    <Badge
                      variant="outline"
                      className={getMethodColor(entry.method)}
                    >
                      {entry.method}
                    </Badge>
                    <span className="flex-1 truncate font-mono text-sm">
                      {entry.path}
                    </span>
                    {shortRequestId(entry.requestId) && (
                      <span className="text-xs font-mono text-muted-foreground">
                        {shortRequestId(entry.requestId)}
                      </span>
                    )}
                    <Badge
                      variant="outline"
                      className={getStatusColor(entry.status)}
                    >
                      {entry.status}
                    </Badge>
                    <span className="text-sm text-muted-foreground w-20 text-right">
                      {formatDuration(entry.duration)}
                    </span>
                  </div>
                ) : (
                  <div
                    key={entry.id}
                    className="flex items-center gap-3 p-3 rounded-lg border border-border bg-muted/30 hover:bg-accent/50 transition-colors"
                  >
                    <Badge
                      variant="outline"
                      className="bg-purple-500/10 text-purple-700 border-purple-700/20 dark:text-purple-400 dark:border-purple-400/20 flex items-center gap-1"
                    >
                      <Database className="h-3 w-3" />
                      SQL
                    </Badge>
                    {entry.operation && (
                      <span className="text-xs uppercase text-muted-foreground shrink-0">
                        {entry.operation}
                        {entry.model ? ` · ${entry.model}` : ''}
                      </span>
                    )}
                    <span className="flex-1 truncate font-mono text-xs" title={entry.query}>
                      {entry.query}
                    </span>
                    {entry.error && (
                      <Badge
                        variant="outline"
                        className="bg-red-500/10 text-red-700 border-red-700/20 dark:text-red-400 dark:border-red-400/20"
                      >
                        error
                      </Badge>
                    )}
                    {shortRequestId(entry.requestId) && (
                      <span className="text-xs font-mono text-muted-foreground">
                        {shortRequestId(entry.requestId)}
                      </span>
                    )}
                    <span className="text-sm text-muted-foreground w-20 text-right">
                      {formatDuration(entry.duration)}
                    </span>
                  </div>
                )
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
