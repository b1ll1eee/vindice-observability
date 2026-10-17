<script setup lang="ts">
import { ref } from 'vue'
import { api, type AlertRule, type Op } from '../api'
import SignalEditor from '../components/SignalEditor.vue'
import { time } from '../format'
import { useLoader } from '../load'
import { describeSignal, measureFormat } from '../signals'

const rules = useLoader(() => api.alerts(), [])
const events = useLoader(() => api.alertEvents(30), [])
function reload() {
  rules.reload()
  events.reload()
}

const ops: Op[] = ['>', '>=', '<', '<=']
const windows = [60, 300, 600, 900, 1800, 3600]
const holds = [0, 60, 120, 300, 600, 1800]

function blank(): AlertRule {
  return {
    name: '', enabled: true, signal: { kind: 'red', measure: 'error_rate' },
    window_sec: 300, op: '>', threshold: 0.05, for_sec: 120,
  }
}

const draft = ref<AlertRule>()
const formError = ref('')

function edit(r?: AlertRule) {
  formError.value = ''
  draft.value = r ? JSON.parse(JSON.stringify({ ...r, status: undefined })) : blank()
}

async function save() {
  formError.value = ''
  try {
    await api.saveAlert(draft.value!)
    draft.value = undefined
    reload()
  } catch (e) {
    formError.value = e instanceof Error ? e.message : String(e)
  }
}

async function toggle(r: AlertRule) {
  await api.saveAlert({ ...r, enabled: !r.enabled, status: undefined })
  reload()
}

async function remove(r: AlertRule) {
  if (!confirm(`Delete alert "${r.name}"?`)) return
  await api.deleteAlert(r.id!)
  reload()
}

const testMsg = ref<Record<string, string>>({})
async function test(r: AlertRule) {
  testMsg.value = { ...testMsg.value, [r.id!]: 'Sending…' }
  try {
    await api.testAlert(r.id!)
    testMsg.value = { ...testMsg.value, [r.id!]: 'Sent ✓' }
  } catch (e) {
    testMsg.value = { ...testMsg.value, [r.id!]: e instanceof Error ? e.message : String(e) }
  }
}

function fmt(r: AlertRule, v: number) {
  return r.signal.kind === 'red' ? measureFormat(r.signal.measure)(v) : String(Math.round(v * 1000) / 1000)
}

function dur(sec: number) {
  return sec === 0 ? 'immediately' : sec % 3600 === 0 ? `${sec / 3600}h` : `${sec / 60}m`
}
</script>

<template>
  <div class="titlebar">
    <h1>Alerts</h1>
    <div class="spacer" />
    <button v-if="!draft" @click="edit()">New alert</button>
  </div>
  <p class="muted">Notifications go to Telegram. Rules are evaluated every 30 seconds.</p>

  <form v-if="draft" class="panel form" @submit.prevent="save">
    <h2>{{ draft.id ? 'Edit alert' : 'New alert' }}</h2>
    <div class="row">
      <label class="grow">Name <input v-model.trim="draft.name" required placeholder="Orders error rate" /></label>
      <label>Telegram chat ID <input v-model.trim="draft.chat_id" placeholder="default chat" /></label>
    </div>
    <SignalEditor v-model="draft.signal" />
    <div class="row cond">
      <span class="muted">Fire when the value over the last</span>
      <select v-model.number="draft.window_sec">
        <option v-for="w in windows" :key="w" :value="w">{{ dur(w) }}</option>
      </select>
      <span class="muted">is</span>
      <select v-model="draft.op">
        <option v-for="o in ops" :key="o" :value="o">{{ o }}</option>
      </select>
      <input v-model.number="draft.threshold" type="number" step="any" required style="width: 110px" />
      <span class="muted">for</span>
      <select v-model.number="draft.for_sec">
        <option v-for="h in holds" :key="h" :value="h">{{ dur(h) }}</option>
      </select>
    </div>
    <p v-if="draft.signal.measure === 'error_rate'" class="muted hint">Error rate is a fraction: 0.05 = 5%. Latency is in milliseconds.</p>
    <div class="row">
      <label class="check"><input v-model="draft.enabled" type="checkbox" /> Enabled</label>
      <div class="spacer" />
      <span v-if="formError" class="bad">{{ formError }}</span>
      <button type="button" @click="draft = undefined">Cancel</button>
      <button type="submit" class="primary">Save</button>
    </div>
  </form>

  <div v-if="rules.error.value" class="error-box">{{ rules.error.value }}</div>
  <div class="panel">
    <div v-if="!rules.data.value?.length" class="empty">{{ rules.loading.value ? 'Loading…' : 'No alerts yet' }}</div>
    <table v-else>
      <thead>
        <tr><th>State</th><th>Alert</th><th>Condition</th><th class="num">Value</th><th /></tr>
      </thead>
      <tbody>
        <tr v-for="r in rules.data.value" :key="r.id" :class="{ off: !r.enabled }">
          <td>
            <span v-if="!r.enabled" class="badge Unset">off</span>
            <span v-else class="badge" :class="r.status?.state ?? 'ok'">{{ r.status?.state ?? 'ok' }}</span>
          </td>
          <td>
            <div>{{ r.name }}</div>
            <div class="muted small" :class="{ bad: r.status?.error }">{{ r.status?.error || (r.status?.breaching ?? []).join(' · ') }}</div>
          </td>
          <td class="mono small">{{ describeSignal(r.signal) }} {{ r.op }} {{ r.threshold }} over {{ dur(r.window_sec) }}, for {{ dur(r.for_sec) }}</td>
          <td class="num">{{ r.status ? fmt(r, r.status.value) : '–' }}</td>
          <td class="actions nowrap">
            <span v-if="testMsg[r.id!]" class="muted small">{{ testMsg[r.id!] }}</span>
            <button @click="test(r)">Test</button>
            <button @click="toggle(r)">{{ r.enabled ? 'Disable' : 'Enable' }}</button>
            <button @click="edit(r)">Edit</button>
            <button @click="remove(r)">Delete</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <h2>Recent events</h2>
  <div class="panel">
    <div v-if="!events.data.value?.length" class="empty">No events yet</div>
    <table v-else>
      <tbody>
        <tr v-for="(e, i) in events.data.value" :key="i">
          <td class="mono nowrap muted">{{ time(e.time) }}</td>
          <td><span class="badge" :class="e.state === 'firing' ? 'firing' : 'ok'">{{ e.state }}</span></td>
          <td>{{ e.rule_name }}</td>
          <td class="num">{{ Math.round(e.value * 1000) / 1000 }}</td>
          <td class="muted small" :class="{ bad: e.message }">{{ e.message }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.titlebar { display: flex; align-items: center; gap: 8px; }
.titlebar h1 { margin: 0; }
.form { padding: 14px; margin-bottom: 16px; display: flex; flex-direction: column; gap: 12px; }
.form h2 { margin: 0; }
.row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.row label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted); }
.row label input { color: var(--text); }
.grow { flex: 1; }
.check { flex-direction: row !important; align-items: center; color: var(--text) !important; font-size: 14px !important; }
.hint { margin: -4px 0 0; font-size: 12px; }
.small { font-size: 12px; }
.actions { text-align: right; }
.actions button { padding: 3px 8px; font-size: 12px; margin-left: 4px; }
tr.off td { opacity: 0.55; }
button.primary { background: var(--accent); color: white; border-color: var(--accent); }
:deep(.badge.ok) { background: color-mix(in srgb, #10b981 15%, transparent); color: #10b981; }
:deep(.badge.pending) { background: color-mix(in srgb, var(--warn) 18%, transparent); color: var(--warn); }
:deep(.badge.firing) { background: color-mix(in srgb, var(--error) 15%, transparent); color: var(--error); }
</style>
