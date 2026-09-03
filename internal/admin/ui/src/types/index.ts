// One row of GET /api/sessions — only what the backend actually serializes
// (sessionRow in internal/admin/sessions.go). The panel has no user
// directory endpoint, so there is no user_id / user_agent to show.
export interface Session {
  id: string
  tokenShort: string
  user: string
  remoteIp: string
  host: string
  pod: string
  instance: string
  firstSeenAt: string
  lastSeenAt: string
  expiresAt: string
}

export interface SessionsResponse {
  enabled: boolean
  reason?: string
  sessions: Session[]
  currentActive: number
  truncatedByLimit: boolean
}

// ── Data Studio types (match backend handleListModels / handleGetSchema) ──

export interface ModelSummary {
  name: string
  plural: string
  table: string
  icon: string
  count: number
  count_known: boolean
  is_estimated?: boolean
  database: string
  engine: string
  counts?: { [alias: string]: number }
  databases?: string[]
}

export interface ModelSchema {
  name: string
  plural: string
  table: string
  primary_key: string
  icon: string
  read_only: boolean
  fields: SchemaField[]
  foreign_keys: ForeignKeyInfo[]
  tenant_field: string
}

export interface SchemaField {
  name: string
  column: string
  label: string
  type: string
  html_type: string
  is_pk: boolean
  is_required: boolean
  is_readonly: boolean
  is_list: boolean
  is_search: boolean
  is_filter: boolean
  is_excluded: boolean
  is_fk: boolean
  is_tenant_field: boolean
  fk_model?: string
  choices?: FieldChoice[]
}

export interface FieldChoice {
  value: string
  label: string
}

export interface ForeignKeyInfo {
  field_name: string
  column: string
  foreign_model: string
  foreign_table: string
  foreign_column: string
}

export interface PaginatedResult {
  items: { [key: string]: unknown }[]
  total: number
  page: number
  page_size: number
  total_pages: number
  is_estimated?: boolean
  has_more?: boolean
}

export interface RuntimeDatabaseInfo {
  alias: string
  engine: string
  dialect: string
  is_default: boolean
  models: string[]
  model_entries: { name: string; plural: string; table: string; count: number; count_known: boolean }[]
  model_count: number
}

export interface RuntimeEngineGroup {
  name: string
  databases: RuntimeDatabaseInfo[]
}

export interface RuntimeInfo {
  environment: string
  databases: RuntimeDatabaseInfo[]
  engines: string[]
  engine_groups: RuntimeEngineGroup[]
  trace_url_template?: string
  models_total: number
  records_total: number
  counts_mode: string
  counts_available: boolean
  sessions_active: number
  multi_tenant_enabled: boolean
  multi_tenant_default: string
  tenant_ids?: string[]
  multi_site_enabled: boolean
  multi_site_default: string
  site_names?: string[]
}

export interface ModelsResponse {
  models: ModelSummary[]
  title: string
  runtime: RuntimeInfo
}

// A record as the data-studio API returns it: keyed by column, values are
// whatever the driver decoded (primitives, nested objects for JSON columns).
export type Record = { [key: string]: unknown }

// ── Other page types ──

export interface AuditLog {
  id: number
  timestamp: string
  userId: string
  username: string
  action: string
  modelName: string
  recordId: string
  ip: string
}

export interface AuditLogPage {
  enabled: boolean
  reason?: string
  entries: AuditLog[]
  total: number
  page: number
  pageSize: number
  totalPages: number
}

export interface AuditLogQuery {
  user_id?: string
  model?: string
  action?: string
  page?: number
  page_size?: number
}

// A casbin policy row as GET /api/rbac/policies serves it. `eft` is the
// effect column (allow|deny); the backend only sets it when the model has
// one, so a missing value means allow.
export interface RBACPolicy {
  sub: string
  obj: string
  act: string
  eft: 'allow' | 'deny'
}

export interface RBACPoliciesResponse {
  enabled: boolean
  reason?: string
  policies: RBACPolicy[]
}

export interface HealthCheck {
  name: string
  status: 'healthy' | 'unhealthy' | 'unknown'
  latency?: number
  error?: string
}

export interface RuntimeQueueSnapshot {
  name: string
  paused: boolean
  latency_ms: number
  size: number
  pending: number
  active: number
  scheduled: number
  retry: number
  archived: number
  completed: number
  aggregating: number
  processed_today: number
  failed_today: number
  processed_all: number
  failed_all: number
}

export interface RuntimeScheduleSnapshot {
  id: string
  spec: string
  task_type: string
  next_enqueue_at?: string
  prev_enqueue_at?: string
}

export interface TasksRuntimeSnapshot {
  enabled: boolean
  generated_at: string
  reason?: string
  queues: RuntimeQueueSnapshot[]
  schedules: RuntimeScheduleSnapshot[]
  total_schedules: number
  total_queues: number
  total_size: number
  total_pending: number
  total_active: number
  total_scheduled: number
  total_retry: number
  total_archived: number
  total_processed_today: number
  total_failed_today: number
}

export interface OutboxRuntimeSnapshot {
  enabled: boolean
  table: string
  flavor?: string
  reason?: string
  pending: number
  processing: number
  delivered: number
  failed: number
  total: number
  oldest_pending_at?: string
  last_delivered_at?: string
}

export interface LiveClusterSnapshot {
  enabled: boolean
  connected: boolean
  node_id?: string
  channel?: string
  reason?: string
  published: number
  dropped: number
  received: number
  ignored: number
}

export interface LiveNodeSnapshot {
  node_id: string
  last_seen_at?: string
  last_event_type?: string
  requests: number
  sql_queries: number
  sessions: number
  status: string
}

export interface SystemDatabasePool {
  alias: string
  engine: string
  dialect: string
  is_default: boolean
  open_connections: number
  in_use: number
  idle: number
  wait_count: number
  wait_duration_ms: number
  max_open_connections: number
  error?: string
}

export interface SystemSnapshot {
  enabled: boolean
  generated_at: string
  go_version: string
  go_os: string
  go_arch: string
  gomaxprocs: number
  cpus: number
  cpu_load: number
  process_cpu_load: number
  goroutines: {
    count: number
    state_counts: Array<{ state: string; count: number }>
  }
  memory: {
    alloc_bytes: number
    heap_alloc_bytes: number
    heap_sys_bytes: number
    stack_in_use_bytes: number
    heap_objects: number
    num_gc: number
    last_pause_ms: number
    pause_total_ms: number
  }
  databases: SystemDatabasePool[]
  jobs: TasksRuntimeSnapshot
  outbox: OutboxRuntimeSnapshot
  cluster: LiveClusterSnapshot
  cluster_nodes: LiveNodeSnapshot[]
  flags: FeatureFlag[]
  telemetry: {
    otlp_configured: boolean
    otlp_endpoint?: string
    trace_links_configured: boolean
    trace_url_template?: string
  }
  environment: Array<{
    name: string
    value: string
    masked: boolean
  }>
}

export interface LiveRequest {
  id: string
  method: string
  path: string
  status: number
  duration: number
  timestamp: string
  requestId?: string
}

// A redacted SQL statement from the live feed (`db.query` on the event bus).
export interface LiveQuery {
  id: string
  query: string
  model?: string
  operation?: string
  duration: number
  timestamp: string
  requestId?: string
  error?: string
}

// One row of the live feed: HTTP traffic and SQL statements interleaved.
export type LiveFeedEntry =
  | ({ kind: 'http' } & LiveRequest)
  | ({ kind: 'sql' } & LiveQuery)

export interface FeatureFlag {
  name: string
  enabled: boolean
}
