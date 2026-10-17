export function duration(ms: number): string {
  if (ms < 1) return `${Math.round(ms * 1000)} µs`
  if (ms < 1000) return `${ms < 10 ? ms.toFixed(1) : Math.round(ms)} ms`
  if (ms < 60e3) return `${(ms / 1000).toFixed(2)} s`
  return `${(ms / 60e3).toFixed(1)} min`
}

export function count(n: number): string {
  if (n < 1000) return String(n)
  if (n < 1e6) return `${(n / 1e3).toFixed(n < 1e4 ? 1 : 0)}k`
  return `${(n / 1e6).toFixed(1)}M`
}

export function percent(r: number): string {
  if (r === 0) return '0%'
  if (r < 0.001) return '<0.1%'
  return `${(r * 100).toFixed(r < 0.1 ? 1 : 0)}%`
}

export function time(iso: string): string {
  const d = new Date(iso)
  const hms = d.toLocaleTimeString(undefined, { hour12: false })
  return `${d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })} ${hms}.${String(d.getMilliseconds()).padStart(3, '0')}`
}

/** Stable colour per service name. */
export function serviceColor(name: string): string {
  let h = 0
  for (const c of name) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return `hsl(${h % 360} 62% 58%)`
}
