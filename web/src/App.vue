<script setup lang="ts">
import { presets, refreshTick, useTimeRange, type PresetKey } from './range'

const { key, setKey } = useTimeRange()

function onRange(e: Event) {
  setKey((e.target as HTMLSelectElement).value as PresetKey)
}
</script>

<template>
  <div class="shell">
    <nav class="sidebar">
      <RouterLink to="/services" class="brand">
        <svg viewBox="0 0 32 32" width="22" height="22" aria-hidden="true">
          <circle cx="16" cy="16" r="13" fill="none" stroke="currentColor" stroke-width="4" />
          <circle cx="16" cy="16" r="5" fill="currentColor" />
        </svg>
        Vindice
      </RouterLink>
      <RouterLink :to="{ path: '/services', query: { range: key } }">Services</RouterLink>
      <RouterLink :to="{ path: '/logs', query: { range: key } }">Logs</RouterLink>
      <RouterLink :to="{ path: '/traces', query: { range: key } }">Traces</RouterLink>
      <RouterLink :to="{ path: '/explore', query: { range: key } }">Explore</RouterLink>
      <RouterLink :to="{ path: '/dashboards', query: { range: key } }">Dashboards</RouterLink>
      <RouterLink to="/alerts">Alerts</RouterLink>
    </nav>
    <div class="main">
      <header class="topbar">
        <div class="spacer" />
        <select :value="key" aria-label="Time range" @change="onRange">
          <option v-for="p in presets" :key="p.key" :value="p.key">{{ p.label }}</option>
        </select>
        <button class="icon" title="Refresh" @click="refreshTick++">↻</button>
      </header>
      <main class="content">
        <RouterView />
      </main>
    </div>
  </div>
</template>
