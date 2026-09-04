import { create } from 'zustand'
import * as api from '@/services/api'

// The panel has no identity endpoint, so the store only knows whether the
// session cookie is accepted — never who the operator is.
interface AuthState {
  isLoading: boolean
  isAuthenticated: boolean
  logout: () => Promise<void>
  checkAuth: () => Promise<void>
}

export const useAuth = create<AuthState>((set) => ({
  isLoading: true,
  isAuthenticated: false,

  logout: async () => {
    await api.logout()
    set({ isAuthenticated: false })
  },

  checkAuth: async () => {
    set({ isLoading: true })
    const ok = await api.checkSession()
    set({ isLoading: false, isAuthenticated: ok })
  },
}))
