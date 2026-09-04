import {
  Toast,
  ToastClose,
  ToastDescription,
  ToastTitle,
  ToastViewport,
} from "@/components/ui/toast"
import { useToast } from "@/components/ui/use-toast"

export function Toaster() {
  const { toasts, dismiss } = useToast()

  return (
    <ToastViewport>
      {/* `open` and `onOpenChange` are toast state, not DOM attributes:
          they are consumed here rather than spread onto the <div>. */}
      {toasts.map(function ({ id, title, description, action, open, onOpenChange: _onOpenChange, variant, ...props }) {
        void _onOpenChange
        return (
          <Toast
            key={id}
            variant={variant}
            data-state={open === false ? "closed" : "open"}
            role={variant === "destructive" ? "alert" : "status"}
            aria-live={variant === "destructive" ? "assertive" : "polite"}
            {...props}
          >
            <div className="grid gap-1">
              {title && <ToastTitle>{title}</ToastTitle>}
              {description && (
                <ToastDescription>{description}</ToastDescription>
              )}
            </div>
            {action}
            <ToastClose aria-label="Dismiss notification" onClick={() => dismiss(id)} />
          </Toast>
        )
      })}
    </ToastViewport>
  )
}
