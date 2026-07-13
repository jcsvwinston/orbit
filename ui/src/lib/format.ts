// Tiny set of formatters shared by the streaming pages. Kept in one
// place so changes (e.g. switching to a real i18n lib) hit one file.

import type { Duration, Timestamp } from '@bufbuild/protobuf'

export function timestampToDate(ts: Timestamp | undefined): Date | undefined {
  if (!ts) return undefined
  // Timestamp.seconds is bigint in protobuf-es 1.x.
  return new Date(Number(ts.seconds) * 1000 + Math.floor(ts.nanos / 1_000_000))
}

// streamRowKey derives a stable React key from an event's node + wall
// clock (nanosecond precision). Index-based keys remount every row when
// a new event is prepended (the whole list shifts), so React can't reuse
// DOM nodes — the ns timestamp keeps keys stable across prepends.
export function streamRowKey(nodeId: string, ts: Timestamp | undefined, fallbackIdx: number): string {
  if (!ts) return `${nodeId}:idx${fallbackIdx}`
  return `${nodeId}:${ts.seconds.toString()}.${ts.nanos.toString()}`
}

export function durationToMillis(d: Duration | undefined): number {
  if (!d) return 0
  return Number(d.seconds) * 1000 + d.nanos / 1_000_000
}

export function formatDuration(ms: number): string {
  if (ms < 1) return `${(ms * 1000).toFixed(0)}µs`
  if (ms < 1000) return `${ms.toFixed(2)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

export function formatRelative(date: Date | undefined): string {
  if (!date) return '—'
  const diff = Date.now() - date.getTime()
  if (diff < 1000) return 'just now'
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  return date.toISOString().slice(0, 19).replace('T', ' ') + ' UTC'
}

export function formatTime(date: Date | undefined): string {
  if (!date) return '—'
  const h = date.getHours().toString().padStart(2, '0')
  const m = date.getMinutes().toString().padStart(2, '0')
  const s = date.getSeconds().toString().padStart(2, '0')
  const ms = date.getMilliseconds().toString().padStart(3, '0')
  return `${h}:${m}:${s}.${ms}`
}

