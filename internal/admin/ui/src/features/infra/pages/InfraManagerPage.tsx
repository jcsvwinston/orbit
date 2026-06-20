import { useEffect, useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import * as api from '@/services/api'
import type { Session } from '@/types'
import { Users, Trash, RefreshCw, Loader2 } from 'lucide-react'
import { useToast } from '@/components/ui/use-toast'

export default function InfraManagerPage() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [loading, setLoading] = useState(true)
  const { toast } = useToast()

  const fetchSessions = async () => {
    setLoading(true)
    try {
      const data = await api.getSessions()
      setSessions(data)
    } catch (error) {
      toast({
        variant: 'destructive',
        title: 'Error',
        description: 'Failed to fetch sessions',
      })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchSessions()
  }, [])

  const handleDeleteSession = async (sessionId: string) => {
    try {
      await api.deleteSession(sessionId)
      toast({
        title: 'Session terminated',
        description: 'The session has been destroyed',
      })
      fetchSessions()
    } catch (error) {
      toast({
        variant: 'destructive',
        title: 'Error',
        description: 'Failed to terminate session',
      })
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Session Manager</h1>
          <p className="text-muted-foreground">Active user sessions</p>
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
          <CardDescription>{sessions.length} active sessions</CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-8 w-8 animate-spin" />
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
                    <TableHead>IP Address</TableHead>
                    <TableHead>User Agent</TableHead>
                    <TableHead>Created</TableHead>
                    <TableHead>Last Activity</TableHead>
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {sessions.map((session) => (
                    <TableRow key={session.id}>
                      <TableCell>
                        <div>
                          <p className="font-medium">{session.username}</p>
                          <p className="text-xs text-muted-foreground">ID: {session.user_id}</p>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline">{session.ip}</Badge>
                      </TableCell>
                      <TableCell className="max-w-xs truncate text-sm">
                        {session.user_agent}
                      </TableCell>
                      <TableCell className="text-sm">
                        {new Date(session.created_at).toLocaleString()}
                      </TableCell>
                      <TableCell className="text-sm">
                        {new Date(session.last_activity).toLocaleString()}
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          variant="destructive"
                          size="sm"
                          onClick={() => handleDeleteSession(session.id)}
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
    </div>
  )
}
