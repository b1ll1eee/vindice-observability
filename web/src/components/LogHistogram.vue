<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Bucket, Histogram, Level } from '../api'
import { count } from '../format'

const props = defineProps<{ hist: Histogram }>()

// Stack order, bottom to top.
const levels: Level[] = ['error', 'warn', 'info', 'debug']
const height = 90

const peak = computed(() =>
  Math.max(1, ...props.hist.buckets.map((b) => b.debug + b.info + b.warn + b.error)),
)
const total = computed(() =>
  props.hist.buckets.reduce((n, b) => n + b.debug + b.info + b.warn + b.error, 0),
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

function label(b: Bucket) {
  const d = new Date(b.t)
  return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false })
}
</script>

<template>
  <div class="hist panel">
    <div class="legend">
      <span class="muted">{{ count(total) }} logs</span>
      <span v-for="l in levels" :key="l"><span class="dot" :style="{ background: `var(--${l})` }" />{{ l }}</span>
      <span v-if="hover" class="tip mono">
        {{ label(hover) }} · {{ hover.error }} error · {{ hover.warn }} warn · {{ hover.info }} info · {{ hover.debug }} debug
      </span>
    </div>
    <svg :viewBox="`0 0 ${hist.buckets.length} ${height}`" preserveAspectRatio="none" @mouseleave="hover = null">
      <g v-for="(b, i) in hist.buckets" :key="b.t" @mouseenter="hover = b">
        <rect :x="i" y="0" width="1" :height="height" class="hit" />
        <rect
          v-for="s in segments(b)"
          :key="s.level"
          :x="i + 0.1"
          :y="s.y"
          width="0.8"
          :height="s.h"
          :fill="`var(--${s.level})`"
        />
      </g>
    </svg>
  </div>
</template>

<style scoped>
.hist { padding: 10px 12px; margin-bottom: 14px; }
.legend { display: flex; gap: 14px; font-size: 12px; margin-bottom: 6px; align-items: center; }
.tip { margin-left: auto; color: var(--muted); }
svg { width: 100%; height: 90px; display: block; }
.hit { fill: transparent; }
g:hover .hit { fill: var(--hover); }
</style>
