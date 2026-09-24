import { cn } from '@/lib/utils'

/**
 * Centered spinner shown while the session check runs (ProtectedRoute) and
 * while a lazy route's chunk is in flight (the Suspense fallback inside
 * DashboardLayout, where it sits in the content area next to the sidebar).
 */
export function RouteFallback({ className }: { className?: string }) {
  return (
    <div
      role="status"
      aria-label="Loading"
      className={cn('flex h-screen items-center justify-center', className)}
    >
      <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
    </div>
  )
}
