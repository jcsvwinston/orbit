// HTTP requests — live stream (design handoff "Orbit Admin", screen 3).
// Rendering only: data wiring is the same useStreamEvents hook as before.
// The div-grid carries table semantics (role table/row/columnheader/cell).
import { PageBody, PageHeader } from '@/components/Page'
import { StreamControls } from '@/components/StreamControls'
import { StreamFilterBar } from '@/components/StreamFilterBar'
import { methodColor, statusColor } from '@/lib/colors'
import { t } from '@/lib/i18n'
import { useStreamEvents } from '@/hooks/useStreamEvents'
import { useStreamFilters } from '@/hooks/useStreamFilters'
import { useNodes } from '@/hooks/useNodes'
import { durationToMillis, formatDuration, formatTime, streamRowKey, timestampToDate } from '@/lib/format'

// Exact column template from the handoff:
// Time / Node / Method / Path / Status / Duration / Remote IP.
const GRID = '96px 92px 62px minmax(0,1fr) 58px 84px 116px'

// Ring buffer cap ~160, render top ~60 (handoff "Interactions & Behavior").
const BUFFER_CAP = 160
const RENDER_CAP = 60

export function HTTPStreamPage() {
  const filters = useStreamFilters('http')
  const { nodes } = useNodes()
  const stream = useStreamEvents({
    filter: filters.filter,
    samplingRate: filters.samplingRate,
    bufferSize: BUFFER_CAP,
    includeRecent: true,
  })

  const rows = stream.events
    .filter((ev) => ev.body.case === 'httpRequest')
    .slice(0, RENDER_CAP)

  return (
    <>
      <PageHeader
        title={t.httpStream.title}
        description={t.httpStream.description}
        actions={
          <StreamControls
            connected={stream.connected}
            paused={stream.paused}
            onTogglePause={() => stream.setPaused(!stream.paused)}
            onClear={stream.clear}
            count={stream.events.length}
            pendingCount={stream.pendingCount}
            error={stream.errorMessage}
          />
        }
      />
      <StreamFilterBar
        kind="http"
        state={filters.state}
        setState={filters.setState}
        reset={filters.reset}
        active={filters.active}
        nodes={nodes}
      />
      <PageBody>
        <div
          role="table"
          aria-label={t.httpStream.tableAria}
          className="overflow-hidden rounded-[10px] border border-t18 bg-t4"
        >
          <div
            role="row"
            className="grid bg-t6 px-4 py-2 text-[10px] font-semibold uppercase tracking-[.08em] text-t30"
            style={{ gridTemplateColumns: GRID }}
          >
            <span role="columnheader">{t.httpStream.colTime}</span>
            <span role="columnheader">{t.httpStream.colNode}</span>
            <span role="columnheader">{t.httpStream.colMethod}</span>
            <span role="columnheader">{t.httpStream.colPath}</span>
            <span role="columnheader" className="text-right">
              {t.httpStream.colStatus}
            </span>
            <span role="columnheader" className="text-right">
              {t.httpStream.colDuration}
            </span>
            <span role="columnheader" className="text-right">
              {t.httpStream.colRemote}
            </span>
          </div>
          {rows.length === 0 && (
            <div className="border-t border-t10 px-4 py-8 text-center text-[12px] text-t26">
              {stream.connected ? t.stream.quiet : t.stream.waiting}
            </div>
          )}
          {rows.map((ev, idx) => {
            if (ev.body.case !== 'httpRequest') return null
            const http = ev.body.value
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
                <span role="cell" className="font-semibold" style={{ color: methodColor(http.method) }}>
                  {http.method}
                </span>
                <span role="cell" className="truncate pr-3 text-t39" title={http.path}>
                  {http.path}
                </span>
                <span role="cell" className="text-right" style={{ color: statusColor(http.status) }}>
                  {http.status}
                </span>
                <span role="cell" className="text-right text-t37 tabular-nums">
                  {formatDuration(durationToMillis(http.duration))}
                </span>
                <span role="cell" className="truncate text-right text-t32">
                  {http.remoteIp}
                </span>
              </div>
            )
          })}
        </div>
      </PageBody>
    </>
  )
}
