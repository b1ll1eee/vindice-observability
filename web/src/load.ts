import { ref, shallowRef, watch, type WatchSource } from 'vue'

/**
 * Runs `fn` now and whenever a source changes, tracking loading and errors.
 * Results of a superseded run are dropped, so fast filter changes never
 * show stale data.
 */
export function useLoader<T>(fn: () => Promise<T>, sources: WatchSource[]) {
  const data = shallowRef<T>()
  const error = ref('')
  const loading = ref(false)
  let run = 0

  async function load() {
    const id = ++run
    loading.value = true
    error.value = ''
    try {
      const result = await fn()
      if (id === run) data.value = result
    } catch (e) {
      if (id === run) error.value = e instanceof Error ? e.message : String(e)
    } finally {
      if (id === run) loading.value = false
    }
  }

  watch(sources, load, { immediate: true })
  return { data, error, loading, reload: load }
}
