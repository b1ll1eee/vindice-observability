<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { can } from '../auth'
import EmptyState from '../components/EmptyState.vue'
import Icon from '../components/Icon.vue'
import PageHeader from '../components/PageHeader.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import StatTile from '../components/StatTile.vue'
import { count, duration, health, percent, serviceColor } from '../format'
import { useLoader } from '../load'
import { useTimeRange } from '../range'

const router = useRouter()
const { key, range } = useTimeRange()
const { data, error, loading } = useLoader(() => api.services(range.value), [range])

const filter = ref('')
const minutes = computed(() => (range.value.to - range.value.from) / 60e3)

// Worst first, so problems are at the top.
const order = { failing: 0, degraded: 1, healthy: 2, idle: 3 }
const rows = computed(() =>
  (data.value ?? [])
    .filter((s) => s.name.toLowerCase().includes(filter.value.toLowerCase()))
    .map((s) => ({ ...s, health: health(s.requests, s.error_rate) }))
    .sort((a, b) => order[a.health] - order[b.health] || b.requests - a.requests),
)

const totals = computed(() => {
  const s = data.value ?? []
  const requests = s.reduce((n, x) => n + x.requests, 0)
  const errors = s.reduce((n, x) => n + x.errors, 0)
  return {
    services: s.length,
    perMin: requests / minutes.value,
    errorRate: requests ? errors / requests : 0,
    errorLogs: s.reduce((n, x) => n + x.log_errors, 0),
    unhealthy: s.filter((x) => ['failing', 'degraded'].includes(health(x.requests, x.error_rate))).length,
  }
})

function open(name: string) {
  router.push({ path: `/services/${encodeURIComponent(name)}`, query: { range: key.value } })
}
</script>

<template>
  <PageHeader title="Services" subtitle="Health of every service that sent data in the selected time range." />

  <div v-if="error" class="error-box">{{ error }}</div>

  <div class="tiles">
    <StatTile label="Services" :value="String(totals.services)"
      :hint="totals.unhealthy ? `${totals.unhealthy} need attention` : 'all healthy'"
      :tone="totals.unhealthy ? 'warn' : undefined" />
    <StatTile label="Requests / min" :value="count(Math.round(totals.perMin))" hint="across all services" />
    <StatTile label="Error rate" :value="percent(totals.errorRate)"
      :tone="totals.errorRate >= 0.05 ? 'bad' : totals.errorRate >= 0.01 ? 'warn' : 'ok'" hint="failed requests" />
    <StatTile label="Error logs" :value="count(totals.errorLogs)" :tone="totals.errorLogs ? 'bad' : undefined" hint="ERROR and above" />
  </div>

  <div class="panel">
    <div class="panel-bar">
      <div class="search">
        <Icon name="search" :size="15" />
        <input v-model="filter" placeholder="Filter services" aria-label="Filter services" />
      </div>
    </div>
    <SkeletonRows v-if="loading && !data" />
    <EmptyState v-else-if="!data?.length" icon="services" title="No services yet"
      text="Services appear here as soon as they send traces or logs to Vindice.">
      <RouterLink v-if="can('admin')" to="/keys" class="btn primary"><Icon name="key" />Create an API key</RouterLink>
    </EmptyState>
    <table v-else>
      <thead>
        <tr>
          <th>Service</th>
          <th>Status</th>
          <th class="num">Req/min</th>
          <th style="width: 180px">Error rate</th>
          <th class="num">p50</th>
          <th class="num">p95</th>
          <th class="num">p99</th>
          <th class="num">Error logs</th>
          <th style="width: 32px" />
        </tr>
      </thead>
      <tbody>
        <tr v-for="s in rows" :key="s.name" class="clickable" @click="open(s.name)">
          <td><span class="dot" :style="{ background: serviceColor(s.name) }" /><b>{{ s.name }}</b></td>
          <td><span class="badge" :class="s.health === 'idle' ? 'off' : s.health">{{ s.health === 'idle' ? 'logs only' : s.health }}</span></td>
          <td class="num">{{ s.requests ? count(Math.round(s.requests / minutes)) : '–' }}</td>
          <td>
            <div v-if="s.requests" class="rate">
              <div class="bar grow"><span :style="{ width: `${Math.max(2, Math.min(100, s.error_rate * 100))}%`, background: s.error_rate >= 0.05 ? 'var(--error)' : s.error_rate >= 0.01 ? 'var(--warn)' : 'var(--ok)' }" /></div>
              <span class="num-label" :class="{ bad: s.error_rate >= 0.05, warnc: s.error_rate >= 0.01 && s.error_rate < 0.05 }">{{ percent(s.error_rate) }}</span>
            </div>
            <span v-else class="muted">–</span>
          </td>
          <td class="num">{{ s.requests ? duration(s.p50_ms) : '–' }}</td>
          <td class="num">{{ s.requests ? duration(s.p95_ms) : '–' }}</td>
          <td class="num">{{ s.requests ? duration(s.p99_ms) : '–' }}</td>
          <td class="num" :class="{ bad: s.log_errors > 0 }">{{ count(s.log_errors) }}</td>
          <td class="muted"><Icon name="chevron" :size="15" /></td>
        </tr>
        <tr v-if="!rows.length">
          <td colspan="9" class="muted center">No service matches “{{ filter }}”</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 14px; margin-bottom: 18px; }
.panel-bar { padding: 12px 16px; border-bottom: 1px solid var(--border); display: flex; gap: 10px; }
.search { display: flex; align-items: center; gap: 8px; color: var(--muted); width: 280px; }
.search input { border: 0; box-shadow: none; padding: 0; min-height: 28px; background: transparent; flex: 1; }
.rate { display: flex; align-items: center; gap: 10px; }
.num-label { font-variant-numeric: tabular-nums; width: 44px; text-align: right; font-size: 13px; }
.center { text-align: center; padding: 24px; }
</style>
