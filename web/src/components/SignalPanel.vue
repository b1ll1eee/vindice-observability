<script setup lang="ts">
import { toRef } from 'vue'
import type { SignalQuery } from '../api'
import { useLoader } from '../load'
import { useTimeRange } from '../range'
import { describeSignal, signalChart } from '../signals'
import LineChart from './LineChart.vue'

const props = defineProps<{ title: string; signal: SignalQuery }>()
const signal = toRef(props, 'signal')
const { range } = useTimeRange()
const { data, error, loading } = useLoader(() => signalChart(signal.value, range.value), [signal, range])
</script>

<template>
  <div class="card">
    <div class="head">
      <div class="min0">
        <div class="card-title ellipsis">{{ title }}</div>
        <div class="card-sub mono ellipsis">{{ describeSignal(signal) }}</div>
      </div>
      <slot name="actions" />
    </div>
    <div v-if="error" class="bad sub">{{ error }}</div>
    <div v-else-if="loading && !data" class="skeleton chart-sk" />
    <LineChart v-else-if="data" :series="data.series" :format="data.format" />
  </div>
</template>

<style scoped>
.head { display: flex; justify-content: space-between; align-items: flex-start; gap: 8px; margin-bottom: 10px; }
.min0 { min-width: 0; }
.sub { font-size: 12px; margin: 2px 0 6px; }
.chart-sk { height: 160px; }
</style>
