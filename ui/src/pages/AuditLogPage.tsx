// Audit log (design handoff screen 11), wired to ManageService.
// ListAudit reads the admin server's own fleet-plane ring: actions an
// operator performed THROUGH this server (Data Studio mutations, and
// future manage actions), attributed via the UI auth chain. Per-app
// admin actions stay in each node's in-process Orbit audit ring.
import { PageBody, PageHeader } from '@/components/Page'
import { Card, GhostButton } from '@/components/ui'
import { useAudit } from '@/hooks/useManage'
import { SEMANTIC } from '@/lib/colors'
import type { Timestamp } from '@bufbuild/protobuf'

const AUDIT_GRID = '150px 170px 170px minmax(0,1fr) 110px'

function actionColor(action: string): string {
  const a = action.toLowerCase()
  if (a.includes('delete') || a.includes('revoke')) return SEMANTIC.red
  if (a.includes('create') || a.includes('add')) return SEMANTIC.green
  return SEMANTIC.blue
}

function formatTime(ts?: Timestamp): string {
  if (!ts) return '—'
  return ts.toDate().toLocaleString(undefined, { hour12: false })
}

export function AuditLogPage() {
  const { entries, isLoading, isError, error, refetch } = useAudit()

  return (
    <>
      <PageHeader
        title="Audit log"
        description="Fleet-plane actions performed through this admin server (in-memory ring, newest first). Per-app admin actions live in each node's Orbit panel."
        actions={<GhostButton onClick={refetch}>Refresh</GhostButton>}
      />
      <PageBody>
        <Card className="overflow-hidden">
          <div
            className="grid bg-t6 px-4 py-2 text-[10px] font-semibold uppercase tracking-[.08em] text-t26"
            style={{ gridTemplateColumns: AUDIT_GRID }}
          >
            <span>Time</span>
            <span>Actor</span>
            <span>Action</span>
            <span>Target</span>
            <span className="text-right">Node</span>
          </div>
          {isError ? (
            <div className="border-t border-t10 px-4 py-6 text-center text-[12.5px] text-t30">
              Failed to load the audit log: {error?.message ?? 'unknown error'}
            </div>
          ) : (
            <>
              {entries.map((a, idx) => (
                <div
                  key={idx}
                  className="grid items-center border-t border-t10 px-4 py-[6.5px] font-mono text-[11.5px] transition-colors hover:bg-t7"
                  style={{ gridTemplateColumns: AUDIT_GRID }}
                >
                  <span className="truncate pr-2.5 text-t25">{formatTime(a.time)}</span>
                  <span className="truncate pr-2.5 text-t42">{a.actor}</span>
                  <span className="truncate pr-2.5" style={{ color: actionColor(a.action) }}>
                    {a.action}
                  </span>
                  <span className="truncate pr-2.5 text-t36">{a.target}</span>
                  <span className="truncate text-right text-t32">{a.nodeId}</span>
                </div>
              ))}
              {entries.length === 0 && (
                <div className="border-t border-t10 px-4 py-6 text-center text-[12.5px] text-t26">
                  {isLoading
                    ? 'Loading audit entries…'
                    : 'No fleet-plane actions recorded yet — mutations made through Data Studio will appear here.'}
                </div>
              )}
            </>
          )}
        </Card>
      </PageBody>
    </>
  )
}
