// useRbac / useAudit are the unary ManageService wrappers. Same TanStack
// Query discipline as useNodes: short-poll for the audit log (it grows
// with fleet activity), fetch-on-mount with manual refetch for RBAC (it
// changes rarely).

import { useQuery } from '@tanstack/react-query'
import { manageClient } from '@/lib/transport'
import type { AuditEntry, RbacPolicy, RbacRole } from '@/gen/nucleus/admin/v1/admin_pb'

export interface UseRbacResult {
  roles: RbacRole[]
  policies: RbacPolicy[]
  isLoading: boolean
  isError: boolean
  error: Error | null
  refetch: () => void
}

export function useRbac(nodeId = ''): UseRbacResult {
  const query = useQuery({
    queryKey: ['admin', 'rbac', nodeId],
    queryFn: async () => {
      const resp = await manageClient.getRbac({ nodeId })
      return { roles: resp.roles, policies: resp.policies }
    },
    staleTime: 15_000,
    retry: false,
  })

  return {
    roles: query.data?.roles ?? [],
    policies: query.data?.policies ?? [],
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error,
    refetch: () => {
      void query.refetch()
    },
  }
}

export interface UseAuditResult {
  entries: AuditEntry[]
  isLoading: boolean
  isError: boolean
  error: Error | null
  refetch: () => void
}

export function useAudit(limit = 200): UseAuditResult {
  const query = useQuery({
    queryKey: ['admin', 'audit', limit],
    queryFn: async () => {
      const resp = await manageClient.listAudit({ limit })
      return resp.entries
    },
    refetchInterval: 5000,
    refetchIntervalInBackground: false,
    staleTime: 2000,
  })

  return {
    entries: query.data ?? [],
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error,
    refetch: () => {
      void query.refetch()
    },
  }
}
