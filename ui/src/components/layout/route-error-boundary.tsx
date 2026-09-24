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
 * binary serving it. Such a failure reloads the page, keeping the spinner up
 * until the new document lands — and, before that, while a probe checks
 * that the server answers. One that follows a reload within a minute,
 * arrives while the browser is offline, or finds the server not answering
 * (the process is restarting, a proxy in front says 502) gets an error
 * state with a Reload button instead (src/lib/chunk-recovery.ts).
 * React.lazy remembers a rejected loader for good, so a reload is the only
 * retry for a chunk. Any
 * other error gets the same state with Try again, which renders the page
 * afresh. DashboardLayout keys this boundary by pathname, so navigating
 * elsewhere always starts clean.
 */
export class RouteErrorBoundary extends Component<Props, State> {
  state: State = { error: undefined, failed: false, reloading: false }

  static getDerivedStateFromError(error: unknown): State {
    return { error, failed: true, reloading: reloadPending(error) }
  }

  componentDidCatch(error: Error) {
    // getDerivedStateFromError read the failure as a reload and put the
    // spinner up; the probe settles whether it is one. Navigating away
    // meanwhile unmounts this boundary, and a late setState is then a no-op.
    void reloadOnce(error).then((reloading) => {
      if (reloading !== this.state.reloading) this.setState({ reloading })
    })
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
