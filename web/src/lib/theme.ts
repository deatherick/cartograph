import { useCallback, useEffect, useState } from 'react'

type Theme = 'light' | 'dark'

const STORAGE_KEY = 'cartograph-theme'

function systemTheme(): Theme {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function initialTheme(): Theme {
  const stored = localStorage.getItem(STORAGE_KEY)
  if (stored === 'light' || stored === 'dark') return stored
  return systemTheme()
}

/** Manual dark/light toggle, persisted, defaulting to the OS preference —
 * mirrors Grafel's NavRail toggle (tokens.css's [data-theme] attribute
 * selector, adapted here without its "warm" palette variant). */
export function useTheme() {
  const [theme, setTheme] = useState<Theme>(initialTheme)

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
    localStorage.setItem(STORAGE_KEY, theme)
  }, [theme])

  const toggle = useCallback(() => {
    setTheme((t) => (t === 'dark' ? 'light' : 'dark'))
  }, [])

  return { theme, toggle }
}

/** Read-only view of whatever `useTheme` above has actually applied to
 * `<html data-theme>` — for consumers (like React Flow's `colorMode`
 * below) that need to follow the app's own toggle, not the OS's
 * `prefers-color-scheme`. Those two can disagree (OS set to dark, app
 * manually toggled to light), and anything keyed off the OS query alone
 * silently renders in the wrong mode. Uses a MutationObserver rather than
 * its own state/effect pair so it never fights `useTheme`'s own
 * read-write-localStorage cycle — just observes the one attribute both
 * `useTheme` instances (NavRail's and this one) ultimately agree on. */
export function useAppliedTheme(): Theme {
  const [theme, setTheme] = useState<Theme>(
    () => (document.documentElement.getAttribute('data-theme') as Theme | null) ?? systemTheme(),
  )

  useEffect(() => {
    const el = document.documentElement
    const observer = new MutationObserver(() => {
      const next = (el.getAttribute('data-theme') as Theme | null) ?? systemTheme()
      setTheme(next)
    })
    observer.observe(el, { attributes: true, attributeFilter: ['data-theme'] })
    return () => observer.disconnect()
  }, [])

  return theme
}
