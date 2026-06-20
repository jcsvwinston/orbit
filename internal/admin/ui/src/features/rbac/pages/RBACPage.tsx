import { useEffect, useState } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
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

export default function RBACPage() {
  const [policies, setPolicies] = useState<RBACPolicy[]>([])
  const [loading, setLoading] = useState(true)
  const [isDialogOpen, setIsDialogOpen] = useState(false)
  const [newPolicy, setNewPolicy] = useState({ ptype: 'p', v0: '', v1: '', v2: '' })
  const [isSubmitting, setIsSubmitting] = useState(false)
  const { toast } = useToast()

  const fetchPolicies = async () => {
    setLoading(true)
    try {
      const data = await api.getRBACPolicies()
      setPolicies(data)
    } catch (error) {
      toast({
        variant: 'destructive',
        title: 'Error',
        description: 'Failed to fetch RBAC policies',
      })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchPolicies()
  }, [])

  const handleAddPolicy = async () => {
    if (!newPolicy.v0 || !newPolicy.v1) {
      toast({
        variant: 'destructive',
        title: 'Validation error',
        description: 'Role and resource are required',
      })
      return
    }

    setIsSubmitting(true)
    try {
      await api.createRBACPolicy(newPolicy)
      toast({
        title: 'Policy created',
        description: 'The RBAC policy has been added successfully',
      })
      setNewPolicy({ ptype: 'p', v0: '', v1: '', v2: '' })
      setIsDialogOpen(false)
      fetchPolicies()
    } catch (error) {
      toast({
        variant: 'destructive',
        title: 'Error',
        description: 'Failed to create RBAC policy',
      })
    } finally {
      setIsSubmitting(false)
    }
  }

  const handleDeletePolicy = async (policy: RBACPolicy) => {
    try {
      await api.deleteRBACPolicy(policy)
      toast({
        title: 'Policy deleted',
        description: 'The RBAC policy has been removed',
      })
      fetchPolicies()
    } catch (error) {
      toast({
        variant: 'destructive',
        title: 'Error',
        description: 'Failed to delete RBAC policy',
      })
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
          <DialogTrigger>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Add Policy
            </Button>
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
                  value={newPolicy.v0}
                  onChange={(e) => setNewPolicy({ ...newPolicy, v0: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="resource">Resource</Label>
                <Input
                  id="resource"
                  placeholder="e.g., admin:users, admin:posts"
                  value={newPolicy.v1}
                  onChange={(e) => setNewPolicy({ ...newPolicy, v1: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="action">Action</Label>
                <Input
                  id="action"
                  placeholder="e.g., read, write, delete"
                  value={newPolicy.v2}
                  onChange={(e) => setNewPolicy({ ...newPolicy, v2: e.target.value })}
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
              <Loader2 className="h-8 w-8 animate-spin" />
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
                    <TableHead>Type</TableHead>
                    <TableHead>Role</TableHead>
                    <TableHead>Resource</TableHead>
                    <TableHead>Action</TableHead>
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {policies.map((policy, index) => (
                    <TableRow key={index}>
                      <TableCell>
                        <Badge variant="outline">{policy.ptype}</Badge>
                      </TableCell>
                      <TableCell className="font-medium">{policy.v0}</TableCell>
                      <TableCell className="font-mono text-sm">{policy.v1}</TableCell>
                      <TableCell>
                        <Badge>{policy.v2}</Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          variant="destructive"
                          size="sm"
                          onClick={() => handleDeletePolicy(policy)}
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
