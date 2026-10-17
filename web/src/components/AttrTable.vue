<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ title: string; attrs: Record<string, string> | null | undefined }>()

const entries = computed(() =>
  Object.entries(props.attrs ?? {}).sort(([a], [b]) => a.localeCompare(b)),
)
</script>

<template>
  <div v-if="entries.length" class="attrs">
    <div v-if="title" class="title">{{ title }}</div>
    <dl>
      <template v-for="[k, v] in entries" :key="k">
        <dt class="mono">{{ k }}</dt>
        <dd class="mono">{{ v }}</dd>
      </template>
    </dl>
  </div>
</template>

<style scoped>
.attrs { margin-top: 12px; min-width: 0; }
.title { font-size: 11px; font-weight: 650; text-transform: uppercase; letter-spacing: 0.06em; color: var(--muted); margin-bottom: 6px; }
dl { display: grid; grid-template-columns: minmax(120px, max-content) 1fr; margin: 0; border: 1px solid var(--border); border-radius: 8px; overflow: hidden; }
dt, dd { margin: 0; padding: 5px 10px; border-bottom: 1px solid var(--border); font-size: 12px; word-break: break-all; }
dt { color: var(--muted); background: var(--panel); }
dd { background: var(--panel); }
dt:nth-last-of-type(1), dd:last-of-type { border-bottom: none; }
</style>
