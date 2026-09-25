import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

export const THEME_STORAGE_KEY = 'temvia.theme'

export type Theme = 'light' | 'dark' | 'system'
type ResolvedTheme = Exclude<Theme, 'system'>

interface ThemeContextValue {
  theme: Theme
  resolvedTheme: ResolvedTheme
  setTheme: (theme: Theme) => void
}

const ThemeContext = createContext<ThemeContextValue | undefined>(undefined)

function isTheme(value: string | null | undefined): value is Theme {
  return value === 'light' || value === 'dark' || value === 'system'
}

function readStoredTheme(): Theme {
  if (typeof window === 'undefined') return 'system'
  try {
    const value = window.localStorage.getItem(THEME_STORAGE_KEY)
    return isTheme(value) ? value : 'system'
  } catch {
    return 'system'
  }
}

function systemTheme(): ResolvedTheme {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function resolveTheme(theme: Theme): ResolvedTheme {
  return theme === 'system' ? systemTheme() : theme
}

export function applyTheme(theme: Theme): void {
  if (typeof document === 'undefined') return
  const resolved = resolveTheme(theme)
  const root = document.documentElement
  root.classList.toggle('light', resolved === 'light')
  root.classList.toggle('dark', resolved === 'dark')
  root.dataset.theme = theme
  root.style.colorScheme = resolved
}

export function initializeTheme(): Theme {
  const theme = readStoredTheme()
  applyTheme(theme)
  return theme
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(readStoredTheme)
  const [systemResolvedTheme, setSystemResolvedTheme] = useState<ResolvedTheme>(systemTheme)

  useEffect(() => {
    applyTheme(theme)
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, theme)
    } catch {
      // A blocked storage implementation should not prevent theme changes.
    }
  }, [theme])

  useEffect(() => {
    const media = typeof window.matchMedia === 'function' ? window.matchMedia('(prefers-color-scheme: dark)') : undefined
    const onSystemThemeChange = () => {
      const next = systemTheme()
      setSystemResolvedTheme(next)
      if (theme === 'system') applyTheme('system')
    }
    const onStorage = (event: StorageEvent) => {
      if (event.key !== THEME_STORAGE_KEY) return
      const next = isTheme(event.newValue) ? event.newValue : 'system'
      setThemeState(next)
      applyTheme(next)
    }
    const addMediaListener = media ? media.addEventListener?.bind(media) ?? media.addListener?.bind(media) : undefined
    const removeMediaListener = media ? media.removeEventListener?.bind(media) ?? media.removeListener?.bind(media) : undefined
    addMediaListener?.('change', onSystemThemeChange)
    window.addEventListener('storage', onStorage)
    return () => {
      removeMediaListener?.('change', onSystemThemeChange)
      window.removeEventListener('storage', onStorage)
    }
  }, [theme])

  const value = useMemo<ThemeContextValue>(() => ({
    theme,
    resolvedTheme: theme === 'system' ? systemResolvedTheme : theme,
    setTheme: (next) => {
      if (next === 'system') setSystemResolvedTheme(systemTheme())
      applyTheme(next)
      setThemeState(next)
    },
  }), [systemResolvedTheme, theme])

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme(): ThemeContextValue {
  const value = useContext(ThemeContext)
  if (!value) throw new Error('useTheme must be used within a ThemeProvider')
  return value
}

export function useOptionalTheme(): ThemeContextValue {
  const value = useContext(ThemeContext)
  const [fallbackTheme, setFallbackTheme] = useState<Theme>(readStoredTheme)
  const fallbackValue = useMemo(() => ({
    theme: fallbackTheme,
    resolvedTheme: resolveTheme(fallbackTheme),
    setTheme: (theme: Theme) => {
      setFallbackTheme(theme)
      applyTheme(theme)
      try {
        window.localStorage.setItem(THEME_STORAGE_KEY, theme)
      } catch {
        // A blocked storage implementation should not prevent theme changes.
      }
    },
  }), [fallbackTheme])
  return value ?? fallbackValue
}
