<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { can, logout, user } from './auth'
import Icon from './components/Icon.vue'
import { autoRefresh, presets, refreshTick, setAutoRefresh, useTimeRange } from './range'
import { theme, type ThemeChoice } from './theme'

const route = useRoute()
const { key, setKey } = useTimeRange()

const nav = computed(() => [
  {
    label: 'Observe',
    items: [
      { to: '/services', icon: 'services', text: 'Services' },
      { to: '/logs', icon: 'logs', text: 'Logs' },
      { to: '/traces', icon: 'traces', text: 'Traces' },
    ],
  },
  {
    label: 'Analyze',
    items: [
      { to: '/explore', icon: 'explore', text: 'Explore' },
      { to: '/dashboards', icon: 'dashboards', text: 'Dashboards' },
      { to: '/alerts', icon: 'alerts', text: 'Alerts' },
    ],
  },
  ...(can('admin')
    ? [{ label: 'Settings', items: [
        { to: '/users', icon: 'users', text: 'Users' },
        { to: '/keys', icon: 'key', text: 'API keys' },
      ] }]
    : []),
])

// Pages without data over time do not need the time range controls.
const timeless = computed(() => ['/alerts', '/users', '/keys', '/account', '/dashboards'].includes(route.path))
// Breadcrumb on detail pages, e.g. Services › demo-api.
const crumb = computed(() => {
  const parents: Record<string, [string, string]> = {
    '/services/': ['Services', '/services'], '/traces/': ['Traces', '/traces'], '/dashboards/': ['Dashboards', '/dashboards'],
  }
  const hit = Object.entries(parents).find(([prefix]) => route.path.startsWith(prefix))
  if (!hit) return undefined
  const leaf = decodeURIComponent(route.path.slice(hit[0].length))
  // Dashboard ids mean nothing to people; the page header shows the title.
  if (hit[0] === '/dashboards/') return { parent: hit[1][0], to: hit[1][1], leaf: '' }
  return { parent: hit[1][0], to: hit[1][1], leaf: leaf.length > 18 ? `${leaf.slice(0, 16)}…` : leaf }
})

const refreshOptions = [
  { s: 0, label: 'Auto-refresh off' },
  { s: 30, label: 'Every 30 s' },
  { s: 60, label: 'Every 1 min' },
  { s: 300, label: 'Every 5 min' },
]

const menuOpen = ref(false)
const themes: { value: ThemeChoice; icon: string; label: string }[] = [
  { value: 'system', icon: 'monitor', label: 'System' },
  { value: 'light', icon: 'sun', label: 'Light' },
  { value: 'dark', icon: 'moon', label: 'Dark' },
]
const initials = computed(() => (user.value?.username ?? '?').slice(0, 2).toUpperCase())
</script>

<template>
  <RouterView v-if="route.meta.public" />
  <div v-else class="shell">
    <nav class="sidebar">
      <RouterLink to="/services" class="brand">
        <span class="logo">
          <svg viewBox="0 0 32 32" width="18" height="18" aria-hidden="true">
            <circle cx="16" cy="16" r="12" fill="none" stroke="currentColor" stroke-width="4" />
            <circle cx="16" cy="16" r="4.5" fill="currentColor" />
          </svg>
        </span>
        Vindice
      </RouterLink>

      <div v-for="g in nav" :key="g.label" class="group">
        <div class="group-label">{{ g.label }}</div>
        <RouterLink v-for="i in g.items" :key="i.to" :to="{ path: i.to, query: { range: key } }" class="nav-link">
          <Icon :name="i.icon" :size="17" />{{ i.text }}
        </RouterLink>
      </div>

      <div class="spacer" />
      <div v-if="user" class="user" @click="menuOpen = !menuOpen">
        <span class="avatar">{{ initials }}</span>
        <span class="who">
          <span class="name ellipsis">{{ user.username }}</span>
          <span class="role">{{ user.role }}</span>
        </span>
        <Icon name="chevron" :size="14" class="caret" :class="{ open: menuOpen }" />
      </div>
      <div v-if="menuOpen" class="menu" @click.stop>
        <div class="menu-label">Theme</div>
        <div class="seg theme">
          <button v-for="t in themes" :key="t.value" :class="{ on: theme === t.value }" :title="t.label" @click="theme = t.value">
            <Icon :name="t.icon" :size="14" />
          </button>
        </div>
        <RouterLink to="/account" class="menu-item" @click="menuOpen = false"><Icon name="user" />Account</RouterLink>
        <button class="menu-item" @click="logout"><Icon name="logout" />Sign out</button>
      </div>
    </nav>

    <div class="main" @click="menuOpen = false">
      <header class="topbar">
        <div v-if="crumb" class="crumb">
          <RouterLink :to="{ path: crumb.to, query: { range: key } }">{{ crumb.parent }}</RouterLink>
          <template v-if="crumb.leaf">
            <Icon name="chevron" :size="14" />
            <span class="leaf">{{ crumb.leaf }}</span>
          </template>
        </div>
        <div class="grow" />
        <template v-if="!timeless">
          <div class="seg" role="group" aria-label="Time range">
            <button v-for="p in presets" :key="p.key" :class="{ on: key === p.key }" :title="p.label" @click="setKey(p.key)">
              {{ p.key }}
            </button>
          </div>
          <select class="refresh" :value="autoRefresh" aria-label="Auto-refresh" @change="setAutoRefresh(Number(($event.target as HTMLSelectElement).value))">
            <option v-for="o in refreshOptions" :key="o.s" :value="o.s">{{ o.label }}</option>
          </select>
        </template>
        <button class="btn icon" title="Refresh now" @click="refreshTick++"><Icon name="refresh" /></button>
      </header>
      <main class="content">
        <RouterView />
      </main>
    </div>
  </div>
</template>

<style scoped>
.shell { display: flex; min-height: 100vh; }

.sidebar {
  width: 224px; flex-shrink: 0; position: sticky; top: 0; height: 100vh;
  background: var(--panel); border-right: 1px solid var(--border);
  padding: 16px 12px; display: flex; flex-direction: column; gap: 4px;
}
.brand { display: flex; align-items: center; gap: 10px; font-weight: 700; font-size: 16px; color: var(--text); padding: 4px 8px 14px; }
.brand:hover { text-decoration: none; }
.logo { width: 30px; height: 30px; border-radius: 9px; display: grid; place-items: center; color: #fff; background: linear-gradient(135deg, var(--accent), #b07cff); }
.group { display: flex; flex-direction: column; gap: 2px; margin-top: 10px; }
.group-label { font-size: 11px; font-weight: 650; letter-spacing: 0.07em; text-transform: uppercase; color: var(--muted); padding: 4px 10px; }
.nav-link {
  display: flex; align-items: center; gap: 10px; padding: 8px 10px; border-radius: 8px;
  color: var(--text-2); font-weight: 550; transition: background 0.12s, color 0.12s;
}
.nav-link:hover { background: var(--hover); color: var(--text); text-decoration: none; }
.nav-link.router-link-active { background: var(--accent-soft); color: var(--accent); }
.spacer { flex: 1; }

.user { display: flex; align-items: center; gap: 10px; padding: 8px; border-radius: 10px; cursor: pointer; border: 1px solid var(--border); }
.user:hover { background: var(--hover); }
.avatar { width: 32px; height: 32px; border-radius: 50%; display: grid; place-items: center; font-size: 12px; font-weight: 700; background: var(--accent-soft); color: var(--accent); flex-shrink: 0; }
.who { display: flex; flex-direction: column; min-width: 0; line-height: 1.25; flex: 1; }
.name { font-weight: 600; }
.role { font-size: 12px; color: var(--muted); }
.caret { color: var(--muted); transform: rotate(-90deg); transition: transform 0.15s; }
.caret.open { transform: rotate(90deg); }
.menu {
  position: absolute; bottom: 72px; left: 12px; width: 200px; z-index: 20; background: var(--panel);
  border: 1px solid var(--border); border-radius: 12px; box-shadow: var(--shadow-lg); padding: 8px;
}
.menu-label { font-size: 11px; font-weight: 650; text-transform: uppercase; letter-spacing: 0.06em; color: var(--muted); padding: 4px 6px; }
.seg.theme { display: flex; margin: 2px 4px 8px; }
.seg.theme button { flex: 1; display: grid; place-items: center; padding: 6px; }
.menu-item {
  display: flex; align-items: center; gap: 10px; width: 100%; padding: 8px 8px; border-radius: 7px; border: 0;
  background: none; color: var(--text); cursor: pointer; font-weight: 500; text-align: left;
}
.menu-item:hover { background: var(--hover); text-decoration: none; }

.main { flex: 1; min-width: 0; display: flex; flex-direction: column; }
.topbar {
  position: sticky; top: 0; z-index: 10; display: flex; align-items: center; gap: 10px; padding: 10px 28px;
  background: color-mix(in srgb, var(--bg) 85%, transparent); backdrop-filter: blur(8px); border-bottom: 1px solid var(--border);
  min-height: 57px;
}
.crumb { display: flex; align-items: center; gap: 6px; font-weight: 550; color: var(--muted); }
.crumb a { color: var(--muted); }
.leaf { color: var(--text); font-family: var(--mono); font-size: 13px; }
.refresh { min-height: 34px; font-size: 13px; }
.content { padding: 24px 28px 48px; }
</style>
