// HTTP requests — live stream (design handoff "Orbit Admin", screen 3).
// Rendering only: data wiring is the same useStreamEvents hook as before.
import { useMemo } from 'react'
import { PageBody, PageHeader } from '@/components/Page'
import { StreamControls } from '@/components/StreamControls'
import { methodColor, statusColor } from '@/lib/colors'
import { useStreamEvents } from '@/hooks/useStreamEvents'
import { Filter, EventType } from '@/gen/nucleus/admin/v1/admin_pb'
import { durationToMillis, formatDuration, formatTime, timestampToDate } from '@/lib/format'

// Exact column template from the handoff:
// Time / Node / Method / Path / Status / Duration / Remote IP.
const GRID = '96px 92px 62px minmax(0,1fr) 58px 84px 116px'

// Ring buffer cap ~160, render top ~60 (handoff "Interactions & Behavior").
const BUFFER_CAP = 160
const RENDER_CAP = 60

export function HTTPStreamPage() {
  // The filter must be referentially stable so useStreamEvents does not
  // re-open on every render.
  const filter = useMemo(() => new Filter({ types: [EventType.HTTP_REQUEST] }), [])

  const stream = useStreamEvents({ filter, bufferSize: BUFFER_CAP, includeRecent: true })

  const rows = stream.events
    .filter((ev) => ev.body.case === 'httpRequest')
    .slice(0, RENDER_CAP)

  return (
    <>
      <PageHeader
        title="HTTP requests"
        description="Live stream from every connected agent. Sensitive query params are starred at the source."
        actions={
          <StreamControls
            connected={stream.connected}
            paused={stream.paused}
            onTogglePause={() => stream.setPaused(!stream.paused)}
            onClear={stream.clear}
            count={stream.events.length}
            error={stream.errorMessage}
          />
        }
      />
      <PageBody>
        <div className="overflow-hidden rounded-[10px] border border-t18 bg-t4">
          <div
            className="grid bg-t6 px-4 py-2 text-[10px] font-semibold uppercase tracking-[.08em] text-t30"
            style={{ gridTemplateColumns: GRID }}
          >
            <span>Time</span>
            <span>Node</span>
            <span>Method</span>
            <span>Path</span>
            <span className="text-right">Status</span>
            <span className="text-right">Duration</span>
            <span className="text-right">Remote</span>
          </div>
          {rows.length === 0 && (
            <div className="border-t border-t10 px-4 py-8 text-center text-[12px] text-t26">
              {stream.connected ? 'No events — stream is quiet' : 'Waiting for events…'}
            </div>
          )}
          {rows.map((ev, idx) => {
            if (ev.body.case !== 'httpRequest') return null
            const http = ev.body.value
            return (
              <div
                key={`${ev.nodeId}-${idx}`}
                className="grid items-center border-t border-t10 px-4 py-[6px] font-mono text-[11.5px] hover:bg-t7"
                style={{ gridTemplateColumns: GRID }}
              >
                <span className="text-t25">{formatTime(timestampToDate(ev.timestamp))}</span>
                <span className="truncate text-t32">{ev.nodeId}</span>
                <span className="font-semibold" style={{ color: methodColor(http.method) }}>
                  {http.method}
                </span>
                <span className="truncate pr-3 text-t39" title={http.path}>
                  {http.path}
                </span>
                <span className="text-right" style={{ color: statusColor(http.status) }}>
                  {http.status}
                </span>
                <span className="text-right text-t37 tabular-nums">
                  {formatDuration(durationToMillis(http.duration))}
                </span>
                <span className="truncate text-right text-t32">{http.remoteIp}</span>
              </div>
            )
          })}
        </div>
      </PageBody>
    </>
  )
}
