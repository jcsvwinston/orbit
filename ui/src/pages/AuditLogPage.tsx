// Audit log (design handoff screen 11), wired to ManageService.
// ListAudit reads the admin server's own fleet-plane ring: actions an
// operator performed THROUGH this server (Data Studio mutations, and
// future manage actions), attributed via the UI auth chain. Per-app
// admin actions stay in each node's in-process Orbit audit ring.
//
// OR-UX-P1-7: review tools — filter by actor/action/node + time range,
// client-side pagination over the full ring, and CSV export. The server
// ring holds up to 2048 entries and ListAudit only takes a `limit`, so we
// fetch the whole ring once and filter/paginate in the browser.
import { useMemo, useState } from 'react'
import { PageBody, PageHeader } from '@/components/Page'
import { Card, GhostButton, Label } from '@/components/ui'
import { useAudit } from '@/hooks/useManage'
import { SEMANTIC } from '@/lib/colors'
import { t } from '@/lib/i18n'
import type { AuditEntry } from '@/gen/nucleus/admin/v1/admin_pb'

const AUDIT_GRID = '150px 170px 170px minmax(0,1fr) 110px'
const RING_CAPACITY = 2048
const PAGE_SIZE = 50

function actionColor(action: string): string {
  const a = action.toLowerCase()
  if (a.includes('delete') || a.includes('revoke')) return SEMANTIC.red
  if (a.includes('create') || a.includes('add')) return SEMANTIC.green
  return SEMANTIC.blue
}

function formatTime(entry: AuditEntry): string {
  if (!entry.time) return t.common.empty
  return entry.time.toDate().toLocaleString(undefined, { hour12: false })
}

const inputClass =
  'rounded-[7px] border border-t19 bg-t8 px-2.5 py-[5.5px] font-mono text-[11.5px] text-t45 placeholder:text-t26 focus:outline-none'

interface Filters {
  actor: string
  action: string
  node: string
  since: string // datetime-local value
  until: string
}

const emptyFilters: Filters = { actor: '', action: '', node: '', since: '', until: '' }

export function AuditLogPage() {
  const { entries, isLoading, isError, error, refetch } = useAudit(RING_CAPACITY)
  const [filters, setFilters] = useState<Filters>(emptyFilters)
  const [page, setPage] = useState(1)

  const set = (patch: Partial<Filters>): void => {
    setFilters((f) => ({ ...f, ...patch }))
    setPage(1)
  }

  const nodeOptions = useMemo(() => {
    const seen = new Set<string>()
    for (const e of entries) if (e.nodeId) seen.add(e.nodeId)
    return Array.from(seen).sort()
  }, [entries])

  const filtered = useMemo(() => {
    const actor = filters.actor.trim().toLowerCase()
    const action = filters.action.trim().toLowerCase()
    const sinceMs = filters.since ? new Date(filters.since).getTime() : null
    const untilMs = filters.until ? new Date(filters.until).getTime() : null
    return entries.filter((e) => {
      if (actor && !e.actor.toLowerCase().includes(actor)) return false
      if (action && !e.action.toLowerCase().includes(action)) return false
      if (filters.node && e.nodeId !== filters.node) return false
      if (sinceMs !== null || untilMs !== null) {
        const t = e.time ? e.time.toDate().getTime() : 0
        if (sinceMs !== null && t < sinceMs) return false
        if (untilMs !== null && t > untilMs) return false
      }
      return true
    })
  }, [entries, filters])

  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE))
  const currentPage = Math.min(page, totalPages)
  const pageRows = filtered.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE)
  const active =
    filters.actor !== '' ||
    filters.action !== '' ||
    filters.node !== '' ||
    filters.since !== '' ||
    filters.until !== ''

  const exportCSV = (): void => {
    const header = ['time', 'actor', 'action', 'target', 'node']
    const lines = [header.join(',')]
    for (const e of filtered) {
      const row = [
        e.time ? e.time.toDate().toISOString() : '',
        e.actor,
        e.action,
        e.target,
        e.nodeId,
      ].map(csvCell)
      lines.push(row.join(','))
    }
    const blob = new Blob([lines.join('\n')], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `orbit-audit-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '')}.csv`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <>
      <PageHeader
        title={t.audit.title}
        description={t.audit.description}
        actions={
          <span className="flex items-center gap-2.5">
            <GhostButton onClick={exportCSV} disabled={filtered.length === 0}>
              {t.audit.exportCSV}
            </GhostButton>
            <GhostButton onClick={refetch}>{t.common.refresh}</GhostButton>
          </span>
        }
      />
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-t14 px-7 py-2.5">
        <input
          type="text"
          value={filters.actor}
          onChange={(e) => set({ actor: e.target.value })}
          placeholder={t.audit.actorPlaceholder}
          aria-label={t.audit.actorAria}
          className={`${inputClass} w-[150px]`}
        />
        <input
          type="text"
          value={filters.action}
          onChange={(e) => set({ action: e.target.value })}
          placeholder={t.audit.actionPlaceholder}
          aria-label={t.audit.actionAria}
          className={`${inputClass} w-[150px]`}
        />
        <span className="flex items-center gap-1.5">
          <Label>{t.audit.nodeLabel}</Label>
          <select
            value={filters.node}
            onChange={(e) => set({ node: e.target.value })}
            aria-label={t.audit.nodeAria}
            className={`${inputClass} max-w-[160px]`}
          >
            <option value="">{t.audit.allNodes}</option>
            {nodeOptions.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        </span>
        <span className="flex items-center gap-1.5">
          <Label>{t.audit.fromLabel}</Label>
          <input
            type="datetime-local"
            value={filters.since}
            onChange={(e) => set({ since: e.target.value })}
            aria-label={t.audit.fromAria}
            className={inputClass}
          />
        </span>
        <span className="flex items-center gap-1.5">
          <Label>{t.audit.toLabel}</Label>
          <input
            type="datetime-local"
            value={filters.until}
            onChange={(e) => set({ until: e.target.value })}
            aria-label={t.audit.toAria}
            className={inputClass}
          />
        </span>
        {active && <GhostButton onClick={() => set(emptyFilters)}>{t.common.clear}</GhostButton>}
      </div>
      <PageBody>
        <Card className="overflow-hidden" role="table" aria-label={t.audit.tableAria}>
          <div
            role="row"
            className="grid bg-t6 px-4 py-2 text-[10px] font-semibold uppercase tracking-[.08em] text-t30"
            style={{ gridTemplateColumns: AUDIT_GRID }}
          >
            <span role="columnheader">{t.audit.colTime}</span>
            <span role="columnheader">{t.audit.colActor}</span>
            <span role="columnheader">{t.audit.colAction}</span>
            <span role="columnheader">{t.audit.colTarget}</span>
            <span role="columnheader" className="text-right">{t.audit.colNode}</span>
          </div>
          {isError ? (
            <div className="border-t border-t10 px-4 py-6 text-center text-[12.5px] text-t30">
              {t.audit.loadFailed(error?.message ?? t.common.unknownError)}
            </div>
          ) : (
            <>
              {pageRows.map((a, idx) => (
                <div
                  key={`${a.time?.toDate().getTime() ?? 0}-${idx}`}
                  role="row"
                  className="grid items-center border-t border-t10 px-4 py-[6.5px] font-mono text-[11.5px] transition-colors hover:bg-t7"
                  style={{ gridTemplateColumns: AUDIT_GRID }}
                >
                  <span role="cell" className="truncate pr-2.5 text-t31">{formatTime(a)}</span>
                  <span role="cell" className="truncate pr-2.5 text-t42">{a.actor}</span>
                  <span role="cell" className="truncate pr-2.5" style={{ color: actionColor(a.action) }}>
                    {a.action}
                  </span>
                  <span role="cell" className="truncate pr-2.5 text-t36">{a.target}</span>
                  <span role="cell" className="truncate text-right text-t32">{a.nodeId}</span>
                </div>
              ))}
              {filtered.length === 0 && (
                <div className="border-t border-t10 px-4 py-6 text-center text-[12.5px] text-t30">
                  {isLoading
                    ? t.audit.loadingEntries
                    : active
                      ? t.audit.noMatches
                      : t.audit.noEntries}
                </div>
              )}
            </>
          )}
        </Card>
        {filtered.length > 0 && (
          <div className="mt-3 flex items-center justify-between font-mono text-[10.5px] text-t30">
            <span>
              {t.audit.entryCount(filtered.length)}
              {active ? t.audit.filteredFrom(entries.length) : ''}
            </span>
            <span className="flex items-center gap-2">
              <GhostButton disabled={currentPage <= 1} onClick={() => setPage(currentPage - 1)}>
                {t.common.prevPage}
              </GhostButton>
              <span>{t.audit.pageOf(currentPage, totalPages)}</span>
              <GhostButton disabled={currentPage >= totalPages} onClick={() => setPage(currentPage + 1)}>
                {t.common.nextPage}
              </GhostButton>
            </span>
          </div>
        )}
      </PageBody>
    </>
  )
}

// csvCell quotes a value when it contains a comma, quote, or newline
// (RFC 4180), doubling embedded quotes.
function csvCell(value: string): string {
  if (/[",\n\r]/.test(value)) {
    return `"${value.replace(/"/g, '""')}"`
  }
  return value
}
