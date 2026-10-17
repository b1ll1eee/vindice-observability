<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../api'
import { user } from '../auth'

const route = useRoute()
const router = useRouter()
const username = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

// Only follow local paths after sign-in, never another site.
function nextPath(): string {
  const n = route.query.next
  return typeof n === 'string' && n.startsWith('/') && !n.startsWith('//') ? n : '/services'
}

async function submit() {
  busy.value = true
  error.value = ''
  try {
    user.value = await api.login(username.value, password.value)
    router.replace(nextPath())
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
    password.value = ''
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="wrap">
    <form class="panel card" @submit.prevent="submit">
      <div class="brand">
        <svg viewBox="0 0 32 32" width="28" height="28" aria-hidden="true">
          <circle cx="16" cy="16" r="13" fill="none" stroke="currentColor" stroke-width="4" />
          <circle cx="16" cy="16" r="5" fill="currentColor" />
        </svg>
        Vindice
      </div>
      <label>Username <input v-model.trim="username" autocomplete="username" autofocus required /></label>
      <label>Password <input v-model="password" type="password" autocomplete="current-password" required /></label>
      <div v-if="error" class="bad">{{ error }}</div>
      <button type="submit" class="primary" :disabled="busy">{{ busy ? 'Signing in…' : 'Sign in' }}</button>
    </form>
  </div>
</template>

<style scoped>
.wrap { min-height: 100vh; display: grid; place-items: center; padding: 20px; }
.card { width: 340px; padding: 26px; display: flex; flex-direction: column; gap: 14px; }
.brand { display: flex; align-items: center; gap: 10px; font-size: 20px; font-weight: 700; color: var(--accent); margin-bottom: 6px; }
label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted); }
label input { color: var(--text); }
.primary { background: var(--accent); color: white; border-color: var(--accent); padding: 8px; }
</style>
