<script setup lang="ts">
import { computed, ref, toRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type Dashboard, type Panel } from '../api'
import SignalEditor from '../components/SignalEditor.vue'
import SignalPanel from '../components/SignalPanel.vue'
import { useLoader } from '../load'

const props = defineProps<{ id: string }>()
const id = toRef(props, 'id')
const route = useRoute()
const router = useRouter()

const { data, error, loading } = useLoader(() => api.dashboard(id.value), [id])

// Editing works on a deep copy; Save sends it, Cancel drops it.
const editing = computed(() => route.query.edit === '1')
const draft = ref<Dashboard>()
watch([data, editing], () => {
  draft.value = editing.value && data.value ? JSON.parse(JSON.stringify(data.value)) : undefined
}, { immediate: true })

const view = computed(() => draft.value ?? data.value)

function setEditing(on: boolean) {
  router.replace({ query: { ...route.query, edit: on ? '1' : undefined } })
}

const newPanel = ref<Panel>(blankPanel())
function blankPanel(): Panel {
  return { title: '', width: 6, signal: { kind: 'red', measure: 'error_rate' } }
}
function addPanel() {
  draft.value!.panels.push(JSON.parse(JSON.stringify(newPanel.value)))
  newPanel.value = blankPanel()
}
function removePanel(i: number) {
  draft.value!.panels.splice(i, 1)
}
function move(i: number, d: number) {
  const p = draft.value!.panels
  const j = i + d
  if (j < 0 || j >= p.length) return
  ;[p[i], p[j]] = [p[j]!, p[i]!]
}

const saveError = ref('')
async function save() {
  saveError.value = ''
  try {
    data.value = await api.saveDashboard(draft.value!)
    setEditing(false)
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e)
  }
}

async function remove() {
  if (!confirm(`Delete dashboard "${data.value?.title}"?`)) return
  await api.deleteDashboard(id.value)
  router.push('/dashboards')
}
</script>

<template>
  <div v-if="error" class="error-box">{{ error }}</div>
  <div v-else-if="loading && !view" class="panel empty">Loading…</div>
  <template v-else-if="view">
    <div class="titlebar">
      <input v-if="draft" v-model="draft.title" class="title-input" />
      <h1 v-else>{{ view.title }}</h1>
      <div class="spacer" />
      <template v-if="draft">
        <span v-if="saveError" class="bad">{{ saveError }}</span>
        <button @click="setEditing(false)">Cancel</button>
        <button class="primary" @click="save">Save</button>
      </template>
      <template v-else>
        <button @click="setEditing(true)">Edit</button>
        <button @click="remove">Delete</button>
      </template>
    </div>

    <div v-if="!view.panels.length && !draft" class="panel empty">No panels yet. Click Edit to add some.</div>
    <div class="grid">
      <div v-for="(p, i) in view.panels" :key="p.id ?? i" :style="{ gridColumn: `span ${p.width}` }">
        <SignalPanel :title="p.title || 'Untitled'" :signal="p.signal">
          <template v-if="draft" #actions>
            <span class="row-actions">
              <select v-model.number="draft.panels[i]!.width" title="Width">
                <option v-for="w in [3, 4, 6, 8, 12]" :key="w" :value="w">{{ w }}/12</option>
              </select>
              <button class="icon" title="Move left" @click="move(i, -1)">←</button>
              <button class="icon" title="Move right" @click="move(i, 1)">→</button>
              <button class="icon" title="Remove" @click="removePanel(i)">×</button>
            </span>
          </template>
        </SignalPanel>
      </div>
    </div>

    <form v-if="draft" class="panel add" @submit.prevent="addPanel">
      <h2>Add panel</h2>
      <label class="field">Title <input v-model.trim="newPanel.title" placeholder="Panel title" required /></label>
      <SignalEditor v-model="newPanel.signal" />
      <div class="actions"><button type="submit">Add panel</button></div>
    </form>
  </template>
</template>

<style scoped>
.titlebar { display: flex; align-items: center; gap: 8px; margin-bottom: 14px; }
.titlebar h1 { margin: 0; }
.title-input { font-size: 18px; font-weight: 600; min-width: 320px; }
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: 14px; }
.row-actions { display: flex; gap: 4px; }
.row-actions select, .row-actions button { padding: 2px 6px; font-size: 12px; }
.add { padding: 14px; margin-top: 18px; }
.add h2 { margin-top: 0; }
.field { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted); margin-bottom: 10px; max-width: 420px; }
.actions { margin-top: 12px; }
button.primary { background: var(--accent); color: white; border-color: var(--accent); }
</style>
