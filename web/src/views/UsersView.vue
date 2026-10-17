<script setup lang="ts">
import { ref } from 'vue'
import { api, type Role, type User } from '../api'
import { user as me } from '../auth'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import Icon from '../components/Icon.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import { useLoader } from '../load'

const roles: { value: Role; label: string; text: string }[] = [
  { value: 'viewer', label: 'Viewer', text: 'Reads everything' },
  { value: 'editor', label: 'Editor', text: 'Also changes dashboards and alerts' },
  { value: 'admin', label: 'Admin', text: 'Also manages users and API keys' },
]

const { data, error, loading, reload } = useLoader(() => api.users(), [])

const adding = ref(false)
const form = ref({ username: '', role: 'viewer' as Role, password: '' })
const formError = ref('')
async function create() {
  formError.value = ''
  try {
    await api.createUser(form.value.username, form.value.role, form.value.password)
    form.value = { username: '', role: 'viewer', password: '' }
    adding.value = false
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

const resetting = ref<User>()
const newPassword = ref('')
async function resetPassword() {
  const u = resetting.value!
  await act(u, () => api.updateUser(u.id, u.role, u.disabled, newPassword.value))
  resetting.value = undefined
  newPassword.value = ''
}

const deleting = ref<User>()
async function remove() {
  const u = deleting.value!
  deleting.value = undefined
  await act(u, () => api.deleteUser(u.id))
}
</script>

<template>
  <PageHeader title="Users" subtitle="Who can sign in, and what they can change.">
    <button class="btn primary" @click="adding = true"><Icon name="plus" />Add user</button>
  </PageHeader>

  <div class="roles">
    <div v-for="r in roles" :key="r.value" class="card role"><b>{{ r.label }}</b><span class="muted small">{{ r.text }}</span></div>
  </div>

  <div v-if="error" class="error-box">{{ error }}</div>
  <div class="panel">
    <SkeletonRows v-if="loading && !data" :rows="3" />
    <table v-else>
      <thead>
        <tr><th>User</th><th style="width: 160px">Role</th><th style="width: 120px">Status</th><th /></tr>
      </thead>
      <tbody>
        <tr v-for="u in data ?? []" :key="u.id" :class="{ off: u.disabled }">
          <td>
            <div class="who">
              <span class="avatar">{{ u.username.slice(0, 2).toUpperCase() }}</span>
              <div>
                <b>{{ u.username }}</b> <span v-if="u.id === me?.id" class="muted small">(you)</span>
                <div v-if="rowError[u.id]" class="bad small">{{ rowError[u.id] }}</div>
              </div>
            </div>
          </td>
          <td>
            <select :value="u.role" aria-label="Role" @change="setRole(u, ($event.target as HTMLSelectElement).value as Role)">
              <option v-for="r in roles" :key="r.value" :value="r.value">{{ r.label }}</option>
            </select>
          </td>
          <td><span class="badge" :class="u.disabled ? 'off' : 'active'">{{ u.disabled ? 'disabled' : 'active' }}</span></td>
          <td class="actions">
            <button class="btn sm" @click="resetting = u"><Icon name="key" :size="13" />Reset password</button>
            <button class="btn sm" @click="toggle(u)">{{ u.disabled ? 'Enable' : 'Disable' }}</button>
            <button class="btn sm icon danger" title="Delete" :disabled="u.id === me?.id" @click="deleting = u"><Icon name="trash" :size="14" /></button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="adding" title="Add user" @close="adding = false">
    <form id="add-user" class="stack" @submit.prevent="create">
      <label class="field">Username <input v-model.trim="form.username" autocomplete="off" required autofocus /></label>
      <label class="field">Role
        <select v-model="form.role">
          <option v-for="r in roles" :key="r.value" :value="r.value">{{ r.label }}: {{ r.text }}</option>
        </select>
      </label>
      <label class="field">Initial password
        <input v-model="form.password" type="password" autocomplete="new-password" minlength="10" required />
        <span class="hint">At least 10 characters. Ask the user to change it after signing in.</span>
      </label>
    </form>
    <div v-if="formError" class="error-box">{{ formError }}</div>
    <template #footer>
      <button class="btn" @click="adding = false">Cancel</button>
      <button class="btn primary" type="submit" form="add-user">Add user</button>
    </template>
  </ModalDialog>

  <ModalDialog v-if="resetting" :title="`Reset password for ${resetting.username}`" @close="resetting = undefined">
    <form id="reset-pw" @submit.prevent="resetPassword">
      <label class="field">New password
        <input v-model="newPassword" type="password" autocomplete="new-password" minlength="10" required autofocus />
        <span class="hint">They will be signed out everywhere.</span>
      </label>
    </form>
    <template #footer>
      <button class="btn" @click="resetting = undefined">Cancel</button>
      <button class="btn primary" type="submit" form="reset-pw">Reset password</button>
    </template>
  </ModalDialog>

  <ConfirmDialog v-if="deleting" title="Delete user?" :message="`${deleting.username} will no longer be able to sign in.`"
    confirm-label="Delete" danger @confirm="remove" @cancel="deleting = undefined" />
</template>

<style scoped>
.roles { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; margin-bottom: 16px; }
.role { display: flex; flex-direction: column; gap: 2px; padding: 12px 16px; }
.who { display: flex; align-items: center; gap: 12px; }
.avatar { width: 32px; height: 32px; border-radius: 50%; display: grid; place-items: center; font-size: 12px; font-weight: 700; background: var(--accent-soft); color: var(--accent); }
td select { min-height: 32px; }
tr.off td { opacity: 0.6; }
</style>
