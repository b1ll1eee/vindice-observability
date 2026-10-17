// Typed client for the Vindice query API (/api/v1).

export type Level = 'debug' | 'info' | 'warn' | 'error'

export interface Range {
  from: number // epoch ms
  to: number
}

export interface Latency {
  p50_ms: number
  p95_ms: number
  p99_ms: number
}

export interface Service extends Latency {
  name: string
  requests: number
  errors: number
  error_rate: number
  logs: number
  log_errors: number
}

export interface Operation extends Latency {
  name: string
  requests: number
  errors: number
  error_rate: number
}

export interface Log {
  time: string
  ts: string // nanosecond cursor
  service: string
  severity: string
  level: Level
  trace_id?: string
  span_id?: string
  body: string
  attributes: Record<string, string> | null
  resource: Record<string, string> | null
}

export interface LogPage {
  logs: Log[]
  next?: string
}

export interface Bucket {
  t: number
  debug: number
  info: number
  warn: number
  error: number
}

export interface Histogram {
  step_ms: number
  buckets: Bucket[]
}

export interface TraceSummary {
  trace_id: string
  service: string
  name: string
  time: string
  duration_ms: number
  status: string
  http_method?: string
  http_route?: string
  http_status?: number
}

export interface SpanEvent {
  offset_ms: number
  name: string
  attributes: Record<string, string> | null
}

export interface Span {
  span_id: string
  parent_id?: string
  service: string
  name: string
  kind: string
  status: string
  status_message?: string
  offset_ms: number
  duration_ms: number
  attributes: Record<string, string>
  resource: Record<string, string> | null
  events: SpanEvent[]
}

export interface Trace {
  trace_id: string
  time: string
  duration_ms: number
  services: string[]
  spans: Span[]
  logs: Log[]
}

export interface LogFilter {
  service?: string
  severity?: number
  q?: string
  trace_id?: string
}

export interface TraceFilter {
  service?: string
  min_duration_ms?: number
  errors?: boolean
}

export interface REDPoint extends Latency {
  t: number
  requests: number
  errors: number
  error_rate: number
}

export interface MetricName {
  name: string
  type: string
  unit?: string
}

export type Agg = 'avg' | 'sum' | 'min' | 'max' | 'count' | 'last' | 'rate'

export interface MetricQuery {
  name: string
  service?: string
  filters?: Record<string, string>
  agg: Agg
  group_by?: string
}

export interface MetricResult {
  step_ms: number
  truncated: boolean
  series: { group: string; points: [number, number | null][] }[]
}

export type SignalKind = 'metric' | 'red' | 'logs'
export type Measure = 'requests' | 'errors' | 'error_rate' | 'p50' | 'p95' | 'p99'

export interface SignalQuery {
  kind: SignalKind
  metric?: MetricQuery
  service?: string
  measure?: Measure
  min_severity?: number
  text?: string
}

export interface Panel {
  id?: string
  title: string
  signal: SignalQuery
  width: number
}

export interface Dashboard {
  id?: string
  title: string
  panels: Panel[]
  updated_at?: string
}

export type Op = '>' | '>=' | '<' | '<='

export interface AlertStatus {
  state: 'ok' | 'pending' | 'firing'
  since: string
  value: number
  eval_at: string
  error?: string
  breaching?: string[]
}

export interface AlertRule {
  id?: string
  name: string
  enabled: boolean
  signal: SignalQuery
  window_sec: number
  op: Op
  threshold: number
  for_sec: number
  chat_id?: string
  updated_at?: string
  status?: AlertStatus
}

export interface AlertEvent {
  rule_id: string
  rule_name: string
  time: string
  state: string
  value: number
  message?: string
}

export type Role = 'viewer' | 'editor' | 'admin'

export interface User {
  id: string
  username: string
  role: Role
  disabled: boolean
  created_at: string
  updated_at: string
}

export interface APIKey {
  id: string
  name: string
  hint: string
  created_by: string
  created_at: string
  revoked: boolean
  revoked_at?: string
  last_used?: string
}

export class ApiError extends Error {
  constructor(
    message: string,
    public status = 0,
  ) {
    super(message)
  }
}

type Params = Record<string, string | number | boolean | string[] | undefined>

async function request<T>(method: string, path: string, params: Params = {}, body?: unknown): Promise<T> {
  const qs = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (Array.isArray(v)) v.forEach((x) => qs.append(k, x))
    else if (v !== undefined && v !== '' && v !== false) qs.set(k, String(v))
  }
  const res = await fetch(`/api/v1${path}${qs.size ? `?${qs}` : ''}`, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (res.status === 401 && !path.startsWith('/auth/') && location.pathname !== '/login') {
    // Session expired or revoked: sign in again and come back.
    location.assign(`/login?next=${encodeURIComponent(location.pathname + location.search)}`)
  }
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(data.error ?? `${res.status} ${res.statusText}`, res.status)
  return data as T
}

const get = <T>(path: string, params: Params = {}) => request<T>('GET', path, params)

function metricParams(q: MetricQuery): Params {
  return {
    name: q.name,
    service: q.service,
    agg: q.agg,
    group_by: q.group_by,
    filter: Object.entries(q.filters ?? {}).map(([k, v]) => `${k}:${v}`),
  }
}

export const api = {
  services: (r: Range) => get<Service[]>('/services', { ...r }),
  operations: (service: string, r: Range) =>
    get<Operation[]>(`/services/${encodeURIComponent(service)}/operations`, { ...r }),
  logs: (r: Range, f: LogFilter, before?: string) =>
    get<LogPage>('/logs', { ...r, ...f, before, limit: 100 }),
  histogram: (r: Range, f: LogFilter) => get<Histogram>('/logs/histogram', { ...r, ...f }),
  traces: (r: Range, f: TraceFilter) => get<TraceSummary[]>('/traces', { ...r, ...f }),
  trace: (id: string) => get<Trace>(`/traces/${id}`),

  serviceSeries: (service: string, r: Range) =>
    get<{ step_ms: number; points: REDPoint[] }>(`/services/${encodeURIComponent(service)}/series`, { ...r }),
  metricNames: (r: Range, service?: string) => get<MetricName[]>('/metrics', { ...r, service }),
  queryMetric: (q: MetricQuery, r: Range) => get<MetricResult>('/metrics/query', { ...r, ...metricParams(q) }),

  dashboards: () => get<Dashboard[]>('/dashboards'),
  dashboard: (id: string) => get<Dashboard>(`/dashboards/${id}`),
  saveDashboard: (d: Dashboard) =>
    d.id ? request<Dashboard>('PUT', `/dashboards/${d.id}`, {}, d) : request<Dashboard>('POST', '/dashboards', {}, d),
  deleteDashboard: (id: string) => request<void>('DELETE', `/dashboards/${id}`),

  alerts: () => get<AlertRule[]>('/alerts'),
  saveAlert: (r: AlertRule) =>
    r.id ? request<AlertRule>('PUT', `/alerts/${r.id}`, {}, r) : request<AlertRule>('POST', '/alerts', {}, r),
  deleteAlert: (id: string) => request<void>('DELETE', `/alerts/${id}`),
  testAlert: (id: string) => request<void>('POST', `/alerts/${id}/test`),
  alertEvents: (limit = 50) => get<AlertEvent[]>('/alerts/events', { limit }),

  login: (username: string, password: string) => request<User>('POST', '/auth/login', {}, { username, password }),
  logout: () => request<void>('POST', '/auth/logout'),
  me: () => get<User>('/auth/me'),
  changePassword: (current: string, next: string) =>
    request<void>('POST', '/auth/password', {}, { current, new: next }),

  users: () => get<User[]>('/users'),
  createUser: (username: string, role: Role, password: string) =>
    request<User>('POST', '/users', {}, { username, role, password }),
  updateUser: (id: string, role: Role, disabled: boolean, password?: string) =>
    request<User>('PUT', `/users/${id}`, {}, { role, disabled, password: password || undefined }),
  deleteUser: (id: string) => request<void>('DELETE', `/users/${id}`),

  keys: () => get<APIKey[]>('/keys'),
  createKey: (name: string) => request<APIKey & { secret: string }>('POST', '/keys', {}, { name }),
  revokeKey: (id: string) => request<void>('DELETE', `/keys/${id}`),
}
