<script setup lang="ts">
import { ref } from 'vue'
import type { Log } from '../api'
import { serviceColor, time } from '../format'
import AttrTable from './AttrTable.vue'

defineProps<{ logs: Log[]; showTraceLink?: boolean }>()

const open = ref(new Set<string>())

function key(l: Log, i: number) {
  return `${l.ts}-${i}`
}

function toggle(k: string) {
  const s = new Set(open.value)
  if (s.has(k)) s.delete(k)
  else s.add(k)
  open.value = s
}
</script>

<template>
  <div class="panel">
    <div v-if="!logs.length" class="empty">No logs</div>
    <table v-else>
      <tbody>
        <template v-for="(l, i) in logs" :key="key(l, i)">
          <tr class="clickable" @click="toggle(key(l, i))">
            <td class="mono nowrap muted">{{ time(l.time) }}</td>
            <td><span class="badge" :class="l.level">{{ l.severity || l.level }}</span></td>
            <td class="nowrap"><span class="dot" :style="{ background: serviceColor(l.service) }" />{{ l.service }}</td>
            <td class="mono body">{{ l.body }}</td>
          </tr>
          <tr v-if="open.has(key(l, i))" class="detail">
            <td colspan="4">
              <div class="mono full">{{ l.body }}</div>
              <div v-if="l.trace_id" class="links">
                <RouterLink v-if="showTraceLink" :to="`/traces/${l.trace_id}`">View trace →</RouterLink>
                <span class="mono muted">trace {{ l.trace_id }}<template v-if="l.span_id"> · span {{ l.span_id }}</template></span>
              </div>
              <AttrTable title="Attributes" :attrs="l.attributes" />
              <AttrTable title="Resource" :attrs="l.resource" />
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.body { width: 100%; max-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.detail td { background: var(--bg); }
.full { white-space: pre-wrap; word-break: break-word; margin-bottom: 8px; }
.links { display: flex; gap: 14px; align-items: center; margin-bottom: 4px; }
</style>
