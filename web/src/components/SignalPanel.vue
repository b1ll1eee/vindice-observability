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
  <div class="panel card">
    <div class="head">
      <strong>{{ title }}</strong>
      <slot name="actions" />
    </div>
    <div class="muted sub mono">{{ describeSignal(signal) }}</div>
    <div v-if="error" class="bad sub">{{ error }}</div>
    <div v-else-if="loading && !data" class="muted sub">Loading…</div>
    <LineChart v-else-if="data" :series="data.series" :format="data.format" />
  </div>
</template>

<style scoped>
.card { padding: 10px 12px; }
.head { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.sub { font-size: 12px; margin: 2px 0 6px; }
</style>
