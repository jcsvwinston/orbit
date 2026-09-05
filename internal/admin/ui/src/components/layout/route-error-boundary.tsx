import { Component, type ReactNode } from 'react'
import { ErrorState } from '@/components/ui/error-state'
import { RouteFallback } from '@/components/ui/route-fallback'
import { isChunkLoadError, reloadOnce, reloadPending } from '@/lib/chunk-recovery'

interface Props {
  children: ReactNode
}

interface State {
  error: unknown
  failed: boolean
  reloading: boolean
}

/**
 * Catches whatever a feature page throws — a chunk that failed to load
 * (React.lazy rejects with it) or an error while rendering — so the failure
 * replaces only the content area and the sidebar stays usable. Without a
 * boundary React 19 unmounts the whole tree and the panel goes blank.
 *
 * A missing chunk usually means the tab holds an index.html older than the
 * binary serving it. The first such failure in a session reloads the page,
 * keeping the spinner up until the new document lands; a later one gets an
 * error state with a Reload button (src/lib/chunk-recovery.ts). React.lazy
 * remembers a rejected loader for good, so a reload is the only retry for a
 * chunk. Any other error gets the same state with Try again, which renders
 * the page afresh. DashboardLayout keys this boundary by pathname, so
 * navigating elsewhere always starts clean.
 */
export class RouteErrorBoundary extends Component<Props, State> {
  state: State = { error: undefined, failed: false, reloading: false }

  static getDerivedStateFromError(error: unknown): State {
    return { error, failed: true, reloading: reloadPending(error) }
  }

  componentDidCatch(error: Error) {
    const reloading = reloadOnce(error)
    if (reloading !== this.state.reloading) this.setState({ reloading })
  }

  private retry = () => {
    this.setState({ error: undefined, failed: false, reloading: false })
  }

  render() {
    const { error, failed, reloading } = this.state
    if (!failed) return this.props.children
    if (reloading) return <RouteFallback className="h-[60vh]" />
    if (isChunkLoadError(error)) {
      return (
        <ErrorState
          error={error}
          title="This page could not be loaded"
          retryLabel="Reload"
          onRetry={() => window.location.reload()}
        />
      )
    }
    return <ErrorState error={error} title="This page failed to render" onRetry={this.retry} />
  }
}
