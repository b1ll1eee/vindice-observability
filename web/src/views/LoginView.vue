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
    <div class="glow" aria-hidden="true" />
    <form class="card login" @submit.prevent="submit">
      <div class="brand">
        <span class="logo">
          <svg viewBox="0 0 32 32" width="22" height="22" aria-hidden="true">
            <circle cx="16" cy="16" r="12" fill="none" stroke="currentColor" stroke-width="4" />
            <circle cx="16" cy="16" r="4.5" fill="currentColor" />
          </svg>
        </span>
        <div>
          <div class="name">Vindice</div>
          <div class="muted small">Logs, traces and metrics in one place</div>
        </div>
      </div>
      <label class="field">Username <input v-model.trim="username" autocomplete="username" autofocus required /></label>
      <label class="field">Password <input v-model="password" type="password" autocomplete="current-password" required /></label>
      <div v-if="error" class="error-box">{{ error }}</div>
      <button type="submit" class="btn primary big" :disabled="busy">{{ busy ? 'Signing in…' : 'Sign in' }}</button>
      <p class="muted small foot">No account? Ask an admin to add you.</p>
    </form>
  </div>
</template>

<style scoped>
.wrap { min-height: 100vh; display: grid; place-items: center; padding: 20px; position: relative; overflow: hidden; }
.glow {
  position: absolute; width: 720px; height: 720px; border-radius: 50%; filter: blur(90px); opacity: 0.35;
  background: radial-gradient(circle, var(--accent) 0%, transparent 65%); top: -280px; left: 50%; transform: translateX(-50%);
}
.login { position: relative; width: 380px; padding: 30px; display: flex; flex-direction: column; gap: 16px; box-shadow: var(--shadow-lg); }
.brand { display: flex; align-items: center; gap: 12px; margin-bottom: 8px; }
.logo { width: 42px; height: 42px; border-radius: 12px; display: grid; place-items: center; color: #fff; background: linear-gradient(135deg, var(--accent), #b07cff); }
.name { font-size: 20px; font-weight: 700; letter-spacing: -0.01em; }
.big { min-height: 40px; }
.foot { text-align: center; margin: 0; }
.error-box { margin: 0; }
</style>
