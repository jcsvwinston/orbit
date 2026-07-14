// useStreamFilters manages the per-page stream filter + sampling state for
// the HTTP / SQL / Session stream pages (OR-UX-P1-3). The proto already
// supports method / path-glob / status-class / sql-model / node filtering
// (server/routing/match.go) and per-kind sampling; this exposes it in the UI.
//
// Design notes:
//   - The applied `filter` / `samplingRate` are derived from a DEBOUNCED copy
//     of the input state and memoized, so their references stay stable while
//     the operator types — useStreamEvents only re-opens the stream once the
//     debounce settles (changing the filter deliberately resets the ring, so
//     what's on screen always matches the active filter).
//   - State persists to localStorage per page, like the theme toggle.

import { useEffect, useMemo, useRef, useState } from 'react'
import { Filter, EventType } from '@/gen/nucleus/admin/v1/admin_pb'

export type FilterKind = 'http' | 'sql' | 'session'

export interface StreamFilterState {
  methods: string[] // HTTP methods (GET, POST, …)
  pathGlob: string // HTTP path glob
  statusClasses: string[] // leading-digit classes: "2","3","4","5"
  sqlModel: string // SQL model name
  nodeId: string // "" = all nodes
  samplingPct: number // 100 | 50 | 10 | 1
}

function emptyState(): StreamFilterState {
  return { methods: [], pathGlob: '', statusClasses: [], sqlModel: '', nodeId: '', samplingPct: 100 }
}

function storageKey(kind: FilterKind): string {
  return `orbit.streamfilter.${kind}`
}

function load(kind: FilterKind): StreamFilterState {
  try {
    const raw = window.localStorage.getItem(storageKey(kind))
    if (!raw) return emptyState()
    return { ...emptyState(), ...(JSON.parse(raw) as Partial<StreamFilterState>) }
  } catch {
    return emptyState()
  }
}

const EVENT_TYPE_KEY: Record<FilterKind, { type: EventType; sampleKey: string }> = {
  http: { type: EventType.HTTP_REQUEST, sampleKey: 'HTTP_REQUEST' },
  sql: { type: EventType.SQL_STATEMENT, sampleKey: 'SQL_STATEMENT' },
  session: { type: EventType.SESSION_CHANGE, sampleKey: 'SESSION_CHANGE' },
}

export interface UseStreamFiltersResult {
  filter: Filter
  samplingRate: Record<string, number>
  state: StreamFilterState
  setState: (patch: Partial<StreamFilterState>) => void
  reset: () => void
  active: boolean
}

export function useStreamFilters(kind: FilterKind): UseStreamFiltersResult {
  const [state, setStateRaw] = useState<StreamFilterState>(() => load(kind))
  // applied is the debounced snapshot the filter/samplingRate derive from.
  const [applied, setApplied] = useState<StreamFilterState>(state)
  const debounceRef = useRef<number | undefined>(undefined)

  const setState = (patch: Partial<StreamFilterState>): void => {
    setStateRaw((prev) => {
      const next = { ...prev, ...patch }
      try {
        window.localStorage.setItem(storageKey(kind), JSON.stringify(next))
      } catch {
        /* ignore quota / disabled storage */
      }
      return next
    })
  }

  const reset = (): void => setState(emptyState())

  // Debounce state → applied (400ms). Chip toggles feel instant enough; the
  // debounce mainly protects the text inputs.
  useEffect(() => {
    window.clearTimeout(debounceRef.current)
    debounceRef.current = window.setTimeout(() => setApplied(state), 400)
    return () => window.clearTimeout(debounceRef.current)
  }, [state])

  const { type, sampleKey } = EVENT_TYPE_KEY[kind]

  const filter = useMemo(() => {
    const f = new Filter({ types: [type] })
    if (kind === 'http') {
      if (applied.methods.length > 0) f.httpMethods = applied.methods
      if (applied.pathGlob.trim() !== '') f.httpPathGlobs = [applied.pathGlob.trim()]
      if (applied.statusClasses.length > 0) f.httpStatusClasses = applied.statusClasses
    }
    if (kind === 'sql' && applied.sqlModel.trim() !== '') {
      f.sqlModels = [applied.sqlModel.trim()]
    }
    if (applied.nodeId.trim() !== '') f.nodeIds = [applied.nodeId.trim()]
    return f
  }, [kind, type, applied])

  const samplingRate = useMemo<Record<string, number>>(() => {
    if (applied.samplingPct >= 100) return {}
    return { [sampleKey]: applied.samplingPct / 100 }
  }, [sampleKey, applied.samplingPct])

  const active =
    applied.methods.length > 0 ||
    applied.pathGlob.trim() !== '' ||
    applied.statusClasses.length > 0 ||
    applied.sqlModel.trim() !== '' ||
    applied.nodeId.trim() !== '' ||
    applied.samplingPct < 100

  return { filter, samplingRate, state, setState, reset, active }
}
