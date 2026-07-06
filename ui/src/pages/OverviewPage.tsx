// Overview — "panorama" variant (design handoff "Orbit Admin", screen 1).
// Replaces the old DashboardPage. All numbers are derived from real signals:
// the event streams (HTTP / SQL / sessions) and ListNodes metadata. No host
// metrics exist in the backend yet, so the DB pool KPI honestly renders an
// em-dash with "awaiting agent metrics" instead of a fake sparkline, and the
// fleet table shows Started / Last seen / Status instead of CPU / Mem / Gor.
import { useMemo, type ReactNode } from 'react'
import { PageBody, PageHeader } from '@/components/Page'
import { Sparkline } from '@/components/Sparkline'
import { Card, Dot, Label, Pill } from '@/components/ui'
import { SEMANTIC, methodColor, sqlKindColor, statusColor } from '@/lib/colors'
import { useNodes } from '@/hooks/useNodes'
import { useFleetStats, formatRate } from '@/hooks/useFleetStats'
import type { Event, NodeInfo } from '@/gen/nucleus/admin/v1/admin_pb'
import {
  durationToMillis,
  formatDuration,
  formatRelative,
  formatTime,
  timestampToDate,
} from '@/lib/format'

// Real-column variant of the prototype's fleet grid (CPU/Mem/Gor. are not
// reported by any agent yet): Node / Version / Started / Last seen / Status.
const FLEET_GRID = 'minmax(0,1fr) 96px 92px 92px 64px'
const HTTP_FEED_GRID = '88px 52px minmax(0,1fr) 40px 66px'
const SQL_FEED_GRID = '88px 58px minmax(0,1fr) 66px'

const FEED_LENGTH = 7
const FRESH_MS = 10_000

export function OverviewPage() {
  const { nodes } = useNodes()
  const { fleet, events, connected } = useFleetStats()

  const online = nodes.filter((n) => n.connected).length

  // Fleet "main" version = most common among connected nodes (all as fallback).
  const mainVersion = useMemo(() => {
    const pool = nodes.filter((n) => n.connected)
    const counts = new Map<string, number>()
    for (const n of (pool.length > 0 ? pool : nodes)) {
      counts.set(n.version, (counts.get(n.version) ?? 0) + 1)
    }
    let best = ''
    let bestCount = 0
    for (const [v, c] of counts) {
      if (c > bestCount) {
        best = v
        bestCount = c
      }
    }
    return best
  }, [nodes])

  const httpFeed = events.filter((ev) => ev.body.case === 'httpRequest').slice(0, FEED_LENGTH)
  const sqlFeed = events.filter((ev) => ev.body.case === 'sqlStatement').slice(0, FEED_LENGTH)

  const checks = healthChecks(nodes, connected)
  const healthy = nodes.length > 0 && checks.every((c) => c.ok)

  const errColor = fleet.errorRatePct > 2 ? SEMANTIC.red : SEMANTIC.green

  return (
    <>
      <PageHeader
        title="Overview"
        description="360° view of the fleet — live, sampled every second."
        actions={
          connected ? (
            <Pill color={SEMANTIC.green} pulse>
              Live
            </Pill>
          ) : (
            <Pill color={SEMANTIC.amber}>Reconnecting</Pill>
          )
        }
      />
      <PageBody className="flex flex-col gap-[18px]">
        {/* KPI row — 6 cards */}
        <div className="grid gap-3" style={{ gridTemplateColumns: 'repeat(6,minmax(0,1fr))' }}>
          <KpiCard
            label="Req/s"
            value={formatRate(fleet.rps)}
            color="var(--accent)"
            spark={fleet.rpsSeries}
            sub="HTTP events/s · 60s window"
          />
          <KpiCard
            label="Latency p95"
            value={fleet.hasHttpTraffic ? formatMs(fleet.p95Ms) : '—'}
            unit={fleet.hasHttpTraffic ? 'ms' : undefined}
            color={SEMANTIC.amber}
            spark={fleet.p95Series}
            sub="HTTP p95 · 60s window"
          />
          <KpiCard
            label="Error rate"
            value={fleet.hasHttpTraffic ? fleet.errorRatePct.toFixed(1) : '—'}
            unit={fleet.hasHttpTraffic ? '%' : undefined}
            color={errColor}
            spark={fleet.errorSeries}
            sub="5xx share · 60s window"
          />
          <KpiCard
            label="Nodes online"
            value={String(online)}
            unit={`/ ${nodes.length}`}
            sub="ListNodes · 3s poll"
          />
          <KpiCard
            label="Sessions"
            value={String(fleet.sessionEventsPerMin)}
            unit="/min"
            color={SEMANTIC.violet}
            spark={fleet.sessionSeries}
            sub="session events · 60s window"
          />
          <KpiCard label="DB pool" value="—" sub="awaiting agent metrics" />
        </div>

        {/* Row 1 — Fleet table + Health list */}
        <div className="grid gap-3.5" style={{ gridTemplateColumns: '1.25fr 1fr' }}>
          <Card className="overflow-hidden">
            <FeedHeader title="Fleet" right={<FeedLink href="#/nodes">All nodes →</FeedLink>} />
            <div
              className="grid px-4 py-[7px] text-[10px] font-semibold uppercase tracking-[.08em] text-t26"
              style={{ gridTemplateColumns: FLEET_GRID }}
            >
              <span>Node</span>
              <span>Version</span>
              <span>Started</span>
              <span>Last seen</span>
              <span>Status</span>
            </div>
            {nodes.length === 0 && <EmptyRow>No nodes registered</EmptyRow>}
            {nodes.map((n) => (
              <div
                key={n.nodeId}
                onClick={() => {
                  window.location.hash = `#/nodes/${encodeURIComponent(n.nodeId)}`
                }}
                className="grid cursor-pointer items-center border-t border-t12 px-4 py-2 font-mono text-[11.5px] hover:bg-t7"
                style={{ gridTemplateColumns: FLEET_GRID }}
              >
                <span className="flex min-w-0 items-center gap-[7px] text-t43">
                  <Dot color={n.connected ? SEMANTIC.green : SEMANTIC.red} size={6} />
                  <span className="truncate">{n.nodeId}</span>
                </span>
                <span
                  className="truncate pr-2"
                  style={{
                    color: n.version && n.version !== mainVersion ? SEMANTIC.amber : 'var(--t36)',
                  }}
                >
                  {n.version || '—'}
                </span>
                <span className="text-t34">{formatRelative(timestampToDate(n.startedAt))}</span>
                <span className="text-t34">{formatRelative(timestampToDate(n.lastSeenAt))}</span>
                <span style={{ color: n.connected ? SEMANTIC.green : 'var(--t26)' }}>
                  {n.connected ? 'online' : 'offline'}
                </span>
              </div>
            ))}
          </Card>

          <Card className="overflow-hidden">
            <FeedHeader
              title="Health"
              right={
                <Pill color={healthy ? SEMANTIC.green : SEMANTIC.amber}>
                  {healthy ? 'Healthy' : 'Degraded'}
                </Pill>
              }
            />
            {checks.length === 0 && <EmptyRow>No checks available</EmptyRow>}
            {checks.map((c) => (
              <div
                key={c.name}
                className="flex items-center justify-between gap-2.5 border-t border-t12 px-4 py-2"
              >
                <span className="flex min-w-0 items-center gap-2 text-[12.5px] text-t41">
                  <Dot color={c.dot} size={7} />
                  <span className="truncate">{c.name}</span>
                </span>
                <span className="shrink-0 font-mono text-[11px] text-t29">{c.detail}</span>
              </div>
            ))}
          </Card>
        </div>

        {/* Row 2 — HTTP feed + SQL feed */}
        <div className="grid gap-3.5" style={{ gridTemplateColumns: '1fr 1fr' }}>
          <Card className="overflow-hidden">
            <FeedHeader title="HTTP requests" right={<FeedLink href="#/http">Open stream →</FeedLink>} />
            {httpFeed.length === 0 && <EmptyRow>No HTTP events yet</EmptyRow>}
            {httpFeed.map((ev, i) => {
              if (ev.body.case !== 'httpRequest') return null
              const http = ev.body.value
              return (
                <div
                  key={feedKey(ev, i)}
                  className="grid items-center border-t border-t10 px-4 py-[5.5px] font-mono text-[11px]"
                  style={{ gridTemplateColumns: HTTP_FEED_GRID }}
                >
                  <span className="text-t25">{formatTime(timestampToDate(ev.timestamp))}</span>
                  <span className="font-semibold" style={{ color: methodColor(http.method) }}>
                    {http.method}
                  </span>
                  <span className="truncate pr-2.5 text-t40" title={http.path}>
                    {http.path}
                  </span>
                  <span className="text-right" style={{ color: statusColor(http.status) }}>
                    {http.status}
                  </span>
                  <span className="text-right text-t34 tabular-nums">
                    {formatDuration(durationToMillis(http.duration))}
                  </span>
                </div>
              )
            })}
          </Card>

          <Card className="overflow-hidden">
            <FeedHeader title="SQL statements" right={<FeedLink href="#/sql">Open stream →</FeedLink>} />
            {sqlFeed.length === 0 && <EmptyRow>No SQL events yet</EmptyRow>}
            {sqlFeed.map((ev, i) => {
              if (ev.body.case !== 'sqlStatement') return null
              const sql = ev.body.value
              const kind = (sql.operation || 'SQL').toUpperCase()
              return (
                <div
                  key={feedKey(ev, i)}
                  className="grid items-center border-t border-t10 px-4 py-[5.5px] font-mono text-[11px]"
                  style={{ gridTemplateColumns: SQL_FEED_GRID }}
                >
                  <span className="text-t25">{formatTime(timestampToDate(ev.timestamp))}</span>
                  <span className="font-semibold" style={{ color: sqlKindColor(kind) }}>
                    {kind}
                  </span>
                  <span className="truncate pr-2.5 text-t38" title={sql.query}>
                    {sql.query}
                  </span>
                  <span className="text-right text-t34 tabular-nums">
                    {formatDuration(durationToMillis(sql.duration))}
                  </span>
                </div>
              )
            })}
          </Card>
        </div>
      </PageBody>
    </>
  )
}

/* ---------- KPI card ---------- */

function KpiCard(props: {
  label: string
  value: string
  unit?: string | undefined
  color?: string | undefined
  spark?: readonly number[] | undefined
  sub: string
}) {
  return (
    <Card className="min-w-0">
      <div style={{ padding: '13px 15px 11px' }}>
        <Label>{props.label}</Label>
        <div className="mt-[7px] flex items-end justify-between gap-2">
          <div
            className="text-[22px] font-semibold leading-none tabular-nums"
            style={{ color: props.color ?? 'var(--t46)' }}
          >
            {props.value}
            {props.unit !== undefined && (
              <span className="ml-[3px] text-[12px] font-normal text-t29">{props.unit}</span>
            )}
          </div>
          {props.spark !== undefined && (
            <Sparkline data={props.spark} width={72} height={26} color={props.color ?? 'var(--accent)'} />
          )}
        </div>
        <div className="mt-1.5 font-mono text-[10.5px] text-t26">{props.sub}</div>
      </div>
    </Card>
  )
}

/* ---------- Card scaffolding ---------- */

function FeedHeader(props: { title: string; right?: ReactNode }) {
  return (
    <div className="flex items-center justify-between border-b border-t14 px-4 py-[11px]">
      <span className="text-[12.5px] font-semibold text-t41">{props.title}</span>
      {props.right}
    </div>
  )
}

function FeedLink(props: { href: string; children: ReactNode }) {
  return (
    <a href={props.href} className="text-[11.5px] text-accent no-underline hover:brightness-110">
      {props.children}
    </a>
  )
}

function EmptyRow(props: { children: ReactNode }) {
  return (
    <div className="border-t border-t12 px-4 py-6 text-center text-[12px] text-t26">
      {props.children}
    </div>
  )
}

function feedKey(ev: Event, i: number): string {
  return `${ev.nodeId}-${ev.timestamp?.seconds ?? 0}-${ev.timestamp?.nanos ?? 0}-${i}`
}

/* ---------- Derivations ---------- */

function formatMs(ms: number): string {
  return ms >= 100 ? ms.toFixed(0) : ms.toFixed(1)
}

interface HealthCheck {
  name: string
  dot: string
  detail: string
  ok: boolean
}

// Freshness logic shared with the Health page's semantics: a node is healthy
// when its agent stream is connected AND its last heartbeat is < 10 s old.
function healthChecks(nodes: NodeInfo[], streamConnected: boolean): HealthCheck[] {
  const now = Date.now()
  const checks: HealthCheck[] = [
    {
      name: 'Admin event stream',
      dot: streamConnected ? SEMANTIC.green : SEMANTIC.amber,
      detail: streamConnected ? 'grpc-stream · connected' : 'reconnecting…',
      ok: streamConnected,
    },
  ]
  for (const n of nodes) {
    const seen = timestampToDate(n.lastSeenAt)
    const fresh = seen !== undefined && now - seen.getTime() < FRESH_MS
    const ok = n.connected && fresh
    checks.push({
      name: `agent ${shortId(n.nodeId)}`,
      dot: !n.connected ? SEMANTIC.red : fresh ? SEMANTIC.green : SEMANTIC.amber,
      detail: `${n.version || 'unknown'} · seen ${formatRelative(seen)}`,
      ok,
    })
  }
  return checks
}

function shortId(id: string): string {
  return id.length > 14 ? `${id.slice(0, 12)}…` : id
}
