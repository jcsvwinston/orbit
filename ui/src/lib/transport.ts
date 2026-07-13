// Connect-Web transport + service client singletons.
//
// The transport hits "/" so the same bundle runs in dev (Vite proxy
// rewrites /nucleus.admin.v1.* to the admin server) and in production
// (admin server serves the UI and the RPC paths from the same origin).

import { createPromiseClient, type Interceptor } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import { Code, ConnectError } from '@connectrpc/connect'
import {
  ControlService,
  DataStudioService,
  ManageService,
} from '@/gen/nucleus/admin/v1/admin_connect.js'

// unauthorizedListeners are notified whenever any RPC comes back
// Unauthenticated. App subscribes to render the not-authorized screen
// instead of leaking a raw network error on every page (OR-UX-P1-5).
type UnauthorizedListener = (err: ConnectError) => void
const unauthorizedListeners = new Set<UnauthorizedListener>()

export function onUnauthorized(fn: UnauthorizedListener): () => void {
  unauthorizedListeners.add(fn)
  return () => {
    unauthorizedListeners.delete(fn)
  }
}

const authInterceptor: Interceptor = (next) => async (req) => {
  try {
    return await next(req)
  } catch (err) {
    if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
      unauthorizedListeners.forEach((fn) => fn(err))
    }
    throw err
  }
}

const transport = createConnectTransport({
  baseUrl: '/',
  useBinaryFormat: false,
  credentials: 'same-origin',
  interceptors: [authInterceptor],
})

export const controlClient = createPromiseClient(ControlService, transport)
export const dataStudioClient = createPromiseClient(DataStudioService, transport)
export const manageClient = createPromiseClient(ManageService, transport)
