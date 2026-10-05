import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'

import { ErrorState } from '@/components/ui/error-state'
import { RouteFallback } from '@/components/ui/route-fallback'
import * as api from '@/services/api'
import type { Dashboard } from '@/services/api'
import WidgetGrid from '../components/WidgetGrid'

/**
 * One of the dashboards an application added beside the overview
 * (orbit.Config.Dashboards). The navigation lists only the ones this operator
 * may open; one reached by its address without the permission is refused by
 * the API, and that 403 is drawn as "no permission" rather than as a broken
 * screen.
 */
export default function DashboardPage() {
  const { id = '' } = useParams()
  const [dashboard, setDashboard] = useState<Dashboard | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [loading, setLoading] = useState(true)
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    let mounted = true
    setLoading(true)
    setError(null)
    api
      .getDashboard(id)
      .then((d) => {
        if (mounted) setDashboard(d)
      })
      .catch((err) => {
        if (mounted) {
          setDashboard(null)
          setError(err)
        }
      })
      .finally(() => {
        if (mounted) setLoading(false)
      })
    return () => {
      mounted = false
    }
  }, [id, reloadKey])

  if (loading) {
    return <RouteFallback className="h-64" />
  }

  if (error || !dashboard) {
    return (
      <ErrorState
        error={error ?? new Error('The dashboard returned no data.')}
        title="Failed to load the dashboard"
        onRetry={() => setReloadKey((k) => k + 1)}
      />
    )
  }

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <h1 className="text-3xl font-bold">{dashboard.title}</h1>
        {dashboard.description && <p className="text-muted-foreground">{dashboard.description}</p>}
      </div>
      <WidgetGrid widgets={dashboard.widgets} columns={dashboard.columns} />
    </div>
  )
}
