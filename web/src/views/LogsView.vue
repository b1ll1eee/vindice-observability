<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type Log, type LogFilter } from '../api'
import LogHistogram from '../components/LogHistogram.vue'
import LogList from '../components/LogList.vue'
import { useLoader } from '../load'
import { useTimeRange } from '../range'

const route = useRoute()
const router = useRouter()
const { range } = useTimeRange()

const severities = [
  { value: 0, label: 'All levels' },
  { value: 5, label: 'Debug and above' },
  { value: 9, label: 'Info and above' },
  { value: 13, label: 'Warn and above' },
  { value: 17, label: 'Errors only' },
]

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
</script>

<template>
  <h1>Logs</h1>
  <form class="toolbar" @submit.prevent="setFilter({ q: text || undefined })">
    <input v-model="text" type="search" placeholder="Search log messages…" />
    <select :value="filter.service ?? ''" @change="setFilter({ service: ($event.target as HTMLSelectElement).value || undefined })">
      <option value="">All services</option>
      <option v-for="s in services.data.value ?? []" :key="s.name" :value="s.name">{{ s.name }}</option>
    </select>
    <select :value="filter.severity ?? 0" @change="setFilter({ severity: ($event.target as HTMLSelectElement).value === '0' ? undefined : ($event.target as HTMLSelectElement).value })">
      <option v-for="s in severities" :key="s.value" :value="s.value">{{ s.label }}</option>
    </select>
    <button type="submit">Search</button>
    <span v-if="filter.trace_id" class="chip mono">
      trace {{ filter.trace_id.slice(0, 8) }}…
      <button type="button" title="Clear" @click="setFilter({ trace_id: undefined })">×</button>
    </span>
  </form>

  <div v-if="page.error.value || hist.error.value" class="error-box">{{ page.error.value || hist.error.value }}</div>
  <LogHistogram v-if="hist.data.value" :hist="hist.data.value" />
  <div v-if="page.loading.value && !logs.length" class="panel empty">Loading…</div>
  <LogList v-else :logs="logs" show-trace-link />
  <div v-if="next" class="more">
    <button :disabled="loadingMore" @click="more">{{ loadingMore ? 'Loading…' : 'Load older logs' }}</button>
  </div>
</template>

<style scoped>
.more { text-align: center; margin-top: 12px; }
</style>
