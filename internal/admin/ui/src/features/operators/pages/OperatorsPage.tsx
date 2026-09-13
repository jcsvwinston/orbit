import { useCallback, useEffect, useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { ErrorState } from '@/components/ui/error-state'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import * as api from '@/services/api'
import type { Operator } from '@/types'
import { UserPlus, KeyRound, Trash, Loader2, UserCheck, UserX } from 'lucide-react'
import { useToast } from '@/components/ui/use-toast'

// parseRoles turns the comma-separated field of the create dialog into the
// list the API takes. Empty entries are dropped rather than granted: a
// trailing comma is a typo, not a role.
export function parseRoles(raw: string): string[] {
  return raw
    .split(',')
    .map((role) => role.trim())
    .filter((role) => role.length > 0)
}

export function StatusBadge({ active }: { active: boolean }) {
  if (active) {
    return (
      <Badge variant="outline" className="border-green-700/40 text-green-700 dark:border-green-400/40 dark:text-green-400" data-testid="operator-status">
        active
      </Badge>
    )
  }
  return (
    <Badge variant="outline" className="border-amber-700/40 text-amber-700 dark:border-amber-400/40 dark:text-amber-400" data-testid="operator-status">
      deactivated
    </Badge>
  )
}

const emptyDraft = { username: '', email: '', password: '', is_superuser: false, roles: '' }

export default function OperatorsPage() {
  const [operators, setOperators] = useState<Operator[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<unknown>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [draft, setDraft] = useState(emptyDraft)
  const [submitting, setSubmitting] = useState(false)
  const [passwordFor, setPasswordFor] = useState<Operator | null>(null)
  const [newPassword, setNewPassword] = useState('')
  const [pendingDelete, setPendingDelete] = useState<Operator | null>(null)
  const [busyID, setBusyID] = useState<string | null>(null)
  const { toast } = useToast()

  const load = useCallback(async () => {
    setLoading(true)
    setLoadError(null)
    try {
      const data = await api.getOperators()
      setOperators(data.operators)
    } catch (error) {
      setLoadError(error)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  // The backend refuses what would lock everyone out (409) and says why.
  // Showing its message instead of a generic one is the whole difference
  // between "something went wrong" and "promote another operator first".
  const failed = (error: unknown, fallback: string) => {
    toast({
      variant: 'destructive',
      title: fallback,
      description: api.errorMessage(error, fallback),
    })
  }

  const submitCreate = async () => {
    setSubmitting(true)
    try {
      await api.createOperator({
        username: draft.username.trim(),
        email: draft.email.trim(),
        password: draft.password,
        is_superuser: draft.is_superuser,
        roles: parseRoles(draft.roles),
      })
      toast({ title: 'Operator created', description: `${draft.username} can sign in now.` })
      setCreateOpen(false)
      setDraft(emptyDraft)
      await load()
    } catch (error) {
      failed(error, 'Could not create the operator')
    } finally {
      setSubmitting(false)
    }
  }

  const submitPassword = async () => {
    if (!passwordFor) return
    setSubmitting(true)
    try {
      await api.setOperatorPassword(passwordFor.id, newPassword)
      toast({ title: 'Password set', description: `${passwordFor.username} signs in with the new password.` })
      setPasswordFor(null)
      setNewPassword('')
    } catch (error) {
      failed(error, 'Could not set the password')
    } finally {
      setSubmitting(false)
    }
  }

  const toggleActive = async (operator: Operator) => {
    setBusyID(operator.id)
    try {
      await api.setOperatorActive(operator.id, !operator.is_active)
      await load()
    } catch (error) {
      failed(error, operator.is_active ? 'Could not deactivate the operator' : 'Could not reactivate the operator')
    } finally {
      setBusyID(null)
    }
  }

  const confirmDelete = async () => {
    if (!pendingDelete) return
    setBusyID(pendingDelete.id)
    try {
      await api.deleteOperator(pendingDelete.id)
      toast({ title: 'Operator deleted', description: `${pendingDelete.username} no longer has an account.` })
      setPendingDelete(null)
      await load()
    } catch (error) {
      failed(error, 'Could not delete the operator')
    } finally {
      setBusyID(null)
    }
  }

  if (loadError) {
    return <ErrorState error={loadError} onRetry={() => void load()} />
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Operators</h1>
          <p className="text-sm text-muted-foreground">
            The people who sign in to this panel. Deactivating keeps the account and its audit trail;
            deleting removes the account.
          </p>
        </div>
        <Button onClick={() => setCreateOpen(true)} data-testid="new-operator">
          <UserPlus className="mr-2 h-4 w-4" />
          New operator
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Accounts</CardTitle>
          <CardDescription>
            {loading ? 'Loading…' : `${operators.length} operator${operators.length === 1 ? '' : 's'}`}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Username</TableHead>
                <TableHead>Email</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Roles</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {operators.map((operator) => (
                <TableRow key={operator.id} data-testid="operator-row">
                  <TableCell className="font-medium">
                    {operator.username}
                    {operator.is_superuser && (
                      <Badge variant="secondary" className="ml-2" data-testid="operator-superuser">
                        superuser
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell>{operator.email}</TableCell>
                  <TableCell>
                    <StatusBadge active={operator.is_active} />
                  </TableCell>
                  <TableCell>
                    {operator.roles.length === 0 ? (
                      <span className="text-muted-foreground">—</span>
                    ) : (
                      operator.roles.map((role) => (
                        <Badge key={role} variant="outline" className="mr-1">
                          {role}
                        </Badge>
                      ))
                    )}
                  </TableCell>
                  <TableCell className="text-right space-x-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        setPasswordFor(operator)
                        setNewPassword('')
                      }}
                      aria-label={`Set a new password for ${operator.username}`}
                    >
                      <KeyRound className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={busyID === operator.id}
                      onClick={() => void toggleActive(operator)}
                      aria-label={
                        operator.is_active
                          ? `Deactivate ${operator.username}`
                          : `Reactivate ${operator.username}`
                      }
                    >
                      {busyID === operator.id ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                      ) : operator.is_active ? (
                        <UserX className="h-4 w-4" />
                      ) : (
                        <UserCheck className="h-4 w-4" />
                      )}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setPendingDelete(operator)}
                      aria-label={`Delete ${operator.username}`}
                    >
                      <Trash className="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New operator</DialogTitle>
            <DialogDescription>
              They sign in with these credentials. Roles are optional and can be changed later.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="operator-username">Username</Label>
              <Input
                id="operator-username"
                value={draft.username}
                onChange={(e) => setDraft({ ...draft, username: e.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="operator-email">Email</Label>
              <Input
                id="operator-email"
                type="email"
                value={draft.email}
                onChange={(e) => setDraft({ ...draft, email: e.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="operator-password">Password</Label>
              <Input
                id="operator-password"
                type="password"
                value={draft.password}
                onChange={(e) => setDraft({ ...draft, password: e.target.value })}
              />
              <p className="text-xs text-muted-foreground">At least 12 characters.</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="operator-roles">Roles</Label>
              <Input
                id="operator-roles"
                placeholder="editors, viewers"
                value={draft.roles}
                onChange={(e) => setDraft({ ...draft, roles: e.target.value })}
              />
            </div>
            <div className="flex items-center gap-2">
              <input
                id="operator-superuser"
                type="checkbox"
                checked={draft.is_superuser}
                onChange={(e) => setDraft({ ...draft, is_superuser: e.target.checked })}
              />
              <Label htmlFor="operator-superuser">Superuser (bypasses every policy)</Label>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>
              Cancel
            </Button>
            <Button onClick={() => void submitCreate()} disabled={submitting}>
              {submitting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Create
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={passwordFor !== null} onOpenChange={(open) => !open && setPasswordFor(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Set a new password</DialogTitle>
            <DialogDescription>
              {passwordFor?.username} signs in with the new password from now on; the old one stops
              working.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="operator-new-password">New password</Label>
            <Input
              id="operator-new-password"
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPasswordFor(null)}>
              Cancel
            </Button>
            <Button onClick={() => void submitPassword()} disabled={submitting}>
              {submitting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Set password
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={pendingDelete !== null} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete {pendingDelete?.username}?</DialogTitle>
            <DialogDescription>
              The account is removed. To keep it — and the name on its audit entries — deactivate it
              instead.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPendingDelete(null)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={() => void confirmDelete()} disabled={busyID !== null}>
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
