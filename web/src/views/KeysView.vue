<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, type APIKey } from '../api'
import { useLoader } from '../load'

const { data, error, reload } = useLoader(() => api.keys(), [])
const name = ref('')
const formError = ref('')
// The secret of a key just created; the server never shows it again.
const created = ref<{ name: string; secret: string }>()
const copied = ref(false)

async function create() {
  formError.value = ''
  try {
    const k = await api.createKey(name.value)
    created.value = { name: k.name, secret: k.secret }
    copied.value = false
    name.value = ''
    reload()
  } catch (e) {
    formError.value = e instanceof Error ? e.message : String(e)
  }
}

async function revoke(k: APIKey) {
  if (!confirm(`Revoke "${k.name}"? Senders using it are rejected within 30 seconds.`)) return
  await api.revokeKey(k.id)
  reload()
}

const ingestHost = computed(() => location.hostname)
const envSnippet = computed(
  () => `OTEL_EXPORTER_OTLP_ENDPOINT=http://${ingestHost.value}:4318
OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer ${created.value?.secret ?? ''}`,
)

async function copy(text: string) {
  await navigator.clipboard.writeText(text)
  copied.value = true
}

function when(t?: string) {
  return t ? new Date(t).toLocaleString(undefined, { hour12: false }) : '–'
}
</script>

<template>
  <h1>API keys</h1>
  <p class="muted">Applications and collectors need an API key to send telemetry to the OTLP ports (4317 gRPC, 4318 HTTP).</p>

  <div v-if="created" class="panel secret">
    <strong>Key "{{ created.name }}" created.</strong> Copy it now; it will not be shown again.
    <div class="row">
      <code class="mono">{{ created.secret }}</code>
      <button @click="copy(created.secret)">{{ copied ? 'Copied ✓' : 'Copy' }}</button>
    </div>
    <div class="muted small">OpenTelemetry SDK environment:</div>
    <pre class="mono">{{ envSnippet }}</pre>
    <button @click="created = undefined">Done</button>
  </div>

  <form class="toolbar" @submit.prevent="create">
    <input v-model.trim="name" placeholder="Key name, e.g. orders-service or k8s-collector" maxlength="100" required />
    <button type="submit">Create key</button>
    <span v-if="formError" class="bad">{{ formError }}</span>
  </form>

  <div v-if="error" class="error-box">{{ error }}</div>
  <div class="panel">
    <div v-if="!data?.length" class="empty">No API keys yet. Without one, ingest rejects all data.</div>
    <table v-else>
      <thead>
        <tr><th>Name</th><th>Key</th><th>Created</th><th>Last used</th><th>Status</th><th /></tr>
      </thead>
      <tbody>
        <tr v-for="k in data" :key="k.id" :class="{ off: k.revoked }">
          <td>{{ k.name }}</td>
          <td class="mono muted">{{ k.hint }}…</td>
          <td class="muted small">{{ when(k.created_at) }} by {{ k.created_by }}</td>
          <td class="muted small">{{ when(k.last_used) }}</td>
          <td>
            <span class="badge" :class="k.revoked ? 'Unset' : 'info'">{{ k.revoked ? 'revoked' : 'active' }}</span>
          </td>
          <td class="actions"><button v-if="!k.revoked" @click="revoke(k)">Revoke</button></td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.secret { padding: 14px; margin-bottom: 16px; display: flex; flex-direction: column; gap: 10px; border-color: var(--warn); }
.secret .row { display: flex; gap: 8px; align-items: center; }
.secret code { background: var(--bg); padding: 6px 10px; border-radius: 6px; word-break: break-all; flex: 1; }
.secret pre { background: var(--bg); padding: 10px; border-radius: 6px; margin: 0; white-space: pre-wrap; word-break: break-all; }
.secret > button { align-self: flex-start; }
.small { font-size: 12px; }
.actions { text-align: right; }
.actions button { padding: 3px 8px; font-size: 12px; }
tr.off td { opacity: 0.55; }
</style>
