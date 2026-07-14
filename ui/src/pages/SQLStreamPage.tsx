// SQL statements — live stream (design handoff "Orbit Admin", screen 4).
// Rendering only: data wiring is the same useStreamEvents hook as before.
import { useState } from 'react'
import { PageBody, PageHeader } from '@/components/Page'
import { StreamControls } from '@/components/StreamControls'
import { StreamFilterBar } from '@/components/StreamFilterBar'
import { SEMANTIC, sqlKindColor } from '@/lib/colors'
import { useStreamEvents } from '@/hooks/useStreamEvents'
import { useStreamFilters } from '@/hooks/useStreamFilters'
import { useNodes } from '@/hooks/useNodes'
import { durationToMillis, formatDuration, formatTime, streamRowKey, timestampToDate } from '@/lib/format'

// Exact column template from the handoff:
// Time / Node / Kind / Statement / Duration / Rows.
const GRID = '96px 92px 70px minmax(0,1fr) 84px 56px'

// Duration turns amber when a statement takes longer than the operator's
// configured threshold (persisted). Was a hardcoded 8ms.
const SLOW_MS_KEY = 'orbit.sql.slowMs'
const DEFAULT_SLOW_MS = 8

function loadSlowMs(): number {
  const raw = Number(window.localStorage.getItem(SLOW_MS_KEY))
  return Number.isFinite(raw) && raw > 0 ? raw : DEFAULT_SLOW_MS
}

// Ring buffer cap ~160, render top ~60 (handoff "Interactions & Behavior").
const BUFFER_CAP = 160
const RENDER_CAP = 60

export function SQLStreamPage() {
  const filters = useStreamFilters('sql')
  const { nodes } = useNodes()
  const [slowMs, setSlowMs] = useState<number>(loadSlowMs)
  const stream = useStreamEvents({
    filter: filters.filter,
    samplingRate: filters.samplingRate,
    bufferSize: BUFFER_CAP,
    includeRecent: true,
  })

  const rows = stream.events
    .filter((ev) => ev.body.case === 'sqlStatement')
    .slice(0, RENDER_CAP)

  return (
    <>
      <PageHeader
        title="SQL statements"
        description="Executed statements across the fleet. Argument values are masked at the source."
        actions={
          <span className="flex items-center gap-2.5">
            <label className="flex items-center gap-1.5 font-mono text-[10.5px] text-t30">
              slow &gt;
              <input
                type="number"
                min={1}
                value={slowMs}
                onChange={(e) => {
                  const v = Number(e.target.value)
                  const next = Number.isFinite(v) && v > 0 ? v : DEFAULT_SLOW_MS
                  setSlowMs(next)
                  window.localStorage.setItem(SLOW_MS_KEY, String(next))
                }}
                aria-label="Slow-statement threshold in milliseconds"
                className="w-[52px] rounded-[6px] border border-t19 bg-t8 px-1.5 py-[3px] text-right font-mono text-[10.5px] text-t45 focus:outline-none"
              />
              ms
            </label>
            <StreamControls
              connected={stream.connected}
              paused={stream.paused}
              onTogglePause={() => stream.setPaused(!stream.paused)}
              onClear={stream.clear}
              count={stream.events.length}
              pendingCount={stream.pendingCount}
              error={stream.errorMessage}
            />
          </span>
        }
      />
      <StreamFilterBar
        kind="sql"
        state={filters.state}
        setState={filters.setState}
        reset={filters.reset}
        active={filters.active}
        nodes={nodes}
      />
      <PageBody>
        <div className="overflow-hidden rounded-[10px] border border-t18 bg-t4">
          <div
            className="grid bg-t6 px-4 py-2 text-[10px] font-semibold uppercase tracking-[.08em] text-t30"
            style={{ gridTemplateColumns: GRID }}
          >
            <span>Time</span>
            <span>Node</span>
            <span>Kind</span>
            <span>Statement</span>
            <span className="text-right">Duration</span>
            <span className="text-right">Rows</span>
          </div>
          {rows.length === 0 && (
            <div className="border-t border-t10 px-4 py-8 text-center text-[12px] text-t26">
              {stream.connected ? 'No events — stream is quiet' : 'Waiting for events…'}
            </div>
          )}
          {rows.map((ev, idx) => {
            if (ev.body.case !== 'sqlStatement') return null
            const sql = ev.body.value
            const ms = durationToMillis(sql.duration)
            const failed = sql.error !== ''
            return (
              <div
                key={streamRowKey(ev.nodeId, ev.timestamp, idx)}
                className="grid items-center border-t border-t10 px-4 py-[6px] font-mono text-[11.5px] hover:bg-t7"
                style={{ gridTemplateColumns: GRID }}
              >
                <span className="text-t31">{formatTime(timestampToDate(ev.timestamp))}</span>
                <span className="truncate text-t32">{ev.nodeId}</span>
                <span className="font-semibold" style={{ color: sqlKindColor(sql.operation) }}>
                  {sql.operation.toUpperCase()}
                </span>
                <span
                  className={failed ? 'truncate pr-3' : 'truncate pr-3 text-t39'}
                  style={failed ? { color: SEMANTIC.red } : undefined}
                  title={failed ? `${sql.query} — ${sql.error}` : sql.query}
                >
                  {sql.query}
                </span>
                <span
                  className="text-right tabular-nums"
                  style={{ color: ms > slowMs ? SEMANTIC.amber : 'var(--t37)' }}
                >
                  {formatDuration(ms)}
                </span>
                {/* 0 means "not reported" (SELECTs, unsupported drivers) —
                    render the honest dash instead of a fake zero. */}
                <span className="text-right text-t32 tabular-nums">
                  {sql.rowsAffected > 0n ? sql.rowsAffected.toString() : '—'}
                </span>
              </div>
            )
          })}
        </div>
      </PageBody>
    </>
  )
}
