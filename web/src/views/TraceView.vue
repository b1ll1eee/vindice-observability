<script setup lang="ts">
import { computed, ref, toRef } from 'vue'
import { api, type Span } from '../api'
import AttrTable from '../components/AttrTable.vue'
import LogList from '../components/LogList.vue'
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
    width: `max(2px, ${(s.duration_ms / total) * 100}%)`,
    background: serviceColor(s.service),
  }
}

function eventLeft(offset: number) {
  return `${(offset / (trace.value?.duration_ms || 1)) * 100}%`
}

const errors = computed(() => trace.value?.spans.filter((s) => s.status === 'Error').length ?? 0)
</script>

<template>
  <div v-if="error" class="error-box">{{ error }}</div>
  <div v-else-if="loading && !trace" class="panel empty">Loading…</div>
  <template v-else-if="trace">
    <h1>{{ rows[0]?.span.name ?? 'Trace' }}</h1>
    <div class="summary">
      <span class="mono muted">{{ trace.trace_id }}</span>
      <span>{{ time(trace.time) }}</span>
      <span>{{ duration(trace.duration_ms) }}</span>
      <span>{{ trace.spans.length }} spans</span>
      <span :class="{ bad: errors }">{{ errors }} errors</span>
      <span v-for="s in trace.services" :key="s"><span class="dot" :style="{ background: serviceColor(s) }" />{{ s }}</span>
    </div>

    <div class="layout">
      <div class="panel waterfall">
        <div class="ruler">
          <span>0</span><span>{{ duration(trace.duration_ms / 2) }}</span><span>{{ duration(trace.duration_ms) }}</span>
        </div>
        <div
          v-for="r in rows"
          :key="r.span.span_id"
          class="row"
          :class="{ selected: r.span.span_id === selectedId }"
          @click="selectedId = r.span.span_id === selectedId ? undefined : r.span.span_id"
        >
          <div class="name" :style="{ paddingLeft: `${8 + r.depth * 14}px` }">
            <span class="dot" :style="{ background: serviceColor(r.span.service) }" />
            <span class="svc">{{ r.span.service }}</span>
            <span class="mono" :class="{ bad: r.span.status === 'Error' }">{{ r.span.name }}</span>
          </div>
          <div class="track">
            <div class="bar" :style="bar(r.span)" :class="{ err: r.span.status === 'Error' }" />
            <div v-for="(e, i) in r.span.events" :key="i" class="event" :style="{ left: eventLeft(e.offset_ms) }" :title="e.name" />
            <span class="dur mono">{{ duration(r.span.duration_ms) }}</span>
          </div>
        </div>
      </div>

      <aside v-if="selected" class="panel side">
        <div class="side-head">
          <strong class="mono">{{ selected.name }}</strong>
          <button class="icon" title="Close" @click="selectedId = undefined">×</button>
        </div>
        <div class="kv">
          <span class="muted">Service</span><span>{{ selected.service }}</span>
          <span class="muted">Kind</span><span>{{ selected.kind }}</span>
          <span class="muted">Status</span><span><span class="badge" :class="selected.status">{{ selected.status }}</span> {{ selected.status_message }}</span>
          <span class="muted">Start</span><span>+{{ duration(selected.offset_ms) }}</span>
          <span class="muted">Duration</span><span>{{ duration(selected.duration_ms) }}</span>
          <span class="muted">Span ID</span><span class="mono">{{ selected.span_id }}</span>
        </div>
        <div v-for="(e, i) in selected.events" :key="i" class="event-box">
          <div><strong>{{ e.name }}</strong> <span class="muted">+{{ duration(e.offset_ms) }}</span></div>
          <AttrTable title="" :attrs="e.attributes" />
        </div>
        <AttrTable title="Attributes" :attrs="selected.attributes" />
        <AttrTable title="Resource" :attrs="selected.resource" />
      </aside>
    </div>

    <h2>
      Logs in this trace
      <label v-if="selectedId" class="only muted">
        <input v-model="onlySpanLogs" type="checkbox" /> only the selected span
      </label>
    </h2>
    <LogList :logs="logs" />
  </template>
</template>

<style scoped>
.summary { display: flex; flex-wrap: wrap; gap: 16px; margin: -6px 0 16px; align-items: center; }
.layout { display: flex; gap: 14px; align-items: flex-start; }
.waterfall { flex: 1; min-width: 0; }
.ruler {
  display: flex;
  justify-content: space-between;
  margin-left: 38%;
  padding: 6px 60px 6px 8px;
  font-size: 11px;
  color: var(--muted);
  border-bottom: 1px solid var(--border);
}
.row { display: flex; align-items: center; height: 28px; cursor: pointer; border-bottom: 1px solid var(--border); }
.row:last-child { border-bottom: none; }
.row:hover { background: var(--hover); }
.row.selected { background: var(--accent-soft); }
.name { width: 38%; flex-shrink: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; font-size: 13px; }
.svc { color: var(--muted); margin-right: 6px; }
.track { position: relative; flex: 1; height: 100%; margin-right: 60px; }
.bar { position: absolute; top: 8px; height: 12px; border-radius: 3px; opacity: 0.85; }
.bar.err { outline: 2px solid var(--error); }
.event { position: absolute; top: 6px; width: 2px; height: 16px; background: var(--text); opacity: 0.6; }
.dur { position: absolute; right: -58px; top: 6px; font-size: 11px; color: var(--muted); white-space: nowrap; }
.side { width: 380px; flex-shrink: 0; padding: 12px 14px; max-height: 75vh; overflow: auto; }
.side-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; word-break: break-all; }
.kv { display: grid; grid-template-columns: 90px 1fr; gap: 4px 10px; font-size: 13px; }
.event-box { margin-top: 10px; padding: 8px; border: 1px solid var(--border); border-radius: 6px; }
.only { font-size: 13px; font-weight: normal; margin-left: 12px; }
</style>
