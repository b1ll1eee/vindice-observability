import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { Range } from './api'

export const presets = [
  { key: '15m', label: 'Last 15 minutes', ms: 15 * 60e3 },
  { key: '1h', label: 'Last 1 hour', ms: 60 * 60e3 },
  { key: '6h', label: 'Last 6 hours', ms: 6 * 60 * 60e3 },
  { key: '24h', label: 'Last 24 hours', ms: 24 * 60 * 60e3 },
  { key: '7d', label: 'Last 7 days', ms: 7 * 24 * 60 * 60e3 },
] as const

export type PresetKey = (typeof presets)[number]['key']

// Bumped by the refresh button; views watch it to reload.
export const refreshTick = ref(0)

/**
 * The selected time range lives in the URL (?range=1h) so links are shareable.
 * `range` is recomputed against "now" whenever the preset or tick changes.
 */
export function useTimeRange() {
  const route = useRoute()
  const router = useRouter()

  const key = computed<PresetKey>(() => {
    const k = route.query.range
    return presets.some((p) => p.key === k) ? (k as PresetKey) : '1h'
  })

  const range = computed<Range>(() => {
    void refreshTick.value
    const to = Date.now()
    const ms = presets.find((p) => p.key === key.value)!.ms
    return { from: to - ms, to }
  })

  function setKey(k: PresetKey) {
    router.replace({ query: { ...route.query, range: k } })
  }

  return { key, range, setKey }
}
