import { create } from 'zustand'

/**
 * The panel's theme, and who chose it.
 *
 * Two keys in the browser's storage, because they answer two questions:
 *
 * - `gf-theme` is the theme the panel showed last. Every version of the
 *   panel writes it, on every load, which is why it cannot say whether the
 *   operator ever chose anything: before A11 the panel wrote the browser's
 *   preference there on the first visit.
 * - `orbit-theme-choice` is written only by the toggle. It is the operator's
 *   own choice, and it wins over the theme the application configured.
 *
 * When the application configured a theme, a script the document loads
 * ahead of this bundle has already applied it — or the operator's choice —
 * and marked the document with `data-theme-from` (internal/admin, the
 * first-frame theme script). Without one, the bundle decides as it always
 * has: the last theme, or else the browser's preference.
 */

export type Theme = 'dark' | 'light'

export const LAST_THEME_KEY = 'gf-theme'
export const CHOICE_KEY = 'orbit-theme-choice'

function read(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function write(key: string, value: string): void {
  try {
    localStorage.setItem(key, value)
  } catch {
    // Storage refused: the theme still changes for this page.
  }
}

/** documentTheme is the theme the document is painted in right now. */
export function documentTheme(): Theme {
  if (typeof document === 'undefined') return 'light'
  return document.documentElement.classList.contains('dark') ? 'dark' : 'light'
}

function paint(theme: Theme): void {
  if (theme === 'dark') document.documentElement.classList.add('dark')
  else document.documentElement.classList.remove('dark')
}

/** applyInitialTheme decides the theme before React renders, unless the
 * document already did. */
export function applyInitialTheme(): void {
  if (typeof document === 'undefined') return
  if (document.documentElement.hasAttribute('data-theme-from')) return
  const stored = read(LAST_THEME_KEY)
  let theme: Theme
  if (stored === 'dark' || stored === 'light') {
    theme = stored
  } else {
    const prefersDark = typeof window.matchMedia === 'function' && window.matchMedia('(prefers-color-scheme: dark)').matches
    theme = prefersDark ? 'dark' : 'light'
  }
  write(LAST_THEME_KEY, theme)
  paint(theme)
}

interface ThemeState {
  theme: Theme
  toggleTheme: () => void
  initTheme: () => void
}

export const useTheme = create<ThemeState>((set, get) => ({
  theme: documentTheme(),

  // The store follows the document: whatever decided the first frame, this
  // is what the operator is looking at.
  initTheme: () => {
    set({ theme: documentTheme() })
  },

  toggleTheme: () => {
    const next: Theme = get().theme === 'dark' ? 'light' : 'dark'
    paint(next)
    write(LAST_THEME_KEY, next)
    write(CHOICE_KEY, next)
    set({ theme: next })
  },
}))
