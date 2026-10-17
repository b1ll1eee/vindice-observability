<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { can } from '../auth'
import EmptyState from '../components/EmptyState.vue'
import Icon from '../components/Icon.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import { useLoader } from '../load'

const router = useRouter()
const { data, error, loading } = useLoader(() => api.dashboards(), [])

const creating = ref(false)
const title = ref('')
const createError = ref('')

async function create() {
  createError.value = ''
  try {
    const d = await api.saveDashboard({ title: title.value, panels: [] })
    router.push({ path: `/dashboards/${d.id}`, query: { edit: '1' } })
  } catch (e) {
    createError.value = e instanceof Error ? e.message : String(e)
  }
}

function when(t?: string) {
  return t ? new Date(t).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short', hour12: false }) : ''
}
</script>

<template>
  <PageHeader title="Dashboards" subtitle="Charts your team checks every day, in one place.">
    <button v-if="can('editor')" class="btn primary" @click="creating = true"><Icon name="plus" />New dashboard</button>
  </PageHeader>

  <div v-if="error" class="error-box">{{ error }}</div>
  <div v-if="loading && !data" class="panel"><SkeletonRows :rows="3" /></div>
  <div v-else-if="!data?.length" class="panel">
    <EmptyState icon="dashboards" title="No dashboards yet" text="Create one, or pin a chart from Explore.">
      <button v-if="can('editor')" class="btn primary" @click="creating = true"><Icon name="plus" />New dashboard</button>
    </EmptyState>
  </div>
  <div v-else class="grid">
    <RouterLink v-for="d in data" :key="d.id" :to="`/dashboards/${d.id}`" class="card tile">
      <div class="icon-wrap"><Icon name="dashboards" :size="18" /></div>
      <div class="name">{{ d.title }}</div>
      <div class="muted small">{{ d.panels.length }} panel{{ d.panels.length === 1 ? '' : 's' }} · updated {{ when(d.updated_at) }}</div>
    </RouterLink>
  </div>

  <ModalDialog v-if="creating" title="New dashboard" @close="creating = false">
    <form id="create-dash" @submit.prevent="create">
      <label class="field">Title <input v-model.trim="title" placeholder="e.g. Checkout overview" required autofocus /></label>
    </form>
    <div v-if="createError" class="bad small">{{ createError }}</div>
    <template #footer>
      <button class="btn" @click="creating = false">Cancel</button>
      <button class="btn primary" type="submit" form="create-dash">Create</button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: 14px; }
.tile { display: flex; flex-direction: column; gap: 4px; color: var(--text); transition: border-color 0.15s, transform 0.15s; }
.tile:hover { text-decoration: none; border-color: var(--accent); transform: translateY(-1px); }
.icon-wrap { width: 36px; height: 36px; border-radius: 9px; display: grid; place-items: center; background: var(--accent-soft); color: var(--accent); margin-bottom: 8px; }
.name { font-weight: 650; font-size: 15px; }
</style>
