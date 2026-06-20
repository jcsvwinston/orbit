import { create } from 'zustand'

interface ThemeState {
  theme: 'dark' | 'light'
  toggleTheme: () => void
  initTheme: () => void
}

export const useTheme = create<ThemeState>((set, get) => ({
  theme: 'light',

  initTheme: () => {
    const savedTheme = localStorage.getItem('gf-theme') as 'dark' | 'light' | null
    if (savedTheme) {
      set({ theme: savedTheme })
    }
  },

  toggleTheme: () => {
    const currentTheme = get().theme
    const newTheme = currentTheme === 'dark' ? 'light' : 'dark'

    if (newTheme === 'dark') {
      document.documentElement.classList.add('dark')
    } else {
      document.documentElement.classList.remove('dark')
    }

    localStorage.setItem('gf-theme', newTheme)
    set({ theme: newTheme })
  },
}))
