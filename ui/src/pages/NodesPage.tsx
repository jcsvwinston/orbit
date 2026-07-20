// Nodes (redesign screen 6): fleet table styled like the stream tables.
// Grid 130/120/110/110/1fr/90; whole row navigates to #/nodes/<id>.
// Version turns amber when it differs from the most common fleet version.
// The div-grid carries table semantics (role table/row/columnheader/cell) and
// rows stay keyboard-operable (tabIndex + Enter) — the row/keyboard pattern
// the other table pages follow.
import type { NodeInfo } from '@/gen/nucleus/admin/v1/admin_pb'
import { fleetMainVersion } from '@/lib/fleet'
import { useNodes } from '@/hooks/useNodes'
import { PageBody, PageHeader } from '@/components/Page'
import { Chip, Dot, GhostButton } from '@/components/ui'
import { SEMANTIC } from '@/lib/colors'
import { t } from '@/lib/i18n'
import { formatRelative, timestampToDate } from '@/lib/format'

const GRID = '130px 120px 110px 110px minmax(0,1fr) 90px'

/** Online (green-bordered) / Offline (neutral) pill, 10.5px like the prototype. */
export function NodeStatusPill(props: { online: boolean }) {
  return props.online ? (
    <span
      className="inline-block rounded-full border px-[9px] py-[1.5px] text-[10.5px]"
      style={{
        color: SEMANTIC.green,
        borderColor: `color-mix(in srgb, ${SEMANTIC.green} 35%, transparent)`,
        background: `color-mix(in srgb, ${SEMANTIC.green} 10%, transparent)`,
      }}
    >
      {t.common.online}
    </span>
  ) : (
    <span className="inline-block rounded-full border border-t20 bg-t8 px-[9px] py-[1.5px] text-[10.5px] text-t32">
      {t.common.offline}
    </span>
  )
}

export function NodesPage() {
  const { nodes, isLoading, isError, error, refetch } = useNodes()
  const mainVersion = fleetMainVersion(nodes)

  return (
    <>
      <PageHeader
        title={t.nodes.title}
        description={t.nodes.description}
        actions={<GhostButton onClick={refetch}>{t.common.refresh}</GhostButton>}
      />
      <PageBody>
        {isError && (
          <div
            className="mb-4 rounded-[10px] border px-4 py-3 text-[12.5px]"
            style={{
              color: SEMANTIC.red,
              borderColor: `color-mix(in srgb, ${SEMANTIC.red} 30%, transparent)`,
              background: `color-mix(in srgb, ${SEMANTIC.red} 8%, transparent)`,
            }}
          >
            {t.nodes.loadFailed(error?.message ?? t.common.unknownError)}
          </div>
        )}
        <div
          role="table"
          aria-label={t.nodes.tableAria}
          className="overflow-hidden rounded-[10px] border border-t18 bg-t4"
        >
          <div
            role="row"
            className="grid bg-t6 px-4 py-2 text-[10px] font-semibold uppercase tracking-[.08em] text-t26"
            style={{ gridTemplateColumns: GRID }}
          >
            <span role="columnheader">{t.nodes.colNodeId}</span>
            <span role="columnheader">{t.nodes.colVersion}</span>
            <span role="columnheader">{t.nodes.colStarted}</span>
            <span role="columnheader">{t.nodes.colLastSeen}</span>
            <span role="columnheader">{t.nodes.colLabels}</span>
            <span role="columnheader" className="text-right">
              {t.nodes.colStatus}
            </span>
          </div>
          {nodes.length === 0 && (
            <div className="border-t border-t10 px-4 py-8 text-center text-[12px] text-t26">
              {isLoading ? t.common.loading : t.nodes.noNodes}
            </div>
          )}
          {nodes.map((n) => (
            <NodeRow key={n.nodeId} node={n} mainVersion={mainVersion} />
          ))}
        </div>
      </PageBody>
    </>
  )
}

function NodeRow(props: { node: NodeInfo; mainVersion: string | null }) {
  const n = props.node
  const versionMismatch =
    props.mainVersion !== null && n.version !== '' && n.version !== props.mainVersion
  const labels = Object.entries(n.labels).sort(([a], [b]) => a.localeCompare(b))

  return (
    <div
      role="row"
      tabIndex={0}
      aria-label={t.nodes.openNodeAria(n.nodeId)}
      onClick={() => {
        window.location.hash = `#/nodes/${encodeURIComponent(n.nodeId)}`
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter') window.location.hash = `#/nodes/${encodeURIComponent(n.nodeId)}`
      }}
      className="grid cursor-pointer items-center border-t border-t10 px-4 py-[9px] text-[12px] transition-colors hover:bg-t7"
      style={{ gridTemplateColumns: GRID }}
    >
      <span role="cell" className="flex min-w-0 items-center gap-[7px] font-mono text-[11.5px] text-t44">
        <Dot color={n.connected ? SEMANTIC.green : 'var(--t26)'} size={6} pulse={n.connected} />
        <span className="truncate">{n.nodeId}</span>
      </span>
      <span
        role="cell"
        className="truncate pr-2 font-mono text-[11.5px]"
        style={{ color: versionMismatch ? SEMANTIC.amber : 'var(--t37)' }}
      >
        {n.version || t.common.empty}
      </span>
      <span role="cell" className="text-t32">
        {formatRelative(timestampToDate(n.startedAt))}
      </span>
      <span role="cell" className="text-t32">
        {formatRelative(timestampToDate(n.lastSeenAt))}
      </span>
      <span role="cell" className="flex min-w-0 flex-wrap gap-1 pr-3">
        {labels.length === 0 && <span className="text-t26">{t.common.empty}</span>}
        {labels.map(([k, v]) => (
          <Chip key={k}>
            {k}={v}
          </Chip>
        ))}
      </span>
      <span role="cell" className="text-right">
        <NodeStatusPill online={n.connected} />
      </span>
    </div>
  )
}
