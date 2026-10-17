<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import Icon from './Icon.vue'

defineProps<{ title: string; width?: number }>()
const emit = defineEmits<{ close: [] }>()

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => document.addEventListener('keydown', onKey))
onBeforeUnmount(() => document.removeEventListener('keydown', onKey))
</script>

<template>
  <Teleport to="body">
    <div class="backdrop" @mousedown.self="emit('close')">
      <div class="dialog" role="dialog" aria-modal="true" :aria-label="title" :style="{ width: `${width ?? 460}px` }">
        <div class="head">
          <strong>{{ title }}</strong>
          <button class="btn ghost sm icon" title="Close" @click="emit('close')"><Icon name="x" /></button>
        </div>
        <div class="body"><slot /></div>
        <div class="foot"><slot name="footer" /></div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.backdrop {
  position: fixed; inset: 0; z-index: 50; display: grid; place-items: center; padding: 20px;
  background: #0b0d1299; backdrop-filter: blur(2px); animation: fade 0.12s ease-out;
}
.dialog {
  max-width: 100%; max-height: 90vh; overflow: auto; background: var(--panel); border: 1px solid var(--border);
  border-radius: 14px; box-shadow: var(--shadow-lg); animation: pop 0.14s ease-out;
}
.head { display: flex; align-items: center; justify-content: space-between; padding: 14px 16px 6px 20px; font-size: 15px; }
.body { padding: 8px 20px 4px; display: flex; flex-direction: column; gap: 14px; }
.foot { display: flex; justify-content: flex-end; gap: 8px; padding: 16px 20px 18px; }
.foot:empty { display: none; }
@keyframes fade { from { opacity: 0; } }
@keyframes pop { from { transform: translateY(6px) scale(0.98); opacity: 0; } }
</style>
