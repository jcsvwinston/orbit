// Metrics — per-node view (design handoff "Orbit Admin", screen 2).
// DATA REALITY: agents now ship HostMetrics on every heartbeat (surfaced via
// NodeInfo.hostMetrics on the useNodes 3s poll), so the six host cards render
// real values with client-side rolling sparklines (useHostMetricSeries). The
// em-dash + "awaiting agent metrics" fallback remains for older agents that
// don't report yet. The two leading cards are derived from the HTTP event
// stream filtered client-side to the selected node (60-sample window).
import { useMemo, useState } from 'react'
import { PageBody, PageHeader } from '@/components/Page'
import { Card, Chip, Dot, Label, Progress, Segmented } from '@/components/ui'
import { SEMANTIC } from '@/lib/colors'
import { t } from '@/lib/i18n'
import { useNodes } from '@/hooks/useNodes'
import { useFleetStats, formatRate } from '@/hooks/useFleetStats'
import { useHostMetricSeries, type HostMetricSeries } from '@/hooks/useHostMetricSeries'
import { timestampToDate } from '@/lib/format'
import type { HostMetrics } from '@/gen/nucleus/admin/v1/admin_pb'

const PLACEHOLDER_CARDS = [
  { label: t.metrics.cardCPU, sub: t.metrics.subHost },
  { label: t.metrics.cardMemoryRSS, sub: t.metrics.subHost },
  { label: t.metrics.cardGoroutines, sub: t.metrics.subRuntime },
  { label: t.metrics.cardHeapAlloc, sub: t.metrics.subRuntime },
  { label: t.metrics.cardGCPauseP99, sub: t.metrics.subRuntime },
  { label: t.metrics.cardDbPool, sub: t.metrics.subSql },
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
  const hostSeries = useHostMetricSeries(current ?? '', info?.hostMetrics)

  const tabs = useMemo(
    () => nodes.map((n) => ({ id: n.nodeId, label: shortId(n.nodeId) })),
    [nodes],
  )

  return (
    <>
      <PageHeader
        title={t.metrics.title}
        description={t.metrics.description}
        actions={
          tabs.length > 0 ? (
            <Segmented options={tabs} value={current ?? ''} onChange={setSelected} />
          ) : undefined
        }
      />
      <PageBody className="flex flex-col gap-4">
        {info === undefined ? (
          <div className="rounded-[10px] border border-t18 bg-t5 px-4 py-10 text-center text-[12px] text-t26">
            {t.metrics.noAgents}
          </div>
        ) : (
          <>
            {/* Node identity strip */}
            <div className="flex flex-wrap items-center gap-[18px] font-mono text-[11.5px] text-t32">
              <span className="flex items-center gap-[7px] text-t43">
                <Dot color={info.connected ? SEMANTIC.green : SEMANTIC.red} size={7} />
                {info.nodeId}
              </span>
              <span>{info.version || t.common.unknown}</span>
              <span>{t.metrics.uptime(formatUptime(timestampToDate(info.startedAt)))}</span>
              {Object.entries(info.labels).map(([k, v]) => (
                <Chip key={k}>
                  {k}={v}
                </Chip>
              ))}
            </div>

            {/* Metric cards */}
            <div className="grid gap-3.5" style={{ gridTemplateColumns: 'repeat(3,minmax(0,1fr))' }}>
              <RealMetricCard
                label={t.metrics.cardReqPerSecNode}
                sub={t.metrics.subHttpWindow}
                value={formatRate(nodeStats?.rps ?? 0)}
                data={nodeStats?.rpsSeries ?? []}
                color="var(--accent)"
              />
              <RealMetricCard
                label={t.metrics.cardRequestsSeen}
                sub={t.metrics.subClientBuffer}
                value={String(nodeStats?.requestsSeen ?? 0)}
                unit="req"
                data={nodeStats?.requestsSeenSeries ?? []}
                color={SEMANTIC.blue}
              />
              {info.hostMetrics !== undefined ? (
                <HostMetricCards hm={info.hostMetrics} series={hostSeries} />
              ) : (
                PLACEHOLDER_CARDS.map((c) => (
                  <AwaitingCard key={c.label} label={c.label} sub={c.sub} />
                ))
              )}
            </div>
          </>
        )}
      </PageBody>
    </>
  )
}

/* ---------- Cards ---------- */

// The six host/runtime metric cards, shared with NodeDetailPage. Values come
// straight from the node's latest HostMetrics sample; sparklines from the
// client-side rolling window (useHostMetricSeries).
export function HostMetricCards(props: { hm: HostMetrics; series: HostMetricSeries }) {
  const { hm, series } = props
  const rssMB = Number(hm.rssBytes) / 1_048_576
  // rss is Linux-only in the agent; 0 means "platform can't report".
  const hasRss = rssMB > 0
  return (
    <>
      <RealMetricCard
        label={t.metrics.cardCPU}
        sub={t.metrics.subHost}
        value={hm.cpuPercent.toFixed(1)}
        unit="%"
        data={series.cpu}
        color="var(--accent)"
      />
      <RealMetricCard
        label={t.metrics.cardMemoryRSS}
        sub={t.metrics.subHost}
        value={hasRss ? rssMB.toFixed(1) : t.common.empty}
        unit={hasRss ? 'MB' : undefined}
        data={series.rssMB}
        color={SEMANTIC.blue}
        caption={hasRss ? undefined : t.metrics.rssNotReported}
      />
      <RealMetricCard
        label={t.metrics.cardGoroutines}
        sub={t.metrics.subRuntime}
        value={String(hm.goroutines)}
        data={series.goroutines}
        color={SEMANTIC.violet}
      />
      <RealMetricCard
        label={t.metrics.cardHeapAlloc}
        sub={t.metrics.subRuntime}
        value={(Number(hm.heapAllocBytes) / 1_048_576).toFixed(1)}
        unit="MB"
        data={series.heapMB}
        color={SEMANTIC.green}
      />
      <RealMetricCard
        label={t.metrics.cardGCPauseP99}
        sub={t.metrics.subRuntime}
        value={hm.gcPauseP99Ms.toFixed(2)}
        unit="ms"
        data={series.gcMs}
        color={SEMANTIC.amber}
      />
      <DbPoolCard hm={hm} />
    </>
  )
}

function RealMetricCard(props: {
  label: string
  sub: string
  value: string
  unit?: string | undefined
  data: readonly number[]
  color: string
  /** When set, replaces the sparkline row (e.g. rss not reported). */
  caption?: string | undefined
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
        {props.caption !== undefined ? (
          <div className="mt-2.5 flex h-[38px] items-center font-mono text-[10.5px] text-t26">
            {props.caption}
          </div>
        ) : (
          <WideSparkline data={props.data} color={props.color} />
        )}
      </div>
    </Card>
  )
}

// DB pool card per design: 6px usage bar instead of a sparkline. dbMaxOpen 0
// means "unlimited" in database/sql, so no ratio can be drawn — value stays
// an em-dash and the caption carries the live counters.
function DbPoolCard(props: { hm: HostMetrics }) {
  const { dbInUse, dbIdle, dbMaxOpen } = props.hm
  const hasMax = dbMaxOpen > 0
  return (
    <Card>
      <div style={{ padding: '15px 17px' }}>
        <div className="flex items-baseline justify-between gap-2">
          <Label>{t.metrics.cardDbPool}</Label>
          <span className="font-mono text-[10.5px] text-t26">{t.metrics.subSql}</span>
        </div>
        <div className="mt-2 text-[24px] font-semibold tabular-nums text-t46">
          {hasMax ? (
            <>
              {dbInUse}
              <span className="ml-[3px] text-[12px] font-normal text-t29">/ {dbMaxOpen}</span>
            </>
          ) : (
            '—'
          )}
        </div>
        <div className="mt-2.5 flex h-[38px] flex-col justify-center gap-2">
          {hasMax && (
            <Progress pct={(dbInUse / dbMaxOpen) * 100} height={6} color="var(--accent)" />
          )}
          <div className="font-mono text-[10.5px] tabular-nums text-t26">
            {t.metrics.dbPoolCaption(dbInUse, dbIdle, hasMax ? String(dbMaxOpen) : t.common.empty)}
          </div>
        </div>
      </div>
    </Card>
  )
}

// Host/runtime metric this agent does not report (older agent, or no
// heartbeat with metrics yet): honest placeholder, no fake sparkline
// (handoff "Data reality").
function AwaitingCard(props: { label: string; sub: string }) {
  return (
    <Card>
      <div style={{ padding: '15px 17px' }}>
        <div className="flex items-baseline justify-between gap-2">
          <Label>{props.label}</Label>
          <span className="font-mono text-[10.5px] text-t26">{props.sub}</span>
        </div>
        <div className="mt-2 text-[24px] font-semibold tabular-nums text-t26">{t.common.empty}</div>
        <div className="mt-2.5 flex h-[38px] items-center font-mono text-[10.5px] text-t26">
          {t.metrics.awaitingAgentMetrics}
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
  if (!start) return t.common.empty
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
