import { ref, watch } from 'vue'

export type ThemeChoice = 'system' | 'light' | 'dark'

function stored(): ThemeChoice {
  try {
    const v = localStorage.getItem('vindice.theme')
    return v === 'light' || v === 'dark' ? v : 'system'
  } catch {
    return 'system'
  }
}

export const theme = ref<ThemeChoice>(stored())

// data-theme overrides the system preference; "system" removes it.
watch(
  theme,
  (t) => {
    if (t === 'system') document.documentElement.removeAttribute('data-theme')
    else document.documentElement.setAttribute('data-theme', t)
    try {
      localStorage.setItem('vindice.theme', t)
    } catch {
      /* private mode: the choice just isn't remembered */
    }
  },
  { immediate: true },
)
