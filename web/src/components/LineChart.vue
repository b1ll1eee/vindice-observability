<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

export interface ChartSeries {
  name: string
  color: string
  points: [number, number | null][] // [epoch ms, value or gap]
}

const props = withDefaults(
  defineProps<{ series: ChartSeries[]; format?: (v: number) => string; height?: number }>(),
  { format: (v: number) => String(Math.round(v * 100) / 100), height: 160 },
)

// Drawn at the container's real width, so text keeps its pixel size.
const box = ref<HTMLDivElement>()
const W = ref(600)
let observer: ResizeObserver | undefined
onMounted(() => {
  observer = new ResizeObserver(([e]) => {
    if (e && e.contentRect.width > 0) W.value = Math.round(e.contentRect.width)
  })
  if (box.value) observer.observe(box.value)
})
onBeforeUnmount(() => observer?.disconnect())
// Unique per chart instance, for gradient ids.
const uid = Math.random().toString(36).slice(2, 8)
const pad = { top: 10, right: 10, bottom: 22, left: 52 }

const bounds = computed(() => {
  let x0 = Infinity, x1 = -Infinity, y1 = 0
  for (const s of props.series)
    for (const [t, v] of s.points) {
      x0 = Math.min(x0, t)
      x1 = Math.max(x1, t)
      if (v !== null) y1 = Math.max(y1, v)
    }
  if (!isFinite(x0)) return null
  // All-zero data still gets a 0..1 axis (ticks at 0, 0.5, 1).
  return { x0, x1: x1 === x0 ? x0 + 1 : x1, y1: (y1 === 0 ? 1 : y1) * 1.1 }
})

const x = (t: number) => {
  const b = bounds.value!
  return pad.left + ((t - b.x0) / (b.x1 - b.x0)) * (W.value - pad.left - pad.right)
}
const y = (v: number) => pad.top + (1 - v / bounds.value!.y1) * (props.height - pad.top - pad.bottom)

// Gaps (null) split a line into segments rather than dropping to zero.
function paths(s: ChartSeries): string {
  let d = ''
  let pen = false
  for (const [t, v] of s.points) {
    if (v === null) {
      pen = false
      continue
    }
    d += `${pen ? 'L' : 'M'}${x(t).toFixed(1)},${y(v).toFixed(1)}`
    pen = true
  }
  return d
}

// Area under a single series, closed down to the x axis per segment.
function area(s: ChartSeries): string {
  let d = ''
  let seg: [number, number][] = []
  const flush = () => {
    if (seg.length > 1) {
      const base = y(0)
      d += `M${x(seg[0]![0])},${base}` + seg.map(([t, v]) => `L${x(t)},${y(v)}`).join('') + `L${x(seg[seg.length - 1]![0])},${base}Z`
    }
    seg = []
  }
  for (const [t, v] of s.points) {
    if (v === null) flush()
    else seg.push([t, v])
  }
  flush()
  return d
}

// A point with gaps on both sides has no line through it; draw it as a dot.
function isolated(s: ChartSeries): [number, number][] {
  const out: [number, number][] = []
  s.points.forEach(([t, v], i) => {
    if (v !== null && (s.points[i - 1]?.[1] ?? null) === null && (s.points[i + 1]?.[1] ?? null) === null) out.push([t, v])
  })
  return out
}

const ticks = computed(() => {
  const b = bounds.value
  if (!b) return { y: [], x: [] }
  const yt = [0, 0.5, 1].map((f) => f * (b.y1 / 1.1))
  const xt = [b.x0, (b.x0 + b.x1) / 2, b.x1]
  return { y: yt, x: xt }
})

function timeLabel(t: number) {
  return new Date(t).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false })
}

const hoverT = ref<number | null>(null)
const hoverRows = computed(() => {
  if (hoverT.value === null) return []
  return props.series
    .map((s) => ({ s, p: s.points.find(([t]) => t === hoverT.value) }))
    .filter((r) => r.p && r.p[1] !== null)
    .map((r) => ({ name: r.s.name, color: r.s.color, value: r.p![1] as number }))
    .sort((a, b) => b.value - a.value)
    .slice(0, 8)
})

function onMove(e: MouseEvent) {
  const b = bounds.value
  const first = props.series[0]
  if (!b || !first) return
  const rect = (e.currentTarget as SVGElement).getBoundingClientRect()
  const fx = ((e.clientX - rect.left) / rect.width) * W.value
  let best: number | null = null
  let dist = Infinity
  for (const [t] of first.points) {
    const d = Math.abs(x(t) - fx)
    if (d < dist) [best, dist] = [t, d]
  }
  hoverT.value = best
}
</script>

<template>
  <div ref="box" class="chart">
    <div v-if="!bounds" class="empty">No data</div>
    <template v-else>
      <svg :viewBox="`0 0 ${W} ${height}`" :width="W" :height="height" @mousemove="onMove" @mouseleave="hoverT = null">
        <g class="grid">
          <template v-for="v in ticks.y" :key="v">
            <line :x1="pad.left" :x2="W - pad.right" :y1="y(v)" :y2="y(v)" />
            <text :x="pad.left - 4" :y="y(v) + 3" text-anchor="end">{{ format(v) }}</text>
          </template>
          <text v-for="(t, i) in ticks.x" :key="t" :x="x(t)" :y="height - 4" :text-anchor="['start', 'middle', 'end'][i]">
            {{ timeLabel(t) }}
          </text>
        </g>
        <defs>
          <linearGradient v-for="(s, i) in series" :id="`fill-${uid}-${i}`" :key="i" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" :stop-color="s.color" stop-opacity="0.28" />
            <stop offset="100%" :stop-color="s.color" stop-opacity="0" />
          </linearGradient>
        </defs>
        <template v-for="(s, i) in series" :key="s.name">
          <path v-if="series.length === 1" :d="area(s)" :fill="`url(#fill-${uid}-${i})`" stroke="none" />
          <path :d="paths(s)" :stroke="s.color" fill="none" stroke-width="1.8" stroke-linejoin="round" />
          <ellipse v-for="[t, v] in isolated(s)" :key="t" :cx="x(t)" :cy="y(v)" rx="2.5" :ry="2.5" :fill="s.color" />
        </template>
        <line v-if="hoverT !== null" class="cursor" :x1="x(hoverT)" :x2="x(hoverT)" :y1="pad.top" :y2="height - pad.bottom" />
      </svg>
      <div v-if="hoverRows.length" class="tip">
        <div class="muted">{{ new Date(hoverT!).toLocaleString(undefined, { hour12: false }) }}</div>
        <div v-for="r in hoverRows" :key="r.name"><span class="dot" :style="{ background: r.color }" />{{ r.name || 'value' }}: <b>{{ format(r.value) }}</b></div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.chart { position: relative; }
svg { width: 100%; display: block; }
.grid line { stroke: var(--border); stroke-width: 1; vector-effect: non-scaling-stroke; }
.grid text { fill: var(--muted); font-size: 11px; font-variant-numeric: tabular-nums; }
.cursor { stroke: var(--muted); stroke-dasharray: 3 3; vector-effect: non-scaling-stroke; }
.tip {
  position: absolute; top: 4px; right: 8px; font-size: 12px; background: var(--panel);
  border: 1px solid var(--border); border-radius: 8px; padding: 6px 10px; pointer-events: none; box-shadow: var(--shadow-lg);
}
.empty { height: 160px; display: grid; place-items: center; color: var(--muted); font-size: 13px; }
</style>
