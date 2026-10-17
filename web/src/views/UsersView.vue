<script setup lang="ts">
import { ref } from 'vue'
import { api, type Role, type User } from '../api'
import { user as me } from '../auth'
import { useLoader } from '../load'

const roles: { value: Role; label: string }[] = [
  { value: 'viewer', label: 'Viewer: read only' },
  { value: 'editor', label: 'Editor: dashboards and alerts' },
  { value: 'admin', label: 'Admin: everything, including users' },
]

const { data, error, reload } = useLoader(() => api.users(), [])

const form = ref({ username: '', role: 'viewer' as Role, password: '' })
const formError = ref('')
async function create() {
  formError.value = ''
  try {
    await api.createUser(form.value.username, form.value.role, form.value.password)
    form.value = { username: '', role: 'viewer', password: '' }
    reload()
  } catch (e) {
    formError.value = e instanceof Error ? e.message : String(e)
  }
}

const rowError = ref<Record<string, string>>({})
async function act(u: User, fn: () => Promise<unknown>) {
  rowError.value = { ...rowError.value, [u.id]: '' }
  try {
    await fn()
    reload()
  } catch (e) {
    rowError.value = { ...rowError.value, [u.id]: e instanceof Error ? e.message : String(e) }
  }
}

function setRole(u: User, role: Role) {
  act(u, () => api.updateUser(u.id, role, u.disabled))
}
function toggle(u: User) {
  act(u, () => api.updateUser(u.id, u.role, !u.disabled))
}
function resetPassword(u: User) {
  const pw = prompt(`New password for ${u.username} (at least 10 characters):`)
  if (pw) act(u, () => api.updateUser(u.id, u.role, u.disabled, pw))
}
function remove(u: User) {
  if (confirm(`Delete user ${u.username}?`)) act(u, () => api.deleteUser(u.id))
}
</script>

<template>
  <h1>Users</h1>
  <form class="toolbar" @submit.prevent="create">
    <input v-model.trim="form.username" placeholder="Username" autocomplete="off" required />
    <select v-model="form.role">
      <option v-for="r in roles" :key="r.value" :value="r.value">{{ r.label }}</option>
    </select>
    <input v-model="form.password" type="password" placeholder="Initial password (10+ characters)" autocomplete="new-password" minlength="10" required />
    <button type="submit">Add user</button>
    <span v-if="formError" class="bad">{{ formError }}</span>
  </form>
  <div v-if="error" class="error-box">{{ error }}</div>
  <div class="panel">
    <table>
      <thead>
        <tr><th>User</th><th>Role</th><th>Status</th><th /></tr>
      </thead>
      <tbody>
        <tr v-for="u in data ?? []" :key="u.id" :class="{ off: u.disabled }">
          <td>
            {{ u.username }} <span v-if="u.id === me?.id" class="muted">(you)</span>
            <div v-if="rowError[u.id]" class="bad small">{{ rowError[u.id] }}</div>
          </td>
          <td>
            <select :value="u.role" @change="setRole(u, ($event.target as HTMLSelectElement).value as Role)">
              <option v-for="r in roles" :key="r.value" :value="r.value">{{ r.value }}</option>
            </select>
          </td>
          <td><span class="badge" :class="u.disabled ? 'Unset' : 'info'">{{ u.disabled ? 'disabled' : 'active' }}</span></td>
          <td class="actions">
            <button @click="resetPassword(u)">Reset password</button>
            <button @click="toggle(u)">{{ u.disabled ? 'Enable' : 'Disable' }}</button>
            <button :disabled="u.id === me?.id" @click="remove(u)">Delete</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.actions { text-align: right; white-space: nowrap; }
.actions button { padding: 3px 8px; font-size: 12px; margin-left: 4px; }
.small { font-size: 12px; }
tr.off td { opacity: 0.6; }
</style>
