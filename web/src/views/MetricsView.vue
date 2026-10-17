<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, type SignalQuery } from '../api'
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
  <h1>Explore</h1>
  <form class="panel editor" @submit.prevent="run">
    <SignalEditor v-model="draft" />
    <div class="actions"><button type="submit">Run</button></div>
  </form>

  <template v-if="shown">
    <SignalPanel :title="describeSignal(shown)" :signal="shown" />
    <div class="toolbar add">
      <select v-model="target">
        <option value="">Add to dashboard…</option>
        <option v-for="d in dashboards.data.value ?? []" :key="d.id" :value="d.id">{{ d.title }}</option>
      </select>
      <button :disabled="!target || saving" @click="addToDashboard">Add</button>
      <span v-if="message" class="bad">{{ message }}</span>
    </div>
  </template>
</template>

<style scoped>
.editor { padding: 14px; margin-bottom: 14px; }
.actions { margin-top: 12px; }
.add { margin-top: 12px; }
</style>
