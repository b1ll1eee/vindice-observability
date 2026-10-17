<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type Log, type LogFilter } from '../api'
import Icon from '../components/Icon.vue'
import LogHistogram from '../components/LogHistogram.vue'
import LogList from '../components/LogList.vue'
import PageHeader from '../components/PageHeader.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import { useLoader } from '../load'
import { useTimeRange } from '../range'

const route = useRoute()
const router = useRouter()
const { range } = useTimeRange()

// "At least this level". Logs without a severity count as Info, as on the server.
const levels = [
  { value: 0, label: 'All', from: ['debug', 'info', 'warn', 'error'] },
  { value: 9, label: 'Info+', from: ['info', 'warn', 'error'] },
  { value: 13, label: 'Warn+', from: ['warn', 'error'] },
  { value: 17, label: 'Errors', from: ['error'] },
] as const

// Filters live in the URL so a search can be shared or bookmarked.
const filter = computed<LogFilter>(() => ({
  service: (route.query.service as string) || undefined,
  severity: Number(route.query.severity) || undefined,
  q: (route.query.q as string) || undefined,
  trace_id: (route.query.trace_id as string) || undefined,
}))

function setFilter(patch: Record<string, string | undefined>) {
  router.replace({ query: { ...route.query, ...patch } })
}

const text = ref(filter.value.q ?? '')
watch(() => filter.value.q, (q) => (text.value = q ?? ''))

const services = useLoader(() => api.services(range.value), [range])
const hist = useLoader(() => api.histogram(range.value, filter.value), [range, filter])
const total = computed(() =>
  (hist.data.value?.buckets ?? []).reduce((n, b) => n + b.debug + b.info + b.warn + b.error, 0),
)

// Counts on the level buttons come from the same filters minus the level,
// so each button shows what it would return.
const allLevels = useLoader(() => api.histogram(range.value, { ...filter.value, severity: undefined }), [range, filter])
const perLevel = computed(() => {
  const sum = { debug: 0, info: 0, warn: 0, error: 0 }
  for (const b of allLevels.data.value?.buckets ?? []) {
    sum.debug += b.debug
    sum.info += b.info
    sum.warn += b.warn
    sum.error += b.error
  }
  return sum
})
// Exact, not rounded: 5,101 and 5,061 must not both read "5.1k".
const exact = (n: number) => n.toLocaleString('en-US')

function levelCount(from: readonly ('debug' | 'info' | 'warn' | 'error')[]) {
  return from.reduce((n, l) => n + perLevel.value[l], 0)
}

const logs = ref<Log[]>([])
const next = ref<string>()
const loadingMore = ref(false)
const page = useLoader(async () => {
  const p = await api.logs(range.value, filter.value)
  logs.value = p.logs
  next.value = p.next
  return p
}, [range, filter])

async function more() {
  if (!next.value) return
  loadingMore.value = true
  try {
    const p = await api.logs(range.value, filter.value, next.value)
    logs.value = [...logs.value, ...p.logs]
    next.value = p.next
  } finally {
    loadingMore.value = false
  }
}

// "/" focuses the search box, as in most log tools.
const searchBox = ref<HTMLInputElement>()
function onKey(e: KeyboardEvent) {
  if (e.key === '/' && document.activeElement?.tagName !== 'INPUT' && document.activeElement?.tagName !== 'TEXTAREA') {
    e.preventDefault()
    searchBox.value?.focus()
  }
}
onMounted(() => document.addEventListener('keydown', onKey))
onBeforeUnmount(() => document.removeEventListener('keydown', onKey))
</script>

<template>
  <PageHeader title="Logs" subtitle="Search every log line. Click a line for its attributes and trace." />

  <form class="search-row" @submit.prevent="setFilter({ q: text || undefined })">
    <div class="searchbox">
      <Icon name="search" :size="16" />
      <input ref="searchBox" v-model="text" placeholder="Search log messages, e.g. timeout" aria-label="Search logs" />
      <kbd v-if="!text">/</kbd>
      <button v-else type="button" class="btn ghost sm icon" title="Clear" @click="text = ''; setFilter({ q: undefined })"><Icon name="x" :size="14" /></button>
    </div>
    <select :value="filter.service ?? ''" aria-label="Service" @change="setFilter({ service: ($event.target as HTMLSelectElement).value || undefined })">
      <option value="">All services</option>
      <option v-for="s in services.data.value ?? []" :key="s.name" :value="s.name">{{ s.name }}</option>
    </select>
    <button type="submit" class="btn primary">Search</button>
  </form>

  <div class="filters">
    <div class="seg" role="group" aria-label="Level">
      <button v-for="l in levels" :key="l.value" :class="{ on: (filter.severity ?? 0) === l.value }"
        @click="setFilter({ severity: l.value ? String(l.value) : undefined })">
        {{ l.label }}<span v-if="allLevels.data.value" class="n">{{ exact(levelCount(l.from)) }}</span>
      </button>
    </div>
    <span v-if="filter.trace_id" class="chip mono">
      trace {{ filter.trace_id.slice(0, 8) }}…
      <button type="button" title="Clear" @click="setFilter({ trace_id: undefined })">×</button>
    </span>
    <span class="grow" />
    <span class="muted small">{{ exact(total) }} matching logs</span>
  </div>

  <div v-if="page.error.value || hist.error.value" class="error-box">{{ page.error.value || hist.error.value }}</div>
  <LogHistogram v-if="hist.data.value" :hist="hist.data.value" />
  <div v-if="page.loading.value && !logs.length" class="panel"><SkeletonRows :rows="8" /></div>
  <LogList v-else :logs="logs" show-trace-link :highlight="filter.q" />
  <div v-if="next" class="more">
    <button class="btn" :disabled="loadingMore" @click="more">{{ loadingMore ? 'Loading…' : 'Load older logs' }}</button>
  </div>
</template>

<style scoped>
.search-row { display: flex; gap: 10px; margin-bottom: 12px; }
.searchbox {
  flex: 1; display: flex; align-items: center; gap: 10px; padding: 0 12px; min-height: 40px;
  background: var(--panel); border: 1px solid var(--border-strong); border-radius: 9px; color: var(--muted);
}
.searchbox:focus-within { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-ring); }
.searchbox input { flex: 1; border: 0; box-shadow: none !important; padding: 0; background: transparent; color: var(--text); font-size: 14px; }
.search-row select { min-width: 170px; }
.filters { display: flex; gap: 12px; align-items: center; margin-bottom: 14px; }
.more { text-align: center; margin-top: 14px; }
.n { margin-left: 6px; font-size: 11.5px; font-weight: 600; color: var(--muted); font-variant-numeric: tabular-nums; }
.seg button.on .n { color: var(--accent); }
</style>
