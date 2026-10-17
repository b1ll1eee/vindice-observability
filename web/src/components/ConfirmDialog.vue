<script setup lang="ts">
import ModalDialog from './ModalDialog.vue'

defineProps<{ title: string; message: string; confirmLabel?: string; danger?: boolean; busy?: boolean }>()
const emit = defineEmits<{ confirm: []; cancel: [] }>()
</script>

<template>
  <ModalDialog :title="title" :width="420" @close="emit('cancel')">
    <p class="msg">{{ message }}</p>
    <template #footer>
      <button class="btn" @click="emit('cancel')">Cancel</button>
      <button class="btn" :class="danger ? 'danger' : 'primary'" :disabled="busy" @click="emit('confirm')">
        {{ confirmLabel ?? 'Confirm' }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.msg { margin: 0; color: var(--text-2); }
</style>
