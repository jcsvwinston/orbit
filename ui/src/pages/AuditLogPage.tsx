// Audit log (design handoff screen 11). The admin server does not expose
// an audit service yet, so the table keeps the design's structure (grid
// columns, header row, action coloring) and renders an honest empty state
// from a typed empty array — wiring it later is a data-only change.
import { PageBody, PageHeader } from '@/components/Page'
import { Card } from '@/components/ui'
import { SEMANTIC } from '@/lib/colors'

interface AuditEntry {
  time: string
  actor: string
  action: string
  target: string
  node: string
}

// No audit RPCs on the server yet — stays empty until the admin server
// exposes the audit service.
const ENTRIES: AuditEntry[] = []

const AUDIT_GRID = '140px 170px 160px minmax(0,1fr) 100px'

function actionColor(action: string): string {
  const a = action.toLowerCase()
  if (a.includes('delete') || a.includes('revoke')) return SEMANTIC.red
  if (a.includes('create') || a.includes('add')) return SEMANTIC.green
  return SEMANTIC.blue
}

export function AuditLogPage() {
  return (
    <>
      <PageHeader
        title="Audit log"
        description="In-memory ring of admin actions performed through Orbit."
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
          {ENTRIES.map((a, idx) => (
            <div
              key={idx}
              className="grid items-center border-t border-t10 px-4 py-[6.5px] font-mono text-[11.5px] transition-colors hover:bg-t7"
              style={{ gridTemplateColumns: AUDIT_GRID }}
            >
              <span className="truncate pr-2.5 text-t25">{a.time}</span>
              <span className="truncate pr-2.5 text-t42">{a.actor}</span>
              <span className="truncate pr-2.5" style={{ color: actionColor(a.action) }}>
                {a.action}
              </span>
              <span className="truncate pr-2.5 text-t36">{a.target}</span>
              <span className="truncate text-right text-t32">{a.node}</span>
            </div>
          ))}
          {ENTRIES.length === 0 && (
            <div className="border-t border-t10 px-4 py-6 text-center text-[12.5px] text-t26">
              No audit entries — the audit service is not exposed by the server yet.
            </div>
          )}
        </Card>
      </PageBody>
    </>
  )
}
