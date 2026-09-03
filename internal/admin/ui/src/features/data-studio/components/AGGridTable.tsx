import { useState, useEffect, useMemo, useCallback, useRef } from 'react'
import { AgGridReact } from 'ag-grid-react'
import type { ColDef, GridApi, GridReadyEvent, RowSelectionOptions, SortChangedEvent, ICellRendererParams, GetRowIdParams, PostSortRowsParams } from 'ag-grid-community'
import 'ag-grid-community/styles/ag-grid.css'
import 'ag-grid-community/styles/ag-theme-quartz.css'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Label } from '@/components/ui/label'
import { ErrorState } from '@/components/ui/error-state'
import { useToast } from '@/components/ui/use-toast'
import { useTheme } from '@/stores/themeStore'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from '@/components/ui/dialog'
import type { ModelSchema, Record as AppRecord } from '@/types'
import * as api from '@/services/api'
import RecordForm from './RecordForm'
import ImportDialog from './ImportDialog'
import { formatCellValue } from '../lib/fieldValues'
import { useRecordsLoader } from '../lib/useRecordsLoader'
import { BATCH_SIZE_OPTIONS, DEFAULT_PAGE_SIZE, FILTER_DEBOUNCE_MS } from '../lib/constants'
import {
  Search, Plus, Pencil, Trash2, Loader2,
  Download, Upload, X, Filter, ChevronDown,
} from 'lucide-react'

interface Props {
  modelName: string
  schema: ModelSchema
  dbAlias?: string
}

// Primary keys are whatever the driver decoded: uint ids, UUID strings,
// composite-ish strings. The SPA never assumes a number.
export type RecordId = string | number

// primaryKeyColumn resolves the column that carries the record id: the
// schema's declared primary key (a Go field name), else the field flagged
// is_pk, else "id".
export function primaryKeyColumn(schema: ModelSchema): string {
  const declared = schema.primary_key
    ? schema.fields.find((f) => f.name === schema.primary_key || f.column === schema.primary_key)
    : undefined
  return declared?.column ?? schema.fields.find((f) => f.is_pk)?.column ?? 'id'
}

function recordId(row: AppRecord, pkColumn: string): RecordId | null {
  const v = row[pkColumn] ?? row.id
  if (typeof v === 'number' || typeof v === 'string') return v
  return null
}

// The bulk endpoint decodes ids as []uint, so only non-negative integers
// (0 included) can travel through it; anything else is deleted one by one.
export function asUintId(id: RecordId): number | null {
  if (typeof id === 'number') return Number.isInteger(id) && id >= 0 ? id : null
  if (/^\d+$/.test(id)) return Number(id)
  return null
}

export default function AGGridTable({ modelName, schema, dbAlias }: Props) {
  const { toast } = useToast()
  const { theme } = useTheme()
  const [gridApi, setGridApi] = useState<GridApi | null>(null)
  const [selectedCount, setSelectedCount] = useState(0)

  // Query state
  const [pageSize, setPageSize] = useState<number>(DEFAULT_PAGE_SIZE)
  const [search, setSearch] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [filterInput, setFilterInput] = useState<{ [column: string]: string }>({})
  const [activeFilters, setActiveFilters] = useState<{ [column: string]: string }>({})
  const [orderBy, setOrderBy] = useState('')
  const [showFilters, setShowFilters] = useState(false)

  // Dialog state
  const [formOpen, setFormOpen] = useState(false)
  const [editingRecord, setEditingRecord] = useState<AppRecord | null>(null)
  const [deleteId, setDeleteId] = useState<RecordId | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [bulkDeleting, setBulkDeleting] = useState(false)
  const [confirmBulk, setConfirmBulk] = useState(false)

  // Export/Import state
  const [showExportImport, setShowExportImport] = useState(false)
  const [exportFormat, setExportFormat] = useState<'csv' | 'json' | 'sql'>('json')
  const [isExporting, setIsExporting] = useState(false)
  const [importOpen, setImportOpen] = useState(false)

  const listFields = useMemo(() => schema.fields.filter((f) => f.is_list && !f.is_excluded), [schema])
  const filterFields = useMemo(() => schema.fields.filter((f) => f.is_filter && !f.is_excluded), [schema])
  const pkColumn = useMemo(() => primaryKeyColumn(schema), [schema])

  const loader = useRecordsLoader({ modelName, dbAlias, pageSize, search, filters: activeFilters, orderBy })
  const { rows, total, isEstimated, hasMore, loading, loadingMore, error, reload, loadMore } = loader

  // Reset query state when the model changes
  useEffect(() => {
    setSearch('')
    setSearchInput('')
    setFilterInput({})
    setActiveFilters({})
    setOrderBy('')
    setSelectedCount(0)
  }, [modelName, dbAlias])

  // Text filters are debounced into the query; the loader resets on change.
  useEffect(() => {
    const timeout = setTimeout(() => setActiveFilters(filterInput), FILTER_DEBOUNCE_MS)
    return () => clearTimeout(timeout)
  }, [filterInput])

  const handleEdit = useCallback((record: AppRecord) => {
    setEditingRecord(record)
    setFormOpen(true)
  }, [])

  // Build column definitions
  const columnDefs = useMemo<ColDef[]>(() => [
    ...listFields.map((f) => ({
      field: f.column,
      colId: f.column,
      headerName: f.label,
      sortable: true,
      filter: false,
      resizable: true,
      minWidth: 120,
      flex: 1,
      cellRenderer: (params: ICellRendererParams) => {
        const value = params.value
        if (f.html_type === 'checkbox') {
          return <Badge variant={value ? 'default' : 'outline'} className="text-xs">{value ? 'Yes' : 'No'}</Badge>
        }
        return formatCellValue(f, value)
      },
    })),
    ...(schema.read_only ? [] : [{
      headerName: 'Actions',
      colId: '__actions',
      width: 100,
      sortable: false,
      filter: false,
      resizable: false,
      suppressHeaderMenuButton: true,
      cellRenderer: (params: ICellRendererParams) => {
        const row = params.data as AppRecord
        const id = recordId(row, pkColumn)
        return (
          <div className="flex items-center justify-end gap-1">
            <button
              type="button"
              onClick={() => handleEdit(row)}
              className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground"
              title="Edit"
              aria-label={`Edit record ${id ?? ''}`}
            >
              <Pencil className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              onClick={() => id !== null && setDeleteId(id)}
              disabled={id === null}
              className="p-1 rounded hover:bg-destructive/10 text-muted-foreground hover:text-destructive disabled:opacity-40"
              title="Delete"
              aria-label={`Delete record ${id ?? ''}`}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
          </div>
        )
      },
    } as ColDef]),
  ], [listFields, schema.read_only, pkColumn, handleEdit])

  const rowSelection = useMemo<RowSelectionOptions | undefined>(
    () => (schema.read_only ? undefined : { mode: 'multiRow', checkboxes: true, headerCheckbox: true, enableClickSelection: false }),
    [schema.read_only],
  )

  const rowKey = useCallback((row: AppRecord) => {
    const id = recordId(row, pkColumn)
    return id === null ? JSON.stringify(row) : String(id)
  }, [pkColumn])

  const getRowId = useCallback((params: GetRowIdParams) => rowKey(params.data as AppRecord), [rowKey])

  // Sorting is server-side (order_by). A header click still makes AG Grid
  // sort the loaded page client-side — and with keyed rows its tie-breaker
  // is the row's original position, not the new array order — so after
  // every grid sort the nodes are put back in the order the server sent.
  const rowOrderRef = useRef<Map<string, number>>(new Map())
  useEffect(() => {
    rowOrderRef.current = new Map(rows.map((row, i) => [rowKey(row), i]))
    gridApi?.refreshClientSideRowModel('sort')
  }, [rows, rowKey, gridApi])

  const postSortRows = useCallback((params: PostSortRowsParams) => {
    const order = rowOrderRef.current
    params.nodes.sort((a, b) => (order.get(a.id ?? '') ?? 0) - (order.get(b.id ?? '') ?? 0))
  }, [])

  const onGridReady = (params: GridReadyEvent) => {
    setGridApi(params.api)
  }

  const onSortChanged = (event: SortChangedEvent) => {
    const sorted = event.api.getColumnState().find((c) => c.sort)
    setOrderBy(sorted ? `${sorted.colId} ${sorted.sort}` : '')
  }

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    setSearch(searchInput)
  }

  const clearSearch = () => {
    setSearchInput('')
    setSearch('')
  }

  const handleCreate = () => {
    setEditingRecord(null)
    setFormOpen(true)
  }

  const handleSave = async (data: AppRecord) => {
    if (editingRecord) {
      const id = recordId(editingRecord, pkColumn)
      if (id === null) throw new Error('Record has no primary key value')
      await api.updateRecord(modelName, String(id), data)
      toast({ title: 'Record updated' })
    } else {
      await api.createRecord(modelName, data)
      toast({ title: 'Record created' })
    }
    reload()
  }

  const confirmDelete = async () => {
    if (deleteId === null) return
    setDeleting(true)
    try {
      await api.deleteRecord(modelName, String(deleteId))
      toast({ title: 'Record deleted' })
      setDeleteId(null)
      reload()
    } catch (err) {
      toast({ variant: 'destructive', title: 'Delete failed', description: api.errorMessage(err) })
    } finally {
      setDeleting(false)
    }
  }

  const handleBulkDelete = async () => {
    const selected = (gridApi?.getSelectedRows() ?? []) as AppRecord[]
    const ids = selected.map((row) => recordId(row, pkColumn)).filter((id): id is RecordId => id !== null)
    if (ids.length === 0) return
    setBulkDeleting(true)
    try {
      const numeric = ids.map(asUintId)
      let deleted = 0
      let failed = 0
      if (numeric.every((n) => n !== null)) {
        const res = await api.bulkDelete(modelName, numeric as number[])
        deleted = res.deleted
        failed = res.failed
      } else {
        // Non-numeric keys cannot go through the []uint bulk endpoint.
        for (const id of ids) {
          try {
            await api.deleteRecord(modelName, String(id))
            deleted++
          } catch {
            failed++
          }
        }
      }
      toast({
        variant: failed > 0 ? 'destructive' : 'default',
        title: `Deleted ${deleted} record${deleted === 1 ? '' : 's'}${failed > 0 ? `, ${failed} failed` : ''}`,
      })
      setConfirmBulk(false)
      gridApi?.deselectAll()
      setSelectedCount(0)
      reload()
    } catch (err) {
      toast({ variant: 'destructive', title: 'Bulk delete failed', description: api.errorMessage(err) })
    } finally {
      setBulkDeleting(false)
    }
  }

  const handleExport = async () => {
    setIsExporting(true)
    try {
      const url = await api.exportData(exportFormat, modelName)
      toast({ title: 'Export ready', description: url ? 'The download opens in a new tab.' : 'The export was queued.' })
      if (url) window.open(url, '_blank')
    } catch (err) {
      toast({ variant: 'destructive', title: 'Export failed', description: api.errorMessage(err) })
    } finally {
      setIsExporting(false)
    }
  }

  const updateFilter = (column: string, value: string, immediate = false) => {
    setFilterInput((prev) => ({ ...prev, [column]: value }))
    if (immediate) setActiveFilters((prev) => ({ ...prev, [column]: value }))
  }

  const clearFilters = () => {
    setFilterInput({})
    setActiveFilters({})
  }

  const activeFilterCount = Object.values(activeFilters).filter((v) => v.trim()).length
  const selectClass = 'flex h-8 rounded-md border border-input bg-background px-2 text-xs ring-offset-background focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring'

  return (
    <div className="flex flex-col h-full">
      {/* Toolbar */}
      <div className="flex flex-wrap items-center gap-2 pb-3 border-b">
        <form onSubmit={handleSearch} role="search" className="relative flex-1 min-w-[200px] max-w-sm">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" aria-hidden="true" />
          <Input
            aria-label="Search records"
            placeholder="Search records..."
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            className="pl-8 pr-8 h-9"
          />
          {searchInput && (
            <button type="button" onClick={clearSearch} aria-label="Clear search" className="absolute right-2.5 top-2.5 text-muted-foreground hover:text-foreground">
              <X className="h-4 w-4" />
            </button>
          )}
        </form>

        {filterFields.length > 0 && (
          <Button variant="outline" size="sm" onClick={() => setShowFilters(!showFilters)} aria-expanded={showFilters} className="gap-1.5">
            <Filter className="h-3.5 w-3.5" />
            Filters
            {activeFilterCount > 0 && (
              <Badge variant="secondary" className="text-xs px-1.5">{activeFilterCount}</Badge>
            )}
          </Button>
        )}

        <div className="flex-1" />

        {selectedCount > 0 && !schema.read_only && (
          <Button variant="destructive" size="sm" onClick={() => setConfirmBulk(true)} className="gap-1.5">
            <Trash2 className="h-3.5 w-3.5" />
            Delete {selectedCount}
          </Button>
        )}

        <Button variant="outline" size="sm" onClick={() => setShowExportImport(!showExportImport)} aria-expanded={showExportImport} className="gap-1.5">
          <Download className="h-3.5 w-3.5" />
          Export / Import
        </Button>

        {!schema.read_only && (
          <Button size="sm" onClick={handleCreate} className="gap-1.5">
            <Plus className="h-3.5 w-3.5" />
            New Record
          </Button>
        )}
      </div>

      {/* Filter bar */}
      {showFilters && filterFields.length > 0 && (
        <div className="flex flex-wrap items-end gap-3 py-3 border-b">
          {filterFields.map((f) => {
            const id = `filter-${f.column}`
            if (f.choices && f.choices.length > 0) {
              return (
                <div key={f.column} className="space-y-1">
                  <Label htmlFor={id} className="text-xs text-muted-foreground">{f.label}</Label>
                  <select
                    id={id}
                    value={filterInput[f.column] ?? ''}
                    onChange={(e) => updateFilter(f.column, e.target.value, true)}
                    className={selectClass}
                  >
                    <option value="">All</option>
                    {f.choices.map((c) => (
                      <option key={c.value} value={c.value}>{c.label || c.value}</option>
                    ))}
                  </select>
                </div>
              )
            }
            if (f.html_type === 'checkbox') {
              return (
                <div key={f.column} className="space-y-1">
                  <Label htmlFor={id} className="text-xs text-muted-foreground">{f.label}</Label>
                  <select
                    id={id}
                    value={filterInput[f.column] ?? ''}
                    onChange={(e) => updateFilter(f.column, e.target.value, true)}
                    className={selectClass}
                  >
                    <option value="">All</option>
                    <option value="1">Yes</option>
                    <option value="0">No</option>
                  </select>
                </div>
              )
            }
            return (
              <div key={f.column} className="space-y-1">
                <Label htmlFor={id} className="text-xs text-muted-foreground">{f.label}</Label>
                <Input
                  id={id}
                  value={filterInput[f.column] ?? ''}
                  onChange={(e) => updateFilter(f.column, e.target.value)}
                  placeholder={f.label}
                  className="h-8 text-xs w-32"
                />
              </div>
            )
          })}
          {activeFilterCount > 0 && (
            <Button variant="ghost" size="sm" onClick={clearFilters} className="text-xs h-8">
              Clear all
            </Button>
          )}
        </div>
      )}

      {/* Export/Import panel */}
      {showExportImport && (
        <div className="flex flex-wrap items-end gap-4 py-3 border-b">
          <div className="flex items-end gap-2">
            <div className="space-y-1" role="group" aria-labelledby="export-format-label">
              <span id="export-format-label" className="block text-xs text-muted-foreground">Format</span>
              <div className="flex gap-1">
                {(['csv', 'json', 'sql'] as const).map((fmt) => (
                  <button
                    key={fmt}
                    type="button"
                    onClick={() => setExportFormat(fmt)}
                    aria-pressed={exportFormat === fmt}
                    className={`px-2 py-1 rounded text-xs transition-colors ${
                      exportFormat === fmt ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    {fmt.toUpperCase()}
                  </button>
                ))}
              </div>
            </div>
            <Button size="sm" variant="outline" onClick={handleExport} disabled={isExporting} className="gap-1.5 h-8">
              {isExporting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
              Export
            </Button>
          </div>
          <div className="h-6 w-px bg-border" />
          <Button size="sm" variant="outline" onClick={() => setImportOpen(true)} disabled={schema.read_only} className="gap-1.5 h-8">
            <Upload className="h-3.5 w-3.5" />
            Import…
          </Button>
        </div>
      )}

      {/* AG Grid */}
      <div className="flex-1 overflow-auto mt-3">
        {error ? (
          <ErrorState error={error} title="Failed to load records" onRetry={reload} />
        ) : loading && rows.length === 0 && !gridApi ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" aria-label="Loading" />
          </div>
        ) : (
          <div className={theme === 'dark' ? 'ag-theme-quartz-dark' : 'ag-theme-quartz'} style={{ height: '100%', width: '100%' }}>
            <AgGridReact
              columnDefs={columnDefs}
              rowData={rows}
              getRowId={getRowId}
              rowSelection={rowSelection}
              onGridReady={onGridReady}
              onSortChanged={onSortChanged}
              postSortRows={postSortRows}
              onSelectionChanged={(e) => setSelectedCount(e.api.getSelectedRows().length)}
              autoSizeStrategy={{ type: 'fitGridWidth' }}
              pagination={false}
              suppressPaginationPanel={true}
              domLayout="autoHeight"
              loading={loading}
            />
          </div>
        )}
      </div>

      {/* Load More */}
      {hasMore && !error && (
        <div className="flex justify-center py-4">
          <Button
            variant="outline"
            size="sm"
            onClick={loadMore}
            disabled={loadingMore || loading}
            className="gap-2"
          >
            {loadingMore ? <Loader2 className="h-4 w-4 animate-spin" /> : <ChevronDown className="h-4 w-4" />}
            Load More Records
          </Button>
        </div>
      )}

      {/* Pagination info */}
      {!error && (
        <div className="flex items-center justify-between pt-3 border-t text-sm">
          <div className="flex items-center gap-2 text-muted-foreground">
            <span aria-live="polite">
              Showing <span className="font-medium text-foreground">{rows.length.toLocaleString()}</span> records
              {total !== 0 && (
                <>
                  {' '}of{' '}
                  <span className="font-medium text-foreground">
                    {total === -1 ? 'many' : total.toLocaleString()}
                  </span>
                  {isEstimated && <span className="ml-1 opacity-70">(estimated)</span>}
                </>
              )}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <Label htmlFor="batch-size" className="text-xs text-muted-foreground">Batch size:</Label>
            <select
              id="batch-size"
              value={pageSize}
              onChange={(e: React.ChangeEvent<HTMLSelectElement>) => setPageSize(Number(e.target.value))}
              className="h-7 rounded border border-input bg-background px-1.5 text-xs font-medium focus:outline-none focus:ring-1 focus:ring-primary"
            >
              {BATCH_SIZE_OPTIONS.map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </select>
          </div>
        </div>
      )}

      {/* Create/Edit Dialog */}
      <RecordForm
        open={formOpen}
        onClose={() => setFormOpen(false)}
        schema={schema}
        record={editingRecord}
        onSave={handleSave}
      />

      {/* Import dialog */}
      <ImportDialog
        open={importOpen}
        onClose={() => setImportOpen(false)}
        modelName={modelName}
        onImported={reload}
      />

      {/* Delete Confirmation Dialog */}
      {deleteId !== null && (
        <Dialog open={true} onOpenChange={(val: boolean) => !val && !deleting && setDeleteId(null)}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Delete {schema.name}</DialogTitle>
              <DialogDescription>
                Are you sure you want to delete record <span className="font-mono">{String(deleteId)}</span>? This action cannot be undone.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" onClick={() => setDeleteId(null)} disabled={deleting}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={confirmDelete} disabled={deleting}>
                {deleting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                Delete
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}

      {/* Bulk delete confirmation */}
      {confirmBulk && (
        <Dialog open={true} onOpenChange={(val: boolean) => !val && !bulkDeleting && setConfirmBulk(false)}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Delete {selectedCount} record{selectedCount === 1 ? '' : 's'}</DialogTitle>
              <DialogDescription>
                The selected {schema.plural || schema.name} will be deleted. This action cannot be undone.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" onClick={() => setConfirmBulk(false)} disabled={bulkDeleting}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={handleBulkDelete} disabled={bulkDeleting}>
                {bulkDeleting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                Delete
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}
