// Access control (design handoff screen 10). The admin server does not
// expose an RBAC service yet, so the page keeps the design's card/table
// structure but renders honest empty states from typed empty arrays —
// wiring it later is a data-only change.
import { PageBody, PageHeader } from '@/components/Page'
import { Card, GhostButton } from '@/components/ui'
import { SEMANTIC } from '@/lib/colors'

interface Role {
  name: string
  members: number
  description: string
}

interface Policy {
  subject: string
  object: string
  action: string
  effect: 'allow' | 'deny'
}

// No RBAC RPCs on the server yet — these stay empty until the admin
// server exposes role/policy data.
const ROLES: Role[] = []
const POLICIES: Policy[] = []

const POLICY_GRID = '120px minmax(0,1fr) 120px 90px 90px'

export function AccessControlPage() {
  return (
    <>
      <PageHeader
        title="Access control"
        description="Casbin roles and policies backing the app's authorizer. Default-deny."
      />
      <PageBody className="flex flex-col gap-4">
        {ROLES.length === 0 ? (
          <Card className="p-6 text-[12.5px] text-t30">
            Access control is not wired to the server yet — role and policy data will appear when
            the admin server exposes the RBAC service.
          </Card>
        ) : (
          <div className="grid grid-cols-4 gap-3">
            {ROLES.map((r) => (
              <Card
                key={r.name}
                className="cursor-pointer px-[15px] py-[13px] transition-colors hover:border-t20"
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate font-mono text-[13px] font-semibold text-t46">
                    {r.name}
                  </span>
                  <span className="shrink-0 font-mono text-[10.5px] text-accent">
                    {r.members} users →
                  </span>
                </div>
                <div className="mt-[7px] text-[11.5px] text-t32">{r.description}</div>
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
            <span className="text-right">Revoke</span>
          </div>
          {POLICIES.map((p) => (
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
              <span className="flex justify-end">
                <GhostButton
                  danger
                  onClick={() => {
                    // No revoke RPC yet — confirm-only placeholder so the
                    // destructive-action pattern is in place for wiring.
                    window.confirm(`Revoke policy ${p.subject} → ${p.object} (${p.action})?`)
                  }}
                >
                  Revoke
                </GhostButton>
              </span>
            </div>
          ))}
          {POLICIES.length === 0 && (
            <div className="border-t border-t10 px-4 py-6 text-center text-[12.5px] text-t26">
              No policies — the RBAC service is not exposed by the server yet.
            </div>
          )}
        </Card>
      </PageBody>
    </>
  )
}
