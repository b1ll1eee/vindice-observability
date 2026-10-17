<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, type SignalQuery } from '../api'
import { can } from '../auth'
import EmptyState from '../components/EmptyState.vue'
import Icon from '../components/Icon.vue'
import PageHeader from '../components/PageHeader.vue'
import SignalEditor from '../components/SignalEditor.vue'
import SignalPanel from '../components/SignalPanel.vue'
import { useLoader } from '../load'
import { describeSignal } from '../signals'

const router = useRouter()

// The editor works on a draft; Run copies it so the chart only reloads on demand.
const draft = ref<SignalQuery>({ kind: 'metric', metric: { name: '', agg: 'avg' } })
const shown = ref<SignalQuery>()

function run() {
  shown.value = JSON.parse(JSON.stringify(draft.value))
}

const dashboards = useLoader(() => api.dashboards(), [])
const target = ref('')
const saving = ref(false)
const message = ref('')

async function addToDashboard() {
  if (!shown.value || !target.value) return
  saving.value = true
  message.value = ''
  try {
    const d = await api.dashboard(target.value)
    d.panels.push({ title: describeSignal(shown.value), signal: shown.value, width: 6 })
    await api.saveDashboard(d)
    router.push(`/dashboards/${d.id}`)
  } catch (e) {
    message.value = e instanceof Error ? e.message : String(e)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <PageHeader title="Explore" subtitle="Chart any metric, service measure or log count. Pin what you find to a dashboard." />

  <form class="card editor" @submit.prevent="run">
    <SignalEditor v-model="draft" />
    <div class="actions">
      <button type="submit" class="btn primary"><Icon name="explore" />Run query</button>
    </div>
  </form>

  <template v-if="shown">
    <SignalPanel class="result" :title="describeSignal(shown)" :signal="shown">
      <template v-if="can('editor')" #actions>
        <div class="row">
          <select v-model="target" aria-label="Dashboard">
            <option value="">Add to dashboard…</option>
            <option v-for="d in dashboards.data.value ?? []" :key="d.id" :value="d.id">{{ d.title }}</option>
          </select>
          <button class="btn" :disabled="!target || saving" @click="addToDashboard"><Icon name="plus" />Add</button>
        </div>
      </template>
    </SignalPanel>
    <div v-if="message" class="error-box">{{ message }}</div>
  </template>
  <div v-else class="card">
    <EmptyState icon="explore" title="Build a query above"
      text="Pick a source: a service's RED measure, any metric with an aggregation such as rate, or a count of matching logs." />
  </div>
</template>

<style scoped>
.editor { margin-bottom: 16px; }
.actions { margin-top: 16px; display: flex; justify-content: flex-end; }
.result :deep(svg) { height: 260px; }
</style>
