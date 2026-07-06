// Metrics — per-node view (design handoff "Orbit Admin", screen 2).
// DATA REALITY: no agent ships host metrics yet (CPU, memory RSS, goroutines,
// heap, GC pause, DB pool). Those six cards render an em-dash plus the muted
// mono caption "awaiting agent metrics" — no fake sparklines. The two leading
// cards ARE real: Req/s and Requests seen, derived from the HTTP event stream
// filtered client-side to the selected node (60-sample window, 1 sample/s).
import { useMemo, useState } from 'react'
import { PageBody, PageHeader } from '@/components/Page'
import { Card, Chip, Dot, Label, Segmented } from '@/components/ui'
import { SEMANTIC } from '@/lib/colors'
import { useNodes } from '@/hooks/useNodes'
import { useFleetStats, formatRate } from '@/hooks/useFleetStats'
import { timestampToDate } from '@/lib/format'

const PLACEHOLDER_CARDS = [
  { label: 'CPU', sub: 'host' },
  { label: 'Memory RSS', sub: 'host' },
  { label: 'Goroutines', sub: 'runtime' },
  { label: 'Heap alloc', sub: 'runtime' },
  { label: 'GC pause p99', sub: 'runtime' },
  { label: 'DB pool', sub: 'sql' },
] as const

export function MetricsPage() {
  const { nodes } = useNodes()
  const [selected, setSelected] = useState<string | null>(null)

  // Fall back to the first node until the user picks one (or if the picked
  // node disappears from the fleet).
  const current = useMemo(() => {
    if (selected !== null && nodes.some((n) => n.nodeId === selected)) return selected
    return nodes.length > 0 ? nodes[0].nodeId : null
  }, [selected, nodes])

  const { node: nodeStats } = useFleetStats(current)
  const info = nodes.find((n) => n.nodeId === current)

  const tabs = useMemo(
    () => nodes.map((n) => ({ id: n.nodeId, label: shortId(n.nodeId) })),
    [nodes],
  )

  return (
    <>
      <PageHeader
        title="Metrics"
        description="Runtime and resource consumption per node — 60 s window."
        actions={
          tabs.length > 0 ? (
            <Segmented options={tabs} value={current ?? ''} onChange={setSelected} />
          ) : undefined
        }
      />
      <PageBody className="flex flex-col gap-4">
        {info === undefined ? (
          <div className="rounded-[10px] border border-t18 bg-t5 px-4 py-10 text-center text-[12px] text-t26">
            No agents connected — per-node metrics unavailable.
          </div>
        ) : (
          <>
            {/* Node identity strip */}
            <div className="flex flex-wrap items-center gap-[18px] font-mono text-[11.5px] text-t32">
              <span className="flex items-center gap-[7px] text-t43">
                <Dot color={info.connected ? SEMANTIC.green : SEMANTIC.red} size={7} />
                {info.nodeId}
              </span>
              <span>{info.version || 'unknown'}</span>
              <span>up {formatUptime(timestampToDate(info.startedAt))}</span>
              {Object.entries(info.labels).map(([k, v]) => (
                <Chip key={k}>
                  {k}={v}
                </Chip>
              ))}
            </div>

            {/* Metric cards */}
            <div className="grid gap-3.5" style={{ gridTemplateColumns: 'repeat(3,minmax(0,1fr))' }}>
              <RealMetricCard
                label="Req/s (node)"
                sub="HTTP · 60s window"
                value={formatRate(nodeStats?.rps ?? 0)}
                data={nodeStats?.rpsSeries ?? []}
                color="var(--accent)"
              />
              <RealMetricCard
                label="Requests seen"
                sub="client buffer"
                value={String(nodeStats?.requestsSeen ?? 0)}
                unit="req"
                data={nodeStats?.requestsSeenSeries ?? []}
                color={SEMANTIC.blue}
              />
              {PLACEHOLDER_CARDS.map((c) => (
                <AwaitingCard key={c.label} label={c.label} sub={c.sub} />
              ))}
            </div>
          </>
        )}
      </PageBody>
    </>
  )
}

/* ---------- Cards ---------- */

function RealMetricCard(props: {
  label: string
  sub: string
  value: string
  unit?: string
  data: readonly number[]
  color: string
}) {
  return (
    <Card>
      <div style={{ padding: '15px 17px' }}>
        <div className="flex items-baseline justify-between gap-2">
          <Label>{props.label}</Label>
          <span className="font-mono text-[10.5px] text-t26">{props.sub}</span>
        </div>
        <div className="mt-2 text-[24px] font-semibold tabular-nums text-t46">
          {props.value}
          {props.unit !== undefined && (
            <span className="ml-[3px] text-[12px] font-normal text-t29">{props.unit}</span>
          )}
        </div>
        <WideSparkline data={props.data} color={props.color} />
      </div>
    </Card>
  )
}

// Host/runtime metric the backend does not report yet: honest placeholder,
// no fake sparkline (handoff "Data reality").
function AwaitingCard(props: { label: string; sub: string }) {
  return (
    <Card>
      <div style={{ padding: '15px 17px' }}>
        <div className="flex items-baseline justify-between gap-2">
          <Label>{props.label}</Label>
          <span className="font-mono text-[10.5px] text-t26">{props.sub}</span>
        </div>
        <div className="mt-2 text-[24px] font-semibold tabular-nums text-t26">—</div>
        <div className="mt-2.5 flex h-[38px] items-center font-mono text-[10.5px] text-t26">
          awaiting agent metrics
        </div>
      </div>
    </Card>
  )
}

/** Full-width 38px area+line sparkline (viewBox 160×40, stretch to fit). */
function WideSparkline(props: { data: readonly number[]; color: string }) {
  const w = 160
  const h = 40
  const pad = 2
  const { data } = props
  if (data.length < 2) {
    return <svg width="100%" height={38} aria-hidden className="mt-2.5 block" />
  }
  const min = Math.min(...data)
  const max = Math.max(...data)
  const span = max - min || 1
  const step = (w - pad * 2) / (data.length - 1)
  const pts = data.map((v, i) => {
    const x = pad + i * step
    const y = pad + (h - pad * 2) * (1 - (v - min) / span)
    return `${x.toFixed(1)},${y.toFixed(1)}`
  })
  const line = pts.join(' ')
  const lastX = (pad + (data.length - 1) * step).toFixed(1)
  const area = `${pad},${h - pad} ${line} ${lastX},${h - pad}`
  return (
    <svg
      width="100%"
      height={38}
      viewBox={`0 0 ${w} ${h}`}
      preserveAspectRatio="none"
      aria-hidden
      className="mt-2.5 block"
    >
      <polygon points={area} fill={props.color} opacity={0.1} />
      <polyline
        points={line}
        fill="none"
        stroke={props.color}
        strokeWidth={1.6}
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  )
}

/* ---------- Helpers ---------- */

function shortId(id: string): string {
  return id.length > 12 ? `${id.slice(0, 10)}…` : id
}

function formatUptime(start: Date | undefined): string {
  if (!start) return '—'
  let s = Math.max(0, Math.floor((Date.now() - start.getTime()) / 1000))
  const d = Math.floor(s / 86_400)
  s -= d * 86_400
  const h = Math.floor(s / 3600)
  s -= h * 3600
  const m = Math.floor(s / 60)
  s -= m * 60
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m ${s}s`
}
