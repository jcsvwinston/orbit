// Access control (design handoff screen 10), wired to ManageService.
// GetRbac routes to a connected agent, which snapshots the application's
// Casbin authorizer (read-only — the app stays the single writer, so
// there is no add/revoke from the fleet plane).
import { PageBody, PageHeader } from '@/components/Page'
import { Card, GhostButton } from '@/components/ui'
import { useRbac } from '@/hooks/useManage'
import { SEMANTIC } from '@/lib/colors'

const POLICY_GRID = '160px minmax(0,1fr) 120px 90px'

export function AccessControlPage() {
  const { roles, policies, isLoading, isError, error, refetch } = useRbac()

  return (
    <>
      <PageHeader
        title="Access control"
        description="Casbin roles and policies backing the app's authorizer. Read-only from the fleet plane; default-deny."
        actions={<GhostButton onClick={refetch}>Refresh</GhostButton>}
      />
      <PageBody className="flex flex-col gap-4">
        {isError ? (
          <Card className="p-6 text-[12.5px] text-t30">
            Failed to load RBAC state: {error?.message ?? 'unknown error'}. The selected node may
            not have an authorizer wired into its agent.
          </Card>
        ) : isLoading ? (
          <Card className="p-6 text-[12.5px] text-t26">Loading roles and policies…</Card>
        ) : (
          <>
            {roles.length === 0 ? (
              <Card className="p-6 text-[12.5px] text-t30">
                No roles — the application's authorizer has no grouping rules yet.
              </Card>
            ) : (
              <div className="grid grid-cols-4 gap-3">
                {roles.map((r) => (
                  <Card key={r.name} className="px-[15px] py-[13px]">
                    <div className="flex items-center justify-between gap-2">
                      <span className="truncate font-mono text-[13px] font-semibold text-t46">
                        {r.name}
                      </span>
                      <span className="shrink-0 font-mono text-[10.5px] text-accent">
                        {r.members} {r.members === 1 ? 'subject' : 'subjects'}
                      </span>
                    </div>
                  </Card>
                ))}
              </div>
            )}

            <Card className="overflow-hidden">
              <div className="border-b border-t14 px-4 py-[11px] text-[12.5px] font-semibold text-t41">
                Policies
              </div>
              <div
                className="grid bg-t6 px-4 py-2 text-[10px] font-semibold uppercase tracking-[.08em] text-t26"
                style={{ gridTemplateColumns: POLICY_GRID }}
              >
                <span>Subject</span>
                <span>Object</span>
                <span>Action</span>
                <span>Effect</span>
              </div>
              {policies.map((p) => (
                <div
                  key={`${p.subject}:${p.object}:${p.action}`}
                  className="grid items-center border-t border-t10 px-4 py-[6.5px] font-mono text-[11.5px] transition-colors hover:bg-t7"
                  style={{ gridTemplateColumns: POLICY_GRID }}
                >
                  <span className="truncate pr-2.5 text-t44">{p.subject}</span>
                  <span className="truncate pr-2.5 text-t36">{p.object}</span>
                  <span className="truncate pr-2.5 text-accent">{p.action}</span>
                  <span style={{ color: p.effect === 'allow' ? SEMANTIC.green : SEMANTIC.red }}>
                    {p.effect}
                  </span>
                </div>
              ))}
              {policies.length === 0 && (
                <div className="border-t border-t10 px-4 py-6 text-center text-[12.5px] text-t26">
                  No policies — the application's authorizer has an empty policy set.
                </div>
              )}
            </Card>
          </>
        )}
      </PageBody>
    </>
  )
}
