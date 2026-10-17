<script setup lang="ts">
import { useRouter } from 'vue-router'
import { api } from '../api'
import { count, duration, percent, serviceColor } from '../format'
import { useLoader } from '../load'
import { useTimeRange } from '../range'

const router = useRouter()
const { key, range } = useTimeRange()
const { data, error, loading } = useLoader(() => api.services(range.value), [range])

function perMin(n: number) {
  const minutes = (range.value.to - range.value.from) / 60e3
  return count(Math.round(n / minutes))
}
</script>

<template>
  <h1>Services</h1>
  <div v-if="error" class="error-box">{{ error }}</div>
  <div class="panel">
    <div v-if="!data?.length" class="empty">{{ loading ? 'Loading…' : 'No data in this time range' }}</div>
    <table v-else>
      <thead>
        <tr>
          <th>Service</th>
          <th class="num">Requests</th>
          <th class="num">Req/min</th>
          <th class="num">Error rate</th>
          <th class="num">p50</th>
          <th class="num">p95</th>
          <th class="num">p99</th>
          <th class="num">Logs</th>
          <th class="num">Error logs</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="s in data"
          :key="s.name"
          class="clickable"
          @click="router.push({ path: `/services/${encodeURIComponent(s.name)}`, query: { range: key } })"
        >
          <td><span class="dot" :style="{ background: serviceColor(s.name) }" />{{ s.name }}</td>
          <td class="num">{{ count(s.requests) }}</td>
          <td class="num">{{ perMin(s.requests) }}</td>
          <td class="num" :class="{ bad: s.error_rate > 0.01 }">{{ s.requests ? percent(s.error_rate) : '–' }}</td>
          <td class="num">{{ s.requests ? duration(s.p50_ms) : '–' }}</td>
          <td class="num">{{ s.requests ? duration(s.p95_ms) : '–' }}</td>
          <td class="num">{{ s.requests ? duration(s.p99_ms) : '–' }}</td>
          <td class="num">{{ count(s.logs) }}</td>
          <td class="num" :class="{ bad: s.log_errors > 0 }">{{ count(s.log_errors) }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
