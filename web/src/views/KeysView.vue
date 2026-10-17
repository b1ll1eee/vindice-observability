<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, type APIKey } from '../api'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import EmptyState from '../components/EmptyState.vue'
import Icon from '../components/Icon.vue'
import ModalDialog from '../components/ModalDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import SkeletonRows from '../components/SkeletonRows.vue'
import { useLoader } from '../load'

const { data, error, loading, reload } = useLoader(() => api.keys(), [])

const creating = ref(false)
const name = ref('')
const formError = ref('')
// The secret of a key just created; the server never shows it again.
const created = ref<{ name: string; secret: string }>()
const copied = ref('')

async function create() {
  formError.value = ''
  try {
    const k = await api.createKey(name.value)
    created.value = { name: k.name, secret: k.secret }
    creating.value = false
    name.value = ''
    reload()
  } catch (e) {
    formError.value = e instanceof Error ? e.message : String(e)
  }
}

const revoking = ref<APIKey>()
async function revoke() {
  await api.revokeKey(revoking.value!.id)
  revoking.value = undefined
  reload()
}

const envSnippet = computed(
  () => `OTEL_EXPORTER_OTLP_ENDPOINT=http://${location.hostname}:4318
OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer ${created.value?.secret ?? ''}`,
)

async function copy(text: string, what: string) {
  await navigator.clipboard.writeText(text)
  copied.value = what
  setTimeout(() => (copied.value = ''), 1500)
}

function when(t?: string) {
  return t ? new Date(t).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short', hour12: false }) : 'never'
}
</script>

<template>
  <PageHeader title="API keys" subtitle="Services and collectors need a key to send telemetry to ports 4317 (gRPC) and 4318 (HTTP).">
    <button class="btn primary" @click="creating = true"><Icon name="plus" />Create key</button>
  </PageHeader>

  <div v-if="error" class="error-box">{{ error }}</div>
  <div class="panel">
    <SkeletonRows v-if="loading && !data" :rows="3" />
    <EmptyState v-else-if="!data?.length" icon="key" title="No API keys yet" text="Without a key, Vindice rejects all incoming telemetry. Create one per service or collector.">
      <button class="btn primary" @click="creating = true"><Icon name="plus" />Create key</button>
    </EmptyState>
    <table v-else>
      <thead>
        <tr><th>Name</th><th>Key</th><th>Created</th><th>Last used</th><th>Status</th><th /></tr>
      </thead>
      <tbody>
        <tr v-for="k in data" :key="k.id" :class="{ off: k.revoked }">
          <td><b>{{ k.name }}</b></td>
          <td class="mono muted">{{ k.hint }}…</td>
          <td class="small">{{ when(k.created_at) }}<div class="muted">by {{ k.created_by }}</div></td>
          <td class="small" :class="{ muted: !k.last_used }">{{ when(k.last_used) }}</td>
          <td><span class="badge" :class="k.revoked ? 'off' : 'active'">{{ k.revoked ? 'revoked' : 'active' }}</span></td>
          <td class="actions"><button v-if="!k.revoked" class="btn sm danger" @click="revoking = k">Revoke</button></td>
        </tr>
      </tbody>
    </table>
  </div>

  <ModalDialog v-if="creating" title="Create API key" @close="creating = false">
    <form id="create-key" @submit.prevent="create">
      <label class="field">Name
        <input v-model.trim="name" placeholder="e.g. orders-service or k8s-collector" maxlength="100" required autofocus />
        <span class="hint">One key per service makes it easy to revoke just one.</span>
      </label>
    </form>
    <div v-if="formError" class="error-box">{{ formError }}</div>
    <template #footer>
      <button class="btn" @click="creating = false">Cancel</button>
      <button class="btn primary" type="submit" form="create-key">Create key</button>
    </template>
  </ModalDialog>

  <ModalDialog v-if="created" :title="`Key “${created.name}” created`" :width="600" @close="created = undefined">
    <div class="warn-note"><Icon name="alerts" :size="15" />Copy it now. For security, it will not be shown again.</div>
    <div class="secret">
      <code class="mono">{{ created.secret }}</code>
      <button class="btn sm" @click="copy(created.secret, 'key')"><Icon :name="copied === 'key' ? 'check' : 'copy'" :size="13" />{{ copied === 'key' ? 'Copied' : 'Copy' }}</button>
    </div>
    <div>
      <div class="section">OpenTelemetry SDK environment</div>
      <div class="secret">
        <pre class="mono">{{ envSnippet }}</pre>
        <button class="btn sm" @click="copy(envSnippet, 'env')"><Icon :name="copied === 'env' ? 'check' : 'copy'" :size="13" /></button>
      </div>
    </div>
    <template #footer>
      <button class="btn primary" @click="created = undefined">Done</button>
    </template>
  </ModalDialog>

  <ConfirmDialog v-if="revoking" title="Revoke key?" :message="`Senders using “${revoking.name}” are rejected within 30 seconds. This cannot be undone.`"
    confirm-label="Revoke" danger @confirm="revoke" @cancel="revoking = undefined" />
</template>

<style scoped>
tr.off td { opacity: 0.55; }
.warn-note { display: flex; gap: 8px; align-items: center; padding: 10px 12px; border-radius: 8px; color: var(--warn); background: color-mix(in srgb, var(--warn) 12%, transparent); font-weight: 550; font-size: 13px; }
.secret { display: flex; gap: 8px; align-items: flex-start; }
.secret code, .secret pre { flex: 1; margin: 0; background: var(--raised); border: 1px solid var(--border); padding: 9px 12px; border-radius: 8px; word-break: break-all; white-space: pre-wrap; font-size: 12.5px; }
.section { font-size: 11px; font-weight: 650; text-transform: uppercase; letter-spacing: 0.06em; color: var(--muted); margin-bottom: 6px; }
</style>
