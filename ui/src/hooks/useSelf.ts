// useSelf fetches the caller's identity + the server version from
// ControlService.GetSelf (OR-UX-P1-6). The operator can then see who their
// actions are audited as, and which server build they are on; the UI also
// hides Data Studio mutation controls when the operator is read-only.

import { useQuery } from '@tanstack/react-query'
import { controlClient } from '@/lib/transport'
import type { SelfInfo } from '@/gen/nucleus/admin/v1/admin_pb'

export interface UseSelfResult {
  self: SelfInfo | undefined
  readOnly: boolean
  isLoading: boolean
}

export function useSelf(): UseSelfResult {
  const query = useQuery({
    queryKey: ['admin', 'self'],
    queryFn: async (): Promise<SelfInfo> => controlClient.getSelf({}),
    staleTime: 60_000,
    retry: false,
  })
  return {
    self: query.data,
    readOnly: query.data?.readOnly ?? false,
    isLoading: query.isLoading,
  }
}
