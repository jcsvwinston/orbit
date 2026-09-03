import { ShieldOff, AlertTriangle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { errorMessage, isApiError } from '@/services/api'

interface Props {
  error: unknown
  title?: string
  onRetry?: () => void
}

// ErrorState renders a failed page load. A 403 gets its own "no permission"
// screen so an operator knows to ask for a role rather than retry.
export function ErrorState({ error, title, onRetry }: Props) {
  const forbidden = isApiError(error) && error.isForbidden
  const Icon = forbidden ? ShieldOff : AlertTriangle
  return (
    <div role="alert" className="flex flex-col items-center justify-center gap-3 py-12 text-center">
      <Icon className="h-10 w-10 text-muted-foreground" aria-hidden="true" />
      <p className="font-medium">
        {forbidden ? 'You do not have permission to view this' : title ?? 'Something went wrong'}
      </p>
      <p className="max-w-md text-sm text-muted-foreground break-words">{errorMessage(error)}</p>
      {!forbidden && onRetry && (
        <Button variant="outline" size="sm" onClick={onRetry}>
          Try again
        </Button>
      )}
    </div>
  )
}
