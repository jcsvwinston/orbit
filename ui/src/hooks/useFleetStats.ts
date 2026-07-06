// useFleetStats derives the fleet-level series shown on Overview and the
// per-node series shown on Metrics from the ONLY real signals the backend
// emits today: the HTTP / SQL / session event streams. No host metrics
// (CPU, memory, goroutines, heap, GC, DB pool) exist yet — pages must render
// those as "awaiting agent metrics", never fake them.
//
// Sampling model (handoff "State Management"): a 60-sample rolling window,
// one sample per second. A 1 s ticker re-buckets the client-side event ring
// by timestamp age, so all series are honest per-second aggregates:
//
//   - rps         — HTTP events in the most recent 1 s bucket
//   - p95         — p95 of HTTP durations over the 60 s window
//                   (spark samples use a trailing 10 s sub-window)
//   - error rate  — % of HTTP events with status >= 500 in the window
//   - sessions    — session-change events seen in the window (per minute)
//   - sql rate    — SQL events in the most recent 1 s bucket
//   - node series — same HTTP aggregates filtered to one nodeId, plus a
//                   cumulative "requests seen in client buffer" counter

import { useEffect, useMemo, useState } from 'react'
import { useStreamEvents } from '@/hooks/useStreamEvents'
import { EventType, Filter, type Event } from '@/gen/nucleus/admin/v1/admin_pb'
import { durationToMillis, timestampToDate } from '@/lib/format'

export const WINDOW_SECONDS = 60

/** Sub-window (seconds) used for the p95 / error-rate spark samples. */
const TRAILING = 10

/** Ring larger than the stream pages' 160 so 60 s of busy fleet fits. */
const BUFFER_CAP = 1024

export interface FleetSeries {
  /** HTTP events in the last full second. */
  rps: number
  rpsSeries: number[]
  /** p95 of HTTP durations (ms) over the 60 s window. 0 when no traffic. */
  p95Ms: number
  p95Series: number[]
  /** % of HTTP events with status >= 500 over the 60 s window. */
  errorRatePct: number
  errorSeries: number[]
  /** Session-change events observed in the 60 s window. */
  sessionEventsPerMin: number
  sessionSeries: number[]
  /** SQL events in the last full second. */
  sqlRps: number
  sqlSeries: number[]
  /** False when zero HTTP events fell inside the window (render "—"). */
  hasHttpTraffic: boolean
}

export interface NodeSeries {
  rps: number
  rpsSeries: number[]
  /** HTTP events from this node currently in the client buffer. */
  requestsSeen: number
  /** Cumulative curve of requestsSeen across the 60 samples. */
  requestsSeenSeries: number[]
}

export interface UseFleetStatsResult {
  fleet: FleetSeries
  /** Present only when a nodeId was passed. */
  node: NodeSeries | null
  /** Raw ring (newest first) for the Overview HTTP / SQL feed cards. */
  events: Event[]
  connected: boolean
}

export function useFleetStats(nodeId?: string | null): UseFleetStatsResult {
  // Stable filter reference — useStreamEvents re-opens the stream otherwise.
  const filter = useMemo(
    () =>
      new Filter({
        types: [EventType.HTTP_REQUEST, EventType.SQL_STATEMENT, EventType.SESSION_CHANGE],
      }),
    [],
  )
  const stream = useStreamEvents({ filter, bufferSize: BUFFER_CAP, includeRecent: true })

  // 1 sample/s ticker: every tick re-buckets the ring by timestamp age.
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(t)
  }, [])

  const derived = useMemo(
    () => compute(stream.events, now, nodeId ?? null),
    [stream.events, now, nodeId],
  )

  return { ...derived, events: stream.events, connected: stream.connected }
}

/** Rate formatter shared by both pages: 1 decimal under 10, integer above. */
export function formatRate(v: number): string {
  return v >= 10 ? v.toFixed(0) : v.toFixed(1)
}

function percentile(sorted: readonly number[], p: number): number {
  if (sorted.length === 0) return 0
  const idx = Math.min(sorted.length - 1, Math.max(0, Math.ceil(p * sorted.length) - 1))
  return sorted[idx]
}

function compute(
  events: readonly Event[],
  now: number,
  nodeId: string | null,
): { fleet: FleetSeries; node: NodeSeries | null } {
  const N = WINDOW_SECONDS
  // Index 0 = newest second (age 0..1 s), index N-1 = oldest.
  const http = new Array<number>(N).fill(0)
  const err = new Array<number>(N).fill(0)
  const sql = new Array<number>(N).fill(0)
  const sess = new Array<number>(N).fill(0)
  const nodeHttp = new Array<number>(N).fill(0)
  const durs: number[][] = Array.from({ length: N }, () => [])
  let nodeSeen = 0

  for (const ev of events) {
    const isNodeHttp = nodeId !== null && ev.body.case === 'httpRequest' && ev.nodeId === nodeId
    if (isNodeHttp) nodeSeen++
    const at = timestampToDate(ev.timestamp)
    if (!at) continue
    const age = now - at.getTime()
    const idx = age < 0 ? 0 : Math.floor(age / 1000)
    if (idx >= N) continue
    switch (ev.body.case) {
      case 'httpRequest': {
        http[idx]++
        durs[idx].push(durationToMillis(ev.body.value.duration))
        if (ev.body.value.status >= 500) err[idx]++
        if (isNodeHttp) nodeHttp[idx]++
        break
      }
      case 'sqlStatement':
        sql[idx]++
        break
      case 'sessionChange':
        sess[idx]++
        break
      default:
        break
    }
  }

  // Series are plotted oldest → newest (Sparkline contract).
  const oldestFirst = (a: readonly number[]): number[] => [...a].reverse()

  const p95Series: number[] = []
  const errorSeries: number[] = []
  for (let i = 0; i < N; i++) {
    const bucket = N - 1 - i // sample i corresponds to this age index
    const windowDurs: number[] = []
    let e = 0
    let t = 0
    for (let k = bucket; k < Math.min(N, bucket + TRAILING); k++) {
      for (const d of durs[k]) windowDurs.push(d)
      e += err[k]
      t += http[k]
    }
    windowDurs.sort((a, b) => a - b)
    p95Series.push(percentile(windowDurs, 0.95))
    errorSeries.push(t > 0 ? (e / t) * 100 : 0)
  }

  const allDurs = durs.flat().sort((a, b) => a - b)
  const totalHttp = http.reduce((a, b) => a + b, 0)
  const totalErr = err.reduce((a, b) => a + b, 0)

  const fleet: FleetSeries = {
    rps: http[0],
    rpsSeries: oldestFirst(http),
    p95Ms: percentile(allDurs, 0.95),
    p95Series,
    errorRatePct: totalHttp > 0 ? (totalErr / totalHttp) * 100 : 0,
    errorSeries,
    sessionEventsPerMin: sess.reduce((a, b) => a + b, 0),
    sessionSeries: oldestFirst(sess),
    sqlRps: sql[0],
    sqlSeries: oldestFirst(sql),
    hasHttpTraffic: totalHttp > 0,
  }

  let node: NodeSeries | null = null
  if (nodeId !== null) {
    const rpsSeries = oldestFirst(nodeHttp)
    const inWindow = rpsSeries.reduce((a, b) => a + b, 0)
    let acc = nodeSeen - inWindow // events in buffer but older than the window
    const requestsSeenSeries = rpsSeries.map((v) => (acc += v))
    node = { rps: nodeHttp[0], rpsSeries, requestsSeen: nodeSeen, requestsSeenSeries }
  }

  return { fleet, node }
}
