// Session activity — live stream (design handoff "Orbit Admin", screen 8).
// Rendering only: data wiring is the same useStreamEvents hook as before.
// The div-grid carries table semantics (role table/row/columnheader/cell).
import { PageBody, PageHeader } from '@/components/Page'
import { StreamControls } from '@/components/StreamControls'
import { StreamFilterBar } from '@/components/StreamFilterBar'
import { SEMANTIC } from '@/lib/colors'
import { t } from '@/lib/i18n'
import { useStreamEvents } from '@/hooks/useStreamEvents'
import { useStreamFilters } from '@/hooks/useStreamFilters'
import { useNodes } from '@/hooks/useNodes'
import { SessionChangeEvent_Kind } from '@/gen/nucleus/admin/v1/admin_pb'
import { formatTime, streamRowKey, timestampToDate } from '@/lib/format'

// Exact column template from the handoff:
// Time / Node / Kind / User / Token / Last route / IP.
const GRID = '96px 92px 88px 120px 120px minmax(0,1fr) 110px'

// Ring buffer cap ~160, render top ~60 (handoff "Interactions & Behavior").
const BUFFER_CAP = 160
const RENDER_CAP = 60

export function SessionsPage() {
  const filters = useStreamFilters('session')
  const { nodes } = useNodes()
  const stream = useStreamEvents({
    filter: filters.filter,
    samplingRate: filters.samplingRate,
    bufferSize: BUFFER_CAP,
    includeRecent: true,
  })

  const rows = stream.events
    .filter((ev) => ev.body.case === 'sessionChange')
    .slice(0, RENDER_CAP)

  return (
    <>
      <PageHeader
        title={t.sessions.title}
        description={t.sessions.description}
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
        kind="session"
        state={filters.state}
        setState={filters.setState}
        reset={filters.reset}
        active={filters.active}
        nodes={nodes}
      />
      <PageBody>
        <div
          role="table"
          aria-label={t.sessions.tableAria}
          className="overflow-hidden rounded-[10px] border border-t18 bg-t4"
        >
          <div
            role="row"
            className="grid bg-t6 px-4 py-2 text-[10px] font-semibold uppercase tracking-[.08em] text-t30"
            style={{ gridTemplateColumns: GRID }}
          >
            <span role="columnheader">{t.sessions.colTime}</span>
            <span role="columnheader">{t.sessions.colNode}</span>
            <span role="columnheader">{t.sessions.colKind}</span>
            <span role="columnheader">{t.sessions.colUser}</span>
            <span role="columnheader">{t.sessions.colToken}</span>
            <span role="columnheader">{t.sessions.colLastRoute}</span>
            <span role="columnheader" className="text-right">
              {t.sessions.colIP}
            </span>
          </div>
          {rows.length === 0 && (
            <div className="border-t border-t10 px-4 py-8 text-center text-[12px] text-t26">
              {stream.connected ? t.stream.quiet : t.stream.waiting}
            </div>
          )}
          {rows.map((ev, idx) => {
            if (ev.body.case !== 'sessionChange') return null
            const s = ev.body.value
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
                <span role="cell">
                  <KindPill kind={s.kind} />
                </span>
                <span role="cell" className="truncate pr-2 text-t42">
                  {s.userId || t.common.empty}
                </span>
                <span role="cell" className="truncate pr-2 text-t32">
                  {s.tokenShort || t.common.empty}
                </span>
                <span role="cell" className="truncate pr-2.5 text-t36" title={s.lastRoute}>
                  {s.lastRoute || t.common.empty}
                </span>
                <span role="cell" className="truncate text-right text-t25">
                  {s.ip || t.common.empty}
                </span>
              </div>
            )
          })}
        </div>
      </PageBody>
    </>
  )
}

/** Tinted kind chip: 12% background tint of the semantic color. */
function KindPill(props: { kind: SessionChangeEvent_Kind }) {
  const { label, color } = describeKind(props.kind)
  return (
    <span
      className="inline-block rounded-full px-2 py-px text-[10.5px]"
      style={{ color, background: `color-mix(in srgb, ${color} 12%, transparent)` }}
    >
      {label}
    </span>
  )
}

function describeKind(k: SessionChangeEvent_Kind): { label: string; color: string } {
  switch (k) {
    case SessionChangeEvent_Kind.CREATED:
      return { label: t.sessions.kindCreated, color: SEMANTIC.green }
    case SessionChangeEvent_Kind.TOUCHED:
      return { label: t.sessions.kindTouched, color: SEMANTIC.blue }
    case SessionChangeEvent_Kind.DESTROYED:
      return { label: t.sessions.kindDestroyed, color: SEMANTIC.red }
    default:
      return { label: t.sessions.kindUnspecified, color: 'var(--t32)' }
  }
}
