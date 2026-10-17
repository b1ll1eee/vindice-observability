<script setup lang="ts">
import { toRef } from 'vue'
import { api } from '../api'
import { count, duration, percent, serviceColor } from '../format'
import { useLoader } from '../load'
import { useTimeRange } from '../range'

const props = defineProps<{ name: string }>()
const name = toRef(props, 'name')
const { key, range } = useTimeRange()
const { data, error, loading } = useLoader(() => api.operations(name.value, range.value), [name, range])
</script>

<template>
  <h1><span class="dot" :style="{ background: serviceColor(name) }" />{{ name }}</h1>
  <div class="toolbar">
    <RouterLink :to="{ path: '/logs', query: { range: key, service: name } }">View logs →</RouterLink>
    <RouterLink :to="{ path: '/traces', query: { range: key, service: name } }">View traces →</RouterLink>
    <RouterLink :to="{ path: '/traces', query: { range: key, service: name, errors: 'true' } }">View failing traces →</RouterLink>
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
