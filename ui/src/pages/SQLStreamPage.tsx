// SQL statements — live stream (design handoff "Orbit Admin", screen 4).
// Rendering only: data wiring is the same useStreamEvents hook as before.
// The div-grid carries table semantics (role table/row/columnheader/cell).
import { useState } from 'react'
import { PageBody, PageHeader } from '@/components/Page'
import { StreamControls } from '@/components/StreamControls'
import { StreamFilterBar } from '@/components/StreamFilterBar'
import { SEMANTIC, sqlKindColor } from '@/lib/colors'
import { t } from '@/lib/i18n'
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
        title={t.sqlStream.title}
        description={t.sqlStream.description}
        actions={
          <span className="flex items-center gap-2.5">
            <label className="flex items-center gap-1.5 font-mono text-[10.5px] text-t30">
              {t.sqlStream.slowPrefix}
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
                aria-label={t.sqlStream.slowAria}
                className="w-[52px] rounded-[6px] border border-t19 bg-t8 px-1.5 py-[3px] text-right font-mono text-[10.5px] text-t45 focus:outline-none"
              />
              {t.sqlStream.slowUnit}
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
        <div
          role="table"
          aria-label={t.sqlStream.tableAria}
          className="overflow-hidden rounded-[10px] border border-t18 bg-t4"
        >
          <div
            role="row"
            className="grid bg-t6 px-4 py-2 text-[10px] font-semibold uppercase tracking-[.08em] text-t30"
            style={{ gridTemplateColumns: GRID }}
          >
            <span role="columnheader">{t.sqlStream.colTime}</span>
            <span role="columnheader">{t.sqlStream.colNode}</span>
            <span role="columnheader">{t.sqlStream.colKind}</span>
            <span role="columnheader">{t.sqlStream.colStatement}</span>
            <span role="columnheader" className="text-right">
              {t.sqlStream.colDuration}
            </span>
            <span role="columnheader" className="text-right">
              {t.sqlStream.colRows}
            </span>
          </div>
          {rows.length === 0 && (
            <div className="border-t border-t10 px-4 py-8 text-center text-[12px] text-t26">
              {stream.connected ? t.stream.quiet : t.stream.waiting}
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
                role="row"
                className="grid items-center border-t border-t10 px-4 py-[6px] font-mono text-[11.5px] hover:bg-t7"
                style={{ gridTemplateColumns: GRID }}
              >
                <span role="cell" className="text-t31">
                  {formatTime(timestampToDate(ev.timestamp))}
                </span>
                <span role="cell" className="truncate text-t32">
                  {ev.nodeId}
                </span>
                <span role="cell" className="font-semibold" style={{ color: sqlKindColor(sql.operation) }}>
                  {sql.operation.toUpperCase()}
                </span>
                <span
                  role="cell"
                  className={failed ? 'truncate pr-3' : 'truncate pr-3 text-t39'}
                  style={failed ? { color: SEMANTIC.red } : undefined}
                  title={failed ? t.sqlStream.failedTitle(sql.query, sql.error) : sql.query}
                >
                  {sql.query}
                </span>
                <span
                  role="cell"
                  className="text-right tabular-nums"
                  style={{ color: ms > slowMs ? SEMANTIC.amber : 'var(--t37)' }}
                >
                  {formatDuration(ms)}
                </span>
                {/* 0 means "not reported" (SELECTs, unsupported drivers) —
                    render the honest dash instead of a fake zero. */}
                <span role="cell" className="text-right text-t32 tabular-nums">
                  {sql.rowsAffected > 0n ? sql.rowsAffected.toString() : t.common.empty}
                </span>
              </div>
            )
          })}
        </div>
      </PageBody>
    </>
  )
}
