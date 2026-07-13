// Toast / notification system for the fleet SPA (OR-UX-P1-1). The
// redesign had no way to confirm a save/delete or surface an error
// outside a modal — actions closed silently. This provides a minimal
// provider with an aria-live region so screen readers announce results.
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { SEMANTIC } from '@/lib/colors'

export type ToastKind = 'success' | 'error' | 'info'

interface Toast {
  id: number
  kind: ToastKind
  message: string
}

interface ToastAPI {
  push: (kind: ToastKind, message: string) => void
  success: (message: string) => void
  error: (message: string) => void
  info: (message: string) => void
}

const ToastContext = createContext<ToastAPI | null>(null)

// eslint-disable-next-line react-refresh/only-export-components
export function useToast(): ToastAPI {
  const ctx = useContext(ToastContext)
  if (!ctx) {
    throw new Error('useToast must be used within a <ToastProvider>')
  }
  return ctx
}

const AUTO_DISMISS_MS = 5000

export function ToastProvider(props: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const nextId = useRef(1)

  const dismiss = useCallback((id: number): void => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  const push = useCallback(
    (kind: ToastKind, message: string): void => {
      const id = nextId.current++
      setToasts((prev) => [...prev, { id, kind, message }])
      window.setTimeout(() => dismiss(id), AUTO_DISMISS_MS)
    },
    [dismiss],
  )

  const api = useMemo<ToastAPI>(
    () => ({
      push,
      success: (m) => push('success', m),
      error: (m) => push('error', m),
      info: (m) => push('info', m),
    }),
    [push],
  )

  return (
    <ToastContext.Provider value={api}>
      {props.children}
      <div
        aria-live="polite"
        aria-atomic="false"
        className="pointer-events-none fixed bottom-4 right-4 z-[100] flex w-[320px] max-w-[calc(100vw-2rem)] flex-col gap-2"
      >
        {toasts.map((t) => (
          <ToastCard key={t.id} toast={t} onDismiss={() => dismiss(t.id)} />
        ))}
      </div>
    </ToastContext.Provider>
  )
}

function toastColor(kind: ToastKind): string {
  switch (kind) {
    case 'success':
      return SEMANTIC.green
    case 'error':
      return SEMANTIC.red
    default:
      return SEMANTIC.blue
  }
}

function ToastCard(props: { toast: Toast; onDismiss: () => void }) {
  const color = toastColor(props.toast.kind)
  return (
    <div
      role={props.toast.kind === 'error' ? 'alert' : 'status'}
      className="pointer-events-auto flex items-start gap-2.5 rounded-[9px] border bg-t5 px-3 py-2.5 text-[12.5px] text-t42 shadow-lg"
      style={{ borderColor: `color-mix(in srgb, ${color} 35%, transparent)` }}
    >
      <span
        className="mt-[5px] h-[7px] w-[7px] shrink-0 rounded-full"
        style={{ background: color }}
      />
      <span className="min-w-0 flex-1 break-words">{props.toast.message}</span>
      <button
        type="button"
        onClick={props.onDismiss}
        aria-label="Dismiss notification"
        className="shrink-0 text-t30 transition-colors hover:text-t45"
      >
        ✕
      </button>
    </div>
  )
}
