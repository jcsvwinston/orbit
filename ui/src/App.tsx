// App shell for the redesigned admin (design handoff "Orbit Admin"):
// grouped hash-based navigation (Observe / Fleet / Manage), node-detail
// sub-route (#/nodes/<id>), and the two-theme token system (data-theme on
// <html>, persisted; prototype default is light).
import { useCallback, useEffect, useState } from 'react'
import { Layout, type NavGroup, type ThemeName } from '@/components/Layout'
import { onUnauthorized } from '@/lib/transport'
import { NotAuthorizedPage } from '@/pages/NotAuthorizedPage'
import { useNodes } from '@/hooks/useNodes'
import { useSelf } from '@/hooks/useSelf'
import { OverviewPage } from '@/pages/OverviewPage'
import { MetricsPage } from '@/pages/MetricsPage'
import { HTTPStreamPage } from '@/pages/HTTPStreamPage'
import { SQLStreamPage } from '@/pages/SQLStreamPage'
import { HealthPage } from '@/pages/HealthPage'
import { NodesPage } from '@/pages/NodesPage'
import { NodeDetailPage } from '@/pages/NodeDetailPage'
import { SessionsPage } from '@/pages/SessionsPage'
import { DataStudioPage } from '@/pages/DataStudioPage'
import { AccessControlPage } from '@/pages/AccessControlPage'
import { AuditLogPage } from '@/pages/AuditLogPage'

type PageID =
  | 'overview'
  | 'metrics'
  | 'http'
  | 'sql'
  | 'health'
  | 'nodes'
  | 'sessions'
  | 'data-studio'
  | 'access'
  | 'audit'

interface Route {
  page: PageID
  nodeId: string | null
}

const PAGE_IDS: ReadonlySet<string> = new Set([
  'overview',
  'metrics',
  'http',
  'sql',
  'health',
  'nodes',
  'sessions',
  'data-studio',
  'access',
  'audit',
])

function routeFromHash(): Route {
  const raw = window.location.hash.replace(/^#\/?/, '')
  const nodeMatch = /^nodes\/(.+)$/.exec(raw)
  if (nodeMatch) {
    return { page: 'nodes', nodeId: decodeURIComponent(nodeMatch[1]) }
  }
  return { page: PAGE_IDS.has(raw) ? (raw as PageID) : 'overview', nodeId: null }
}

const THEME_KEY = 'orbit.theme'

function initialTheme(): ThemeName {
  const stored = window.localStorage.getItem(THEME_KEY)
  return stored === 'dark' || stored === 'light' ? stored : 'light'
}

function App(): React.JSX.Element {
  const [route, setRoute] = useState<Route>(routeFromHash)
  const [theme, setTheme] = useState<ThemeName>(initialTheme)
  const [unauthorized, setUnauthorized] = useState(false)
  const { nodes, isError } = useNodes()
  const { self } = useSelf()

  useEffect(() => {
    const onHash = (): void => setRoute(routeFromHash())
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [])

  // Any Unauthenticated RPC flips the whole shell to the not-authorized
  // screen instead of leaking a raw network error on each page.
  useEffect(() => onUnauthorized(() => setUnauthorized(true)), [])

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
    window.localStorage.setItem(THEME_KEY, theme)
  }, [theme])

  const navigate = useCallback((id: string): void => {
    window.location.hash = `#/${id}`
  }, [])

  const groups: NavGroup[] = [
    {
      name: 'Observe',
      items: [
        { id: 'overview', label: 'Overview' },
        { id: 'metrics', label: 'Metrics' },
        { id: 'http', label: 'HTTP requests' },
        { id: 'sql', label: 'SQL statements' },
        { id: 'health', label: 'Health' },
      ],
    },
    {
      name: 'Fleet',
      items: [
        { id: 'nodes', label: 'Nodes', badge: nodes.length },
        { id: 'sessions', label: 'Sessions' },
      ],
    },
    {
      name: 'Manage',
      items: [
        { id: 'data-studio', label: 'Data Studio' },
        { id: 'access', label: 'Access control' },
        { id: 'audit', label: 'Audit log' },
      ],
    },
  ]

  const { page, nodeId } = route

  if (unauthorized) {
    return <NotAuthorizedPage onRetry={() => window.location.reload()} />
  }

  // Footer: the real server version + the operator identity actions are
  // audited under (OR-UX-P1-6).
  const version = self?.serverVersion ? `orbit ${self.serverVersion}` : ''
  const identity = self?.subject
    ? `${self.subject}${self.readOnly ? ' (viewer)' : ''}`
    : ''

  return (
    <Layout
      current={page}
      groups={groups}
      onNavigate={navigate}
      serverHealthy={!isError}
      version={version}
      identity={identity}
      theme={theme}
      onToggleTheme={() => setTheme((t) => (t === 'dark' ? 'light' : 'dark'))}
    >
      {page === 'overview' && <OverviewPage />}
      {page === 'metrics' && <MetricsPage />}
      {page === 'http' && <HTTPStreamPage />}
      {page === 'sql' && <SQLStreamPage />}
      {page === 'health' && <HealthPage />}
      {page === 'nodes' && nodeId === null && <NodesPage />}
      {page === 'nodes' && nodeId !== null && <NodeDetailPage nodeId={nodeId} />}
      {page === 'sessions' && <SessionsPage />}
      {page === 'data-studio' && <DataStudioPage />}
      {page === 'access' && <AccessControlPage />}
      {page === 'audit' && <AuditLogPage />}
    </Layout>
  )
}

export default App
