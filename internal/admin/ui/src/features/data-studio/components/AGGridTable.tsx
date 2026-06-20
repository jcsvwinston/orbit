import { useState, useEffect, useCallback, useRef } from 'react'
import { AgGridReact } from 'ag-grid-react'
import type { ColDef, GridApi, GridReadyEvent, RowSelectionOptions } from 'ag-grid-community'
import 'ag-grid-community/styles/ag-grid.css'
import 'ag-grid-community/styles/ag-theme-quartz.css'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { useToast } from '@/components/ui/use-toast'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from '@/components/ui/dialog'
import type { ModelSchema, PaginatedResult, Record as AppRecord } from '@/types'
import * as api from '@/services/api'
import RecordForm from './RecordForm'
import {
  Search, Plus, Pencil, Trash2, Loader2,
  Download, Upload, FileText, X, Filter, ChevronDown,
} from 'lucide-react'

interface Props {
  modelName: string
  schema: ModelSchema
  dbAlias?: string
}

export default function AGGridTable({ modelName, schema, dbAlias }: Props) {
  const { toast } = useToast()
  const gridRef = useRef<AgGridReact>(null)
  const [gridApi, setGridApi] = useState<GridApi | null>(null)

  // Data state
  const [result, setResult] = useState<PaginatedResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)

  // Query state
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const [search, setSearch] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [activeFilters, setActiveFilters] = useState<Record<string, string>>({})
  const [showFilters, setShowFilters] = useState(false)

  // Dialog state
  const [formOpen, setFormOpen] = useState(false)
  const [editingRecord, setEditingRecord] = useState<AppRecord | null>(null)
  const [deleteId, setDeleteId] = useState<number | null>(null)
  const [deleting, setDeleting] = useState(false)

  // Export/Import state
  const [showExportImport, setShowExportImport] = useState(false)
  const [exportFormat, setExportFormat] = useState<'csv' | 'json' | 'sql'>('json')
  const [isExporting, setIsExporting] = useState(false)
  const [isImporting, setIsImporting] = useState(false)
  const [importFile, setImportFile] = useState<File | null>(null)

  const listFields = schema.fields.filter((f) => f.is_list && !f.is_excluded)
  const filterFields = schema.fields.filter((f) => f.is_filter && !f.is_excluded)
  const pkField = schema.fields.find((f) => f.is_pk)
  const pkColumn = pkField?.column ?? 'id'

  // Build column definitions
  const columnDefs: ColDef[] = [
    ...(schema.read_only ? [] : [{
      headerCheckboxSelection: true,
      checkboxSelection: true,
      width: 50,
      suppressMenu: true,
      sortable: false,
      resizable: false,
    } as ColDef]),
    ...listFields.map((f) => ({
      field: f.column,
      headerName: f.label,
      sortable: true,
      filter: true,
      resizable: true,
      minWidth: 120,
      flex: 1,
      cellRenderer: (params: any) => {
        const value = params.value
        const htmlType = f.html_type
        const fieldType = f.type
        if (htmlType === 'checkbox') {
          return <Badge variant={value ? 'default' : 'outline'} className="text-xs">{value ? 'Yes' : 'No'}</Badge>
        }
        if (value === null || value === undefined) return '—'
        if (htmlType === 'datetime-local' || fieldType === 'time.Time') {
          if (typeof value === 'string' && value.length > 0) {
            try {
              return new Date(value).toLocaleString()
            } catch { return String(value) }
          }
        }
        const s = String(value)
        return s.length > 80 ? s.slice(0, 80) + '...' : s
      },
    })),
    ...(schema.read_only ? [] : [{
      headerName: 'Actions',
      width: 100,
      suppressMenu: true,
      sortable: false,
      filter: false,
      resizable: false,
      cellRenderer: (params: any) => {
        const row = params.data
        const id = row[pkColumn]
        return (
          <div className="flex items-center justify-end gap-1">
            <button
              onClick={() => handleEdit(row)}
              className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground"
              title="Edit"
            >
              <Pencil className="h-3.5 w-3.5" />
            </button>
            <button
              onClick={() => setDeleteId(id)}
              className="p-1 rounded hover:bg-destructive/10 text-muted-foreground hover:text-destructive"
              title="Delete"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
          </div>
        )
      },
    } as ColDef]),
  ]

  // Row selection options
  const rowSelection: RowSelectionOptions = {
    mode: 'multiRow',
    checkboxes: true,
  }

  const fetchRecords = useCallback(async (isLoadMore = false) => {
    if (isLoadMore) setLoadingMore(true)
    else setLoading(true)

    try {
      const cleanFilters: Record<string, string> = {}
      for (const [k, v] of Object.entries(activeFilters)) {
        if (v.trim()) cleanFilters[k] = v
      }
      const res = await api.getRecordsPaginated(modelName, {
        page: isLoadMore ? page + 1 : 1,
        page_size: pageSize,
        search: search || undefined,
        db_alias: dbAlias,
        filters: Object.keys(cleanFilters).length > 0 ? cleanFilters : undefined,
      })
      
      setResult(res)
      if (isLoadMore) {
        if (gridApi) {
          res.items?.forEach(item => gridApi.applyTransaction({ add: [item] }))
        }
        setPage(prev => prev + 1)
      } else {
        if (gridApi) {
          gridApi.setGridOption('rowData', res.items || [])
        }
        setPage(1)
      }
    } catch (err: any) {
      toast({ variant: 'destructive', title: 'Failed to load records', description: err.message })
    } finally {
      setLoading(false)
      setLoadingMore(false)
    }
  }, [modelName, page, pageSize, search, activeFilters, dbAlias, gridApi])

  // Reset state when model changes
  useEffect(() => {
    setPage(1)
    setSearch('')
    setSearchInput('')
    setActiveFilters({})
    setGridApi(null)
  }, [modelName, dbAlias])

  // Load data when grid is ready or when search/filters change
  useEffect(() => {
    const loadData = async () => {
      setLoading(true)
      try {
        const cleanFilters: Record<string, string> = {}
        for (const [k, v] of Object.entries(activeFilters)) {
          if (v.trim()) cleanFilters[k] = v
        }
        const res = await api.getRecordsPaginated(modelName, {
          page: 1,
          page_size: pageSize,
          search: search || undefined,
          db_alias: dbAlias,
          filters: Object.keys(cleanFilters).length > 0 ? cleanFilters : undefined,
        })
        setResult(res)
      } catch (err: any) {
        toast({ variant: 'destructive', title: 'Failed to load records', description: err.message })
        console.error('AG Grid load error:', err)
      } finally {
        setLoading(false)
      }
    }
    
    loadData()
  }, [modelName, dbAlias, pageSize, search, activeFilters])

  const onGridReady = (params: GridReadyEvent) => {
    setGridApi(params.api)
    // Auto-fit columns to container width
    setTimeout(() => {
      params.api.sizeColumnsToFit()
    }, 100)
  }

  // Handle window resize to refit columns
  useEffect(() => {
    const handleResize = () => {
      if (gridApi) {
        gridApi.sizeColumnsToFit()
      }
    }
    window.addEventListener('resize', handleResize)
    return () => window.removeEventListener('resize', handleResize)
  }, [gridApi])

  // Refit columns when data loads or changes
  useEffect(() => {
    if (gridApi && result?.items) {
      setTimeout(() => {
        gridApi.sizeColumnsToFit()
      }, 50)
    }
  }, [result, gridApi])

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    setSearch(searchInput)
    setPage(1)
  }

  const clearSearch = () => {
    setSearchInput('')
    setSearch('')
    setPage(1)
  }

  const handleCreate = () => {
    setEditingRecord(null)
    setFormOpen(true)
  }

  const handleEdit = (record: AppRecord) => {
    setEditingRecord(record)
    setFormOpen(true)
  }

  const handleSave = async (data: AppRecord) => {
    if (editingRecord) {
      const id = String(editingRecord[pkColumn] || editingRecord.id)
      await api.updateRecord(modelName, id, data)
      toast({ title: 'Record updated' })
    } else {
      await api.createRecord(modelName, data)
      toast({ title: 'Record created' })
    }
    await fetchRecords(false)
  }

  const confirmDelete = async () => {
    if (deleteId === null) return
    setDeleting(true)
    try {
      await api.deleteRecord(modelName, String(deleteId))
      toast({ title: 'Record deleted' })
      setDeleteId(null)
      fetchRecords(false)
    } catch (err: any) {
      toast({ variant: 'destructive', title: 'Delete failed', description: err.message })
    } finally {
      setDeleting(false)
    }
  }

  const handleBulkDelete = async () => {
    const selectedNodes = gridApi?.getSelectedNodes() || []
    if (selectedNodes.length === 0) return
    const ids = selectedNodes.map(node => node.data[pkColumn] as number)
    try {
      const res = await api.bulkDelete(modelName, ids)
      toast({ title: `Deleted ${res.deleted} record(s)${res.failed > 0 ? `, ${res.failed} failed` : ''}` })
      fetchRecords(false)
    } catch (err: any) {
      toast({ variant: 'destructive', title: 'Bulk delete failed', description: err.message })
    }
  }

  const handleExport = async () => {
    setIsExporting(true)
    try {
      const url = await api.exportData(exportFormat, modelName)
      toast({ title: 'Export successful' })
      if (url) window.open(url, '_blank')
    } catch (err: any) {
      toast({ variant: 'destructive', title: 'Export failed', description: err.message })
    } finally {
      setIsExporting(false)
    }
  }

  const handleImport = async () => {
    if (!importFile) return
    setIsImporting(true)
    try {
      await api.importData(importFile)
      toast({ title: 'Import successful' })
      setImportFile(null)
      fetchRecords(false)
    } catch (err: any) {
      toast({ variant: 'destructive', title: 'Import failed', description: err.message })
    } finally {
      setIsImporting(false)
    }
  }

  const updateFilter = (column: string, value: string) => {
    setActiveFilters((prev) => ({ ...prev, [column]: value }))
    setPage(1)
  }

  const clearFilters = () => {
    setActiveFilters({})
    setPage(1)
  }

  const activeFilterCount = Object.values(activeFilters).filter((v) => v.trim()).length

  const total = result?.total ?? 0
  const isEstimated = result?.is_estimated ?? false
  const hasMore = result?.has_more ?? false

  return (
    <div className="flex flex-col h-full">
      {/* Toolbar */}
      <div className="flex flex-wrap items-center gap-2 pb-3 border-b">
        <form onSubmit={handleSearch} className="relative flex-1 min-w-[200px] max-w-sm">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search records..."
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            className="pl-8 pr-8 h-9"
          />
          {searchInput && (
            <button type="button" onClick={clearSearch} className="absolute right-2.5 top-2.5 text-muted-foreground hover:text-foreground">
              <X className="h-4 w-4" />
            </button>
          )}
        </form>

        {filterFields.length > 0 && (
          <Button variant="outline" size="sm" onClick={() => setShowFilters(!showFilters)} className="gap-1.5">
            <Filter className="h-3.5 w-3.5" />
            Filters
            {activeFilterCount > 0 && (
              <Badge variant="secondary" className="text-[10px] px-1.5">{activeFilterCount}</Badge>
            )}
          </Button>
        )}

        <div className="flex-1" />

        {gridApi && gridApi.getSelectedRows().length > 0 && !schema.read_only && (
          <Button variant="destructive" size="sm" onClick={handleBulkDelete} className="gap-1.5">
            <Trash2 className="h-3.5 w-3.5" />
            Delete {gridApi.getSelectedRows().length}
          </Button>
        )}

        <Button variant="outline" size="sm" onClick={() => setShowExportImport(!showExportImport)} className="gap-1.5">
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
            if (f.choices && f.choices.length > 0) {
              return (
                <div key={f.column} className="space-y-1">
                  <label className="text-xs text-muted-foreground">{f.label}</label>
                  <select
                    value={activeFilters[f.column] ?? ''}
                    onChange={(e) => updateFilter(f.column, e.target.value)}
                    className="flex h-8 rounded-md border border-input bg-background px-2 text-xs ring-offset-background focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
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
                  <label className="text-xs text-muted-foreground">{f.label}</label>
                  <select
                    value={activeFilters[f.column] ?? ''}
                    onChange={(e) => updateFilter(f.column, e.target.value)}
                    className="flex h-8 rounded-md border border-input bg-background px-2 text-xs ring-offset-background focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
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
                <label className="text-xs text-muted-foreground">{f.label}</label>
                <Input
                  value={activeFilters[f.column] ?? ''}
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
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">Format</label>
              <div className="flex gap-1">
                {(['csv', 'json', 'sql'] as const).map((fmt) => (
                  <button
                    key={fmt}
                    onClick={() => setExportFormat(fmt)}
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
          <div className="flex items-end gap-2">
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">Import file</label>
              <label className="flex items-center gap-1.5 px-2 py-1 rounded border border-dashed text-xs cursor-pointer hover:bg-muted h-8">
                <FileText className="h-3.5 w-3.5" />
                {importFile ? importFile.name : 'Choose file...'}
                <input type="file" accept=".csv,.json,.sql" onChange={(e) => setImportFile(e.target.files?.[0] || null)} className="hidden" />
              </label>
            </div>
            <Button size="sm" variant="outline" onClick={handleImport} disabled={isImporting || !importFile} className="gap-1.5 h-8">
              {isImporting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
              Import
            </Button>
          </div>
        </div>
      )}

      {/* AG Grid */}
      <div className="flex-1 overflow-auto mt-3">
        {loading && !gridApi ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <div className="ag-theme-quartz" style={{ height: '100%', width: '100%' }}>
            <AgGridReact
              ref={gridRef}
              columnDefs={columnDefs}
              rowData={result?.items || []}
              rowSelection={rowSelection}
              onGridReady={onGridReady}
              pagination={false}
              suppressPaginationPanel={true}
              domLayout="autoHeight"
              loading={loading}
            />
          </div>
        )}
      </div>

      {/* Load More */}
      {hasMore && (
        <div className="flex justify-center py-4">
          <Button 
            variant="outline" 
            size="sm" 
            onClick={() => fetchRecords(true)} 
            disabled={loadingMore}
            className="gap-2"
          >
            {loadingMore ? <Loader2 className="h-4 w-4 animate-spin" /> : <ChevronDown className="h-4 w-4" />}
            Load More Records
          </Button>
        </div>
      )}

      {/* Pagination info */}
      {result && (
        <div className="flex items-center justify-between pt-3 border-t text-sm">
          <div className="flex items-center gap-2 text-muted-foreground">
            <span>
              Showing <span className="font-medium text-foreground">{gridApi?.getDisplayedRowCount() || 0}</span> records
              {total > 0 && (
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
            <span className="text-xs text-muted-foreground">Batch size:</span>
            <select
              value={pageSize}
              onChange={(e: React.ChangeEvent<HTMLSelectElement>) => setPageSize(Number(e.target.value))}
              className="h-7 rounded border border-input bg-background px-1.5 text-xs font-medium focus:outline-none focus:ring-1 focus:ring-primary"
            >
              {[25, 50, 100, 200, 500].map((size) => (
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

      {/* Delete Confirmation Dialog */}
      {deleteId !== null && (
        <Dialog open={true} onOpenChange={(val: boolean) => !val && setDeleteId(null)}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Delete {schema.name}</DialogTitle>
              <DialogDescription>
                Are you sure you want to delete record #{deleteId}? This action cannot be undone.
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
    </div>
  )
}
