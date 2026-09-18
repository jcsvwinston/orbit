import { create } from 'zustand'
import { buildAdminPath } from '@/config'

/**
 * The panel's own phrases, in the language the application declared.
 *
 * The catalogue comes from the backend (`<prefix>/ui/messages.json`), which
 * is what lets an application translate the chrome — or add a language the
 * panel does not ship — without a rebuild of this bundle. It covers the
 * panel's words only: a model called Invoice is called Invoice in every
 * language, and an error the application returns is its own sentence.
 *
 * A key with no translation falls back to the English the payload always
 * carries, and a payload that never arrives falls back to the label passed at
 * the call site: the panel is readable before, during and after the fetch.
 */

interface MessagesState {
  locale: string
  messages: Record<string, string>
  loaded: boolean
  load: () => Promise<void>
}

function injectedLocale(): string {
  if (typeof document === 'undefined') return 'en'
  const meta = document.querySelector<HTMLMetaElement>('meta[name="nucleus-admin-locale"]')
  return meta?.content?.trim() || 'en'
}

export const useMessages = create<MessagesState>((set, get) => ({
  locale: injectedLocale(),
  messages: {},
  loaded: false,

  load: async () => {
    if (get().loaded) return
    try {
      const locale = injectedLocale()
      const response = await fetch(buildAdminPath(`/ui/messages.json?locale=${encodeURIComponent(locale)}`), {
        credentials: 'same-origin',
      })
      if (!response.ok) throw new Error(`messages: ${response.status}`)
      const payload = (await response.json()) as { locale?: string; messages?: Record<string, string> }
      set({
        locale: payload.locale ?? locale,
        messages: payload.messages ?? {},
        loaded: true,
      })
    } catch {
      // An English panel is a working panel. Marking it loaded stops a
      // failed fetch from being retried on every render.
      set({ loaded: true })
    }
  },
}))

/**
 * translate looks a phrase up, with the English written at the call site as
 * the last fallback — so a key added to the interface before it is added to
 * the catalogue reads as a sentence and not as `nav.something`.
 */
export function translate(messages: Record<string, string>, key: string, fallback: string): string {
  const value = messages[key]
  return value && value.trim() ? value : fallback
}

/** useTranslate returns the t() a component renders with. */
export function useTranslate(): (key: string, fallback: string) => string {
  const messages = useMessages((state) => state.messages)
  return (key: string, fallback: string) => translate(messages, key, fallback)
}
