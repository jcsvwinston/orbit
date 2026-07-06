import type { NodeInfo } from '@/gen/nucleus/admin/v1/admin_pb'

// fleetMainVersion returns the modal (most common) version across the fleet;
// nodes running something else render amber in the tables (design handoff).
export function fleetMainVersion(nodes: ReadonlyArray<NodeInfo>): string {
  const counts = new Map<string, number>()
  for (const n of nodes) {
    counts.set(n.version, (counts.get(n.version) ?? 0) + 1)
  }
  let best = ''
  let bestCount = 0
  for (const [v, c] of counts) {
    if (c > bestCount) {
      best = v
      bestCount = c
    }
  }
  return best
}
