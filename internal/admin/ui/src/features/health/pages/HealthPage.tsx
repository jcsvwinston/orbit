import { useEffect, useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import * as api from '@/services/api'
import type { HealthCheck } from '@/types'
import { HeartPulse, RefreshCw, Loader2, CheckCircle, XCircle, AlertCircle } from 'lucide-react'
import { useToast } from '@/components/ui/use-toast'

function HealthStatusIcon({ status }: { status: HealthCheck['status'] }) {
  switch (status) {
    case 'healthy':
      return <CheckCircle className="h-5 w-5 text-green-500" />
    case 'unhealthy':
      return <XCircle className="h-5 w-5 text-red-500" />
    default:
      return <AlertCircle className="h-5 w-5 text-yellow-500" />
  }
}

export default function HealthPage() {
  const [healthChecks, setHealthChecks] = useState<HealthCheck[]>([])
  const [loading, setLoading] = useState(true)
  const { toast } = useToast()

  const fetchHealth = async () => {
    setLoading(true)
    try {
      const data = await api.getHealthChecks()
      setHealthChecks(data)
    } catch (error) {
      toast({
        variant: 'destructive',
        title: 'Error',
        description: 'Failed to fetch health checks',
      })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchHealth()
  }, [])

  const healthyCount = healthChecks.filter(h => h.status === 'healthy').length
  const unhealthyCount = healthChecks.filter(h => h.status === 'unhealthy').length

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Health Checks</h1>
          <p className="text-muted-foreground">Service health monitoring</p>
        </div>
        <Button onClick={fetchHealth} disabled={loading}>
          {loading ? (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          ) : (
            <RefreshCw className="mr-2 h-4 w-4" />
          )}
          Refresh
        </Button>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Total Checks</CardTitle>
            <HeartPulse className="h-4 w-4 text-blue-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{healthChecks.length}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Healthy</CardTitle>
            <CheckCircle className="h-4 w-4 text-green-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-500">{healthyCount}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Unhealthy</CardTitle>
            <XCircle className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-500">{unhealthyCount}</div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Service Status</CardTitle>
          <CardDescription>Health check results for all services</CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-8 w-8 animate-spin" />
            </div>
          ) : healthChecks.length === 0 ? (
            <div className="text-center py-12">
              <HeartPulse className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
              <p className="text-muted-foreground">No health checks configured</p>
            </div>
          ) : (
            <div className="space-y-3">
              {healthChecks.map((check, index) => (
                <div
                  key={index}
                  className="flex items-center justify-between p-4 rounded-lg border border-border"
                >
                  <div className="flex items-center gap-3">
                    <HealthStatusIcon status={check.status} />
                    <div>
                      <p className="font-medium">{check.name}</p>
                      {check.error && (
                        <p className="text-sm text-destructive">{check.error}</p>
                      )}
                    </div>
                  </div>
                  <div className="flex items-center gap-4">
                    {check.latency !== undefined && (
                      <span className="text-sm text-muted-foreground">
                        {check.latency.toFixed(2)}ms
                      </span>
                    )}
                    <Badge
                      variant="outline"
                      className={
                        check.status === 'healthy'
                          ? 'border-green-500/20 text-green-500'
                          : check.status === 'unhealthy'
                          ? 'border-red-500/20 text-red-500'
                          : 'border-yellow-500/20 text-yellow-500'
                      }
                    >
                      {check.status}
                    </Badge>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
