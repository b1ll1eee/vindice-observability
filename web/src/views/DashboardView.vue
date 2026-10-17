<script setup lang="ts">
import { computed, ref, toRef, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, type Dashboard, type Panel } from '../api'
import { can } from '../auth'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import EmptyState from '../components/EmptyState.vue'
import Icon from '../components/Icon.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import SignalEditor from '../components/SignalEditor.vue'
import SignalPanel from '../components/SignalPanel.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import { useLoader } from '../load'

const props = defineProps<{ id: string }>()
const id = toRef(props, 'id')
const route = useRoute()
const router = useRouter()

const { data, error, loading } = useLoader(() => api.dashboard(id.value), [id])

// Editing works on a deep copy; Save sends it, Cancel drops it.
const editing = computed(() => route.query.edit === '1' && can('editor'))
const draft = ref<Dashboard>()
watch([data, editing], () => {
  draft.value = editing.value && data.value ? JSON.parse(JSON.stringify(data.value)) : undefined
}, { immediate: true })

const view = computed(() => draft.value ?? data.value)

function setEditing(on: boolean) {
  router.replace({ query: { ...route.query, edit: on ? '1' : undefined } })
}

const adding = ref(false)
const newPanel = ref<Panel>(blankPanel())
function blankPanel(): Panel {
  return { title: '', width: 6, signal: { kind: 'red', measure: 'error_rate' } }
}
function addPanel() {
  draft.value!.panels.push(JSON.parse(JSON.stringify(newPanel.value)))
  newPanel.value = blankPanel()
  adding.value = false
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

const confirmDelete = ref(false)
async function remove() {
  await api.deleteDashboard(id.value)
  router.push('/dashboards')
}
</script>

<template>
  <div v-if="error" class="error-box">{{ error }}</div>
  <div v-else-if="loading && !view" class="panel"><SkeletonRows :rows="4" /></div>
  <template v-else-if="view">
    <PageHeader :title="draft ? 'Edit dashboard' : view.title" :subtitle="draft ? undefined : `${view.panels.length} panels`">
      <template v-if="draft">
        <span v-if="saveError" class="bad small">{{ saveError }}</span>
        <button class="btn" @click="adding = true"><Icon name="plus" />Add panel</button>
        <button class="btn" @click="setEditing(false)">Cancel</button>
        <button class="btn primary" @click="save"><Icon name="check" />Save</button>
      </template>
      <template v-else-if="can('editor')">
        <button class="btn" @click="setEditing(true)"><Icon name="edit" />Edit</button>
        <button class="btn danger" @click="confirmDelete = true"><Icon name="trash" />Delete</button>
      </template>
    </PageHeader>

    <label v-if="draft" class="field title-field">Title <input v-model="draft.title" /></label>

    <div v-if="!view.panels.length" class="panel">
      <EmptyState icon="dashboards" title="This dashboard is empty" text="Add a panel for a service measure, a metric or a log count.">
        <button v-if="draft" class="btn primary" @click="adding = true"><Icon name="plus" />Add panel</button>
        <button v-else-if="can('editor')" class="btn primary" @click="setEditing(true)"><Icon name="edit" />Edit dashboard</button>
      </EmptyState>
    </div>
    <div class="grid">
      <div v-for="(p, i) in view.panels" :key="p.id ?? i" :style="{ gridColumn: `span ${p.width}` }">
        <SignalPanel :title="p.title || 'Untitled'" :signal="p.signal">
          <template v-if="draft" #actions>
            <span class="panel-actions">
              <select v-model.number="draft.panels[i]!.width" title="Width" class="sm">
                <option v-for="w in [3, 4, 6, 8, 12]" :key="w" :value="w">{{ Math.round((w / 12) * 100) }}%</option>
              </select>
              <button class="btn sm icon" title="Move left" @click="move(i, -1)"><Icon name="chevron" :size="14" class="flip" /></button>
              <button class="btn sm icon" title="Move right" @click="move(i, 1)"><Icon name="chevron" :size="14" /></button>
              <button class="btn sm icon danger" title="Remove" @click="removePanel(i)"><Icon name="trash" :size="14" /></button>
            </span>
          </template>
        </SignalPanel>
      </div>
    </div>
  </template>

  <ModalDialog v-if="adding" title="Add panel" :width="640" @close="adding = false">
    <form id="add-panel" class="stack" @submit.prevent="addPanel">
      <label class="field">Panel title <input v-model.trim="newPanel.title" placeholder="e.g. Checkout error rate" required /></label>
      <SignalEditor v-model="newPanel.signal" />
    </form>
    <template #footer>
      <button class="btn" @click="adding = false">Cancel</button>
      <button class="btn primary" type="submit" form="add-panel"><Icon name="plus" />Add panel</button>
    </template>
  </ModalDialog>

  <ConfirmDialog v-if="confirmDelete" title="Delete dashboard?" :message="`“${data?.title}” and its panels will be removed for everyone.`"
    confirm-label="Delete" danger @confirm="remove" @cancel="confirmDelete = false" />
</template>

<style scoped>
.title-field { max-width: 420px; margin-bottom: 16px; }
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: 14px; }
.panel-actions { display: flex; gap: 4px; flex-shrink: 0; }
select.sm { min-height: 28px; padding: 2px 6px; font-size: 12px; }
.flip { transform: rotate(180deg); }
</style>
