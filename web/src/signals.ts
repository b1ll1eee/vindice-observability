// Turns a SignalQuery (shared by dashboard panels and alert rules) into chart series.
import { api, type Measure, type Range, type SignalQuery } from './api'
import type { ChartSeries } from './components/LineChart.vue'
import { count, duration, percent, serviceColor } from './format'

export interface SignalChart {
  series: ChartSeries[]
  format: (v: number) => string
  unit: string
}

const palette = ['#7c6cf0', '#22b8cf', '#f59e0b', '#ef4444', '#10b981', '#ec4899', '#3b82f6', '#a3a3a3']

export const measures: { value: Measure; label: string }[] = [
  { value: 'requests', label: 'Requests / min' },
  { value: 'errors', label: 'Errors / min' },
  { value: 'error_rate', label: 'Error rate' },
  { value: 'p50', label: 'Latency p50' },
  { value: 'p95', label: 'Latency p95' },
  { value: 'p99', label: 'Latency p99' },
]

export function measureFormat(m?: Measure): (v: number) => string {
  if (m === 'error_rate') return percent
  if (m === 'p50' || m === 'p95' || m === 'p99') return duration
  return (v) => count(Math.round(v))
}

const plain = (v: number) => (Math.abs(v) >= 1000 ? count(Math.round(v)) : String(Math.round(v * 1000) / 1000))

export async function signalChart(sig: SignalQuery, r: Range): Promise<SignalChart> {
  if (sig.kind === 'metric' && sig.metric) {
    const res = await api.queryMetric(sig.metric, r)
    return {
      series: res.series.map((s, i) => ({
        name: s.group,
        color: s.group ? serviceColor(s.group) : palette[i % palette.length],
        points: s.points,
      })),
      format: plain,
      unit: sig.metric.agg === 'rate' ? '/s' : '',
    }
  }

  if (sig.kind === 'red' && sig.service) {
    const m = sig.measure ?? 'requests'
    const res = await api.serviceSeries(sig.service, r)
    const perMin = res.step_ms / 60e3
    const value = (p: (typeof res.points)[number]): number | null => {
      switch (m) {
        case 'requests':
          return p.requests / perMin
        case 'errors':
          return p.errors / perMin
        case 'error_rate':
          return p.requests ? p.error_rate : null
        default:
          return p.requests ? p[`${m}_ms`] : null
      }
    }
    return {
      series: [{ name: m, color: palette[0], points: res.points.map((p) => [p.t, value(p)]) }],
      format: measureFormat(m),
      unit: '',
    }
  }

  // logs: matching log lines per bucket
  const h = await api.histogram(r, { service: sig.service, severity: sig.min_severity, q: sig.text })
  return {
    series: [{ name: 'logs', color: palette[1], points: h.buckets.map((b) => [b.t, b.debug + b.info + b.warn + b.error]) }],
    format: plain,
    unit: '',
  }
}

export function describeSignal(s: SignalQuery): string {
  if (s.kind === 'metric' && s.metric) {
    const f = Object.entries(s.metric.filters ?? {}).map(([k, v]) => `${k}=${v}`)
    const sel = [s.metric.service ? `service=${s.metric.service}` : '', ...f].filter(Boolean).join(', ')
    return `${s.metric.agg}(${s.metric.name}${sel ? ` {${sel}}` : ''})${s.metric.group_by ? ` by ${s.metric.group_by}` : ''}`
  }
  if (s.kind === 'red') return `${s.measure} of ${s.service}`
  const parts = ['log count']
  if (s.service) parts.push(`of ${s.service}`)
  if ((s.min_severity ?? 0) >= 17) parts.push('(errors)')
  if (s.text) parts.push(`matching "${s.text}"`)
  return parts.join(' ')
}
