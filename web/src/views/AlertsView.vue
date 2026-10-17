<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, type AlertRule, type Op } from '../api'
import { can } from '../auth'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import EmptyState from '../components/EmptyState.vue'
import Icon from '../components/Icon.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import SignalEditor from '../components/SignalEditor.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import StatTile from '../components/StatTile.vue'
import { time } from '../format'
import { useLoader } from '../load'
import { describeSignal, measureFormat } from '../signals'

const rules = useLoader(() => api.alerts(), [])
const events = useLoader(() => api.alertEvents(30), [])
function reload() {
  rules.reload()
  events.reload()
}

const counts = computed(() => {
  const r = (rules.data.value ?? []).filter((x) => x.enabled)
  return {
    firing: r.filter((x) => x.status?.state === 'firing').length,
    pending: r.filter((x) => x.status?.state === 'pending').length,
    ok: r.filter((x) => !x.status || x.status.state === 'ok').length,
  }
})

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

const deleting = ref<AlertRule>()
async function remove() {
  await api.deleteAlert(deleting.value!.id!)
  deleting.value = undefined
  reload()
}

const testMsg = ref<Record<string, { text: string; ok: boolean }>>({})
async function test(r: AlertRule) {
  testMsg.value = { ...testMsg.value, [r.id!]: { text: 'Sending…', ok: true } }
  try {
    await api.testAlert(r.id!)
    testMsg.value = { ...testMsg.value, [r.id!]: { text: 'Test sent to Telegram', ok: true } }
  } catch (e) {
    testMsg.value = { ...testMsg.value, [r.id!]: { text: e instanceof Error ? e.message : String(e), ok: false } }
  }
}

function fmt(r: AlertRule, v: number) {
  return r.signal.kind === 'red' ? measureFormat(r.signal.measure)(v) : String(Math.round(v * 1000) / 1000)
}

function dur(sec: number) {
  return sec === 0 ? 'immediately' : sec % 3600 === 0 ? `${sec / 3600}h` : `${sec / 60}m`
}

function stateOf(r: AlertRule) {
  return !r.enabled ? 'off' : (r.status?.state ?? 'ok')
}
</script>

<template>
  <PageHeader title="Alerts" subtitle="Rules are checked every 30 seconds. Firing and resolved alerts go to Telegram.">
    <button v-if="can('editor')" class="btn primary" @click="edit()"><Icon name="plus" />New alert</button>
  </PageHeader>

  <div class="tiles">
    <StatTile label="Firing" :value="String(counts.firing)" :tone="counts.firing ? 'bad' : undefined" hint="need attention now" />
    <StatTile label="Pending" :value="String(counts.pending)" :tone="counts.pending ? 'warn' : undefined" hint="breaching, waiting out the hold time" />
    <StatTile label="OK" :value="String(counts.ok)" :tone="counts.ok ? 'ok' : undefined" hint="enabled and quiet" />
  </div>

  <div v-if="rules.error.value" class="error-box">{{ rules.error.value }}</div>
  <div class="panel">
    <SkeletonRows v-if="rules.loading.value && !rules.data.value" :rows="3" />
    <EmptyState v-else-if="!rules.data.value?.length" icon="alerts" title="No alerts yet"
      text="For example: alert when a service's error rate stays above 5% for 2 minutes.">
      <button v-if="can('editor')" class="btn primary" @click="edit()"><Icon name="plus" />New alert</button>
    </EmptyState>
    <table v-else>
      <thead>
        <tr><th style="width: 110px">State</th><th>Alert</th><th class="num">Current value</th><th v-if="can('editor')" /></tr>
      </thead>
      <tbody>
        <tr v-for="r in rules.data.value" :key="r.id" :class="{ off: !r.enabled }">
          <td><span class="badge" :class="[stateOf(r), { pulse: stateOf(r) === 'firing' }]">{{ stateOf(r) }}</span></td>
          <td>
            <div class="name">{{ r.name }}</div>
            <div class="muted small mono">{{ describeSignal(r.signal) }} {{ r.op }} {{ r.threshold }} · over {{ dur(r.window_sec) }} · for {{ dur(r.for_sec) }}</div>
            <div v-if="r.status?.error" class="bad small">{{ r.status.error }}</div>
            <div v-else-if="r.status?.breaching?.length" class="warnc small">{{ r.status.breaching.join(' · ') }}</div>
            <div v-if="testMsg[r.id!]" class="small" :class="testMsg[r.id!]!.ok ? 'good' : 'bad'">{{ testMsg[r.id!]!.text }}</div>
          </td>
          <td class="num value">{{ r.status ? fmt(r, r.status.value) : '–' }}</td>
          <td v-if="can('editor')" class="actions">
            <button class="btn sm" title="Send a test message" @click="test(r)"><Icon name="send" :size="13" />Test</button>
            <button class="btn sm" @click="toggle(r)">{{ r.enabled ? 'Disable' : 'Enable' }}</button>
            <button class="btn sm icon" title="Edit" @click="edit(r)"><Icon name="edit" :size="14" /></button>
            <button class="btn sm icon danger" title="Delete" @click="deleting = r"><Icon name="trash" :size="14" /></button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <h2>Recent events</h2>
  <div class="panel">
    <EmptyState v-if="!events.data.value?.length" icon="clock" title="No events yet" text="Each time an alert fires or resolves, it is listed here." />
    <ul v-else class="timeline">
      <li v-for="(e, i) in events.data.value" :key="i">
        <span class="mark" :class="e.state" />
        <span class="mono muted small when">{{ time(e.time) }}</span>
        <span class="badge" :class="e.state">{{ e.state }}</span>
        <span class="name">{{ e.rule_name }}</span>
        <span class="muted small">value {{ Math.round(e.value * 1000) / 1000 }}</span>
        <span v-if="e.message" class="bad small">{{ e.message }}</span>
      </li>
    </ul>
  </div>

  <ModalDialog v-if="draft" :title="draft.id ? 'Edit alert' : 'New alert'" :width="680" @close="draft = undefined">
    <form id="alert-form" class="stack" @submit.prevent="save">
      <div class="two">
        <label class="field">Name <input v-model.trim="draft.name" required placeholder="Checkout error rate" /></label>
        <label class="field">Telegram chat ID <input v-model.trim="draft.chat_id" placeholder="default chat" />
          <span class="hint">Leave empty to use the default chat.</span></label>
      </div>
      <div>
        <div class="section">What to watch</div>
        <SignalEditor v-model="draft.signal" />
      </div>
      <div>
        <div class="section">When to fire</div>
        <div class="cond">
          <span class="muted">Value over the last</span>
          <select v-model.number="draft.window_sec">
            <option v-for="w in windows" :key="w" :value="w">{{ dur(w) }}</option>
          </select>
          <span class="muted">is</span>
          <select v-model="draft.op" class="op">
            <option v-for="o in ops" :key="o" :value="o">{{ o }}</option>
          </select>
          <input v-model.number="draft.threshold" type="number" step="any" required class="threshold" />
          <span class="muted">for</span>
          <select v-model.number="draft.for_sec">
            <option v-for="h in holds" :key="h" :value="h">{{ dur(h) }}</option>
          </select>
        </div>
        <p v-if="draft.signal.measure === 'error_rate'" class="hint">Error rate is a fraction: 0.05 means 5%.</p>
        <p v-else-if="['p50', 'p95', 'p99'].includes(draft.signal.measure ?? '')" class="hint">Latency is in milliseconds.</p>
      </div>
      <label class="check"><input v-model="draft.enabled" type="checkbox" /> Enabled</label>
    </form>
    <div v-if="formError" class="error-box">{{ formError }}</div>
    <template #footer>
      <button class="btn" @click="draft = undefined">Cancel</button>
      <button class="btn primary" type="submit" form="alert-form"><Icon name="check" />Save alert</button>
    </template>
  </ModalDialog>

  <ConfirmDialog v-if="deleting" title="Delete alert?" :message="`“${deleting.name}” will stop being checked.`"
    confirm-label="Delete" danger @confirm="remove" @cancel="deleting = undefined" />
</template>

<style scoped>
.tiles { display: grid; grid-template-columns: repeat(3, 1fr); gap: 14px; margin-bottom: 18px; }
.name { font-weight: 600; }
.value { font-size: 15px; font-weight: 600; }
tr.off td { opacity: 0.55; }
.timeline { list-style: none; margin: 0; padding: 6px 0; }
.timeline li { display: flex; align-items: center; gap: 12px; padding: 9px 18px; border-bottom: 1px solid var(--border); }
.timeline li:last-child { border-bottom: none; }
.mark { width: 8px; height: 8px; border-radius: 50%; background: var(--muted); }
.mark.firing { background: var(--error); }
.mark.resolved { background: var(--ok); }
.when { width: 170px; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.section { font-size: 11px; font-weight: 650; text-transform: uppercase; letter-spacing: 0.06em; color: var(--muted); margin-bottom: 8px; }
.cond { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.op { width: 64px; }
.threshold { width: 110px; }
.check { display: flex; gap: 8px; align-items: center; font-weight: 550; }
.hint { margin: 8px 0 0; }
</style>
