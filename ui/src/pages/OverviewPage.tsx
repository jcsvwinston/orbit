// Overview — "panorama" variant (design handoff "Orbit Admin", screen 1).
// Replaces the old DashboardPage. All numbers are derived from real signals:
// the event streams (HTTP / SQL / sessions) and ListNodes metadata — now
// including per-node HostMetrics from agent heartbeats, which feed the DB
// pool KPI as a fleet aggregate ("awaiting agent metrics" only when no node
// reports). The fleet table shows Started / Last seen / Status.
import { useMemo, type ReactNode } from 'react'
import { PageBody, PageHeader } from '@/components/Page'
import { Sparkline } from '@/components/Sparkline'
import { Card, Dot, Label, Pill } from '@/components/ui'
import { SEMANTIC, methodColor, sqlKindColor, statusColor } from '@/lib/colors'
import { t } from '@/lib/i18n'
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

  // Fleet-aggregate DB pool from connected nodes' host metrics. dbMaxOpen 0
  // means "unlimited" (database/sql), so a single unlimited node makes the
  // fleet total unbounded — show only the in-use count then.
  const dbPool = useMemo(() => {
    let reporting = false
    let inUse = 0
    let maxOpen = 0
    let unlimited = false
    for (const n of nodes) {
      if (!n.connected || n.hostMetrics === undefined) continue
      reporting = true
      inUse += n.hostMetrics.dbInUse
      if (n.hostMetrics.dbMaxOpen === 0) unlimited = true
      else maxOpen += n.hostMetrics.dbMaxOpen
    }
    return { reporting, inUse, maxOpen, unlimited }
  }, [nodes])

  const httpFeed = events.filter((ev) => ev.body.case === 'httpRequest').slice(0, FEED_LENGTH)
  const sqlFeed = events.filter((ev) => ev.body.case === 'sqlStatement').slice(0, FEED_LENGTH)

  const checks = healthChecks(nodes, connected)
  const healthy = nodes.length > 0 && checks.every((c) => c.ok)

  const errColor = fleet.errorRatePct > 2 ? SEMANTIC.red : SEMANTIC.green

  return (
    <>
      <PageHeader
        title={t.overview.title}
        description={t.overview.description}
        actions={
          connected ? (
            <Pill color={SEMANTIC.green} pulse>
              {t.stream.live}
            </Pill>
          ) : (
            <Pill color={SEMANTIC.amber}>{t.stream.reconnecting}</Pill>
          )
        }
      />
      <PageBody className="flex flex-col gap-[18px]">
        {/* KPI row — 6 cards */}
        <div className="grid gap-3" style={{ gridTemplateColumns: 'repeat(6,minmax(0,1fr))' }}>
          <KpiCard
            label={t.overview.kpiReqPerSec}
            value={formatRate(fleet.rps)}
            color="var(--accent)"
            spark={fleet.rpsSeries}
            sub={t.overview.kpiReqPerSecSub}
          />
          <KpiCard
            label={t.overview.kpiLatencyP95}
            value={fleet.hasHttpTraffic ? formatMs(fleet.p95Ms) : t.common.empty}
            unit={fleet.hasHttpTraffic ? 'ms' : undefined}
            color={SEMANTIC.amber}
            spark={fleet.p95Series}
            sub={t.overview.kpiLatencyP95Sub}
          />
          <KpiCard
            label={t.overview.kpiErrorRate}
            value={fleet.hasHttpTraffic ? fleet.errorRatePct.toFixed(1) : t.common.empty}
            unit={fleet.hasHttpTraffic ? '%' : undefined}
            color={errColor}
            spark={fleet.errorSeries}
            sub={t.overview.kpiErrorRateSub}
          />
          <KpiCard
            label={t.overview.kpiNodesOnline}
            value={String(online)}
            unit={`/ ${nodes.length}`}
            sub={t.overview.kpiNodesOnlineSub}
          />
          <KpiCard
            label={t.overview.kpiSessions}
            value={String(fleet.sessionEventsPerMin)}
            unit="/min"
            color={SEMANTIC.violet}
            spark={fleet.sessionSeries}
            sub={t.overview.kpiSessionsSub}
          />
          {dbPool.reporting ? (
            <KpiCard
              label={t.overview.kpiDbPool}
              value={String(dbPool.inUse)}
              unit={dbPool.unlimited ? t.overview.kpiDbPoolInUse : `/ ${dbPool.maxOpen}`}
              sub={t.overview.kpiDbPoolFleetSub}
            />
          ) : (
            <KpiCard label={t.overview.kpiDbPool} value={t.common.empty} sub={t.overview.kpiDbPoolAwaitingSub} />
          )}
        </div>

        {/* Row 1 — Fleet table + Health list */}
        <div className="grid gap-3.5" style={{ gridTemplateColumns: '1.25fr 1fr' }}>
          <Card className="overflow-hidden" role="table" aria-label={t.overview.fleetTitle}>
            <FeedHeader title={t.overview.fleetTitle} right={<FeedLink href="#/nodes">{t.overview.fleetAllNodes}</FeedLink>} />
            <div
              role="row"
              className="grid px-4 py-[7px] text-[10px] font-semibold uppercase tracking-[.08em] text-t26"
              style={{ gridTemplateColumns: FLEET_GRID }}
            >
              <span role="columnheader">{t.nodes.colNodeId}</span>
              <span role="columnheader">{t.nodes.colVersion}</span>
              <span role="columnheader">{t.nodes.colStarted}</span>
              <span role="columnheader">{t.nodes.colLastSeen}</span>
              <span role="columnheader">{t.nodes.colStatus}</span>
            </div>
            {nodes.length === 0 && <EmptyRow>{t.overview.noNodes}</EmptyRow>}
            {nodes.map((n) => (
              <div
                key={n.nodeId}
                role="row"
                tabIndex={0}
                aria-label={t.overview.openNodeAria(n.nodeId)}
                onClick={() => {
                  window.location.hash = `#/nodes/${encodeURIComponent(n.nodeId)}`
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') window.location.hash = `#/nodes/${encodeURIComponent(n.nodeId)}`
                }}
                className="grid cursor-pointer items-center border-t border-t12 px-4 py-2 font-mono text-[11.5px] hover:bg-t7"
                style={{ gridTemplateColumns: FLEET_GRID }}
              >
                <span role="cell" className="flex min-w-0 items-center gap-[7px] text-t43">
                  <Dot color={n.connected ? SEMANTIC.green : SEMANTIC.red} size={6} />
                  <span className="truncate">{n.nodeId}</span>
                </span>
                <span
                  role="cell"
                  className="truncate pr-2"
                  style={{
                    color: n.version && n.version !== mainVersion ? SEMANTIC.amber : 'var(--t36)',
                  }}
                >
                  {n.version || t.common.empty}
                </span>
                <span role="cell" className="text-t34">{formatRelative(timestampToDate(n.startedAt))}</span>
                <span role="cell" className="text-t34">{formatRelative(timestampToDate(n.lastSeenAt))}</span>
                <span role="cell" style={{ color: n.connected ? SEMANTIC.green : 'var(--t26)' }}>
                  {n.connected ? t.overview.statusOnline : t.overview.statusOffline}
                </span>
              </div>
            ))}
          </Card>

          <Card className="overflow-hidden">
            <FeedHeader
              title={t.overview.healthTitle}
              right={
                <Pill color={healthy ? SEMANTIC.green : SEMANTIC.amber}>
                  {healthy ? t.overview.healthy : t.overview.degraded}
                </Pill>
              }
            />
            {checks.length === 0 && <EmptyRow>{t.overview.noChecks}</EmptyRow>}
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
          <Card className="overflow-hidden" role="table" aria-label={t.overview.httpFeedTitle}>
            <FeedHeader title={t.overview.httpFeedTitle} right={<FeedLink href="#/http">{t.overview.openStream}</FeedLink>} />
            {httpFeed.length === 0 && <EmptyRow>{t.overview.noHttpEvents}</EmptyRow>}
            {httpFeed.map((ev, i) => {
              if (ev.body.case !== 'httpRequest') return null
              const http = ev.body.value
              return (
                <div
                  key={feedKey(ev, i)}
                  role="row"
                  className="grid items-center border-t border-t10 px-4 py-[5.5px] font-mono text-[11px]"
                  style={{ gridTemplateColumns: HTTP_FEED_GRID }}
                >
                  <span role="cell" className="text-t25">{formatTime(timestampToDate(ev.timestamp))}</span>
                  <span role="cell" className="font-semibold" style={{ color: methodColor(http.method) }}>
                    {http.method}
                  </span>
                  <span role="cell" className="truncate pr-2.5 text-t40" title={http.path}>
                    {http.path}
                  </span>
                  <span role="cell" className="text-right" style={{ color: statusColor(http.status) }}>
                    {http.status}
                  </span>
                  <span role="cell" className="text-right text-t34 tabular-nums">
                    {formatDuration(durationToMillis(http.duration))}
                  </span>
                </div>
              )
            })}
          </Card>

          <Card className="overflow-hidden" role="table" aria-label={t.overview.sqlFeedTitle}>
            <FeedHeader title={t.overview.sqlFeedTitle} right={<FeedLink href="#/sql">{t.overview.openStream}</FeedLink>} />
            {sqlFeed.length === 0 && <EmptyRow>{t.overview.noSqlEvents}</EmptyRow>}
            {sqlFeed.map((ev, i) => {
              if (ev.body.case !== 'sqlStatement') return null
              const sql = ev.body.value
              const kind = (sql.operation || 'SQL').toUpperCase()
              return (
                <div
                  key={feedKey(ev, i)}
                  role="row"
                  className="grid items-center border-t border-t10 px-4 py-[5.5px] font-mono text-[11px]"
                  style={{ gridTemplateColumns: SQL_FEED_GRID }}
                >
                  <span role="cell" className="text-t25">{formatTime(timestampToDate(ev.timestamp))}</span>
                  <span role="cell" className="font-semibold" style={{ color: sqlKindColor(kind) }}>
                    {kind}
                  </span>
                  <span role="cell" className="truncate pr-2.5 text-t38" title={sql.query}>
                    {sql.query}
                  </span>
                  <span role="cell" className="text-right text-t34 tabular-nums">
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
      name: t.overview.checkEventStream,
      dot: streamConnected ? SEMANTIC.green : SEMANTIC.amber,
      detail: streamConnected ? t.overview.checkStreamConnected : t.overview.checkStreamReconnecting,
      ok: streamConnected,
    },
  ]
  for (const n of nodes) {
    const seen = timestampToDate(n.lastSeenAt)
    const fresh = seen !== undefined && now - seen.getTime() < FRESH_MS
    const ok = n.connected && fresh
    checks.push({
      name: t.overview.checkAgent(shortId(n.nodeId)),
      dot: !n.connected ? SEMANTIC.red : fresh ? SEMANTIC.green : SEMANTIC.amber,
      detail: t.overview.checkAgentDetail(n.version || t.common.unknown, formatRelative(seen)),
      ok,
    })
  }
  return checks
}

function shortId(id: string): string {
  return id.length > 14 ? `${id.slice(0, 12)}…` : id
}
