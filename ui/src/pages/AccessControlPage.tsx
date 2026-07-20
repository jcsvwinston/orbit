// Access control (design handoff screen 10), wired to ManageService.
// GetRbac routes to a connected agent, which snapshots the application's
// Casbin authorizer (read-only — the app stays the single writer, so
// there is no add/revoke from the fleet plane).
import { PageBody, PageHeader } from '@/components/Page'
import { Card, GhostButton } from '@/components/ui'
import { useRbac } from '@/hooks/useManage'
import { SEMANTIC } from '@/lib/colors'
import { t } from '@/lib/i18n'

const POLICY_GRID = '160px minmax(0,1fr) 120px 90px'

export function AccessControlPage() {
  const { roles, policies, isLoading, isError, error, refetch } = useRbac()

  return (
    <>
      <PageHeader
        title={t.access.title}
        description={t.access.description}
        actions={<GhostButton onClick={refetch}>{t.common.refresh}</GhostButton>}
      />
      <PageBody className="flex flex-col gap-4">
        {isError ? (
          <Card className="p-6 text-[12.5px] text-t30">
            {t.access.loadFailed(error?.message ?? t.common.unknownError)}
          </Card>
        ) : isLoading ? (
          <Card className="p-6 text-[12.5px] text-t26">{t.access.loading}</Card>
        ) : (
          <>
            {roles.length === 0 ? (
              <Card className="p-6 text-[12.5px] text-t30">{t.access.noRoles}</Card>
            ) : (
              <div className="grid grid-cols-4 gap-3">
                {roles.map((r) => (
                  <Card key={r.name} className="px-[15px] py-[13px]">
                    <div className="flex items-center justify-between gap-2">
                      <span className="truncate font-mono text-[13px] font-semibold text-t46">
                        {r.name}
                      </span>
                      <span className="shrink-0 font-mono text-[10.5px] text-accent">
                        {t.access.subjects(r.members)}
                      </span>
                    </div>
                  </Card>
                ))}
              </div>
            )}

            <Card className="overflow-hidden" role="table" aria-label={t.access.policiesAria}>
              <div className="border-b border-t14 px-4 py-[11px] text-[12.5px] font-semibold text-t41">
                {t.access.policiesTitle}
              </div>
              <div
                role="row"
                className="grid bg-t6 px-4 py-2 text-[10px] font-semibold uppercase tracking-[.08em] text-t26"
                style={{ gridTemplateColumns: POLICY_GRID }}
              >
                <span role="columnheader">{t.access.colSubject}</span>
                <span role="columnheader">{t.access.colObject}</span>
                <span role="columnheader">{t.access.colAction}</span>
                <span role="columnheader">{t.access.colEffect}</span>
              </div>
              {policies.map((p) => (
                <div
                  key={`${p.subject}:${p.object}:${p.action}`}
                  role="row"
                  className="grid items-center border-t border-t10 px-4 py-[6.5px] font-mono text-[11.5px] transition-colors hover:bg-t7"
                  style={{ gridTemplateColumns: POLICY_GRID }}
                >
                  <span role="cell" className="truncate pr-2.5 text-t44">{p.subject}</span>
                  <span role="cell" className="truncate pr-2.5 text-t36">{p.object}</span>
                  <span role="cell" className="truncate pr-2.5 text-accent">{p.action}</span>
                  <span role="cell" style={{ color: p.effect === 'allow' ? SEMANTIC.green : SEMANTIC.red }}>
                    {p.effect}
                  </span>
                </div>
              ))}
              {policies.length === 0 && (
                <div className="border-t border-t10 px-4 py-6 text-center text-[12.5px] text-t26">
                  {t.access.noPolicies}
                </div>
              )}
            </Card>
          </>
        )}
      </PageBody>
    </>
  )
}
