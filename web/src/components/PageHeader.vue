<script setup lang="ts">
defineProps<{ title: string; subtitle?: string }>()
</script>

<template>
  <header class="page-header">
    <div class="titles">
      <div class="row"><slot name="before" /><h1>{{ title }}</h1><slot name="badge" /></div>
      <p v-if="subtitle" class="muted">{{ subtitle }}</p>
    </div>
    <div class="actions"><slot /></div>
  </header>
</template>

<style scoped>
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 20px; }
.titles p { margin: 4px 0 0; font-size: 13.5px; }
.actions { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
</style>
