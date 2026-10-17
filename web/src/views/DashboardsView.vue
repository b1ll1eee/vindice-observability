<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { can } from '../auth'
import { useLoader } from '../load'

const router = useRouter()
const { data, error, loading } = useLoader(() => api.dashboards(), [])
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
</script>

<template>
  <h1>Dashboards</h1>
  <form v-if="can('editor')" class="toolbar" @submit.prevent="create">
    <input v-model.trim="title" placeholder="New dashboard title" required />
    <button type="submit">Create</button>
    <span v-if="createError" class="bad">{{ createError }}</span>
  </form>
  <div v-if="error" class="error-box">{{ error }}</div>
  <div class="panel">
    <div v-if="!data?.length" class="empty">{{ loading ? 'Loading…' : 'No dashboards yet' }}</div>
    <table v-else>
      <thead>
        <tr><th>Title</th><th class="num">Panels</th><th class="num">Updated</th></tr>
      </thead>
      <tbody>
        <tr v-for="d in data" :key="d.id" class="clickable" @click="router.push(`/dashboards/${d.id}`)">
          <td>{{ d.title }}</td>
          <td class="num">{{ d.panels.length }}</td>
          <td class="num muted">{{ d.updated_at ? new Date(d.updated_at).toLocaleString(undefined, { hour12: false }) : '' }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
