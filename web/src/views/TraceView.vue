<script setup lang="ts">
import { computed, ref, toRef } from 'vue'
import { api, type Span } from '../api'
import AttrTable from '../components/AttrTable.vue'
import EmptyState from '../components/EmptyState.vue'
import Icon from '../components/Icon.vue'
import LogList from '../components/LogList.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import { duration, serviceColor, time } from '../format'
import { useLoader } from '../load'

const props = defineProps<{ id: string }>()
const id = toRef(props, 'id')
const { data: trace, error, loading } = useLoader(() => api.trace(id.value), [id])

interface Row {
  span: Span
  depth: number
}

// Depth-first order, children sorted by start time. Spans whose parent was
// not received (e.g. sampled out upstream) are shown as roots.
const rows = computed<Row[]>(() => {
  const t = trace.value
  if (!t) return []
  const ids = new Set(t.spans.map((s) => s.span_id))
  const children = new Map<string, Span[]>()
  const roots: Span[] = []
  for (const s of t.spans) {
    if (s.parent_id && ids.has(s.parent_id)) {
      const list = children.get(s.parent_id) ?? []
      list.push(s)
      children.set(s.parent_id, list)
    } else {
      roots.push(s)
    }
  }
  const out: Row[] = []
  const visit = (s: Span, depth: number) => {
    out.push({ span: s, depth })
    for (const c of (children.get(s.span_id) ?? []).sort((a, b) => a.offset_ms - b.offset_ms)) visit(c, depth + 1)
  }
  roots.sort((a, b) => a.offset_ms - b.offset_ms).forEach((r) => visit(r, 0))
  return out
})

const selectedId = ref<string>()
const selected = computed(() => trace.value?.spans.find((s) => s.span_id === selectedId.value))
const onlySpanLogs = ref(false)
const logs = computed(() => {
  const all = trace.value?.logs ?? []
  return onlySpanLogs.value && selectedId.value ? all.filter((l) => l.span_id === selectedId.value) : all
})

function bar(s: Span) {
  const total = trace.value?.duration_ms || 1
  return {
    left: `${(s.offset_ms / total) * 100}%`,
    width: `max(3px, ${(s.duration_ms / total) * 100}%)`,
    background: s.status === 'Error' ? 'var(--error)' : serviceColor(s.service),
  }
}

function eventLeft(offset: number) {
  return `${(offset / (trace.value?.duration_ms || 1)) * 100}%`
}

const errors = computed(() => trace.value?.spans.filter((s) => s.status === 'Error').length ?? 0)
const ticks = computed(() => [0, 0.25, 0.5, 0.75, 1].map((f) => ({ f, label: duration((trace.value?.duration_ms ?? 0) * f) })))

const copied = ref(false)
async function copyId() {
  await navigator.clipboard.writeText(id.value)
  copied.value = true
  setTimeout(() => (copied.value = false), 1500)
}
</script>

<template>
  <div v-if="error" class="error-box">{{ error }}</div>
  <div v-else-if="loading && !trace" class="panel"><SkeletonRows :rows="8" /></div>
  <template v-else-if="trace">
    <div class="card head">
      <div class="title-row">
        <h1 class="mono">{{ rows[0]?.span.name ?? 'Trace' }}</h1>
        <span class="badge" :class="errors ? 'failing' : 'healthy'">{{ errors ? `${errors} error${errors > 1 ? 's' : ''}` : 'ok' }}</span>
      </div>
      <div class="facts">
        <div><span class="k">Duration</span><b>{{ duration(trace.duration_ms) }}</b></div>
        <div><span class="k">Spans</span><b>{{ trace.spans.length }}</b></div>
        <div><span class="k">Started</span><b>{{ time(trace.time) }}</b></div>
        <div><span class="k">Logs</span><b>{{ trace.logs.length }}</b></div>
        <div class="svcs">
          <span class="k">Services</span>
          <span><span v-for="s in trace.services" :key="s" class="svc"><span class="dot" :style="{ background: serviceColor(s) }" />{{ s }}</span></span>
        </div>
        <button class="btn sm" @click="copyId"><Icon :name="copied ? 'check' : 'copy'" :size="13" />{{ copied ? 'Copied' : 'Copy trace ID' }}</button>
      </div>
    </div>

    <div class="layout">
      <div class="panel waterfall">
        <div class="ruler">
          <div class="name-col">Span</div>
          <div class="track">
            <span v-for="t in ticks" :key="t.f" class="tick" :style="{ left: `${t.f * 100}%` }">{{ t.label }}</span>
          </div>
        </div>
        <div v-for="r in rows" :key="r.span.span_id" class="row" :class="{ selected: r.span.span_id === selectedId }"
          @click="selectedId = r.span.span_id === selectedId ? undefined : r.span.span_id">
          <div class="name-col" :style="{ paddingLeft: `${14 + r.depth * 16}px` }">
            <span class="dot" :style="{ background: serviceColor(r.span.service) }" />
            <span class="svc-name">{{ r.span.service }}</span>
            <span class="mono span-name" :class="{ bad: r.span.status === 'Error' }">{{ r.span.name }}</span>
          </div>
          <div class="track">
            <span v-for="t in ticks" :key="t.f" class="grid" :style="{ left: `${t.f * 100}%` }" />
            <div class="span-bar" :style="bar(r.span)" />
            <div v-for="(e, i) in r.span.events" :key="i" class="event" :style="{ left: eventLeft(e.offset_ms) }" :title="e.name" />
          </div>
          <div class="dur mono">{{ duration(r.span.duration_ms) }}</div>
        </div>
      </div>

      <aside v-if="selected" class="card side">
        <div class="side-head">
          <strong class="mono">{{ selected.name }}</strong>
          <button class="btn ghost sm icon" title="Close" @click="selectedId = undefined"><Icon name="x" /></button>
        </div>
        <dl class="kv">
          <dt>Service</dt><dd><span class="dot" :style="{ background: serviceColor(selected.service) }" />{{ selected.service }}</dd>
          <dt>Status</dt><dd><span class="badge" :class="selected.status">{{ selected.status === 'Unset' ? 'ok' : selected.status }}</span> {{ selected.status_message }}</dd>
          <dt>Kind</dt><dd>{{ selected.kind }}</dd>
          <dt>Starts at</dt><dd>+{{ duration(selected.offset_ms) }}</dd>
          <dt>Duration</dt><dd>{{ duration(selected.duration_ms) }}</dd>
          <dt>Span ID</dt><dd class="mono">{{ selected.span_id }}</dd>
        </dl>
        <div v-for="(e, i) in selected.events" :key="i" class="event-box">
          <div><strong>{{ e.name }}</strong> <span class="muted small">+{{ duration(e.offset_ms) }}</span></div>
          <AttrTable title="" :attrs="e.attributes" />
        </div>
        <AttrTable title="Attributes" :attrs="selected.attributes" />
        <AttrTable title="Resource" :attrs="selected.resource" />
      </aside>
    </div>

    <div class="logs-head">
      <h2>Logs in this trace <span class="muted">({{ logs.length }})</span></h2>
      <label v-if="selectedId" class="only">
        <input v-model="onlySpanLogs" type="checkbox" /> Only the selected span
      </label>
    </div>
    <LogList v-if="logs.length" :logs="logs" />
    <div v-else class="panel">
      <EmptyState icon="logs" title="No logs in this trace"
        text="Logs show up here when the service writes them inside the request, e.g. with the Vindice Go SDK's logger." />
    </div>
  </template>
</template>

<style scoped>
.head { margin-bottom: 16px; }
.title-row { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.title-row h1 { font-size: 18px; }
.facts { display: flex; flex-wrap: wrap; gap: 28px; align-items: flex-end; }
.facts > div { display: flex; flex-direction: column; gap: 2px; }
.k { font-size: 11px; font-weight: 650; text-transform: uppercase; letter-spacing: 0.06em; color: var(--muted); }
.svc { margin-right: 14px; font-weight: 550; }
.facts .btn { margin-left: auto; }

.layout { display: flex; gap: 16px; align-items: flex-start; }
.waterfall { flex: 1; min-width: 0; }
.ruler, .row { display: grid; grid-template-columns: minmax(220px, 34%) 1fr 76px; align-items: center; }
.ruler { height: 34px; border-bottom: 1px solid var(--border); background: var(--raised); font-size: 11px; color: var(--muted); }
.ruler .name-col { padding-left: 14px; font-weight: 650; text-transform: uppercase; letter-spacing: 0.06em; }
.ruler .track { height: 100%; }
.tick { position: absolute; top: 10px; transform: translateX(-50%); white-space: nowrap; }
.tick:first-child { transform: none; }
.tick:last-child { transform: translateX(-100%); }
.row { height: 32px; cursor: pointer; border-bottom: 1px solid var(--border); }
.row:last-child { border-bottom: none; }
.row:hover { background: var(--hover); }
.row.selected { background: var(--accent-soft); }
.name-col { overflow: hidden; white-space: nowrap; text-overflow: ellipsis; font-size: 13px; padding-right: 10px; }
.svc-name { color: var(--muted); margin-right: 8px; }
.span-name { font-size: 12.5px; }
.track { position: relative; height: 100%; margin: 0 12px; }
.grid { position: absolute; top: 0; bottom: 0; width: 1px; background: var(--border); }
.span-bar { position: absolute; top: 9px; height: 14px; border-radius: 4px; opacity: 0.9; }
.event { position: absolute; top: 7px; width: 2px; height: 18px; background: var(--text); opacity: 0.55; border-radius: 1px; }
.dur { font-size: 12px; color: var(--muted); text-align: right; padding-right: 14px; white-space: nowrap; }

.side { width: 380px; flex-shrink: 0; max-height: 78vh; overflow: auto; position: sticky; top: 76px; }
.side-head { display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 10px; word-break: break-all; }
.kv { display: grid; grid-template-columns: 90px 1fr; gap: 6px 10px; font-size: 13px; margin: 0; }
.kv dt { color: var(--muted); }
.kv dd { margin: 0; }
.event-box { margin-top: 12px; padding: 10px; border: 1px solid var(--border); border-radius: 8px; }

.logs-head { display: flex; align-items: center; gap: 14px; }
.only { display: flex; gap: 6px; align-items: center; font-size: 13px; color: var(--muted); margin-top: 16px; }
</style>
