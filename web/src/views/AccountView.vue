<script setup lang="ts">
import { ref } from 'vue'
import { api } from '../api'
import { user } from '../auth'

const current = ref('')
const next = ref('')
const again = ref('')
const message = ref('')
const error = ref('')

async function submit() {
  message.value = ''
  error.value = ''
  if (next.value !== again.value) {
    error.value = 'The new passwords do not match'
    return
  }
  try {
    await api.changePassword(current.value, next.value)
    message.value = 'Password changed. Other devices have been signed out.'
    current.value = next.value = again.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}
</script>

<template>
  <h1>Account</h1>
  <p class="muted">Signed in as <b>{{ user?.username }}</b> ({{ user?.role }})</p>
  <form class="panel form" @submit.prevent="submit">
    <h2>Change password</h2>
    <label>Current password <input v-model="current" type="password" autocomplete="current-password" required /></label>
    <label>New password <input v-model="next" type="password" autocomplete="new-password" minlength="10" required /></label>
    <label>Repeat new password <input v-model="again" type="password" autocomplete="new-password" required /></label>
    <div v-if="error" class="bad">{{ error }}</div>
    <div v-if="message" class="ok">{{ message }}</div>
    <div><button type="submit">Change password</button></div>
  </form>
</template>

<style scoped>
.form { padding: 16px; max-width: 420px; display: flex; flex-direction: column; gap: 12px; }
.form h2 { margin: 0; }
label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted); }
label input { color: var(--text); }
.ok { color: #10b981; }
</style>
