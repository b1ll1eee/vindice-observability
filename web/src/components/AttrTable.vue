<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ title: string; attrs: Record<string, string> | null | undefined }>()

const entries = computed(() =>
  Object.entries(props.attrs ?? {}).sort(([a], [b]) => a.localeCompare(b)),
)
</script>

<template>
  <div v-if="entries.length" class="attrs">
    <div class="title">{{ title }}</div>
    <table>
      <tbody>
        <tr v-for="[k, v] in entries" :key="k">
          <td class="mono key">{{ k }}</td>
          <td class="mono">{{ v }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.attrs { margin-top: 10px; }
.title { font-size: 12px; font-weight: 600; color: var(--muted); margin-bottom: 4px; }
td { padding: 3px 8px; word-break: break-all; }
.key { color: var(--muted); width: 30%; white-space: nowrap; }
</style>
