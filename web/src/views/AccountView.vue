<script setup lang="ts">
import { ref } from 'vue'
import { api } from '../api'
import { user } from '../auth'
import Icon from '../components/Icon.vue'
import PageHeader from '../components/PageHeader.vue'
import { theme, type ThemeChoice } from '../theme'

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
    message.value = 'Password changed. Your other devices have been signed out.'
    current.value = next.value = again.value = ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
}

const themes: { value: ThemeChoice; icon: string; label: string }[] = [
  { value: 'system', icon: 'monitor', label: 'System' },
  { value: 'light', icon: 'sun', label: 'Light' },
  { value: 'dark', icon: 'moon', label: 'Dark' },
]
</script>

<template>
  <PageHeader title="Account" />
  <div class="cols">
    <div class="card">
      <div class="profile">
        <span class="avatar">{{ (user?.username ?? '?').slice(0, 2).toUpperCase() }}</span>
        <div>
          <div class="name">{{ user?.username }}</div>
          <span class="badge info">{{ user?.role }}</span>
        </div>
      </div>
      <div class="section">Appearance</div>
      <div class="seg">
        <button v-for="t in themes" :key="t.value" :class="{ on: theme === t.value }" @click="theme = t.value">
          <span class="row"><Icon :name="t.icon" :size="14" />{{ t.label }}</span>
        </button>
      </div>
    </div>

    <form class="card stack" @submit.prevent="submit">
      <div>
        <div class="card-title">Change password</div>
        <div class="card-sub">Changing it signs you out on every other device.</div>
      </div>
      <label class="field">Current password <input v-model="current" type="password" autocomplete="current-password" required /></label>
      <label class="field">New password <input v-model="next" type="password" autocomplete="new-password" minlength="10" required />
        <span class="hint">At least 10 characters.</span></label>
      <label class="field">Repeat new password <input v-model="again" type="password" autocomplete="new-password" required /></label>
      <div v-if="error" class="error-box">{{ error }}</div>
      <div v-if="message" class="good small"><Icon name="check" :size="14" class="inline" /> {{ message }}</div>
      <div><button type="submit" class="btn primary">Change password</button></div>
    </form>
  </div>
</template>

<style scoped>
.cols { display: grid; grid-template-columns: minmax(260px, 340px) minmax(300px, 460px); gap: 16px; align-items: start; }
.profile { display: flex; align-items: center; gap: 14px; margin-bottom: 20px; }
.avatar { width: 52px; height: 52px; border-radius: 50%; display: grid; place-items: center; font-size: 17px; font-weight: 700; background: var(--accent-soft); color: var(--accent); }
.name { font-size: 17px; font-weight: 650; margin-bottom: 4px; }
.section { font-size: 11px; font-weight: 650; text-transform: uppercase; letter-spacing: 0.06em; color: var(--muted); margin-bottom: 8px; }
.inline { display: inline; vertical-align: -2px; }
</style>
