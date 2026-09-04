import * as React from "react"

import type { ToastActionElement, ToastProps } from "@/components/ui/toast"

const TOAST_LIMIT = 5
// Every toast auto-dismisses after this long; the close button dismisses
// earlier. The unmount runs shortly after so the exit transition can play.
export const TOAST_AUTO_DISMISS_MS = 5000
const TOAST_REMOVE_DELAY = 300

type ToasterToast = ToastProps & {
  id: string
  title?: React.ReactNode
  description?: React.ReactNode
  action?: ToastActionElement
  open?: boolean
  onOpenChange?: (open: boolean) => void
}

type ActionType = {
  ADD_TOAST: "ADD_TOAST"
  UPDATE_TOAST: "UPDATE_TOAST"
  DISMISS_TOAST: "DISMISS_TOAST"
  REMOVE_TOAST: "REMOVE_TOAST"
}

let count = 0

function genId() {
  count = (count + 1) % Number.MAX_SAFE_INTEGER
  return count.toString()
}

type Action =
  | {
      type: ActionType["ADD_TOAST"]
      toast: ToasterToast
    }
  | {
      type: ActionType["UPDATE_TOAST"]
      toast: Partial<ToasterToast>
    }
  | {
      type: ActionType["DISMISS_TOAST"]
      toastId?: ToasterToast["id"]
    }
  | {
      type: ActionType["REMOVE_TOAST"]
      toastId?: ToasterToast["id"]
    }

interface State {
  toasts: ToasterToast[]
}

const removeTimeouts = new Map<string, ReturnType<typeof setTimeout>>()
const dismissTimeouts = new Map<string, ReturnType<typeof setTimeout>>()

const clearTimers = (toastId: string) => {
  const dismissTimer = dismissTimeouts.get(toastId)
  if (dismissTimer) {
    clearTimeout(dismissTimer)
    dismissTimeouts.delete(toastId)
  }
  const removeTimer = removeTimeouts.get(toastId)
  if (removeTimer) {
    clearTimeout(removeTimer)
    removeTimeouts.delete(toastId)
  }
}

const addToRemoveQueue = (toastId: string) => {
  if (removeTimeouts.has(toastId)) {
    return
  }

  const timeout = setTimeout(() => {
    removeTimeouts.delete(toastId)
    dispatch({
      type: "REMOVE_TOAST",
      toastId: toastId,
    })
  }, TOAST_REMOVE_DELAY)

  removeTimeouts.set(toastId, timeout)
}

const scheduleAutoDismiss = (toastId: string) => {
  const existing = dismissTimeouts.get(toastId)
  if (existing) clearTimeout(existing)
  const timeout = setTimeout(() => {
    dismissTimeouts.delete(toastId)
    dispatch({ type: "DISMISS_TOAST", toastId })
  }, TOAST_AUTO_DISMISS_MS)
  dismissTimeouts.set(toastId, timeout)
}

export const reducer = (state: State, action: Action): State => {
  switch (action.type) {
    case "ADD_TOAST":
      return {
        ...state,
        toasts: [action.toast, ...state.toasts].slice(0, TOAST_LIMIT),
      }

    case "UPDATE_TOAST":
      return {
        ...state,
        toasts: state.toasts.map((t) =>
          t.id === action.toast.id ? { ...t, ...action.toast } : t
        ),
      }

    case "DISMISS_TOAST": {
      const { toastId } = action

      if (toastId) {
        addToRemoveQueue(toastId)
      } else {
        state.toasts.forEach((toast) => {
          addToRemoveQueue(toast.id)
        })
      }

      return {
        ...state,
        toasts: state.toasts.map((t) =>
          t.id === toastId || toastId === undefined
            ? {
                ...t,
                open: false,
              }
            : t
        ),
      }
    }
    case "REMOVE_TOAST":
      if (action.toastId === undefined) {
        state.toasts.forEach((t) => clearTimers(t.id))
        return {
          ...state,
          toasts: [],
        }
      }
      clearTimers(action.toastId)
      return {
        ...state,
        toasts: state.toasts.filter((t) => t.id !== action.toastId),
      }
  }
}

const listeners: Array<(state: State) => void> = []

let memoryState: State = { toasts: [] }

function dispatch(action: Action) {
  memoryState = reducer(memoryState, action)
  listeners.forEach((listener) => {
    listener(memoryState)
  })
}

type Toast = Omit<ToasterToast, "id">

function toast({ ...props }: Toast) {
  const id = genId()

  const update = (props: ToasterToast) =>
    dispatch({
      type: "UPDATE_TOAST",
      toast: { ...props, id },
    })
  const dismiss = () => dispatch({ type: "DISMISS_TOAST", toastId: id })

  dispatch({
    type: "ADD_TOAST",
    toast: {
      ...props,
      id,
      open: true,
      onOpenChange: (open: boolean) => {
        if (!open) dismiss()
      },
    },
  })
  // A toast the reducer evicted (TOAST_LIMIT) never renders, so only the
  // ones still in state get a timer; the evicted ids just expire silently.
  scheduleAutoDismiss(id)

  return {
    id: id,
    dismiss,
    update,
  }
}

function useToast() {
  const [state, setState] = React.useState<State>(memoryState)

  // Subscribe once per mount, not once per state change: the old
  // `[state]` dependency tore down and re-registered the listener on
  // every toast, which is wasteful and races with dispatch.
  React.useEffect(() => {
    listeners.push(setState)
    return () => {
      const index = listeners.indexOf(setState)
      if (index > -1) {
        listeners.splice(index, 1)
      }
    }
  }, [])

  return {
    ...state,
    toast,
    dismiss: (toastId?: string) => dispatch({ type: "DISMISS_TOAST", toastId }),
  }
}

export { useToast, toast }
