<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, type Agg, type SignalKind, type SignalQuery } from '../api'
import { useLoader } from '../load'
import { useTimeRange } from '../range'
import { measures } from '../signals'

// Edits a SignalQuery in place; shared by the panel and alert rule forms.
const model = defineModel<SignalQuery>({ required: true })

const { range } = useTimeRange()
const services = useLoader(() => api.services(range.value), [range])
const names = useLoader(() => api.metricNames(range.value), [range])

const aggs: Agg[] = ['avg', 'sum', 'min', 'max', 'count', 'last', 'rate']
const severities = [
  { value: 0, label: 'All levels' },
  { value: 13, label: 'Warn and above' },
  { value: 17, label: 'Errors only' },
]

function setKind(kind: SignalKind) {
  model.value =
    kind === 'metric'
      ? { kind, metric: { name: '', agg: 'avg' } }
      : kind === 'red'
        ? { kind, service: model.value.service, measure: 'error_rate' }
        : { kind, service: model.value.service }
}

// Filters edited as "key=value, key2=value2".
const filterText = ref('')
watch(
  () => model.value.metric?.filters,
  (f) => (filterText.value = Object.entries(f ?? {}).map(([k, v]) => `${k}=${v}`).join(', ')),
  { immediate: true },
)
function applyFilters() {
  if (!model.value.metric) return
  const filters: Record<string, string> = {}
  for (const part of filterText.value.split(',')) {
    const [k, ...v] = part.split('=')
    if (k?.trim() && v.length) filters[k.trim()] = v.join('=').trim()
  }
  model.value.metric.filters = Object.keys(filters).length ? filters : undefined
}

const serviceNames = computed(() => (services.data.value ?? []).map((s) => s.name))
</script>

<template>
  <div class="signal">
    <label>
      Source
      <select :value="model.kind" @change="setKind(($event.target as HTMLSelectElement).value as SignalKind)">
        <option value="red">Service (RED)</option>
        <option value="metric">Metric</option>
        <option value="logs">Log count</option>
      </select>
    </label>

    <template v-if="model.kind === 'metric' && model.metric">
      <label>
        Metric
        <input v-model="model.metric.name" list="metric-names" placeholder="http.server.duration" required />
        <datalist id="metric-names">
          <option v-for="n in names.data.value ?? []" :key="n.name" :value="n.name">{{ n.type }}</option>
        </datalist>
      </label>
      <label>
        Aggregation
        <select v-model="model.metric.agg">
          <option v-for="a in aggs" :key="a" :value="a">{{ a }}</option>
        </select>
      </label>
      <label>
        Service
        <select v-model="model.metric.service">
          <option :value="undefined">Any</option>
          <option v-for="s in serviceNames" :key="s" :value="s">{{ s }}</option>
        </select>
      </label>
      <label>
        Group by
        <input v-model.trim="model.metric.group_by" placeholder="attribute, e.g. http.route" />
      </label>
      <label class="wide">
        Filters
        <input v-model="filterText" placeholder="key=value, key2=value2" @change="applyFilters" />
      </label>
    </template>

    <template v-else>
      <label>
        Service
        <select v-model="model.service" :required="model.kind === 'red'">
          <option v-if="model.kind === 'logs'" :value="undefined">Any</option>
          <option v-for="s in serviceNames" :key="s" :value="s">{{ s }}</option>
        </select>
      </label>
      <label v-if="model.kind === 'red'">
        Measure
        <select v-model="model.measure">
          <option v-for="m in measures" :key="m.value" :value="m.value">{{ m.label }}</option>
        </select>
      </label>
      <template v-if="model.kind === 'logs'">
        <label>
          Level
          <select v-model="model.min_severity">
            <option v-for="s in severities" :key="s.value" :value="s.value || undefined">{{ s.label }}</option>
          </select>
        </label>
        <label class="wide">
          Matching text
          <input v-model.trim="model.text" placeholder="optional" />
        </label>
      </template>
    </template>
  </div>
</template>

<style scoped>
.signal { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 10px; }
label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted); }
label input, label select { color: var(--text); }
.wide { grid-column: span 2; }
</style>
