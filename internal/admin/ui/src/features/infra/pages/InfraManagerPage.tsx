import { useCallback, useEffect, useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ErrorState } from '@/components/ui/error-state'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import * as api from '@/services/api'
import type { Session, SessionsResponse } from '@/types'
import { Users, Trash, RefreshCw, Loader2, UserX } from 'lucide-react'
import { useToast } from '@/components/ui/use-toast'

function formatTime(value: string): string {
  if (!value) return '—'
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? value : d.toLocaleString()
}

function sessionOrigin(session: Session): string {
  return [session.pod, session.host, session.instance].filter(Boolean).join(' / ') || '—'
}

export default function InfraManagerPage() {
  const [data, setData] = useState<SessionsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)
  const [pendingTerminate, setPendingTerminate] = useState<Session | null>(null)
  const [terminating, setTerminating] = useState(false)
  // The user whose every session is about to be ended — the string the row
  // shows, which is what the backend matches on.
  const [pendingRevokeAll, setPendingRevokeAll] = useState<string | null>(null)
  const [revokingAll, setRevokingAll] = useState(false)
  const { toast } = useToast()

  const fetchSessions = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      setData(await api.getSessions())
    } catch (err) {
      setError(err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchSessions()
  }, [fetchSessions])

  const confirmTerminate = async () => {
    if (!pendingTerminate) return
    setTerminating(true)
    try {
      await api.deleteSession(pendingTerminate.id)
      toast({
        title: 'Session terminated',
        description: 'The session has been destroyed',
      })
      setPendingTerminate(null)
      fetchSessions()
    } catch (err) {
      toast({
        variant: 'destructive',
        title: 'Failed to terminate session',
        description: api.errorMessage(err),
      })
    } finally {
      setTerminating(false)
    }
  }

  const confirmRevokeAll = async () => {
    if (!pendingRevokeAll) return
    setRevokingAll(true)
    try {
      const result = await api.revokeUserSessions(pendingRevokeAll)
      toast({
        title: `Revoked ${result.revoked} session${result.revoked === 1 ? '' : 's'} of ${result.user}`,
        description: result.keptCurrent
          ? 'Your own session was kept: the request that revokes never revokes itself.'
          : result.revoked === 0
            ? 'That user had no open session.'
            : 'Every device signed in as that user has been signed out.',
      })
      setPendingRevokeAll(null)
      fetchSessions()
    } catch (err) {
      toast({
        variant: 'destructive',
        title: 'Failed to revoke sessions',
        description: api.errorMessage(err),
      })
    } finally {
      setRevokingAll(false)
    }
  }

  const sessions = data?.sessions ?? []

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Session Manager</h1>
          <p className="text-muted-foreground">Active admin sessions</p>
        </div>
        <Button onClick={fetchSessions} disabled={loading}>
          {loading ? (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          ) : (
            <RefreshCw className="mr-2 h-4 w-4" />
          )}
          Refresh
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Users className="h-5 w-5" />
            Active Sessions
          </CardTitle>
          <CardDescription>
            {data ? `${data.currentActive} active session${data.currentActive === 1 ? '' : 's'}` : 'Loading sessions'}
            {data?.truncatedByLimit && ` (showing the first ${sessions.length})`}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loading && !data ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-8 w-8 animate-spin" aria-label="Loading" />
            </div>
          ) : error ? (
            <ErrorState error={error} title="Failed to load sessions" onRetry={fetchSessions} />
          ) : data && !data.enabled ? (
            <div className="text-center py-12">
              <Users className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
              <p className="text-muted-foreground">Session listing is not available</p>
              {data.reason && <p className="text-sm text-muted-foreground mt-2">{data.reason}</p>}
            </div>
          ) : sessions.length === 0 ? (
            <div className="text-center py-12">
              <Users className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
              <p className="text-muted-foreground">No active sessions</p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>User</TableHead>
                    <TableHead>Device</TableHead>
                    <TableHead>Session</TableHead>
                    <TableHead>IP Address</TableHead>
                    <TableHead className="hidden lg:table-cell">Origin</TableHead>
                    <TableHead>First seen</TableHead>
                    <TableHead>Last seen</TableHead>
                    <TableHead className="hidden lg:table-cell">Expires</TableHead>
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {sessions.map((session) => (
                    <TableRow key={session.id} data-testid="session-row">
                      <TableCell className="font-medium">
                        {session.user || '—'}
                        {session.current && (
                          <Badge variant="secondary" className="ml-2" data-testid="session-current">
                            you
                          </Badge>
                        )}
                      </TableCell>
                      <TableCell className="text-sm" title={session.userAgent || undefined} data-testid="session-device">
                        {session.device || session.userAgent || '—'}
                      </TableCell>
                      <TableCell className="font-mono text-xs">{session.tokenShort || session.id}</TableCell>
                      <TableCell>
                        {session.remoteIp ? <Badge variant="outline">{session.remoteIp}</Badge> : '—'}
                      </TableCell>
                      <TableCell className="hidden lg:table-cell text-sm text-muted-foreground">{sessionOrigin(session)}</TableCell>
                      <TableCell className="text-sm">{formatTime(session.firstSeenAt)}</TableCell>
                      <TableCell className="text-sm">{formatTime(session.lastSeenAt)}</TableCell>
                      <TableCell className="hidden lg:table-cell text-sm">{formatTime(session.expiresAt)}</TableCell>
                      <TableCell className="text-right whitespace-nowrap">
                        <Button
                          variant="outline"
                          size="sm"
                          className="mr-2"
                          aria-label={`Revoke all sessions of ${session.user || 'this user'}`}
                          disabled={!session.user}
                          title={session.user ? `Sign out every device signed in as ${session.user}` : 'This session names no user to match on'}
                          onClick={() => setPendingRevokeAll(session.user)}
                        >
                          <UserX className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="destructive"
                          size="sm"
                          aria-label={`Terminate session ${session.tokenShort || session.id}`}
                          onClick={() => setPendingTerminate(session)}
                        >
                          <Trash className="h-4 w-4" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      {pendingRevokeAll && (
        <Dialog open={true} onOpenChange={(val: boolean) => !val && !revokingAll && setPendingRevokeAll(null)}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Revoke all sessions</DialogTitle>
              <DialogDescription>
                Sign out every device signed in as <span className="font-medium">{pendingRevokeAll}</span>?
                If that is you, the session of this browser is kept: the request that revokes never revokes itself.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" onClick={() => setPendingRevokeAll(null)} disabled={revokingAll}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={confirmRevokeAll} disabled={revokingAll} data-testid="confirm-revoke-all">
                {revokingAll ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                Revoke all
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}

      {pendingTerminate && (
        <Dialog open={true} onOpenChange={(val: boolean) => !val && !terminating && setPendingTerminate(null)}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Terminate session</DialogTitle>
              <DialogDescription>
                Sign out {pendingTerminate.user ? <span className="font-medium">{pendingTerminate.user}</span> : 'this session'}
                {' '}(<span className="font-mono">{pendingTerminate.tokenShort || pendingTerminate.id}</span>)? If it is your own session you will be signed out too.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" onClick={() => setPendingTerminate(null)} disabled={terminating}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={confirmTerminate} disabled={terminating}>
                {terminating ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                Terminate
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}
