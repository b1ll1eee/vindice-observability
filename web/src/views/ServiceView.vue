<script setup lang="ts">
import { computed, toRef } from 'vue'
import { api, type Measure, type SignalQuery } from '../api'
import EmptyState from '../components/EmptyState.vue'
import Icon from '../components/Icon.vue'
import PageHeader from '../components/PageHeader.vue'
import SignalPanel from '../components/SignalPanel.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import StatTile from '../components/StatTile.vue'
import { count, duration, health, percent, serviceColor } from '../format'
import { useLoader } from '../load'
import { useTimeRange } from '../range'

const props = defineProps<{ name: string }>()
const name = toRef(props, 'name')
const { key, range } = useTimeRange()
const { data, error, loading } = useLoader(() => api.operations(name.value, range.value), [name, range])
const summary = useLoader(() => api.services(range.value), [range])
const me = computed(() => summary.data.value?.find((s) => s.name === name.value))
const state = computed(() => (me.value ? health(me.value.requests, me.value.error_rate) : 'idle'))
const minutes = computed(() => (range.value.to - range.value.from) / 60e3)

// Stable objects, so the charts reload only when the service changes.
const charts = computed(() =>
  ([['Requests / min', 'requests'], ['Error rate', 'error_rate'], ['Latency p95', 'p95']] as [string, Measure][]).map(
    ([title, measure]) => ({ title, signal: { kind: 'red', service: name.value, measure } as SignalQuery }),
  ),
)
const busiest = computed(() => Math.max(1, ...(data.value ?? []).map((o) => o.requests)))
</script>

<template>
  <PageHeader :title="name">
    <template #before><span class="dot big" :style="{ background: serviceColor(name) }" /></template>
    <template #badge><span v-if="me" class="badge" :class="state === 'idle' ? 'off' : state">{{ state === 'idle' ? 'logs only' : state }}</span></template>
    <RouterLink :to="{ path: '/logs', query: { range: key, service: name } }" class="btn"><Icon name="logs" />Logs</RouterLink>
    <RouterLink :to="{ path: '/traces', query: { range: key, service: name } }" class="btn"><Icon name="traces" />Traces</RouterLink>
    <RouterLink :to="{ path: '/traces', query: { range: key, service: name, errors: 'true' } }" class="btn danger"><Icon name="alerts" />Failing traces</RouterLink>
  </PageHeader>

  <div v-if="me" class="tiles">
    <StatTile label="Requests / min" :value="count(Math.round(me.requests / minutes))" :hint="`${count(me.requests)} in range`" />
    <StatTile label="Error rate" :value="percent(me.error_rate)" :tone="me.error_rate >= 0.05 ? 'bad' : me.error_rate >= 0.01 ? 'warn' : 'ok'" :hint="`${count(me.errors)} failed`" />
    <StatTile label="Latency p95" :value="me.requests ? duration(me.p95_ms) : '–'" :hint="me.requests ? `p50 ${duration(me.p50_ms)} · p99 ${duration(me.p99_ms)}` : ''" />
    <StatTile label="Error logs" :value="count(me.log_errors)" :tone="me.log_errors ? 'bad' : undefined" :hint="`${count(me.logs)} logs in total`" />
  </div>

  <div class="charts">
    <SignalPanel v-for="c in charts" :key="c.title" :title="c.title" :signal="c.signal" />
  </div>

  <div v-if="error" class="error-box">{{ error }}</div>
  <h2>Operations</h2>
  <div class="panel">
    <SkeletonRows v-if="loading && !data" :rows="4" />
    <EmptyState v-else-if="!data?.length" icon="services" title="No requests in this time range" text="This service sent logs but no traces, or nothing at all in the selected range." />
    <table v-else>
      <thead>
        <tr>
          <th>Operation</th>
          <th style="width: 220px">Requests</th>
          <th class="num">Error rate</th>
          <th class="num">p50</th>
          <th class="num">p95</th>
          <th class="num">p99</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="o in data" :key="o.name">
          <td class="mono">{{ o.name }}</td>
          <td>
            <div class="reqs">
              <div class="bar grow"><span :style="{ width: `${(o.requests / busiest) * 100}%`, background: 'var(--accent)' }" /></div>
              <span class="n">{{ count(o.requests) }}</span>
            </div>
          </td>
          <td class="num" :class="{ bad: o.error_rate >= 0.05, warnc: o.error_rate >= 0.01 && o.error_rate < 0.05 }">{{ percent(o.error_rate) }}</td>
          <td class="num">{{ duration(o.p50_ms) }}</td>
          <td class="num">{{ duration(o.p95_ms) }}</td>
          <td class="num">{{ duration(o.p99_ms) }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.dot.big { width: 12px; height: 12px; margin-right: 2px; }
.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 14px; margin-bottom: 14px; }
.charts { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 14px; }
.reqs { display: flex; align-items: center; gap: 10px; }
.n { width: 48px; text-align: right; font-variant-numeric: tabular-nums; font-size: 13px; }
</style>
