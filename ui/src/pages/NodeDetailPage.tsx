// Node detail (redesign screen 7). Honest-UI notes: agents now ship
// HostMetrics with every heartbeat (NodeInfo.hostMetrics via the 3s poll), so
// the metric cards show real values with client-side rolling sparklines; the
// "awaiting agent metrics" placeholders remain only for agents that don't
// report yet. Recent activity is a live node-filtered HTTP+SQL stream (the
// node_id correlation was fixed upstream so per-node filtering works).
import { useMemo } from 'react'
import { Filter, EventType, type NodeInfo } from '@/gen/nucleus/admin/v1/admin_pb'
import { useNodes } from '@/hooks/useNodes'
import { useHostMetricSeries } from '@/hooks/useHostMetricSeries'
import { useStreamEvents } from '@/hooks/useStreamEvents'
import { PageBody } from '@/components/Page'
import { Card, Chip, Dot, Label } from '@/components/ui'
import { SEMANTIC, methodColor, sqlKindColor, statusColor } from '@/lib/colors'
import { formatRelative, formatTime, streamRowKey, timestampToDate } from '@/lib/format'
import { NodeStatusPill } from '@/pages/NodesPage'
import { HostMetricCards } from '@/pages/MetricsPage'

/** Coarse uptime from startedAt (local helper; format.ts has no uptime fn). */
function formatUptime(started: Date | undefined): string {
  if (!started) return '—'
  let s = Math.max(0, Math.floor((Date.now() - started.getTime()) / 1000))
  const d = Math.floor(s / 86_400)
  s -= d * 86_400
  const h = Math.floor(s / 3600)
  s -= h * 3600
  const m = Math.floor(s / 60)
  s -= m * 60
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}

const METRIC_SLOTS = ['CPU', 'Memory RSS', 'Goroutines', 'Heap alloc', 'GC pause p99'] as const

export function NodeDetailPage(props: { nodeId: string }) {
  const { nodes, isLoading } = useNodes()
  const node = nodes.find((n) => n.nodeId === props.nodeId)
  const hostSeries = useHostMetricSeries(props.nodeId, node?.hostMetrics)

  return (
    <>
      <DetailHeader nodeId={props.nodeId} node={node} />
      <PageBody className="flex flex-col gap-4">
        {node === undefined ? (
          <Card className="px-[17px] py-[13px] text-[12.5px] text-t32">
            {isLoading ? 'Loading…' : `Node "${props.nodeId}" is not registered with this admin server.`}
          </Card>
        ) : (
          <>
            <InfoStrip node={node} />
            {node.connected ? (
              <div
                className="grid gap-3.5"
                style={{ gridTemplateColumns: 'repeat(3,minmax(0,1fr))' }}
              >
                {node.hostMetrics !== undefined ? (
                  <HostMetricCards hm={node.hostMetrics} series={hostSeries} />
                ) : (
                  METRIC_SLOTS.map((label) => (
                    <PlaceholderMetricCard key={label} label={label} />
                  ))
                )}
              </div>
            ) : (
              <div
                className="rounded-[10px] border px-[17px] py-[13px] text-[12.5px]"
                style={{
                  color: SEMANTIC.amber,
                  borderColor: `color-mix(in srgb, ${SEMANTIC.amber} 30%, transparent)`,
                  background: `color-mix(in srgb, ${SEMANTIC.amber} 8%, transparent)`,
                }}
              >
                Agent disconnected — no live metrics for this node. Last report{' '}
                {formatRelative(timestampToDate(node.lastSeenAt))}; showing registration info only.
              </div>
            )}
            <RecentActivityCard nodeId={props.nodeId} connected={node.connected} />
          </>
        )}
      </PageBody>
    </>
  )
}

function DetailHeader(props: { nodeId: string; node: NodeInfo | undefined }) {
  const { node } = props
  const labels = node ? Object.entries(node.labels).sort(([a], [b]) => a.localeCompare(b)) : []
  const online = node?.connected ?? false

  return (
    <header className="flex items-start justify-between gap-4 border-b border-t14 px-7 pb-4 pt-3.5">
      <div className="min-w-0">
        <button
          type="button"
          onClick={() => {
            window.location.hash = '#/nodes'
          }}
          className="text-[12.5px] text-t32 transition-colors hover:text-t45"
        >
          ← All nodes
        </button>
        <h1 className="mb-0 mt-[5px] flex items-center gap-[9px] font-mono text-[17px] font-semibold text-t46">
          <Dot color={online ? SEMANTIC.green : 'var(--t26)'} size={8} pulse={online} />
          <span className="truncate">{props.nodeId}</span>
        </h1>
        {labels.length > 0 && (
          <div className="mt-[7px] flex flex-wrap gap-1.5">
            {labels.map(([k, v]) => (
              <Chip key={k}>
                {k}={v}
              </Chip>
            ))}
          </div>
        )}
      </div>
      {node !== undefined && (
        <div className="shrink-0">
          <NodeStatusPill online={online} />
        </div>
      )}
    </header>
  )
}

// Five columns, all real: no Go runtime / host metric slots because the agent
// does not report them. Transport is truthful — agents attach over the
// ControlService gRPC stream.
function InfoStrip(props: { node: NodeInfo }) {
  const n = props.node
  const cells: Array<{ label: string; value: string; mono: boolean }> = [
    { label: 'Version', value: n.version || '—', mono: true },
    { label: 'Uptime', value: formatUptime(timestampToDate(n.startedAt)), mono: false },
    { label: 'Started', value: formatRelative(timestampToDate(n.startedAt)), mono: false },
    { label: 'Last seen', value: formatRelative(timestampToDate(n.lastSeenAt)), mono: false },
    {
      label: 'Transport',
      value: n.connected ? 'grpc-stream · connected' : 'grpc-stream · disconnected',
      mono: true,
    },
  ]
  return (
    <Card className="grid grid-cols-5 gap-3.5 px-[17px] py-3.5">
      {cells.map((c) => (
        <div key={c.label} className="min-w-0">
          <Label>{c.label}</Label>
          <div
            className={[
              'mt-[5px] truncate text-[13px] text-t44',
              c.mono ? 'font-mono text-[12px]' : '',
            ].join(' ')}
          >
            {c.value}
          </div>
        </div>
      ))}
    </Card>
  )
}

// Fallback when this node's agent does not report host metrics (older agent,
// or no heartbeat with metrics yet): em-dash values, no fake sparklines.
function PlaceholderMetricCard(props: { label: string }) {
  return (
    <Card className="px-[17px] py-[15px]">
      <div className="flex items-baseline justify-between">
        <Label>{props.label}</Label>
        <span className="font-mono text-[10.5px] text-t26">n/a</span>
      </div>
      <div className="mt-2 text-[24px] font-semibold tabular-nums text-t46">—</div>
      <div className="mt-2.5 font-mono text-[10.5px] text-t26">awaiting agent metrics</div>
    </Card>
  )
}

const ACTIVITY_RENDER = 15

// RecentActivityCard is a live HTTP+SQL feed scoped to this node. It reuses
// the same stream the HTTP/SQL pages do, filtered by node id (correlation
// fixed upstream), and includes the server's recent-event replay so the panel
// shows something immediately.
function RecentActivityCard(props: { nodeId: string; connected: boolean }) {
  const filter = useMemo(
    () =>
      new Filter({
        types: [EventType.HTTP_REQUEST, EventType.SQL_STATEMENT],
        nodeIds: [props.nodeId],
      }),
    [props.nodeId],
  )
  const stream = useStreamEvents({ filter, bufferSize: 60, includeRecent: true })
  const rows = stream.events
    .filter((ev) => ev.body.case === 'httpRequest' || ev.body.case === 'sqlStatement')
    .slice(0, ACTIVITY_RENDER)

  return (
    <div className="overflow-hidden rounded-[10px] border border-t18 bg-t4">
      <div className="flex items-center justify-between border-b border-t14 px-4 py-[11px]">
        <span className="text-[12.5px] font-semibold text-t46">
          Recent activity{' '}
          <span className="text-[11px] font-normal text-t26">— HTTP + SQL on this node</span>
        </span>
        <span className="flex items-center gap-1.5 font-mono text-[10.5px] text-t26">
          <Dot color={stream.connected ? SEMANTIC.green : SEMANTIC.red} size={6} pulse={stream.connected} />
          {stream.connected ? 'live' : 'reconnecting'}
        </span>
      </div>
      {rows.length === 0 ? (
        <div className="px-4 py-[18px] text-[12px] text-t26">
          {props.connected
            ? 'No recent HTTP or SQL activity on this node yet.'
            : 'Agent disconnected — showing buffered activity only.'}
        </div>
      ) : (
        <div className="min-w-0">
          {rows.map((ev, idx) => {
            const time = formatTime(timestampToDate(ev.timestamp))
            if (ev.body.case === 'httpRequest') {
              const h = ev.body.value
              return (
                <div
                  key={streamRowKey(ev.nodeId, ev.timestamp, idx)}
                  className="grid items-center gap-2 border-t border-t10 px-4 py-[6px] font-mono text-[11px]"
                  style={{ gridTemplateColumns: '84px 52px minmax(0,1fr) 46px' }}
                >
                  <span className="text-t25">{time}</span>
                  <span className="font-semibold" style={{ color: methodColor(h.method) }}>
                    {h.method}
                  </span>
                  <span className="truncate text-t39" title={h.path}>
                    {h.path}
                  </span>
                  <span className="text-right" style={{ color: statusColor(h.status) }}>
                    {h.status}
                  </span>
                </div>
              )
            }
            if (ev.body.case !== 'sqlStatement') return null
            const s = ev.body.value
            return (
              <div
                key={streamRowKey(ev.nodeId, ev.timestamp, idx)}
                className="grid items-center gap-2 border-t border-t10 px-4 py-[6px] font-mono text-[11px]"
                style={{ gridTemplateColumns: '84px 52px minmax(0,1fr) 46px' }}
              >
                <span className="text-t25">{time}</span>
                <span className="font-semibold" style={{ color: sqlKindColor(s.operation) }}>
                  {s.operation.toUpperCase().slice(0, 6)}
                </span>
                <span className="truncate text-t39" title={s.query}>
                  {s.query}
                </span>
                <span className="text-right text-t26">SQL</span>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
