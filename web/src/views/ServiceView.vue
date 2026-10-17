<script setup lang="ts">
import { computed, toRef } from 'vue'
import { api, type Measure, type SignalQuery } from '../api'
import SignalPanel from '../components/SignalPanel.vue'
import { count, duration, percent, serviceColor } from '../format'
import { useLoader } from '../load'
import { useTimeRange } from '../range'

const props = defineProps<{ name: string }>()
const name = toRef(props, 'name')
const { key, range } = useTimeRange()
const { data, error, loading } = useLoader(() => api.operations(name.value, range.value), [name, range])

// Stable objects, so the charts reload only when the service changes.
const charts = computed(() =>
  ([['Requests / min', 'requests'], ['Error rate', 'error_rate'], ['Latency p95', 'p95']] as [string, Measure][]).map(
    ([title, measure]) => ({ title, signal: { kind: 'red', service: name.value, measure } as SignalQuery }),
  ),
)
</script>

<template>
  <h1><span class="dot" :style="{ background: serviceColor(name) }" />{{ name }}</h1>
  <div class="toolbar">
    <RouterLink :to="{ path: '/logs', query: { range: key, service: name } }">View logs →</RouterLink>
    <RouterLink :to="{ path: '/traces', query: { range: key, service: name } }">View traces →</RouterLink>
    <RouterLink :to="{ path: '/traces', query: { range: key, service: name, errors: 'true' } }">View failing traces →</RouterLink>
  </div>
  <div class="charts">
    <SignalPanel v-for="c in charts" :key="c.title" :title="c.title" :signal="c.signal" />
  </div>
  <div v-if="error" class="error-box">{{ error }}</div>
  <h2>Operations</h2>
  <div class="panel">
    <div v-if="!data?.length" class="empty">{{ loading ? 'Loading…' : 'No requests in this time range' }}</div>
    <table v-else>
      <thead>
        <tr>
          <th>Operation</th>
          <th class="num">Requests</th>
          <th class="num">Error rate</th>
          <th class="num">p50</th>
          <th class="num">p95</th>
          <th class="num">p99</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="o in data" :key="o.name">
          <td class="mono">{{ o.name }}</td>
          <td class="num">{{ count(o.requests) }}</td>
          <td class="num" :class="{ bad: o.error_rate > 0.01 }">{{ percent(o.error_rate) }}</td>
          <td class="num">{{ duration(o.p50_ms) }}</td>
          <td class="num">{{ duration(o.p95_ms) }}</td>
          <td class="num">{{ duration(o.p99_ms) }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.charts { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 14px; }
</style>
