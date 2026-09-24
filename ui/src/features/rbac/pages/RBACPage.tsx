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
  DialogTrigger,
} from '@/components/ui/dialog'
import * as api from '@/services/api'
import type { RBACPolicy } from '@/types'
import { Shield, Plus, Trash, Loader2 } from 'lucide-react'
import { useToast } from '@/components/ui/use-toast'

export function policyKey(policy: RBACPolicy): string {
  return `${policy.eft}:${policy.sub}:${policy.obj}:${policy.act}`
}

// EffectBadge is the one visual that separates a deny rule from an allow
// rule for the same (sub, obj, act). Before it existed the SPA dropped the
// `eft` column and painted every policy as an allow.
export function EffectBadge({ eft }: { eft: RBACPolicy['eft'] }) {
  if (eft === 'deny') {
    return (
      <Badge variant="outline" className="border-red-700/40 text-red-700 dark:border-red-400/40 dark:text-red-400" data-testid="policy-effect">
        deny
      </Badge>
    )
  }
  return (
    <Badge variant="outline" className="border-green-700/40 text-green-700 dark:border-green-400/40 dark:text-green-400" data-testid="policy-effect">
      allow
    </Badge>
  )
}

export default function RBACPage() {
  const [policies, setPolicies] = useState<RBACPolicy[]>([])
  const [enabled, setEnabled] = useState(true)
  const [disabledReason, setDisabledReason] = useState<string | undefined>(undefined)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<unknown>(null)
  const [isDialogOpen, setIsDialogOpen] = useState(false)
  const [newPolicy, setNewPolicy] = useState({ sub: '', obj: '', act: '' })
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<RBACPolicy | null>(null)
  const [deleting, setDeleting] = useState(false)
  const { toast } = useToast()

  const fetchPolicies = useCallback(async () => {
    setLoading(true)
    setLoadError(null)
    try {
      const data = await api.getRBACPolicies()
      setPolicies(data.policies)
      setEnabled(data.enabled)
      setDisabledReason(data.reason)
    } catch (error) {
      setLoadError(error)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchPolicies()
  }, [fetchPolicies])

  const handleAddPolicy = async () => {
    if (!newPolicy.sub || !newPolicy.obj || !newPolicy.act) {
      toast({
        variant: 'destructive',
        title: 'Validation error',
        description: 'Role, resource and action are required',
      })
      return
    }

    setIsSubmitting(true)
    try {
      await api.createRBACPolicy(newPolicy)
      toast({
        title: 'Policy created',
        description: 'The RBAC policy has been added',
      })
      setNewPolicy({ sub: '', obj: '', act: '' })
      setIsDialogOpen(false)
      fetchPolicies()
    } catch (error) {
      toast({
        variant: 'destructive',
        title: 'Failed to create policy',
        description: api.errorMessage(error),
      })
    } finally {
      setIsSubmitting(false)
    }
  }

  const confirmDeletePolicy = async () => {
    if (!pendingDelete) return
    setDeleting(true)
    try {
      await api.deleteRBACPolicy(pendingDelete)
      toast({
        title: 'Policy deleted',
        description: 'The RBAC policy has been removed',
      })
      setPendingDelete(null)
      fetchPolicies()
    } catch (error) {
      toast({
        variant: 'destructive',
        title: 'Failed to delete policy',
        description: api.errorMessage(error),
      })
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Access Control</h1>
          <p className="text-muted-foreground">RBAC policy management</p>
        </div>
        <Dialog open={isDialogOpen} onOpenChange={setIsDialogOpen}>
          <DialogTrigger render={<Button />}>
            <Plus className="mr-2 h-4 w-4" />
            Add Policy
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Add RBAC Policy</DialogTitle>
              <DialogDescription>
                Create a new role-based access control policy
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-4">
              <div className="space-y-2">
                <Label htmlFor="role">Role</Label>
                <Input
                  id="role"
                  placeholder="e.g., admin, editor, viewer"
                  value={newPolicy.sub}
                  onChange={(e) => setNewPolicy({ ...newPolicy, sub: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="resource">Resource</Label>
                <Input
                  id="resource"
                  placeholder="e.g., admin:users, admin:posts"
                  value={newPolicy.obj}
                  onChange={(e) => setNewPolicy({ ...newPolicy, obj: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="action">Action</Label>
                <Input
                  id="action"
                  placeholder="e.g., read, write, delete"
                  value={newPolicy.act}
                  onChange={(e) => setNewPolicy({ ...newPolicy, act: e.target.value })}
                />
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setIsDialogOpen(false)}>
                Cancel
              </Button>
              <Button onClick={handleAddPolicy} disabled={isSubmitting}>
                {isSubmitting ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    Adding...
                  </>
                ) : (
                  'Add Policy'
                )}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Shield className="h-5 w-5" />
            RBAC Policies
          </CardTitle>
          <CardDescription>{policies.length} policies defined</CardDescription>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-8 w-8 animate-spin" aria-label="Loading" />
            </div>
          ) : loadError ? (
            <ErrorState error={loadError} title="Failed to load RBAC policies" onRetry={fetchPolicies} />
          ) : !enabled ? (
            <div className="text-center py-12">
              <Shield className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
              <p className="text-muted-foreground">RBAC is not enabled</p>
              {disabledReason && <p className="text-sm text-muted-foreground mt-2">{disabledReason}</p>}
            </div>
          ) : policies.length === 0 ? (
            <div className="text-center py-12">
              <Shield className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
              <p className="text-muted-foreground">No RBAC policies defined</p>
              <p className="text-sm text-muted-foreground mt-2">
                Superusers have full access by default
              </p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Role</TableHead>
                    <TableHead>Resource</TableHead>
                    <TableHead>Action</TableHead>
                    <TableHead>Effect</TableHead>
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {policies.map((policy) => (
                    <TableRow key={policyKey(policy)}>
                      <TableCell className="font-medium">{policy.sub}</TableCell>
                      <TableCell className="font-mono text-sm">{policy.obj}</TableCell>
                      <TableCell>
                        <Badge>{policy.act}</Badge>
                      </TableCell>
                      <TableCell>
                        <EffectBadge eft={policy.eft} />
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          variant="destructive"
                          size="sm"
                          aria-label={`Delete policy ${policy.sub} ${policy.obj} ${policy.act}`}
                          onClick={() => setPendingDelete(policy)}
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

      {pendingDelete && (
        <Dialog open={true} onOpenChange={(val: boolean) => !val && !deleting && setPendingDelete(null)}>
          <DialogContent className="max-w-sm">
            <DialogHeader>
              <DialogTitle>Delete policy</DialogTitle>
              <DialogDescription>
                Remove the {pendingDelete.eft} rule for <span className="font-mono">{pendingDelete.sub}</span> on{' '}
                <span className="font-mono">{pendingDelete.obj}</span> ({pendingDelete.act})? This takes effect immediately.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" onClick={() => setPendingDelete(null)} disabled={deleting}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={confirmDeletePolicy} disabled={deleting}>
                {deleting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                Delete
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}
