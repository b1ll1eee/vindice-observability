<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type TraceFilter } from '../api'
import EmptyState from '../components/EmptyState.vue'
import Icon from '../components/Icon.vue'
import PageHeader from '../components/PageHeader.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
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
const longest = computed(() => Math.max(1, ...(data.value ?? []).map((t) => t.duration_ms)))
</script>

<template>
  <PageHeader title="Traces" subtitle="Every failing or slow trace is kept, plus a sample of the rest." />

  <div class="toolbar">
    <select :value="filter.service ?? ''" aria-label="Service" @change="setFilter({ service: ($event.target as HTMLSelectElement).value || undefined })">
      <option value="">All services</option>
      <option v-for="s in services.data.value ?? []" :key="s.name" :value="s.name">{{ s.name }}</option>
    </select>
    <div class="dur">
      <span class="muted small">Slower than</span>
      <input type="number" min="0" :value="filter.min_duration_ms ?? ''" placeholder="0" aria-label="Minimum duration in ms"
        @change="setFilter({ min_duration_ms: ($event.target as HTMLInputElement).value || undefined })" />
      <span class="muted small">ms</span>
    </div>
    <div class="seg" role="group" aria-label="Status">
      <button :class="{ on: !filter.errors }" @click="setFilter({ errors: undefined })">All</button>
      <button :class="{ on: filter.errors }" @click="setFilter({ errors: 'true' })">Errors only</button>
    </div>
  </div>

  <div v-if="error" class="error-box">{{ error }}</div>
  <div class="panel">
    <SkeletonRows v-if="loading && !data" :rows="8" />
    <EmptyState v-else-if="!data?.length" icon="traces" title="No traces found"
      text="Try a wider time range, another service, or clear the filters." />
    <table v-else>
      <thead>
        <tr><th>Time</th><th>Service</th><th>Root span</th><th>Status</th><th style="width: 240px">Duration</th><th style="width: 32px" /></tr>
      </thead>
      <tbody>
        <tr v-for="t in data" :key="t.trace_id" class="clickable" @click="router.push(`/traces/${t.trace_id}`)">
          <td class="mono nowrap muted">{{ time(t.time) }}</td>
          <td class="nowrap"><span class="dot" :style="{ background: serviceColor(t.service) }" />{{ t.service }}</td>
          <td class="mono">
            {{ t.name }}
            <span v-if="t.http_status" class="http" :class="{ bad: t.http_status >= 500, warnc: t.http_status >= 400 && t.http_status < 500 }">{{ t.http_status }}</span>
          </td>
          <td><span class="badge" :class="t.status">{{ t.status === 'Unset' ? 'ok' : t.status }}</span></td>
          <td>
            <div class="durcell">
              <div class="bar grow"><span :style="{ width: `${Math.max(2, (t.duration_ms / longest) * 100)}%`, background: t.status === 'Error' ? 'var(--error)' : 'var(--accent)' }" /></div>
              <span class="num">{{ duration(t.duration_ms) }}</span>
            </div>
          </td>
          <td class="muted"><Icon name="chevron" :size="15" /></td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.dur { display: flex; align-items: center; gap: 8px; }
.dur input { width: 90px; }
.http { margin-left: 8px; font-size: 12px; color: var(--muted); }
.durcell { display: flex; align-items: center; gap: 10px; }
.num { width: 70px; text-align: right; font-variant-numeric: tabular-nums; font-size: 13px; }
</style>
