// Light or dark: the system's, unless this browser picked one. The choice is
// data-theme on <html> (styles.css reads it); index.html applies a saved one
// before the first paint so the page never flashes the other theme.
import { useSyncExternalStore } from 'react'

export type Theme = 'system' | 'light' | 'dark'

const key = 'makerapp:theme'
const listeners = new Set<() => void>()

function saved(): Theme {
  try {
    const t = localStorage.getItem(key)
    return t === 'light' || t === 'dark' ? t : 'system'
  } catch {
    return 'system'
  }
}

let current = saved()

export function setTheme(t: Theme) {
  current = t
  try {
    if (t === 'system') localStorage.removeItem(key)
    else localStorage.setItem(key, t)
  } catch {
    // Private windows: it applies to this visit only.
  }
  if (t === 'system') delete document.documentElement.dataset.theme
  else document.documentElement.dataset.theme = t
  listeners.forEach((l) => l())
}

export function useTheme(): Theme {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => current,
  )
}

export const themes: { value: Theme; label: string }[] = [
  { value: 'system', label: 'System' },
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
]
