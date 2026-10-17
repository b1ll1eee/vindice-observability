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

export class ApiError extends Error {}

type Params = Record<string, string | number | boolean | undefined>

async function get<T>(path: string, params: Params = {}): Promise<T> {
  const qs = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '' && v !== false) qs.set(k, String(v))
  }
  const res = await fetch(`/api/v1${path}${qs.size ? `?${qs}` : ''}`)
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(body.error ?? `${res.status} ${res.statusText}`)
  return body as T
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
}
