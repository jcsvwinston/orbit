// useStreamEvents drives ControlService.StreamEvents on behalf of a
// page. It maintains a bounded ring of recent events, exposes
// connection state, and lets the consumer pause/resume the live feed.
//
// Key design choices:
//
//   - Pause is tracked in a ref, not in state, so toggling pause does
//     NOT re-open the upstream stream. The async iterator keeps reading
//     and, while paused, BUFFERS events (up to bufferSize) instead of
//     discarding them; on resume the buffer is flushed into the visible
//     ring and pendingCount drops back to 0 (OR-UX-P1-4).
//
//   - Reconnect is automatic on transport errors with a small exponential
//     backoff (1s, 2s, 4s, capped at 5s). We don't surface that on the
//     UI beyond "connected: false" — this is observability, brief blips
//     are not interesting.
//
//   - The hook accepts a stable `filter` reference. Callers that build
//     filters inline must memoize them, or every render re-opens the
//     upstream call.

import { useEffect, useRef, useState } from 'react'
import { ConnectError, Code } from '@connectrpc/connect'
import { controlClient } from '@/lib/transport'
import { Filter, type Event } from '@/gen/nucleus/admin/v1/admin_pb'

export interface UseStreamEventsOptions {
  filter: Filter
  bufferSize?: number
  includeRecent?: boolean
  // samplingRate is the per-event-kind sampling map the server applies at
  // fanout time (key = EventType name without the EVENT_TYPE_ prefix, e.g.
  // "HTTP_REQUEST"; value 0.0–1.0). Must be a stable reference — like
  // `filter`, changing it re-opens the upstream stream.
  samplingRate?: Record<string, number>
}

export interface UseStreamEventsResult {
  events: Event[]
  connected: boolean
  paused: boolean
  setPaused: (v: boolean) => void
  clear: () => void
  errorMessage: string | null
  // pendingCount is how many events arrived while paused and are waiting
  // to be flushed on resume (0 when live).
  pendingCount: number
}

export function useStreamEvents(opts: UseStreamEventsOptions): UseStreamEventsResult {
  const bufferSize = opts.bufferSize ?? 200
  const [events, setEvents] = useState<Event[]>([])
  const [connected, setConnected] = useState(false)
  const [paused, setPausedState] = useState(false)
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const [pendingCount, setPendingCount] = useState(0)

  const pausedRef = useRef(false)
  // pendingRef holds events received while paused, newest-first, capped
  // at bufferSize. Flushed into `events` on resume.
  const pendingRef = useRef<Event[]>([])
  const setPaused = (v: boolean): void => {
    pausedRef.current = v
    setPausedState(v)
    if (!v && pendingRef.current.length > 0) {
      // Resume: prepend the buffered events (they are newer than what's
      // visible) and trim to the ring size.
      const buffered = pendingRef.current
      pendingRef.current = []
      setPendingCount(0)
      setEvents((prev) => {
        const next = [...buffered, ...prev]
        if (next.length > bufferSize) next.length = bufferSize
        return next
      })
    }
  }

  useEffect(() => {
    const ctrl = new AbortController()
    let alive = true
    let backoffMs = 1000

    async function loop(): Promise<void> {
      while (alive) {
        try {
          setConnected(true)
          setErrorMessage(null)
          const stream = controlClient.streamEvents(
            {
              filter: opts.filter,
              samplingRate: opts.samplingRate ?? {},
              includeRecent: opts.includeRecent ?? true,
            },
            { signal: ctrl.signal },
          )
          for await (const ev of stream) {
            if (!alive) return
            if (pausedRef.current) {
              // Buffer instead of discarding; cap and report the count.
              const buf = pendingRef.current
              buf.unshift(ev)
              if (buf.length > bufferSize) buf.length = bufferSize
              setPendingCount(buf.length)
              continue
            }
            setEvents((prev) => {
              const next = [ev, ...prev]
              if (next.length > bufferSize) next.length = bufferSize
              return next
            })
          }
          // Stream finished cleanly (server-initiated). Reconnect.
          backoffMs = 1000
        } catch (err) {
          if (!alive) return
          if (err instanceof ConnectError && err.code === Code.Canceled) {
            // Local cancel; the effect cleanup is the only place that
            // aborts our controller intentionally.
            return
          }
          setErrorMessage(err instanceof Error ? err.message : String(err))
        } finally {
          if (alive) setConnected(false)
        }

        if (!alive) return
        await sleep(backoffMs)
        backoffMs = Math.min(5000, backoffMs * 2)
      }
    }

    void loop()
    return () => {
      alive = false
      ctrl.abort()
    }
  }, [opts.filter, opts.samplingRate, opts.includeRecent, bufferSize])

  return {
    events,
    connected,
    paused,
    setPaused,
    clear: () => {
      pendingRef.current = []
      setPendingCount(0)
      setEvents([])
    },
    errorMessage,
    pendingCount,
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
