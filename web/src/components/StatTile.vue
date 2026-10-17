<script setup lang="ts">
defineProps<{ label: string; value: string; hint?: string; tone?: 'ok' | 'warn' | 'bad' }>()
</script>

<template>
  <div class="card tile">
    <div class="label">{{ label }}</div>
    <div class="value" :class="tone">{{ value }}</div>
    <div v-if="hint" class="hint">{{ hint }}</div>
  </div>
</template>

<style scoped>
.tile { padding: 14px 18px; min-width: 0; }
.label { font-size: 12px; color: var(--muted); font-weight: 550; text-transform: uppercase; letter-spacing: 0.04em; }
.value { font-size: 26px; font-weight: 650; letter-spacing: -0.02em; margin-top: 4px; font-variant-numeric: tabular-nums; }
.value.ok { color: var(--ok); }
.value.warn { color: var(--warn); }
.value.bad { color: var(--error); }
.hint { margin-top: 2px; }
</style>
