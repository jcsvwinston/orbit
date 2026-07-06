// Session activity — live stream (design handoff "Orbit Admin", screen 8).
// Rendering only: data wiring is the same useStreamEvents hook as before.
import { useMemo } from 'react'
import { PageBody, PageHeader } from '@/components/Page'
import { StreamControls } from '@/components/StreamControls'
import { SEMANTIC } from '@/lib/colors'
import { useStreamEvents } from '@/hooks/useStreamEvents'
import {
  Filter,
  EventType,
  SessionChangeEvent_Kind,
} from '@/gen/nucleus/admin/v1/admin_pb'
import { formatTime, timestampToDate } from '@/lib/format'

// Exact column template from the handoff:
// Time / Node / Kind / User / Token / Last route / IP.
const GRID = '96px 92px 88px 120px 120px minmax(0,1fr) 110px'

// Ring buffer cap ~160, render top ~60 (handoff "Interactions & Behavior").
const BUFFER_CAP = 160
const RENDER_CAP = 60

export function SessionsPage() {
  // The filter must be referentially stable so useStreamEvents does not
  // re-open on every render.
  const filter = useMemo(() => new Filter({ types: [EventType.SESSION_CHANGE] }), [])

  const stream = useStreamEvents({ filter, bufferSize: BUFFER_CAP, includeRecent: true })

  const rows = stream.events
    .filter((ev) => ev.body.case === 'sessionChange')
    .slice(0, RENDER_CAP)

  return (
    <>
      <PageHeader
        title="Session activity"
        description="Created / touched / destroyed lifecycle events. Tokens are non-reversible prefixes."
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
            <span>Kind</span>
            <span>User</span>
            <span>Token</span>
            <span>Last route</span>
            <span className="text-right">IP</span>
          </div>
          {rows.length === 0 && (
            <div className="border-t border-t10 px-4 py-8 text-center text-[12px] text-t26">
              {stream.connected ? 'No events — stream is quiet' : 'Waiting for events…'}
            </div>
          )}
          {rows.map((ev, idx) => {
            if (ev.body.case !== 'sessionChange') return null
            const s = ev.body.value
            return (
              <div
                key={`${ev.nodeId}-${idx}`}
                className="grid items-center border-t border-t10 px-4 py-[6px] font-mono text-[11.5px] hover:bg-t7"
                style={{ gridTemplateColumns: GRID }}
              >
                <span className="text-t25">{formatTime(timestampToDate(ev.timestamp))}</span>
                <span className="truncate text-t32">{ev.nodeId}</span>
                <span>
                  <KindPill kind={s.kind} />
                </span>
                <span className="truncate pr-2 text-t42">{s.userId || '—'}</span>
                <span className="truncate pr-2 text-t32">{s.tokenShort || '—'}</span>
                <span className="truncate pr-2.5 text-t36" title={s.lastRoute}>
                  {s.lastRoute || '—'}
                </span>
                <span className="truncate text-right text-t25">{s.ip || '—'}</span>
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
      return { label: 'created', color: SEMANTIC.green }
    case SessionChangeEvent_Kind.TOUCHED:
      return { label: 'touched', color: SEMANTIC.blue }
    case SessionChangeEvent_Kind.DESTROYED:
      return { label: 'destroyed', color: SEMANTIC.red }
    default:
      return { label: 'unspecified', color: 'var(--t32)' }
  }
}
