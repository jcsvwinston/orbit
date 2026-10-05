import { useState, useEffect, useMemo, useCallback, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { AgGridReact } from 'ag-grid-react'
import type { ColDef, GridApi, GridReadyEvent, RowSelectionOptions, SortChangedEvent, ICellRendererParams, GetRowIdParams, PostSortRowsParams } from 'ag-grid-community'
import { gridThemeDark, gridThemeLight } from '../lib/grid'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Label } from '@/components/ui/label'
import { ErrorState } from '@/components/ui/error-state'
import { useToast } from '@/components/ui/use-toast'
import { useTheme } from '@/stores/themeStore'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from '@/components/ui/dialog'
import type { ModelSchema, ModelActionSpec, ActionInputValues, Record as AppRecord, SavedView } from '@/types'
import * as api from '@/services/api'
import RecordForm from './RecordForm'
import ActionFormDialog from './ActionFormDialog'
import RecordActionsMenu from './RecordActionsMenu'
import RecordHistoryDialog from './RecordHistoryDialog'
import RenderedField from './RenderedField'
import ImportDialog from './ImportDialog'
import { formatCellValue } from '../lib/fieldValues'
import { useRecordsLoader } from '../lib/useRecordsLoader'
import { BATCH_SIZE_OPTIONS, DEFAULT_PAGE_SIZE, FILTER_DEBOUNCE_MS } from '../lib/constants'
import { primaryKeyColumn, recordId, toApiId, type RecordId } from '../lib/recordIds'
import { isSearchable } from '../lib/searchable'
import { screenCapabilities } from '../lib/capabilities'
import { gridQueryFromString, gridQueryToString } from '../lib/savedViews'
import { offeredOnRecord, offeredOnSelection } from '../lib/actionPlacement'
import { redirectDestination, saveFile } from '@/lib/actionAnswer'
import { lazyRoutes } from '@/routes'
import {
  Search, Plus, Pencil, Trash2, Loader2, History, Bookmark,
  Download, Upload, X, Filter, ChevronDown, Play,
} from 'lucide-react'

interface Props {
  modelName: string
  schema: ModelSchema
  dbAlias?: string
  // focusRecord is the record a link asked to open (?record= on the Data
  // Studio URL — where an action's redirect lands): its record view opens
  // once the grid is up. onFocusDone is told when that view closes, so the
  // link stops asking.
  focusRecord?: string | null
  onFocusDone?: () => void
}

// The first path segments the SPA's own router draws. A redirect an action
// answers with that starts with one of them is followed without reloading
// the document; any other page of the panel (an application's /x/ screen)
// is a document the server serves.
const spaRoots = lazyRoutes.map((route) => route.path)

// What an application action runs on: the grid's selection, or one record
// (its record view, its row's menu).
type ActionSubject = { kind: 'selection' } | { kind: 'record'; id: RecordId }

export default function AGGridTable({ modelName, schema, dbAlias, focusRecord, onFocusDone }: Props) {
  const { toast } = useToast()
  const navigate = useNavigate()
  // What this operator may do with this model (see lib/capabilities).
  const { canCreate, canUpdate, canDelete } = screenCapabilities(schema)
  const { theme } = useTheme()
  const [gridApi, setGridApi] = useState<GridApi | null>(null)
  const [selectedCount, setSelectedCount] = useState(0)
  // The action an application declared and this operator asked for, while
  // it is pending confirmation or running.
  const [pendingAction, setPendingAction] = useState<ModelActionSpec | null>(null)
  const [runningAction, setRunningAction] = useState(false)
  // The action whose form is open: one that declared fields asks for them
  // here instead of in the plain confirmation.
  const [formAction, setFormAction] = useState<ModelActionSpec | null>(null)
  // What the pending or open action runs on.
  const [actionSubject, setActionSubject] = useState<ActionSubject>({ kind: 'selection' })

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
  // The row whose history is open: the audit trail read by record, which is
  // what an operator asks for after a bad edit.
  const [historyId, setHistoryId] = useState<RecordId | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [bulkDeleting, setBulkDeleting] = useState(false)
  const [confirmBulk, setConfirmBulk] = useState(false)

  // Saved views: the filter set this operator returns to.
  const [savedViews, setSavedViews] = useState<SavedView[]>([])
  const [viewName, setViewName] = useState('')
  const [savingView, setSavingView] = useState(false)

  // Export/Import state
  const [showExportImport, setShowExportImport] = useState(false)
  const [exportFormat, setExportFormat] = useState<'csv' | 'json' | 'sql'>('json')
  const [isExporting, setIsExporting] = useState(false)
  const [importOpen, setImportOpen] = useState(false)

  const listFields = useMemo(() => schema.fields.filter((f) => f.is_list && !f.is_excluded), [schema])
  const filterFields = useMemo(() => schema.fields.filter((f) => f.is_filter && !f.is_excluded), [schema])
  const pkColumn = useMemo(() => primaryKeyColumn(schema), [schema])
  const searchable = useMemo(() => isSearchable(schema), [schema])

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

  // Actions the application declared for this model. The schema only
  // carries the ones this operator may run, so a button here is a button
  // they hold — the enforcer is asked again on the call regardless. Each is
  // drawn where it is offered: the toolbar for a selection, the record view
  // and the row's menu for one record.
  const declaredActions: ModelActionSpec[] = useMemo(() => schema.actions ?? [], [schema])
  const selectionActions = useMemo(() => declaredActions.filter(offeredOnSelection), [declaredActions])
  const recordActions = useMemo(() => declaredActions.filter(offeredOnRecord), [declaredActions])

  // The row menu starts an action through whatever startAction is on this
  // render; reading it through a ref keeps the column definitions — and
  // the grid's own state — from being rebuilt on every render.
  const startActionRef = useRef<(action: ModelActionSpec, subject: ActionSubject) => void>(() => {})

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
        const builtIn = f.html_type === 'checkbox'
          ? <Badge variant={value ? 'default' : 'outline'} className="text-xs">{value ? 'Yes' : 'No'}</Badge>
          : formatCellValue(f, value)
        // A field the application draws with its own renderer (EXT-07);
        // the panel's drawing is what the cell shows when it fails.
        if (f.renderer) {
          return (
            <RenderedField
              renderer={f.renderer}
              value={value}
              model={modelName}
              field={f.name}
              column={f.column}
              record={(params.data as AppRecord | undefined) ?? null}
              where="list"
              fallback={builtIn}
            />
          )
        }
        return builtIn
      },
    })),
    ...([{
      headerName: 'Actions',
      colId: '__actions',
      width: recordActions.length > 0 ? 128 : 100,
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
              onClick={() => id !== null && setHistoryId(id)}
              disabled={id === null}
              className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground disabled:opacity-40"
              title="History"
              aria-label={`History of record ${id ?? ''}`}
            >
              <History className="h-3.5 w-3.5" />
            </button>
            {canUpdate && (
              <button
                type="button"
                onClick={() => handleEdit(row)}
                className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground"
                title="Edit"
                aria-label={`Edit record ${id ?? ''}`}
              >
                <Pencil className="h-3.5 w-3.5" />
              </button>
            )}
            {canDelete && (
              <button
                type="button"
                onClick={() => id !== null && setDeleteId(id)}
                disabled={id === null}
                className="p-1 rounded hover:bg-destructive/10 text-muted-foreground hover:text-destructive-text disabled:opacity-40"
                title="Delete"
                aria-label={`Delete record ${id ?? ''}`}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            )}
            {recordActions.length > 0 && (
              <RecordActionsMenu
                actions={recordActions}
                recordLabel={String(id ?? '')}
                disabled={id === null}
                onSelect={(action) => id !== null && startActionRef.current(action, { kind: 'record', id })}
              />
            )}
          </div>
        )
      },
    } as ColDef]),
  ], [listFields, canUpdate, canDelete, pkColumn, handleEdit, recordActions, modelName])

  // Rows can be selected when something can be done with a selection: a
  // delete, or an action that runs over one. An operator who may publish
  // but not delete still has to be able to pick what to publish.
  const selectable = canDelete || selectionActions.some((action) => action.requires_selection)
  const rowSelection = useMemo<RowSelectionOptions | undefined>(
    () => (selectable ? { mode: 'multiRow', checkboxes: true, headerCheckbox: true, enableClickSelection: false } : undefined),
    [selectable],
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
    if (!searchable) return
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
      await api.updateRecord(modelName, toApiId(id), data)
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
      await api.deleteRecord(modelName, toApiId(deleteId))
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
      // Every key — integer, UUID, string — goes through the bulk endpoint
      // as a string; the backend reports per-id failures in the result.
      const { deleted, failed } = await api.bulkDelete(modelName, ids.map(toApiId))
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

  const selectedIds = (): RecordId[] => {
    const selected = (gridApi?.getSelectedRows() ?? []) as AppRecord[]
    return selected.map((row) => recordId(row, pkColumn)).filter((id): id is RecordId => id !== null)
  }

  // followRedirect takes the operator to the page an action answered with.
  // A screen of the SPA's own is reached through the router; any other
  // page of the panel is loaded as a document. A path that is not one of
  // the panel's is not followed: the server refuses those, and this is the
  // second lock on the same door.
  const followRedirect = (path: string) => {
    const destination = redirectDestination(path, spaRoots)
    if (!destination) {
      toast({ variant: 'destructive', title: 'Redirect not followed', description: `${path} is not a page of this panel.` })
      return
    }
    if (destination.router) navigate(destination.to)
    else window.location.assign(destination.href)
  }

  // answer shows what an action answered with: its message, the page it
  // sends the operator to, or the file it handed back.
  const answer = (action: ModelActionSpec, outcome: api.ActionOutcome) => {
    if (outcome.kind === 'download') {
      saveFile(outcome.blob, outcome.filename)
      toast({ title: `${action.label}: ${outcome.filename}`, description: 'The file was downloaded.' })
      return
    }
    const result = outcome.result
    // The application's own message is the one worth showing: it knows
    // what it did, the panel only knows that it ran.
    toast({
      variant: result.failed > 0 || !result.ran ? 'destructive' : 'default',
      title: result.message ?? `${action.label}: ${result.affected} record${result.affected === 1 ? '' : 's'}`,
      description: result.failed > 0 ? `${result.failed} row${result.failed === 1 ? '' : 's'} were out of scope` : undefined,
    })
    if (result.redirect) followRedirect(result.redirect)
  }

  // performAction makes the call and reports it. It throws when the server
  // refuses, so each flow decides where the refusal is shown: a toast for
  // the plain confirmation, the fields for a form.
  const performAction = async (action: ModelActionSpec, subject: ActionSubject, input?: ActionInputValues) => {
    const outcome = subject.kind === 'record'
      ? await api.runRecordAction(modelName, action.name, toApiId(subject.id), input)
      : await api.runModelAction(modelName, action.name, selectedIds().map(toApiId), input)
    if (subject.kind === 'selection') {
      gridApi?.deselectAll()
      setSelectedCount(0)
    }
    reload()
    answer(action, outcome)
  }

  const runAction = async (action: ModelActionSpec, subject: ActionSubject) => {
    if (subject.kind === 'selection' && selectedIds().length === 0 && action.requires_selection) return
    setRunningAction(true)
    try {
      await performAction(action, subject)
      setPendingAction(null)
    } catch (err) {
      toast({ variant: 'destructive', title: `${action.label} failed`, description: api.errorMessage(err) })
    } finally {
      setRunningAction(false)
    }
  }

  // The form's submit: a refusal propagates to the dialog, which puts it
  // on the fields, and the dialog closes only when the action ran.
  const submitActionForm = async (action: ModelActionSpec, input: ActionInputValues) => {
    setRunningAction(true)
    try {
      await performAction(action, actionSubject, input)
      setFormAction(null)
    } finally {
      setRunningAction(false)
    }
  }

  // An action that declared fields asks for them in a form; one with a
  // confirmation asks that; one with neither runs on the click. All of them
  // end in the same call, on the subject they were started on.
  const startAction = (action: ModelActionSpec, subject: ActionSubject) => {
    setActionSubject(subject)
    if (action.fields && action.fields.length > 0) {
      setFormAction(action)
      return
    }
    if (action.confirm) {
      setPendingAction(action)
      return
    }
    void runAction(action, subject)
  }
  useEffect(() => {
    startActionRef.current = startAction
  })

  // What the dialogs say the action runs on.
  const subjectText = (action: ModelActionSpec, subject: ActionSubject): string => {
    if (subject.kind === 'record') return `Record ${String(subject.id)}.`
    return action.requires_selection ? `${selectedCount} record${selectedCount === 1 ? '' : 's'} selected.` : ''
  }

  // The record view closes before an action started from it opens its own
  // form or confirmation: one dialog at a time.
  const startActionFromRecordView = (action: ModelActionSpec) => {
    const id = editingRecord ? recordId(editingRecord, pkColumn) : null
    closeForm()
    if (id !== null) startAction(action, { kind: 'record', id })
  }

  const closeForm = () => {
    setFormOpen(false)
    if (focusRecord) onFocusDone?.()
  }

  // A link that names a record opens its view once the grid is up — the
  // landing of an action's redirect to "/data-studio?model=…&record=…".
  // The record is read once per link: the callback is read through a ref,
  // so a parent that hands a new one on a render does not read it again
  // and reset what the operator is typing.
  const onFocusDoneRef = useRef(onFocusDone)
  useEffect(() => {
    onFocusDoneRef.current = onFocusDone
  })
  useEffect(() => {
    if (!focusRecord || schema.name !== modelName) return
    let cancelled = false
    api.getRecord(modelName, focusRecord)
      .then((record) => {
        if (cancelled) return
        setEditingRecord(record)
        setFormOpen(true)
      })
      .catch((err) => {
        if (cancelled) return
        toast({ variant: 'destructive', title: 'Could not open the record', description: api.errorMessage(err) })
        onFocusDoneRef.current?.()
      })
    return () => { cancelled = true }
  }, [focusRecord, modelName, schema.name, toast])

  const reloadViews = useCallback(async () => {
    try {
      setSavedViews(await api.getSavedViews(modelName))
    } catch {
      // A panel with no database handle has no views; the control simply
      // does not appear, which is better than an error on every model.
      setSavedViews([])
    }
  }, [modelName])

  useEffect(() => { void reloadViews() }, [reloadViews])

  const applyView = (view: SavedView) => {
    const query = gridQueryFromString(view.query)
    setSearch(query.search)
    setSearchInput(query.search)
    setFilterInput(query.filters)
    setActiveFilters(query.filters)
    setOrderBy(query.orderBy)
    if (query.pageSize) setPageSize(query.pageSize)
  }

  const saveCurrentView = async () => {
    const name = viewName.trim()
    if (!name) return
    setSavingView(true)
    try {
      await api.createSavedView({
        model: modelName,
        name,
        query: gridQueryToString({ search, filters: activeFilters, orderBy, pageSize }),
      })
      setViewName('')
      await reloadViews()
      toast({ title: 'View saved', description: name })
    } catch (err) {
      toast({ variant: 'destructive', title: 'Could not save the view', description: api.errorMessage(err) })
    } finally {
      setSavingView(false)
    }
  }

  const removeView = async (view: SavedView) => {
    try {
      await api.deleteSavedView(view.id)
      await reloadViews()
    } catch (err) {
      toast({ variant: 'destructive', title: 'Could not remove the view', description: api.errorMessage(err) })
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
            placeholder={searchable ? 'Search records...' : 'Search is not enabled for this model'}
            title={searchable ? undefined : 'No field of this model is searchable. Enable is_search in Field settings.'}
            disabled={!searchable}
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

        {selectedCount > 0 && canDelete && (
          <Button variant="destructive" size="sm" onClick={() => setConfirmBulk(true)} className="gap-1.5">
            <Trash2 className="h-3.5 w-3.5" />
            Delete {selectedCount}
          </Button>
        )}

        {/* What this application added to the model. An action over a
            selection only appears once there is one, the same way Delete
            does; one whose subject is the table is always there. */}
        {selectionActions
          .filter((action) => selectedCount > 0 || !action.requires_selection)
          .map((action) => (
            <Button
              key={action.name}
              variant={action.destructive ? 'destructive' : 'outline'}
              size="sm"
              disabled={runningAction}
              title={action.description}
              onClick={() => startAction(action, { kind: 'selection' })}
              className="gap-1.5"
            >
              <Play className="h-3.5 w-3.5" />
              {action.label}
              {action.requires_selection ? ` ${selectedCount}` : ''}
            </Button>
          ))}

        <Button variant="outline" size="sm" onClick={() => setShowExportImport(!showExportImport)} aria-expanded={showExportImport} className="gap-1.5">
          <Download className="h-3.5 w-3.5" />
          Export / Import
        </Button>

        {canCreate && (
          <Button size="sm" onClick={handleCreate} className="gap-1.5">
            <Plus className="h-3.5 w-3.5" />
            New Record
          </Button>
        )}
      </div>

      {/* Saved views: pick one, or keep the filters you are looking at. */}
      {(savedViews.length > 0 || showFilters) && (
        <div className="flex flex-wrap items-center gap-2 py-2 border-b">
          <Bookmark className="h-3.5 w-3.5 text-muted-foreground" />
          {savedViews.map((view) => (
            <span key={view.id} className="inline-flex items-center rounded-full border px-2 py-0.5 text-xs">
              <button type="button" onClick={() => applyView(view)} className="hover:underline">
                {view.name}
              </button>
              <button
                type="button"
                onClick={() => void removeView(view)}
                className="ml-1 text-muted-foreground hover:text-destructive-text"
                aria-label={`Remove the view ${view.name}`}
              >
                <X className="h-3 w-3" />
              </button>
            </span>
          ))}
          <div className="flex items-center gap-1">
            <Input
              value={viewName}
              onChange={(e) => setViewName(e.target.value)}
              placeholder="Save this view as…"
              className="h-7 w-44 text-xs"
              aria-label="Name for the current view"
            />
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="h-7 text-xs"
              disabled={!viewName.trim() || savingView}
              onClick={() => void saveCurrentView()}
            >
              {savingView ? <Loader2 className="h-3 w-3 animate-spin" /> : 'Save'}
            </Button>
          </div>
        </div>
      )}

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
          <Button size="sm" variant="outline" onClick={() => setImportOpen(true)} disabled={!canCreate} className="gap-1.5 h-8">
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
          <div style={{ height: '100%', width: '100%' }}>
            <AgGridReact
              theme={theme === 'dark' ? gridThemeDark : gridThemeLight}
              columnDefs={columnDefs}
              rowData={rows}
              getRowId={getRowId}
              rowSelection={rowSelection}
              onGridReady={onGridReady}
              onSortChanged={onSortChanged}
              postSortRows={postSortRows}
              onSelectionChanged={(e) => setSelectedCount(e.api.getSelectedRows().length)}
              autoSizeStrategy={{ type: 'fitGridWidth' }}
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
        onClose={closeForm}
        schema={schema}
        record={editingRecord}
        onSave={handleSave}
        readOnly={editingRecord !== null && !canUpdate}
        actions={recordActions}
        onAction={startActionFromRecordView}
      />

      {/* The record's own history, read from the audit trail */}
      <RecordHistoryDialog
        open={historyId !== null}
        onClose={() => setHistoryId(null)}
        modelName={modelName}
        recordId={historyId}
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

      {/* An application action that asked to be confirmed */}
      {pendingAction && (
        <Dialog open={true} onOpenChange={(val: boolean) => !val && !runningAction && setPendingAction(null)}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>{pendingAction.label}</DialogTitle>
              <DialogDescription>
                {[pendingAction.confirm, subjectText(pendingAction, actionSubject)].filter(Boolean).join(' ')}
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" onClick={() => setPendingAction(null)} disabled={runningAction}>
                Cancel
              </Button>
              <Button
                variant={pendingAction.destructive ? 'destructive' : 'default'}
                onClick={() => void runAction(pendingAction, actionSubject)}
                disabled={runningAction}
              >
                {runningAction ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                {pendingAction.label}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}

      {/* An application action that asks for something before it runs */}
      {formAction && (
        <ActionFormDialog
          action={formAction}
          subject={subjectText(formAction, actionSubject)}
          onCancel={() => setFormAction(null)}
          onSubmit={(input) => submitActionForm(formAction, input)}
        />
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
