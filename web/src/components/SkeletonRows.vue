<script setup lang="ts">
withDefaults(defineProps<{ rows?: number }>(), { rows: 5 })
</script>

<template>
  <div class="sk" aria-busy="true" aria-label="Loading">
    <div v-for="i in rows" :key="i" class="line">
      <span class="skeleton a" /><span class="skeleton b" :style="{ width: `${40 + ((i * 17) % 45)}%` }" /><span class="skeleton c" />
    </div>
  </div>
</template>

<style scoped>
.sk { padding: 8px 16px; }
.line { display: flex; gap: 16px; align-items: center; padding: 10px 0; border-bottom: 1px solid var(--border); }
.line:last-child { border-bottom: none; }
.skeleton { height: 12px; }
.a { width: 110px; }
.c { width: 60px; margin-left: auto; }
</style>
