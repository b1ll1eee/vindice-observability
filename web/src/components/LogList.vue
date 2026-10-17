<script setup lang="ts">
import { ref } from 'vue'
import type { Log } from '../api'
import { serviceColor, time } from '../format'
import AttrTable from './AttrTable.vue'
import EmptyState from './EmptyState.vue'
import Icon from './Icon.vue'

const props = defineProps<{ logs: Log[]; showTraceLink?: boolean; highlight?: string }>()

const open = ref(new Set<string>())
const copied = ref('')

function key(l: Log, i: number) {
  return `${l.ts}-${i}`
}

function toggle(k: string) {
  const s = new Set(open.value)
  if (s.has(k)) s.delete(k)
  else s.add(k)
  open.value = s
}

// Splits the body around the search text so matches can be marked.
function parts(body: string): { text: string; hit: boolean }[] {
  const q = props.highlight?.trim()
  if (!q) return [{ text: body, hit: false }]
  const out: { text: string; hit: boolean }[] = []
  const lower = body.toLowerCase()
  const needle = q.toLowerCase()
  let i = 0
  for (let j = lower.indexOf(needle); j >= 0 && out.length < 50; j = lower.indexOf(needle, i)) {
    if (j > i) out.push({ text: body.slice(i, j), hit: false })
    out.push({ text: body.slice(j, j + q.length), hit: true })
    i = j + q.length
  }
  if (i < body.length) out.push({ text: body.slice(i), hit: false })
  return out
}

async function copy(text: string) {
  await navigator.clipboard.writeText(text)
  copied.value = text
  setTimeout(() => (copied.value = ''), 1500)
}
</script>

<template>
  <div class="panel">
    <EmptyState v-if="!logs.length" icon="logs" title="No logs" text="Nothing matches in this time range. Try a wider range or fewer filters." />
    <div v-else class="list">
      <template v-for="(l, i) in logs" :key="key(l, i)">
        <div class="log" :class="[l.level, { open: open.has(key(l, i)) }]" @click="toggle(key(l, i))">
          <span class="time mono">{{ time(l.time) }}</span>
          <span class="badge" :class="l.level">{{ l.severity || l.level }}</span>
          <span class="svc ellipsis"><span class="dot" :style="{ background: serviceColor(l.service) }" />{{ l.service }}</span>
          <span class="body mono ellipsis"><template v-for="(p, k) in parts(l.body)" :key="k"><mark v-if="p.hit">{{ p.text }}</mark><template v-else>{{ p.text }}</template></template></span>
          <Icon v-if="l.trace_id" name="traces" :size="14" class="has-trace" />
        </div>
        <div v-if="open.has(key(l, i))" class="detail">
          <pre class="mono full">{{ l.body }}</pre>
          <div class="meta">
            <span class="muted small">{{ new Date(l.time).toISOString() }}</span>
            <template v-if="l.trace_id">
              <button class="btn sm" @click.stop="copy(l.trace_id)">
                <Icon :name="copied === l.trace_id ? 'check' : 'copy'" :size="13" />Trace ID
              </button>
              <RouterLink v-if="showTraceLink" :to="`/traces/${l.trace_id}`" class="btn sm primary" @click.stop>
                Open trace <Icon name="arrow" :size="13" />
              </RouterLink>
            </template>
          </div>
          <div class="attrs">
            <AttrTable title="Attributes" :attrs="l.attributes" />
            <AttrTable title="Resource" :attrs="l.resource" />
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.log {
  display: grid; grid-template-columns: 170px 72px minmax(90px, 160px) 1fr 18px; gap: 14px; align-items: center;
  padding: 7px 16px 7px 13px; border-bottom: 1px solid var(--border); border-left: 3px solid transparent;
  cursor: pointer; font-size: 13px;
}
.log:hover { background: var(--hover); }
.log.open { background: var(--hover); }
.log.error { border-left-color: var(--error); }
.log.warn { border-left-color: var(--warn); }
.log.info { border-left-color: color-mix(in srgb, var(--info) 55%, transparent); }
.log.debug { border-left-color: var(--border-strong); }
.time { color: var(--muted); font-size: 12px; }
.badge { justify-self: start; }
.svc { color: var(--text-2); }
.body { color: var(--text); }
mark { background: color-mix(in srgb, var(--warn) 35%, transparent); color: inherit; border-radius: 3px; padding: 0 1px; }
.has-trace { color: var(--muted); }
.detail { padding: 14px 18px 16px 32px; background: var(--raised); border-bottom: 1px solid var(--border); }
.full { white-space: pre-wrap; word-break: break-word; margin: 0 0 10px; font-size: 12.5px; }
.meta { display: flex; gap: 8px; align-items: center; margin-bottom: 4px; }
.meta .small { margin-right: auto; }
.attrs { display: grid; grid-template-columns: 1fr 1fr; gap: 0 24px; }
.list > :last-child { border-bottom: none; }
</style>
