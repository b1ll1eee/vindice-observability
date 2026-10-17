<script setup lang="ts">
import Icon from './Icon.vue'

withDefaults(defineProps<{ icon?: string; title: string; text?: string }>(), { icon: 'inbox' })
</script>

<template>
  <div class="empty-state">
    <div class="icon-wrap"><Icon :name="icon" :size="22" /></div>
    <div class="title">{{ title }}</div>
    <p v-if="text" class="muted">{{ text }}</p>
    <div class="actions"><slot /></div>
  </div>
</template>

<style scoped>
.empty-state { padding: 48px 24px; text-align: center; display: flex; flex-direction: column; align-items: center; gap: 8px; }
.icon-wrap {
  width: 48px; height: 48px; border-radius: 12px; display: grid; place-items: center;
  background: var(--accent-soft); color: var(--accent); margin-bottom: 6px;
}
.title { font-weight: 650; font-size: 15px; }
p { margin: 0; max-width: 440px; font-size: 13.5px; }
.actions { margin-top: 10px; display: flex; gap: 8px; }
.actions:empty { display: none; }
</style>
