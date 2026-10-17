<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Bucket, Histogram, Level } from '../api'

const props = defineProps<{ hist: Histogram }>()

// Stack order, bottom to top.
const levels: Level[] = ['error', 'warn', 'info', 'debug']
const height = 72

const peak = computed(() =>
  Math.max(1, ...props.hist.buckets.map((b) => b.debug + b.info + b.warn + b.error)),
)

const hover = ref<Bucket | null>(null)

function segments(b: Bucket) {
  let y = height
  return levels.map((l) => {
    const h = (b[l] / peak.value) * height
    y -= h
    return { level: l, y, h }
  })
}

function label(t: number) {
  return new Date(t).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false })
}

const axis = computed(() => {
  const b = props.hist.buckets
  return b.length ? [b[0]!.t, b[Math.floor(b.length / 2)]!.t, b[b.length - 1]!.t] : []
})
</script>

<template>
  <div class="card hist">
    <div class="legend">
      <span v-for="l in levels" :key="l" class="key"><span class="dot" :style="{ background: `var(--${l})` }" />{{ l }}</span>
      <span class="grow" />
      <span v-if="hover" class="tip mono">
        {{ label(hover.t) }} ·
        <b class="bad">{{ hover.error }}</b> error · <b class="warnc">{{ hover.warn }}</b> warn · {{ hover.info }} info · {{ hover.debug }} debug
      </span>
    </div>
    <svg :viewBox="`0 0 ${hist.buckets.length} ${height}`" preserveAspectRatio="none" @mouseleave="hover = null">
      <g v-for="(b, i) in hist.buckets" :key="b.t" @mouseenter="hover = b">
        <rect :x="i" y="0" width="1" :height="height" class="hit" />
        <rect v-for="s in segments(b)" :key="s.level" :x="i + 0.12" :y="s.y" width="0.76" :height="s.h" :fill="`var(--${s.level})`" rx="0.15" />
      </g>
    </svg>
    <div class="axis muted"><span v-for="t in axis" :key="t">{{ label(t) }}</span></div>
  </div>
</template>

<style scoped>
.hist { padding: 12px 16px 8px; margin-bottom: 14px; }
.legend { display: flex; gap: 14px; font-size: 12px; margin-bottom: 8px; align-items: center; color: var(--muted); text-transform: capitalize; }
.tip { font-size: 12px; text-transform: none; color: var(--text-2); }
svg { width: 100%; height: 72px; display: block; }
.hit { fill: transparent; }
g:hover .hit { fill: var(--hover); }
.axis { display: flex; justify-content: space-between; font-size: 11px; margin-top: 4px; }
</style>
