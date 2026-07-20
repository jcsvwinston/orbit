// Health (redesign screen 5). Honest-UI notes: the backend exposes only
// NodeInfo liveness (no component health checks), so the check list is built
// from what is real — control-server reachability (the ListNodes poll) and
// per-node agent freshness (lastSeenAt within ~10s). The connectivity table's
// transport column is truthful: agents attach over the ControlService gRPC
// stream.
import type { NodeInfo } from '@/gen/nucleus/admin/v1/admin_pb'
import { useNodes } from '@/hooks/useNodes'
import { PageBody, PageHeader } from '@/components/Page'
import { Card, Dot, Pill } from '@/components/ui'
import { SEMANTIC } from '@/lib/colors'
import { t } from '@/lib/i18n'
import { formatRelative, timestampToDate } from '@/lib/format'
import { fleetMainVersion } from '@/lib/fleet'

/** An agent is "fresh" when the server marks it connected and it reported recently. */
const FRESH_MS = 10_000

function isFresh(n: NodeInfo, now: number): boolean {
  const seen = timestampToDate(n.lastSeenAt)
  return n.connected && seen !== undefined && now - seen.getTime() <= FRESH_MS
}

type CheckStatus = 'ok' | 'warn' | 'fail'

interface Check {
  name: string
  status: CheckStatus
  statusLabel: string
  message: string
  detail: string
}

const STATUS_COLOR: Record<CheckStatus, string> = {
  ok: SEMANTIC.green,
  warn: SEMANTIC.amber,
  fail: SEMANTIC.red,
}

export function HealthPage() {
  const { nodes, isLoading, isError, error } = useNodes()
  const now = Date.now()
  const freshCount = nodes.filter((n) => isFresh(n, now)).length
  const allFresh = !isError && nodes.length > 0 && freshCount === nodes.length
  const healthy = !isError && (nodes.length === 0 || allFresh)
  const mainVersion = fleetMainVersion(nodes)

  const checks: Check[] = [
    {
      name: t.health.controlServer,
      status: isError ? 'fail' : 'ok',
      statusLabel: isError ? t.health.statusFail : t.health.statusPass,
      message: isError
        ? t.health.serverUnreachable(error?.message ?? t.common.unknownError)
        : t.health.serverOk,
      detail: isError ? t.health.serverFailDetail : t.health.serverOkDetail(nodes.length),
    },
    ...nodes.map((n): Check => {
      const fresh = isFresh(n, now)
      const seen = formatRelative(timestampToDate(n.lastSeenAt))
      return {
        name: n.nodeId,
        status: fresh ? 'ok' : 'warn',
        statusLabel: fresh ? t.health.statusPass : n.connected ? t.health.statusStale : t.health.statusOffline,
        message: fresh
          ? t.health.agentFresh
          : n.connected
            ? t.health.agentStale
            : t.health.agentOffline,
        detail: t.health.agentDetail(n.version || t.health.versionUnknown, seen),
      }
    }),
  ]

  return (
    <>
      <PageHeader
        title={t.health.title}
        description={t.health.description}
        actions={
          <Pill color={healthy ? SEMANTIC.green : SEMANTIC.amber} pulse={healthy}>
            {healthy ? t.health.healthy : t.health.degraded}
          </Pill>
        }
      />
      <PageBody className="flex flex-col gap-4">
        <div className="grid gap-3.5" style={{ gridTemplateColumns: 'repeat(3, minmax(0,1fr))' }}>
          {checks.map((c) => (
            <CheckCard key={c.name} check={c} />
          ))}
        </div>
        <Card className="overflow-hidden !bg-t4" role="table" aria-label={t.health.connectivityAria}>
          <div className="border-b border-t14 px-4 py-[11px] text-[12.5px] font-semibold text-t46">
            {t.health.connectivityTitle}
          </div>
          {nodes.length === 0 && (
            <div className="px-4 py-[18px] text-[12px] text-t26">
              {isLoading ? t.common.loading : t.health.noAgents}
            </div>
          )}
          {nodes.map((n) => (
            <AgentRow key={n.nodeId} node={n} now={now} mainVersion={mainVersion} />
          ))}
        </Card>
      </PageBody>
    </>
  )
}

function CheckCard(props: { check: Check }) {
  const c = props.check
  const color = STATUS_COLOR[c.status]
  return (
    <Card className="px-[17px] py-[15px]">
      <div className="flex items-center justify-between gap-2">
        <span className="flex min-w-0 items-center gap-2 text-[13px] font-semibold text-t46">
          <Dot color={color} size={8} pulse={c.status === 'ok'} />
          <span className="truncate font-mono">{c.name}</span>
        </span>
        <span
          className="shrink-0 rounded-full px-2 py-[1.5px] text-[10.5px]"
          style={{ color, background: `color-mix(in srgb, ${color} 12%, transparent)` }}
        >
          {c.statusLabel}
        </span>
      </div>
      <div className="mt-2.5 text-[12px] text-t32">{c.message}</div>
      <div className="mt-2 font-mono text-[11px] text-t26">{c.detail}</div>
    </Card>
  )
}

function AgentRow(props: { node: NodeInfo; now: number; mainVersion: string | null }) {
  const n = props.node
  const fresh = isFresh(n, props.now)
  const versionMismatch =
    props.mainVersion !== null && n.version !== '' && n.version !== props.mainVersion
  return (
    <div
      role="row"
      className="grid items-center border-t border-t12 px-4 py-2 font-mono text-[11.5px]"
      style={{ gridTemplateColumns: '120px 110px 1fr 110px' }}
    >
      <span role="cell" className="flex min-w-0 items-center gap-[7px] text-t43">
        <Dot color={fresh ? SEMANTIC.green : 'var(--t26)'} size={6} pulse={fresh} />
        <span className="truncate">{n.nodeId}</span>
      </span>
      <span
        role="cell"
        className="truncate pr-2"
        style={{ color: versionMismatch ? SEMANTIC.amber : 'var(--t37)' }}
      >
        {n.version || t.common.empty}
      </span>
      <span role="cell" className="truncate text-t29">
        {fresh ? t.health.transportConnected : t.health.transportStale}
      </span>
      <span role="cell" className="text-right text-t32">{formatRelative(timestampToDate(n.lastSeenAt))}</span>
    </div>
  )
}
