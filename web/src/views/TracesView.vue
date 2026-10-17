<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type TraceFilter } from '../api'
import { duration, serviceColor, time } from '../format'
import { useLoader } from '../load'
import { useTimeRange } from '../range'

const route = useRoute()
const router = useRouter()
const { range } = useTimeRange()

const filter = computed<TraceFilter>(() => ({
  service: (route.query.service as string) || undefined,
  min_duration_ms: Number(route.query.min_duration_ms) || undefined,
  errors: route.query.errors === 'true',
}))

function setFilter(patch: Record<string, string | undefined>) {
  router.replace({ query: { ...route.query, ...patch } })
}

const services = useLoader(() => api.services(range.value), [range])
const { data, error, loading } = useLoader(() => api.traces(range.value, filter.value), [range, filter])
</script>

<template>
  <h1>Traces</h1>
  <div class="toolbar">
    <select :value="filter.service ?? ''" @change="setFilter({ service: ($event.target as HTMLSelectElement).value || undefined })">
      <option value="">All services</option>
      <option v-for="s in services.data.value ?? []" :key="s.name" :value="s.name">{{ s.name }}</option>
    </select>
    <label>
      Slower than
      <input
        type="number"
        min="0"
        style="width: 90px"
        :value="filter.min_duration_ms ?? ''"
        placeholder="ms"
        @change="setFilter({ min_duration_ms: ($event.target as HTMLInputElement).value || undefined })"
      />
      ms
    </label>
    <label>
      <input type="checkbox" :checked="filter.errors" @change="setFilter({ errors: ($event.target as HTMLInputElement).checked ? 'true' : undefined })" />
      Errors only
    </label>
  </div>
  <p class="muted">Sampled: every failing or slow trace is kept, plus a share of the rest.</p>
  <div v-if="error" class="error-box">{{ error }}</div>
  <div class="panel">
    <div v-if="!data?.length" class="empty">{{ loading ? 'Loading…' : 'No traces in this time range' }}</div>
    <table v-else>
      <thead>
        <tr>
          <th>Time</th>
          <th>Service</th>
          <th>Root span</th>
          <th>Status</th>
          <th class="num">Duration</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="t in data" :key="t.trace_id" class="clickable" @click="router.push(`/traces/${t.trace_id}`)">
          <td class="mono nowrap muted">{{ time(t.time) }}</td>
          <td class="nowrap"><span class="dot" :style="{ background: serviceColor(t.service) }" />{{ t.service }}</td>
          <td class="mono">
            {{ t.name }}
            <span v-if="t.http_status" class="muted"> · {{ t.http_status }}</span>
          </td>
          <td><span class="badge" :class="t.status">{{ t.status }}</span></td>
          <td class="num">{{ duration(t.duration_ms) }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
