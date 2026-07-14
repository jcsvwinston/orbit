// TanStack Query wrappers around DataStudioService. Mutations
// invalidate the related query keys so the UI auto-refreshes after a
// CRUD operation.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { dataStudioClient } from '@/lib/transport'
import type { BulkActionResponse, ModelInfo, ModelSchema, PaginatedRecords, Record as PBRecord } from '@/gen/nucleus/admin/v1/admin_pb'

// nodeId targets a specific connected agent for the operation ("" = the
// server picks any agent that knows the model). It threads through every
// Data Studio request so a multi-node fleet can browse one node's data.
export function useModels(includeCounts = false, nodeId = '') {
  return useQuery({
    queryKey: ['data-studio', 'models', includeCounts, nodeId],
    queryFn: async (): Promise<ModelInfo[]> => {
      const resp = await dataStudioClient.listModels({ includeCounts, nodeId })
      return resp.models
    },
    staleTime: 10_000,
  })
}

export function useSchema(modelName: string | null, nodeId = '') {
  return useQuery({
    queryKey: ['data-studio', 'schema', modelName, nodeId],
    enabled: !!modelName,
    queryFn: async (): Promise<ModelSchema> => {
      return dataStudioClient.getSchema({ modelName: modelName ?? '', nodeId })
    },
    staleTime: 60_000,
  })
}

export interface ListRecordsParams {
  modelName: string
  page: number
  pageSize: number
  search?: string
  orderBy?: string
  nodeId?: string
}

export function useRecords(params: ListRecordsParams | null) {
  return useQuery({
    queryKey: [
      'data-studio',
      'records',
      params?.modelName,
      params?.page,
      params?.pageSize,
      params?.search,
      params?.orderBy,
      params?.nodeId,
    ],
    enabled: !!params,
    queryFn: async (): Promise<PaginatedRecords> => {
      if (!params) throw new Error('params required')
      return dataStudioClient.listRecords({
        modelName: params.modelName,
        page: params.page,
        pageSize: params.pageSize,
        search: params.search ?? '',
        orderBy: params.orderBy ?? '',
        nodeId: params.nodeId ?? '',
      })
    },
    staleTime: 0,
  })
}

export function useDeleteRecord(modelName: string, nodeId = '') {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: string): Promise<boolean> => {
      const resp = await dataStudioClient.deleteRecord({ modelName, id, nodeId })
      return resp.deleted
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['data-studio', 'records', modelName] })
    },
  })
}

// useBulkAction runs a BulkAction (today: delete) over a set of record ids on
// one model. The RPC + agent + audit are already wired; the UI just calls it.
export function useBulkAction(modelName: string, nodeId = '') {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: { action: string; ids: string[] }): Promise<BulkActionResponse> => {
      return dataStudioClient.bulkAction({ modelName, nodeId, action: vars.action, ids: vars.ids })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['data-studio', 'records', modelName] })
    },
  })
}

export interface SaveRecordVars {
  id?: string
  values: Record<string, string>
}

export function useSaveRecord(modelName: string, nodeId = '') {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: SaveRecordVars): Promise<PBRecord> => {
      if (vars.id !== undefined && vars.id !== '') {
        return dataStudioClient.updateRecord({
          modelName,
          id: vars.id,
          nodeId,
          record: { valuesJson: vars.values },
        })
      }
      return dataStudioClient.createRecord({
        modelName,
        nodeId,
        record: { valuesJson: vars.values },
      })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['data-studio', 'records', modelName] })
    },
  })
}
