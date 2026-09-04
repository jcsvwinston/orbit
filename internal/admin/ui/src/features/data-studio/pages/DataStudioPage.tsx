import { useState, useEffect, useMemo, useCallback } from 'react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ErrorState } from '@/components/ui/error-state'
import { useToast } from '@/components/ui/use-toast'
import * as api from '@/services/api'
import type { ModelSummary, ModelSchema, RuntimeInfo } from '@/types'
import ModelSidebar from '../components/ModelSidebar'
import AGGridTable from '../components/AGGridTable'
import FieldConfigPanel from '../components/FieldConfigPanel'
import { Database, Loader2, Server, Settings2 } from 'lucide-react'

export default function DataStudioPage() {
  const { toast } = useToast()

  const [models, setModels] = useState<ModelSummary[]>([])
  const [runtime, setRuntime] = useState<RuntimeInfo | null>(null)
  const [loadingModels, setLoadingModels] = useState(true)
  const [modelsError, setModelsError] = useState<unknown>(null)
  const [modelsReloadKey, setModelsReloadKey] = useState(0)

  const [selectedModel, setSelectedModel] = useState<string | null>(null)
  const [schema, setSchema] = useState<ModelSchema | null>(null)
  const [loadingSchema, setLoadingSchema] = useState(false)
  const [schemaError, setSchemaError] = useState<unknown>(null)
  const [dbAlias, setDbAlias] = useState<string | undefined>(undefined)
  const [fieldConfigOpen, setFieldConfigOpen] = useState(false)

  // Load models on mount
  useEffect(() => {
    let cancelled = false
    const load = async () => {
      setLoadingModels(true)
      setModelsError(null)
      try {
        const res = await api.getModelsWithRuntime(true)
        if (cancelled) return
        setModels(res.models ?? [])
        setRuntime(res.runtime ?? null)
      } catch (err) {
        if (!cancelled) setModelsError(err)
      } finally {
        if (!cancelled) setLoadingModels(false)
      }
    }
    load()
    return () => { cancelled = true }
  }, [modelsReloadKey])

  // Load schema when model is selected
  const loadSchema = useCallback(async (name: string) => {
    setLoadingSchema(true)
    setSchemaError(null)
    try {
      const s = await api.getModelSchema(name)
      setSchema(s)
    } catch (err) {
      setSchemaError(err)
      setSchema(null)
    } finally {
      setLoadingSchema(false)
    }
  }, [])

  useEffect(() => {
    if (!selectedModel) { setSchema(null); return }
    loadSchema(selectedModel)
  }, [selectedModel, loadSchema])

  // Databases the selected model lives on
  const modelDbs = useMemo(() => {
    if (!selectedModel || !runtime?.databases) return []
    const summary = models.find((m) => m.name === selectedModel)
    if (!summary?.databases?.length) return []
    return summary.databases.map((alias) => {
      const db = runtime.databases.find((d) => d.alias === alias)
      return {
        alias,
        engine: db?.dialect || db?.engine || 'unknown',
        isDefault: db?.is_default ?? false,
        count: summary.counts?.[alias] ?? -1,
        countKnown: summary.count_known && summary.counts?.[alias] !== undefined,
      }
    })
  }, [selectedModel, models, runtime])

  const handleSelectModel = (name: string, alias?: string) => {
    setSelectedModel(name)
    setDbAlias(alias)
  }

  const handleFieldConfigSaved = () => {
    toast({ title: 'Field configuration updated' })
    if (selectedModel) loadSchema(selectedModel)
  }

  return (
    <div className="flex flex-col h-[calc(100vh-4rem)]">
      {/* Header */}
      <div className="flex items-center justify-between pb-4">
        <div>
          <h1 className="text-2xl font-bold">Data Studio</h1>
          <p className="text-sm text-muted-foreground">
            Browse, search and manage your application data
          </p>
        </div>
      </div>

      {modelsError ? (
        <ErrorState
          error={modelsError}
          title="Failed to load models"
          onRetry={() => setModelsReloadKey((k) => k + 1)}
        />
      ) : (
      <div className="flex flex-1 min-h-0 gap-4">
        {/* Sidebar */}
        <div className="w-64 flex-shrink-0 border rounded-lg bg-card overflow-hidden">
          {loadingModels ? (
            <div className="flex items-center justify-center h-full">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <ModelSidebar
              models={models}
              runtime={runtime}
              selectedModel={selectedModel}
              selectedDbAlias={dbAlias}
              onSelectModel={handleSelectModel}
            />
          )}
        </div>

        {/* Content area */}
        <div className="flex-1 min-w-0 border rounded-lg bg-card p-4 overflow-hidden flex flex-col">
          {!selectedModel ? (
            <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-3">
              <Database className="h-12 w-12 opacity-30" />
              <p className="text-sm">Select a model from the sidebar to browse its records</p>
              {models.length > 0 && (
                <p className="text-xs">
                  {models.length} model{models.length !== 1 ? 's' : ''} registered
                  {runtime?.databases && runtime.databases.length > 1
                    ? ` across ${runtime.databases.length} databases (${runtime.engines.join(', ')})`
                    : runtime?.engines?.length
                      ? ` on ${runtime.engines.join(', ')}`
                      : ''}
                </p>
              )}
            </div>
          ) : loadingSchema ? (
            <div className="flex items-center justify-center h-full">
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : schema ? (
            <>
              {/* Model header + database selector + field config */}
              <div className="flex items-start justify-between gap-4 pb-3 mb-0">
                <div>
                  <h2 className="text-lg font-semibold leading-tight">{schema.plural || schema.name}</h2>
                  <p className="text-xs text-muted-foreground">
                    {schema.table}
                    {schema.read_only && ' (read-only)'}
                  </p>
                </div>

                <div className="flex items-center gap-2 flex-shrink-0">
                  {/* Multi-database selector */}
                  {modelDbs.length > 1 && (
                    <div className="flex items-center gap-1.5">
                      <Server className="h-3.5 w-3.5 text-muted-foreground" />
                      <div className="flex gap-1">
                        {modelDbs.map((db) => {
                          const isActive = dbAlias === db.alias || (!dbAlias && db.isDefault)
                          return (
                            <Button
                              key={db.alias}
                              variant={isActive ? 'default' : 'outline'}
                              size="sm"
                              className="h-7 text-xs gap-1.5"
                              onClick={() => setDbAlias(db.alias)}
                            >
                              {db.alias}
                              <span className="opacity-80 text-xs">{db.engine}</span>
                              {db.countKnown && (
                                <Badge variant={isActive ? 'secondary' : 'outline'} className="text-xs px-1 py-0">
                                  {db.count.toLocaleString()}
                                </Badge>
                              )}
                            </Button>
                          )
                        })}
                      </div>
                    </div>
                  )}

                  {/* Single-database indicator */}
                  {modelDbs.length === 1 && (
                    <span className="flex items-center gap-1 text-xs text-muted-foreground">
                      <Server className="h-3 w-3" />
                      {modelDbs[0].alias}
                      <span className="opacity-60">({modelDbs[0].engine})</span>
                    </span>
                  )}

                  {/* Field config button */}
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 text-xs gap-1.5"
                    onClick={() => setFieldConfigOpen(true)}
                    title="Configure fields (list, search, filter...)"
                    aria-label="Configure fields"
                  >
                    <Settings2 className="h-3.5 w-3.5" />
                    Fields
                  </Button>
                </div>
              </div>

              <AGGridTable
                modelName={selectedModel}
                schema={schema}
                dbAlias={dbAlias}
              />

              {/* Field Configuration Dialog */}
              <FieldConfigPanel
                open={fieldConfigOpen}
                onClose={() => setFieldConfigOpen(false)}
                schema={schema}
                onSaved={handleFieldConfigSaved}
              />
            </>
          ) : (
            <ErrorState
              error={schemaError ?? new Error(`No schema for ${selectedModel}`)}
              title={`Failed to load schema for ${selectedModel}`}
              onRetry={() => loadSchema(selectedModel)}
            />
          )}
        </div>
      </div>
      )}
    </div>
  )
}
