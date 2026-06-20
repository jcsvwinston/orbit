import { useEffect, useState, useRef } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import * as api from '@/services/api'
import type { LiveRequest } from '@/types'
import { Network, Play, Pause, Trash } from 'lucide-react'

function formatDuration(ms: number): string {
  if (ms < 1) return `${(ms * 1000).toFixed(0)}µs`
  if (ms < 1000) return `${ms.toFixed(2)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

function getStatusColor(status: number): string {
  if (status < 300) return 'bg-green-500/10 text-green-500 border-green-500/20'
  if (status < 400) return 'bg-yellow-500/10 text-yellow-500 border-yellow-500/20'
  return 'bg-red-500/10 text-red-500 border-red-500/20'
}

function getMethodColor(method: string): string {
  switch (method.toUpperCase()) {
    case 'GET': return 'bg-blue-500/10 text-blue-500 border-blue-500/20'
    case 'POST': return 'bg-green-500/10 text-green-500 border-green-500/20'
    case 'PUT': return 'bg-yellow-500/10 text-yellow-500 border-yellow-500/20'
    case 'DELETE': return 'bg-red-500/10 text-red-500 border-red-500/20'
    default: return 'bg-gray-500/10 text-gray-500 border-gray-500/20'
  }
}

export default function NetworkInspectorPage() {
  const [requests, setRequests] = useState<LiveRequest[]>([])
  const [isMonitoring, setIsMonitoring] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)

  const fetchSnapshot = async () => {
    try {
      const data = await api.getLiveRequests()
      setRequests(data)
    } catch (error) {
      console.error('Failed to fetch requests:', error)
    }
  }

  const connectWebSocket = () => {
    const ws = api.getLiveWebSocket()
    if (!ws) return

    wsRef.current = ws

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        if (data.type === 'request') {
          setRequests(prev => [data.request, ...prev.slice(0, 99)])
        }
      } catch (error) {
        console.error('WebSocket message error:', error)
      }
    }

    ws.onerror = (error) => {
      console.error('WebSocket error:', error)
    }
  }

  const startMonitoring = () => {
    setIsMonitoring(true)
    connectWebSocket()
  }

  const stopMonitoring = () => {
    setIsMonitoring(false)
    wsRef.current?.close()
  }

  const clearRequests = () => {
    setRequests([])
  }

  useEffect(() => {
    fetchSnapshot()
    return () => {
      wsRef.current?.close()
    }
  }, [])

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Network Inspector</h1>
          <p className="text-muted-foreground">Live HTTP traffic monitoring</p>
        </div>
        <div className="flex gap-2">
          {isMonitoring ? (
            <button
              onClick={stopMonitoring}
              className="flex items-center gap-2 px-3 py-2 rounded-md bg-destructive text-destructive-foreground hover:bg-destructive/90 transition-colors"
            >
              <Pause className="h-4 w-4" />
              Stop
            </button>
          ) : (
            <button
              onClick={startMonitoring}
              className="flex items-center gap-2 px-3 py-2 rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors"
            >
              <Play className="h-4 w-4" />
              Start Live
            </button>
          )}
          <button
            onClick={clearRequests}
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
            Request Log
          </CardTitle>
          <CardDescription>
            {requests.length} requests captured
            {isMonitoring && ' (Live)'}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {requests.length === 0 ? (
            <div className="text-center py-12">
              <Network className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
              <p className="text-muted-foreground">No requests recorded</p>
              <p className="text-sm text-muted-foreground mt-2">
                Click "Start Live" to monitor traffic in real-time
              </p>
            </div>
          ) : (
            <div className="space-y-2 max-h-96 overflow-y-auto">
              {requests.map((req, index) => (
                <div
                  key={req.id || index}
                  className="flex items-center gap-3 p-3 rounded-lg border border-border hover:bg-accent/50 transition-colors"
                >
                  <Badge
                    variant="outline"
                    className={getMethodColor(req.method)}
                  >
                    {req.method}
                  </Badge>
                  <span className="flex-1 truncate font-mono text-sm">
                    {req.path}
                  </span>
                  <Badge
                    variant="outline"
                    className={getStatusColor(req.status)}
                  >
                    {req.status}
                  </Badge>
                  <span className="text-sm text-muted-foreground w-20 text-right">
                    {formatDuration(req.duration)}
                  </span>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
