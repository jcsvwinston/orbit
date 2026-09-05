import type {
  Session, SessionsResponse, Record as AppRecord, AuditLogPage, AuditLogQuery, RBACPolicy, RBACPoliciesResponse,
  HealthCheck, LiveRequest, LiveQuery, LiveFeedEntry, ModelsResponse, ModelSchema, PaginatedResult, SystemSnapshot,
} from '@/types'
import { buildAdminPath } from '@/config'

// ApiError carries the HTTP status and the decoded error body so pages can
// tell a 403 (no permission) from a 500 (something broke) and show the
// backend's own message instead of a generic one.
export class ApiError extends Error {
  readonly status: number
  readonly body: unknown

  constructor(status: number, message: string, body: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.body = body
  }

  get isForbidden(): boolean {
    return this.status === 403
  }
}

export function isApiError(err: unknown): err is ApiError {
  return err instanceof ApiError
}

export function isAbortError(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError'
}

// errorMessage extracts a human message from anything a call site catches.
export function errorMessage(err: unknown, fallback = 'Unexpected error'): string {
  if (err instanceof Error && err.message) return err.message
  if (typeof err === 'string' && err) return err
  return fallback
}

// The backend answers errors as {"error": {"code","message"}} (domain errors)
// or {"error": "message"} (plain ones); read both, then fall back to text.
function messageFromBody(body: unknown, raw: string, response: Response): string {
  if (body && typeof body === 'object' && 'error' in body) {
    const e = (body as { error: unknown }).error
    if (typeof e === 'string' && e) return e
    if (e && typeof e === 'object' && 'message' in e) {
      const m = (e as { message: unknown }).message
      if (typeof m === 'string' && m) return m
    }
  }
  if (raw.trim()) return raw.trim()
  return `${response.status} ${response.statusText}`.trim()
}

async function throwApiError(response: Response): Promise<never> {
  const raw = await response.text()
  let body: unknown
  try {
    body = raw ? JSON.parse(raw) : null
  } catch {
    body = null
  }
  throw new ApiError(response.status, messageFromBody(body, raw, response), body)
}

function isRedirectToLogin(response: Response): boolean {
  const loginPath = buildAdminPath('/login')

  if (response.status === 401) {
    return true
  }

  if (!response.redirected || !response.url) {
    return false
  }

  try {
    const redirectedURL = new URL(response.url, window.location.origin)
    return redirectedURL.pathname === loginPath
  } catch {
    return false
  }
}

function redirectToLogin(): never {
  window.location.href = buildAdminPath('/login')
  throw new ApiError(401, 'Unauthorized', null)
}

async function fetchAPI<T = unknown>(path: string, options?: RequestInit): Promise<T> {
  const url = buildAdminPath(path)

  const response = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
    credentials: 'same-origin',
  })

  if (isRedirectToLogin(response)) {
    redirectToLogin()
  }

  if (!response.ok) {
    await throwApiError(response)
  }

  const contentType = response.headers.get('content-type') ?? ''
  if (!contentType.includes('application/json')) {
    throw new ApiError(response.status, `Unexpected content type: ${contentType || 'unknown'}`, null)
  }

  return response.json() as Promise<T>
}

export async function logout(): Promise<void> {
  await fetchAPI('/api/logout', { method: 'POST' })
  window.location.href = buildAdminPath('/login')
}

// checkSession reports whether the session cookie is still accepted. The
// panel has no identity endpoint (no /api/me): the SPA knows it is signed
// in, not who it is, and must not invent a username.
export async function checkSession(): Promise<boolean> {
  try {
    const response = await fetch(buildAdminPath('/api/models'), {
      credentials: 'same-origin',
    })
    return !isRedirectToLogin(response) && response.ok
  } catch {
    return false
  }
}

// ── Data Studio API ──

export async function getModelsWithRuntime(includeCounts = true): Promise<ModelsResponse> {
  const qs = includeCounts ? '?include_counts=true' : ''
  return fetchAPI<ModelsResponse>(`/api/models${qs}`)
}

export async function getModelSchema(name: string): Promise<ModelSchema> {
  return fetchAPI<ModelSchema>(`/api/models/${encodeURIComponent(name)}/schema`)
}

export interface FieldMetaUpdate {
  is_list?: boolean
  is_search?: boolean
  is_filter?: boolean
  is_excluded?: boolean
  is_readonly?: boolean
  label?: string
  html_type?: string
}

export async function updateFieldsMeta(
  modelName: string,
  fields: { [fieldName: string]: FieldMetaUpdate },
): Promise<void> {
  await fetchAPI(`/api/models/${encodeURIComponent(modelName)}/schema/fields`, {
    method: 'PUT',
    body: JSON.stringify({ fields }),
  })
}

export interface RecordsQuery {
  page?: number
  page_size?: number
  search?: string
  order_by?: string
  db_alias?: string
  filters?: { [column: string]: string }
}

export async function getRecordsPaginated(
  name: string,
  params: RecordsQuery,
  signal?: AbortSignal,
): Promise<PaginatedResult> {
  const searchParams = new URLSearchParams()
  if (params.page) searchParams.set('page', String(params.page))
  if (params.page_size) searchParams.set('page_size', String(params.page_size))
  if (params.search) searchParams.set('search', params.search)
  if (params.order_by) searchParams.set('order_by', params.order_by)
  if (params.db_alias) searchParams.set('db_alias', params.db_alias)
  if (params.filters) {
    for (const [k, v] of Object.entries(params.filters)) {
      searchParams.set(k, v)
    }
  }
  return fetchAPI<PaginatedResult>(`/api/models/${encodeURIComponent(name)}?${searchParams}`, { signal })
}

export async function createRecord(name: string, data: AppRecord): Promise<AppRecord> {
  return fetchAPI(`/api/models/${encodeURIComponent(name)}`, {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateRecord(name: string, id: string, data: AppRecord): Promise<AppRecord> {
  return fetchAPI(`/api/models/${encodeURIComponent(name)}/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deleteRecord(name: string, id: string): Promise<void> {
  await fetchAPI(`/api/models/${encodeURIComponent(name)}/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export interface BulkDeleteResult {
  deleted: number
  failed: number
  // errors[].id echoes the id as the string it is at the boundary.
  errors?: Array<{ id: string; error: string }>
}

// Ids travel as strings — the boundary type of the backend's datasource
// contract — so a UUID key is as valid as an integer one. The backend
// narrows each id to the model's key type and reports the ones it cannot
// (or cannot reach) per id in `errors`, never as a failed request.
export async function bulkDelete(name: string, ids: Array<string | number>): Promise<BulkDeleteResult> {
  return fetchAPI(`/api/models/${encodeURIComponent(name)}/bulk`, {
    method: 'POST',
    body: JSON.stringify({ action: 'delete', ids: ids.map(String) }),
  })
}

// ── Sessions ──

interface RawSessionRow {
  id: string
  token_short?: string
  user?: string
  first_seen_at?: string
  last_seen_at?: string
  expires_at?: string
  pod?: string
  host?: string
  instance?: string
  remote_ip?: string
}

export async function getSessions(): Promise<SessionsResponse> {
  // The backend serves an opaque per-session id (never the bearer token —
  // that credential would let any admin replay another admin's session);
  // the id is what DELETE /api/sessions/{id} resolves server-side.
  const response = await fetchAPI<{
    enabled?: boolean
    reason?: string
    sessions?: RawSessionRow[]
    current_active?: number
    truncated_by_limit?: boolean
  }>('/api/sessions')

  const sessions: Session[] = (response.sessions ?? []).map((row) => ({
    id: row.id,
    tokenShort: row.token_short ?? '',
    user: row.user ?? '',
    remoteIp: row.remote_ip ?? '',
    host: row.host ?? '',
    pod: row.pod ?? '',
    instance: row.instance ?? '',
    firstSeenAt: row.first_seen_at ?? '',
    lastSeenAt: row.last_seen_at ?? '',
    expiresAt: row.expires_at ?? '',
  }))

  return {
    enabled: response.enabled ?? false,
    reason: response.reason,
    sessions,
    currentActive: response.current_active ?? sessions.length,
    truncatedByLimit: response.truncated_by_limit ?? false,
  }
}

export async function deleteSession(sessionId: string): Promise<void> {
  await fetchAPI(`/api/sessions/${encodeURIComponent(sessionId)}`, { method: 'DELETE' })
}

// ── Audit ──

interface RawAuditEntry {
  id: number
  user_id?: string
  username?: string
  action: string
  model_name?: string
  record_id?: string
  ip?: string
  created_at: string
}

// The backend filters by user_id / model / action and pages with
// page / page_size (internal/admin/audit.go handleListAuditLog); there is
// no free-text search.
export async function getAuditLogs(query: AuditLogQuery = {}): Promise<AuditLogPage> {
  const searchParams = new URLSearchParams()
  if (query.user_id) searchParams.set('user_id', query.user_id)
  if (query.model) searchParams.set('model', query.model)
  if (query.action) searchParams.set('action', query.action)
  if (query.page) searchParams.set('page', String(query.page))
  if (query.page_size) searchParams.set('page_size', String(query.page_size))

  const response = await fetchAPI<{
    enabled?: boolean
    reason?: string
    entries?: RawAuditEntry[]
    total?: number
    page?: number
    page_size?: number
    total_pages?: number
  }>(`/api/audit?${searchParams}`)

  return {
    enabled: response.enabled ?? false,
    reason: response.reason,
    entries: (response.entries ?? []).map((entry) => ({
      id: entry.id,
      timestamp: entry.created_at,
      userId: entry.user_id ?? '',
      username: entry.username ?? '',
      action: entry.action,
      modelName: entry.model_name ?? '',
      recordId: entry.record_id ?? '',
      ip: entry.ip ?? '',
    })),
    total: response.total ?? 0,
    page: response.page ?? query.page ?? 1,
    pageSize: response.page_size ?? query.page_size ?? 50,
    totalPages: response.total_pages ?? 1,
  }
}

// ── RBAC ──

export async function getRBACPolicies(): Promise<RBACPoliciesResponse> {
  const response = await fetchAPI<{
    enabled?: boolean
    reason?: string
    policies?: Array<{ sub: string; obj: string; act: string; eft?: string }>
  }>('/api/rbac/policies')

  const policies: RBACPolicy[] = (response.policies ?? []).map((policy) => ({
    sub: policy.sub,
    obj: policy.obj,
    act: policy.act,
    eft: policy.eft === 'deny' ? 'deny' : 'allow',
  }))

  return {
    enabled: response.enabled ?? true,
    reason: response.reason,
    policies,
  }
}

export type RBACPolicyInput = Pick<RBACPolicy, 'sub' | 'obj' | 'act'>

export async function createRBACPolicy(policy: RBACPolicyInput): Promise<void> {
  await fetchAPI('/api/rbac/policies', {
    method: 'POST',
    body: JSON.stringify({ sub: policy.sub, obj: policy.obj, act: policy.act }),
  })
}

export async function deleteRBACPolicy(policy: RBACPolicyInput): Promise<void> {
  await fetchAPI('/api/rbac/policies', {
    method: 'DELETE',
    body: JSON.stringify({ sub: policy.sub, obj: policy.obj, act: policy.act }),
  })
}

// ── Health / system ──

export async function getHealthChecks(): Promise<HealthCheck[]> {
  const response = await fetchAPI<{
    checks?: Array<{ name: string; status: 'healthy' | 'unhealthy' | 'unknown'; message?: string; latency_ms?: number }>
  }>('/api/health')

  return (response.checks ?? []).map((check) => ({
    name: check.name,
    status: check.status,
    latency: check.latency_ms,
    error: check.status === 'healthy' ? undefined : check.message,
  }))
}

export async function getSystemSnapshot(): Promise<SystemSnapshot> {
  return fetchAPI<SystemSnapshot>('/api/system/snapshot')
}

// ── Live feed ──

interface RawLiveRequest {
  request_id?: string
  timestamp: string
  method: string
  path: string
  status: number
  duration_ms: number
}

interface RawLiveQuery {
  request_id?: string
  timestamp: string
  model_name?: string
  operation?: string
  query: string
  duration_ms: number
  error?: string
}

// Feed rows need stable, unique React keys; request_id repeats across the
// SQL statements of one request, so keys get a local sequence instead.
let liveFeedSeq = 0
const nextFeedId = (prefix: string) => `${prefix}-${++liveFeedSeq}`

export function mapLiveRequest(raw: RawLiveRequest): LiveRequest {
  return {
    id: nextFeedId('http'),
    method: raw.method,
    path: raw.path,
    status: raw.status,
    duration: raw.duration_ms,
    timestamp: raw.timestamp,
    requestId: raw.request_id,
  }
}

export function mapLiveQuery(raw: RawLiveQuery): LiveQuery {
  return {
    id: nextFeedId('sql'),
    query: raw.query,
    model: raw.model_name,
    operation: raw.operation,
    duration: raw.duration_ms,
    timestamp: raw.timestamp,
    requestId: raw.request_id,
    error: raw.error,
  }
}

// getLiveFeed returns the live snapshot's HTTP requests and SQL statements as
// one list, newest first — the same rows the `/api/live/ws` stream then
// prepends via `http.request` / `db.query` envelopes.
export async function getLiveFeed(): Promise<LiveFeedEntry[]> {
  const response = await fetchAPI<{
    requests?: RawLiveRequest[]
    queries?: RawLiveQuery[]
  }>('/api/live/snapshot')

  const entries: LiveFeedEntry[] = [
    ...(response.requests ?? []).map((r) => ({ kind: 'http' as const, ...mapLiveRequest(r) })),
    ...(response.queries ?? []).map((q) => ({ kind: 'sql' as const, ...mapLiveQuery(q) })),
  ]
  return entries.sort((a, b) => (a.timestamp < b.timestamp ? 1 : a.timestamp > b.timestamp ? -1 : 0))
}

export function getLiveWebSocket(): WebSocket | null {
  try {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const path = buildAdminPath('/api/live/ws')
    return new WebSocket(`${protocol}//${window.location.host}${path}`)
  } catch {
    return null
  }
}

// ── Export / import ──

export async function exportData(format: 'csv' | 'json' | 'sql', modelName?: string): Promise<string> {
  // Backend routes are /api/exports (create) and /api/exports/download
  // (?key=). The old /api/export paths never existed — every export
  // clicked in the UI 404'd.
  const response = await fetchAPI<{ url?: string; storage_key?: string; id?: string }>('/api/exports', {
    method: 'POST',
    body: JSON.stringify({
      format,
      models: modelName ? [modelName] : [],
    }),
  })
  // If the backend returns a full URL, use it directly
  if (response.url) return response.url
  // Otherwise construct a download URL from the storage key
  const key = response.storage_key || response.id
  if (key) {
    return buildAdminPath(`/api/exports/download?key=${encodeURIComponent(key)}`)
  }
  return ''
}

export interface ImportUpload {
  key: string
  size: number
  format: string
  filename: string
}

export interface ImportRowError {
  row: number
  field?: string
  message: string
}

export interface ImportValidation {
  total_records: number
  valid_records: number
  errors: ImportRowError[]
  can_proceed: boolean
}

export interface ImportReport {
  total: number
  imported: number
  skipped: number
  updated: number
  failed: number
  errors: ImportRowError[]
  dry_run: boolean
}

// The import is three round trips (internal/admin/management.go): the
// multipart upload parks the file in storage and returns its key; validate
// dry-runs the parse against the model; execute writes. Nothing is
// imported until execute returns.
export async function uploadImportFile(file: File): Promise<ImportUpload> {
  const formData = new FormData()
  formData.append('file', file)

  const response = await fetch(buildAdminPath('/api/imports'), {
    method: 'POST',
    body: formData,
    credentials: 'same-origin',
  })
  if (isRedirectToLogin(response)) {
    redirectToLogin()
  }
  if (!response.ok) {
    await throwApiError(response)
  }
  return response.json() as Promise<ImportUpload>
}

// ImportTarget is the body both validate and execute expect: the model and
// the file format the upload step detected (csv|json). The backend does not
// infer the format from the stored key, so omitting it fails the parse.
export interface ImportTarget {
  model: string
  format: string
}

export async function validateImport(key: string, target: ImportTarget): Promise<ImportValidation> {
  return fetchAPI<ImportValidation>(`/api/import/validate?key=${encodeURIComponent(key)}`, {
    method: 'POST',
    body: JSON.stringify(target),
  })
}

export async function executeImport(key: string, target: ImportTarget): Promise<ImportReport> {
  return fetchAPI<ImportReport>(`/api/import/execute?key=${encodeURIComponent(key)}`, {
    method: 'POST',
    body: JSON.stringify(target),
  })
}
