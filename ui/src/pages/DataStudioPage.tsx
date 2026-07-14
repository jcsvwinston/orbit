// Data Studio (design handoff screen 9): model list + Records/Schema panel +
// edit/create modal. All data wiring (useDataStudio hooks, JSON value
// encoding/decoding) is preserved from the previous implementation — only the
// rendering changed to the redesign language. The filter input is wired to
// the hook's server-side `search` parameter (debounced).
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { PageBody, PageHeader } from '@/components/Page'
import { AccentButton, Card, Chip, GhostButton, Label, Segmented } from '@/components/ui'
import { useToast } from '@/components/Toast'
import {
  useBulkAction,
  useDeleteRecord,
  useModels,
  useRecords,
  useSaveRecord,
  useSchema,
} from '@/hooks/useDataStudio'
import { useNodes } from '@/hooks/useNodes'
import { useSelf } from '@/hooks/useSelf'
import type { ModelField, ModelInfo, Record as PBRecord } from '@/gen/nucleus/admin/v1/admin_pb'

const PAGE_SIZE = 20

const TABS = [
  { id: 'records', label: 'Records' },
  { id: 'schema', label: 'Schema' },
] as const

type TabID = 'records' | 'schema'

export function DataStudioPage() {
  const [selectedModel, setSelectedModel] = useState<string | null>(null)
  const [tab, setTab] = useState<TabID>('records')
  const [page, setPage] = useState(1)
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [editing, setEditing] = useState<{ id?: string; values: Record<string, string> } | null>(
    null,
  )
  // nodeId scopes every Data Studio call to one connected agent ("" = the
  // server picks any that knows the model). Empty by default.
  const [nodeId, setNodeId] = useState('')
  // Selected record ids for bulk actions.
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())

  const { nodes } = useNodes()
  const { readOnly } = useSelf()
  const models = useModels(true, nodeId)
  const activeModel = selectedModel ?? models.data?.[0]?.name ?? null

  const schema = useSchema(activeModel, nodeId)
  const records = useRecords(
    activeModel && tab === 'records'
      ? { modelName: activeModel, page, pageSize: PAGE_SIZE, search, nodeId }
      : null,
  )

  const toast = useToast()
  const deleteMut = useDeleteRecord(activeModel ?? '', nodeId)
  const saveMut = useSaveRecord(activeModel ?? '', nodeId)
  const bulkMut = useBulkAction(activeModel ?? '', nodeId)

  // The row that is currently being deleted (its id), so the row can show
  // a "deleting…" state instead of appearing to hang.
  const [deletingId, setDeletingId] = useState<string | null>(null)

  // Clear the selection whenever the visible record set changes.
  useEffect(() => {
    setSelectedIds(new Set())
  }, [activeModel, page, search, nodeId])

  // Debounce the filter input into the server-side search param.
  useEffect(() => {
    const t = window.setTimeout(() => {
      setSearch(searchInput)
      setPage(1)
    }, 250)
    return () => window.clearTimeout(t)
  }, [searchInput])

  const listFields = useMemo<ModelField[]>(() => {
    if (!schema.data) return []
    const inList = schema.data.fields.filter((f) => f.isInList && !f.isExcluded)
    return inList.length > 0 ? inList : schema.data.fields.filter((f) => !f.isExcluded).slice(0, 6)
  }, [schema.data])

  const total = records.data ? Number(records.data.total) : 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const currentPage = records.data?.page ? records.data.page : page

  const selectModel = (name: string): void => {
    setSelectedModel(name)
    setPage(1)
    setSearchInput('')
    setSearch('')
    setEditing(null)
  }

  // navigateToFK jumps to a foreign-key target: select the referenced model
  // and pre-fill the search with the key value.
  const navigateToFK = (model: string, value: string): void => {
    setSelectedModel(model)
    setTab('records')
    setPage(1)
    setSearchInput(value)
    setSearch(value)
    setEditing(null)
  }

  const scopeSelectClass =
    'shrink-0 rounded-[7px] border border-t19 bg-t8 px-2.5 py-[5.5px] font-mono text-[11.5px] text-t45 focus:outline-none'

  return (
    <>
      <PageHeader
        title="Data Studio"
        description="Browse and edit registered models. Operations execute on a connected agent — signals, validation and tenant filters apply."
        actions={
          <span className="flex items-center gap-2.5">
            {nodes.length > 0 && (
              <select
                value={nodeId}
                onChange={(e) => {
                  setNodeId(e.target.value)
                  setSelectedModel(null)
                }}
                aria-label="Target node"
                className={scopeSelectClass}
                title="Route Data Studio operations to a specific node"
              >
                <option value="">any node</option>
                {nodes.map((n) => (
                  <option key={n.nodeId} value={n.nodeId}>
                    {n.nodeId}
                  </option>
                ))}
              </select>
            )}
            {!readOnly && (
              <AccentButton
                disabled={!activeModel || !schema.data}
                onClick={() => setEditing({ values: {} })}
              >
                + New record
              </AccentButton>
            )}
            {readOnly && (
              <span className="font-mono text-[10.5px] text-t30">read-only</span>
            )}
          </span>
        }
      />
      <PageBody>
        <div
          className="grid items-start gap-4"
          style={{ gridTemplateColumns: '210px minmax(0,1fr)' }}
        >
          {/* Model list */}
          <Card className="p-2.5">
            <Label className="px-2 pb-2 pt-0.5">Registered models</Label>
            <div className="flex flex-col gap-px">
              {models.isLoading && <div className="px-2 py-1 text-[12px] text-t30">Loading…</div>}
              {models.isError && (
                <div className="px-2 py-1 text-[12px] text-t51">{models.error.message}</div>
              )}
              {(models.data ?? []).map((m: ModelInfo) => {
                const active = m.name === activeModel
                return (
                  <button
                    key={m.name}
                    type="button"
                    onClick={() => selectModel(m.name)}
                    className={[
                      'flex w-full items-center justify-between gap-2 rounded-[6px] px-2.5 py-1.5 text-left text-[13px] transition-colors',
                      active ? 'bg-t13 text-t47' : 'text-t32 hover:bg-t9 hover:text-t45',
                    ].join(' ')}
                  >
                    <span className="flex min-w-0 items-center gap-2">
                      {active && (
                        <span
                          className="h-[13px] w-[3px] shrink-0 rounded-full"
                          style={{ background: 'var(--accent)' }}
                        />
                      )}
                      <span className="truncate">{m.name}</span>
                    </span>
                    <span className="shrink-0 font-mono text-[10.5px] text-t31">
                      {/* -1 = "count unknown" (no agent queried yet) —
                          render an em dash, not a literal "-1". */}
                      {m.recordCount < 0n ? '—' : String(m.recordCount)}
                    </span>
                  </button>
                )
              })}
              {!models.isLoading && !models.isError && (models.data ?? []).length === 0 && (
                <div className="px-2 py-1 text-[12px] text-t30">
                  No agents are reporting models. Connect an agent with{' '}
                  <code className="font-mono text-t39">Registry</code> wired in its config.
                </div>
              )}
            </div>
          </Card>

          {/* Right panel */}
          <Card className="min-w-0 overflow-hidden">
            {!activeModel ? (
              <div className="p-6 text-[12.5px] text-t30">
                Select a model on the left to browse its records.
              </div>
            ) : (
              <>
                <div className="flex items-center justify-between gap-3 px-4 py-3">
                  <div className="flex min-w-0 items-center gap-3">
                    <h2 className="m-0 truncate text-[15px] font-semibold text-t46">
                      {activeModel}
                    </h2>
                    <Segmented
                      options={TABS}
                      value={tab}
                      onChange={(id) => setTab(id as TabID)}
                    />
                  </div>
                  {tab === 'records' && (
                    <input
                      type="text"
                      value={searchInput}
                      onChange={(e) => setSearchInput(e.target.value)}
                      placeholder="filter records…"
                      className="w-[220px] shrink-0 rounded-[7px] border border-t19 bg-t8 px-2.5 py-[5.5px] font-mono text-[11.5px] text-t45 placeholder:text-t26 focus:outline-none"
                    />
                  )}
                </div>

                {tab === 'records' ? (
                  <>
                    {!readOnly && selectedIds.size > 0 && (
                      <div className="flex items-center justify-between border-t border-t10 bg-t6 px-4 py-2">
                        <span className="font-mono text-[11px] text-t35">
                          {selectedIds.size} selected
                        </span>
                        <span className="flex items-center gap-2">
                          <GhostButton onClick={() => setSelectedIds(new Set())}>
                            Clear
                          </GhostButton>
                          <GhostButton
                            danger
                            disabled={bulkMut.isPending}
                            onClick={() => {
                              const ids = Array.from(selectedIds)
                              if (!window.confirm(`Delete ${ids.length} ${activeModel} record(s)?`)) {
                                return
                              }
                              bulkMut.mutate(
                                { action: 'delete', ids },
                                {
                                  onSuccess: (res) => {
                                    setSelectedIds(new Set())
                                    if (res.failed > 0) {
                                      toast.error(
                                        `Deleted ${res.affected}, ${res.failed} failed: ${res.errors[0] ?? ''}`,
                                      )
                                    } else {
                                      toast.success(`Deleted ${res.affected} ${activeModel} record(s)`)
                                    }
                                  },
                                  onError: (err) =>
                                    toast.error(
                                      `Bulk delete failed: ${err instanceof Error ? err.message : String(err)}`,
                                    ),
                                },
                              )
                            }}
                          >
                            {bulkMut.isPending ? 'Deleting…' : `Delete ${selectedIds.size}`}
                          </GhostButton>
                        </span>
                      </div>
                    )}
                    <RecordsTable
                      fields={listFields}
                      loading={records.isLoading || schema.isLoading}
                      error={
                        records.isError
                          ? records.error.message
                          : schema.isError
                            ? schema.error.message
                            : null
                      }
                      records={records.data?.items ?? []}
                      readOnly={readOnly}
                      selectedIds={selectedIds}
                      onToggleSelect={(id) =>
                        setSelectedIds((prev) => {
                          const next = new Set(prev)
                          if (next.has(id)) next.delete(id)
                          else next.add(id)
                          return next
                        })
                      }
                      onToggleAll={(ids, checked) =>
                        setSelectedIds((prev) => {
                          const next = new Set(prev)
                          for (const id of ids) {
                            if (checked) next.add(id)
                            else next.delete(id)
                          }
                          return next
                        })
                      }
                      onNavigateFK={navigateToFK}
                      onEdit={(rec) => {
                        const values: Record<string, string> = {}
                        for (const [k, v] of Object.entries(rec.valuesJson)) {
                          values[k] = v
                        }
                        const id = unquoteJSON(
                          values['ID'] ?? values['Id'] ?? values['id'] ?? '',
                        )
                        setEditing({ id, values })
                      }}
                      deletingId={deletingId}
                      onDelete={(rec) => {
                        const id = unquoteJSON(
                          rec.valuesJson['ID'] ?? rec.valuesJson['Id'] ?? rec.valuesJson['id'] ?? '',
                        )
                        if (!id) return
                        if (!window.confirm(`Delete record ${id}?`)) return
                        setDeletingId(id)
                        deleteMut.mutate(id, {
                          onSuccess: () => {
                            toast.success(`Deleted ${activeModel} ${id}`)
                          },
                          onError: (err) => {
                            // Without this the row simply "won't disappear"
                            // on an FK/permission failure, with no reason.
                            toast.error(
                              `Couldn't delete ${activeModel} ${id}: ${
                                err instanceof Error ? err.message : String(err)
                              }`,
                            )
                          },
                          onSettled: () => setDeletingId(null),
                        })
                      }}
                    />
                    <div className="flex items-center justify-between border-t border-t10 px-4 py-2.5">
                      <GhostButton
                        disabled={currentPage <= 1}
                        onClick={() => setPage((p) => Math.max(1, p - 1))}
                      >
                        ← Prev
                      </GhostButton>
                      <span className="font-mono text-[10.5px] text-t30">
                        page {currentPage}/{totalPages} · {total} records
                      </span>
                      <GhostButton
                        disabled={!records.data?.hasMore}
                        onClick={() => setPage((p) => p + 1)}
                      >
                        Next →
                      </GhostButton>
                    </div>
                  </>
                ) : (
                  <SchemaTable
                    fields={schema.data?.fields ?? []}
                    loading={schema.isLoading}
                    error={schema.isError ? schema.error.message : null}
                  />
                )}
              </>
            )}
          </Card>
        </div>

        {editing !== null && schema.data && activeModel && (
          <RecordEditor
            title={
              editing.id !== undefined && editing.id !== ''
                ? `Edit record — ${editing.id}`
                : `New ${activeModel}`
            }
            schema={schema.data.fields}
            initial={editing.values}
            onCancel={() => setEditing(null)}
            onSave={(values) => {
              const editId = editing.id
              const vars = editId !== undefined && editId !== '' ? { id: editId, values } : { values }
              saveMut.mutate(vars, {
                onSuccess: () => {
                  setEditing(null)
                  toast.success(
                    editId !== undefined && editId !== ''
                      ? `Saved ${activeModel} ${editId}`
                      : `Created ${activeModel}`,
                  )
                },
              })
            }}
            saving={saveMut.isPending}
            error={saveMut.error?.message}
          />
        )}
      </PageBody>
    </>
  )
}

/* ------------------------------------------------------------------ */
/* Records tab                                                         */
/* ------------------------------------------------------------------ */

function recordId(rec: PBRecord): string {
  return unquoteJSON(rec.valuesJson['ID'] ?? rec.valuesJson['Id'] ?? rec.valuesJson['id'] ?? '')
}

function RecordsTable(props: {
  fields: ModelField[]
  records: PBRecord[]
  loading: boolean
  error: string | null
  deletingId: string | null
  readOnly: boolean
  selectedIds: Set<string>
  onToggleSelect: (id: string) => void
  onToggleAll: (ids: string[], checked: boolean) => void
  onNavigateFK: (model: string, value: string) => void
  onEdit: (rec: PBRecord) => void
  onDelete: (rec: PBRecord) => void
}) {
  const cols: string[] = []
  if (!props.readOnly) cols.push('28px')
  cols.push(`repeat(${Math.max(1, props.fields.length)}, minmax(0,1fr))`)
  if (!props.readOnly) cols.push('120px')
  const gridCols = cols.join(' ')
  const selectableIds = props.records.map(recordId).filter((id) => id !== '')
  const allSelected =
    selectableIds.length > 0 && selectableIds.every((id) => props.selectedIds.has(id))
  return (
    <div className="min-w-0">
      <div
        className="grid bg-t6 px-4 py-2 text-[10px] font-semibold uppercase tracking-[.08em] text-t26"
        style={{ gridTemplateColumns: gridCols }}
      >
        {!props.readOnly && (
          <span className="flex items-center">
            <input
              type="checkbox"
              checked={allSelected}
              onChange={(e) => props.onToggleAll(selectableIds, e.target.checked)}
              aria-label="Select all rows on this page"
              disabled={selectableIds.length === 0}
            />
          </span>
        )}
        {props.fields.map((f) => (
          <span key={f.name} className="truncate pr-2">
            {f.label || f.name}
          </span>
        ))}
        {!props.readOnly && <span className="text-right">Actions</span>}
      </div>

      {props.loading && (
        <div className="border-t border-t10 px-4 py-6 text-center text-[12.5px] text-t26">
          Loading…
        </div>
      )}
      {props.error !== null && (
        <div className="border-t border-t10 px-4 py-4 text-center font-mono text-[11.5px] text-t51">
          {props.error}
        </div>
      )}
      {!props.loading && props.error === null && props.records.length === 0 && (
        <div className="border-t border-t10 px-4 py-6 text-center text-[12.5px] text-t26">
          No records match.
        </div>
      )}

      {props.records.map((rec, idx) => {
        const rowId = recordId(rec)
        const deleting = props.deletingId !== null && props.deletingId === rowId
        const selected = rowId !== '' && props.selectedIds.has(rowId)
        return (
          <div
            key={rowId || idx}
            className={[
              'grid items-center border-t border-t10 px-4 py-1.5 transition-colors hover:bg-t7',
              deleting ? 'opacity-50' : '',
              selected ? 'bg-t8' : '',
            ].join(' ')}
            style={{ gridTemplateColumns: gridCols }}
          >
            {!props.readOnly && (
              <span className="flex items-center">
                <input
                  type="checkbox"
                  checked={selected}
                  disabled={rowId === ''}
                  onChange={() => props.onToggleSelect(rowId)}
                  aria-label={`Select record ${rowId}`}
                />
              </span>
            )}
            {props.fields.map((f) => (
              <RecordCell
                key={f.name}
                field={f}
                raw={rec.valuesJson[f.name]}
                onNavigateFK={props.onNavigateFK}
              />
            ))}
            {!props.readOnly && (
              <span className="flex justify-end gap-1.5">
                {deleting ? (
                  <span className="font-mono text-[10.5px] text-t30">deleting…</span>
                ) : (
                  <>
                    <GhostButton onClick={() => props.onEdit(rec)}>Edit</GhostButton>
                    <GhostButton danger onClick={() => props.onDelete(rec)}>
                      Delete
                    </GhostButton>
                  </>
                )}
              </span>
            )}
          </div>
        )
      })}
    </div>
  )
}

function RecordCell(props: {
  field: ModelField
  raw: string | undefined
  onNavigateFK: (model: string, value: string) => void
}) {
  const text = formatCell(props.raw)
  if (text === '' || text === '∅') {
    return <span className="pr-2.5 font-mono text-[11.5px] text-t26">∅</span>
  }
  // Foreign-key cell → link to the referenced model, pre-filtered by the raw
  // (untruncated) key value.
  if (props.field.isForeignKey && props.field.foreignModel) {
    const key = unquoteJSON(props.raw ?? '')
    if (key !== '') {
      return (
        <span className="truncate pr-2.5">
          <button
            type="button"
            onClick={() => props.onNavigateFK(props.field.foreignModel, key)}
            title={`Open ${props.field.foreignModel} ${key}`}
            className="font-mono text-[11.5px] text-accent underline decoration-dotted underline-offset-2 hover:brightness-125"
          >
            {text}
          </button>
        </span>
      )
    }
  }
  return <span className="truncate pr-2.5 font-mono text-[11.5px] text-t39">{text}</span>
}

/* ------------------------------------------------------------------ */
/* Schema tab                                                          */
/* ------------------------------------------------------------------ */

const SCHEMA_GRID = '150px 110px 110px minmax(0,1fr)'

function SchemaTable(props: { fields: ModelField[]; loading: boolean; error: string | null }) {
  return (
    <div className="min-w-0">
      <div
        className="grid bg-t6 px-4 py-2 text-[10px] font-semibold uppercase tracking-[.08em] text-t26"
        style={{ gridTemplateColumns: SCHEMA_GRID }}
      >
        <span>Field</span>
        <span>Go type</span>
        <span>HTML type</span>
        <span>Flags</span>
      </div>
      {props.loading && (
        <div className="border-t border-t10 px-4 py-6 text-center text-[12.5px] text-t26">
          Loading…
        </div>
      )}
      {props.error !== null && (
        <div className="border-t border-t10 px-4 py-4 text-center font-mono text-[11.5px] text-t51">
          {props.error}
        </div>
      )}
      {props.fields.map((f) => (
        <div
          key={f.name}
          className="grid items-center border-t border-t10 px-4 py-[7px] font-mono text-[11.5px]"
          style={{ gridTemplateColumns: SCHEMA_GRID }}
        >
          <span className="truncate pr-2 text-t44">{f.name}</span>
          <span className="truncate pr-2 text-accent">{f.goType}</span>
          <span className="truncate pr-2 text-t32">{f.htmlType}</span>
          <span className="flex flex-wrap gap-1.5">
            {f.isRequired && <Chip>required</Chip>}
            {f.isReadonly && <Chip>readonly</Chip>}
            {f.isInList && <Chip>in list</Chip>}
            {f.isExcluded && <Chip>excluded</Chip>}
          </span>
        </div>
      ))}
    </div>
  )
}

/* ------------------------------------------------------------------ */
/* Edit / create modal                                                 */
/* ------------------------------------------------------------------ */

function RecordEditor(props: {
  title: string
  schema: ModelField[]
  initial: Record<string, string>
  onCancel: () => void
  onSave: (values: Record<string, string>) => void
  saving: boolean
  error: string | undefined
}) {
  const [values, setValues] = useState<Record<string, string>>(() => ({ ...props.initial }))
  const dialogRef = useRef<HTMLDivElement>(null)
  const titleId = useRef(`ds-modal-${Math.random().toString(36).slice(2)}`).current

  const editable = props.schema.filter((f) => !f.isExcluded && !f.isReadonly)

  const onCancel = props.onCancel
  // Escape closes; focus moves into the dialog on open (basic focus
  // management for keyboard users — OR-UX-P1-8).
  useEffect(() => {
    const onKey = (e: KeyboardEvent): void => {
      if (e.key === 'Escape') onCancel()
    }
    document.addEventListener('keydown', onKey)
    const first = dialogRef.current?.querySelector<HTMLElement>('input, textarea, button')
    first?.focus()
    return () => document.removeEventListener('keydown', onKey)
  }, [onCancel])

  // Trap Tab within the dialog so focus can't escape to the page behind.
  const onKeyDown = useCallback((e: React.KeyboardEvent): void => {
    if (e.key !== 'Tab' || !dialogRef.current) return
    const focusables = dialogRef.current.querySelectorAll<HTMLElement>(
      'input, textarea, button, [href], select, [tabindex]:not([tabindex="-1"])',
    )
    if (focusables.length === 0) return
    const first = focusables[0]
    const last = focusables[focusables.length - 1]
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault()
      last.focus()
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault()
      first.focus()
    }
  }, [])

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-5"
      style={{ background: 'rgba(0,0,0,.72)' }}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onCancel()
      }}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        onKeyDown={onKeyDown}
        className="max-h-[86vh] w-full max-w-[600px] overflow-y-auto rounded-[12px] border border-t19 bg-t5"
        style={{ boxShadow: '0 28px 70px rgba(0,0,0,.55)' }}
      >
        <div className="flex items-center justify-between border-b border-t14 px-[18px] py-[13px]">
          <h3 id={titleId} className="m-0 text-[15px] font-semibold text-t46">
            {props.title}
          </h3>
          <button
            type="button"
            onClick={props.onCancel}
            aria-label="Close dialog"
            className="border-none bg-transparent text-[14px] text-t30 transition-colors hover:text-t45"
          >
            ✕
          </button>
        </div>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            props.onSave(values)
          }}
        >
          <div className="flex flex-col gap-3 p-[18px]">
            {editable.map((f) => (
              <FieldEditor
                key={f.name}
                field={f}
                value={values[f.name] ?? ''}
                onChange={(v) => setValues((prev) => ({ ...prev, [f.name]: v }))}
              />
            ))}
            {props.error !== undefined && (
              <div className="rounded-[7px] border border-t51 bg-t4 px-3 py-2 font-mono text-[11.5px] text-t51">
                {props.error}
              </div>
            )}
          </div>
          <div className="flex items-center justify-end gap-2 border-t border-t14 px-[18px] py-[13px]">
            <GhostButton onClick={props.onCancel}>Cancel</GhostButton>
            <AccentButton disabled={props.saving} onClick={() => props.onSave(values)}>
              {props.saving ? 'Saving…' : 'Save'}
            </AccentButton>
          </div>
        </form>
      </div>
    </div>
  )
}

function isDateField(f: ModelField): boolean {
  const ht = f.htmlType.toLowerCase()
  return (
    ht === 'date' ||
    ht === 'datetime' ||
    ht === 'datetime-local' ||
    f.goType.toLowerCase().includes('time.time')
  )
}

function FieldEditor(props: { field: ModelField; value: string; onChange: (v: string) => void }) {
  const f = props.field
  const isArea = f.htmlType === 'textarea'
  const hasChoices = f.choices.length > 0
  const isDate = isDateField(f)
  const inputClass =
    'w-full box-border rounded-[7px] border border-t19 bg-t8 px-2.5 py-[7px] font-mono text-[12px] text-t45 placeholder:text-t26 focus:outline-none'

  let control: ReactNode
  if (hasChoices) {
    // Enum field → real <select> over the declared choices.
    control = (
      <select
        value={parseDisplay(props.value)}
        onChange={(e) => props.onChange(encodeUserValue(e.target.value, f.goType))}
        className={inputClass}
      >
        <option value="">— none —</option>
        {f.choices.map((c) => (
          <option key={c.value} value={c.value}>
            {c.label || c.value}
          </option>
        ))}
      </select>
    )
  } else if (isDate) {
    // Time field → native datetime-local picker; stored as a quoted RFC3339
    // string. Conversion is local-time ↔ ISO.
    control = (
      <input
        type="datetime-local"
        value={rfc3339ToLocalInput(props.value)}
        onChange={(e) => props.onChange(localInputToJSON(e.target.value))}
        className={inputClass}
      />
    )
  } else if (isArea) {
    control = (
      <textarea
        rows={4}
        value={parseDisplay(props.value)}
        onChange={(e) => props.onChange(JSON.stringify(e.target.value))}
        placeholder={f.goType}
        className={`${inputClass} resize-y`}
      />
    )
  } else {
    control = (
      <input
        type="text"
        value={parseDisplay(props.value)}
        onChange={(e) => {
          // Encode as JSON: numeric strings become numbers, "true"/"false"
          // become booleans, everything else becomes a JSON string.
          props.onChange(encodeUserValue(e.target.value, f.goType))
        }}
        placeholder={f.goType}
        className={inputClass}
      />
    )
  }

  return (
    <label className="block">
      <span className="mb-[5px] block text-[10px] font-semibold uppercase tracking-[.09em] text-t28">
        {f.label || f.name}
        {f.isRequired && <span className="text-t51"> *</span>}
      </span>
      {control}
    </label>
  )
}

// rfc3339ToLocalInput converts a stored JSON value (a quoted RFC3339 string,
// or plain) into the "YYYY-MM-DDTHH:mm" a datetime-local input expects, in
// local time. Returns "" when the value is empty or unparseable.
function rfc3339ToLocalInput(raw: string): string {
  const s = parseDisplay(raw)
  if (!s) return ''
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number): string => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

// localInputToJSON converts a datetime-local value (local time) into a quoted
// RFC3339 (UTC) JSON string. Empty input → "".
function localInputToJSON(local: string): string {
  if (!local) return ''
  const d = new Date(local)
  if (Number.isNaN(d.getTime())) return ''
  return JSON.stringify(d.toISOString())
}

/* ------------------------------------------------------------------ */
/* JSON value helpers (unchanged data behavior)                        */
/* ------------------------------------------------------------------ */

function parseDisplay(raw: string): string {
  if (!raw) return ''
  try {
    const parsed: unknown = JSON.parse(raw)
    if (parsed === null || parsed === undefined) return ''
    if (typeof parsed === 'string') return parsed
    if (typeof parsed === 'number' || typeof parsed === 'boolean') return String(parsed)
    return JSON.stringify(parsed)
  } catch {
    return raw
  }
}

function encodeUserValue(raw: string, goType: string): string {
  if (raw === '') return ''
  const lower = goType.toLowerCase()
  if (lower.startsWith('int') || lower.startsWith('uint')) {
    const n = Number(raw)
    if (Number.isFinite(n)) return JSON.stringify(n)
  }
  if (lower.startsWith('float')) {
    const n = Number(raw)
    if (Number.isFinite(n)) return JSON.stringify(n)
  }
  if (lower === 'bool') {
    if (raw.toLowerCase() === 'true') return 'true'
    if (raw.toLowerCase() === 'false') return 'false'
  }
  return JSON.stringify(raw)
}

function formatCell(raw: string | undefined): string {
  if (!raw) return ''
  try {
    const v: unknown = JSON.parse(raw)
    if (v === null) return '∅'
    if (typeof v === 'string') {
      return v.length > 80 ? v.slice(0, 77) + '…' : v
    }
    if (typeof v === 'number' || typeof v === 'boolean') return String(v)
    return JSON.stringify(v)
  } catch {
    return raw
  }
}

function unquoteJSON(raw: string): string {
  try {
    const v: unknown = JSON.parse(raw)
    if (typeof v === 'string') return v
    if (typeof v === 'number') return String(v)
    return ''
  } catch {
    return raw
  }
}
