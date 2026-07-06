// Client-side rolling window of HostMetrics samples per node. The backend
// only exposes the LATEST sample (NodeInfo.hostMetrics, refreshed by the
// agent heartbeat ~10s and surfaced through the useNodes 3s poll), so the
// sparkline history lives here: a module-level cache keyed by nodeId that
// survives navigating away and back (history only resets on a full reload).

import { useRef } from 'react'
import type { HostMetrics } from '@/gen/nucleus/admin/v1/admin_pb'

export interface HostMetricSeries {
  /** CPU busy share, 0–100. */
  cpu: number[]
  /** Heap alloc, MB. */
  heapMB: number[]
  /** Resident set size, MB (0 when the platform cannot report it). */
  rssMB: number[]
  goroutines: number[]
  /** GC pause p99, ms. */
  gcMs: number[]
}

const MAX_SAMPLES = 60
const BYTES_PER_MB = 1_048_576

const cache = new Map<string, HostMetricSeries>()

const EMPTY: HostMetricSeries = { cpu: [], heapMB: [], rssMB: [], goroutines: [], gcMs: [] }

function push(series: number[], value: number): void {
  series.push(value)
  if (series.length > MAX_SAMPLES) series.splice(0, series.length - MAX_SAMPLES)
}

/**
 * Appends a sample whenever `hm` changes and returns the rolling window for
 * `nodeId`. ListNodes returns fresh message instances on every 3s poll while
 * the agent only heartbeats ~every 10s, so identical consecutive samples are
 * deduped by value instead of by reference.
 */
export function useHostMetricSeries(
  nodeId: string,
  hm: HostMetrics | undefined,
): HostMetricSeries {
  const lastKey = useRef('')

  if (hm !== undefined && nodeId !== '') {
    const key = [
      nodeId,
      hm.cpuPercent,
      hm.rssBytes,
      hm.heapAllocBytes,
      hm.goroutines,
      hm.gcPauseP99Ms,
      hm.dbInUse,
      hm.dbIdle,
      hm.dbMaxOpen,
    ].join('|')
    if (key !== lastKey.current) {
      lastKey.current = key
      let s = cache.get(nodeId)
      if (s === undefined) {
        s = { cpu: [], heapMB: [], rssMB: [], goroutines: [], gcMs: [] }
        cache.set(nodeId, s)
      }
      push(s.cpu, hm.cpuPercent)
      push(s.heapMB, Number(hm.heapAllocBytes) / BYTES_PER_MB)
      push(s.rssMB, Number(hm.rssBytes) / BYTES_PER_MB)
      push(s.goroutines, hm.goroutines)
      push(s.gcMs, hm.gcPauseP99Ms)
    }
  }

  return cache.get(nodeId) ?? EMPTY
}
