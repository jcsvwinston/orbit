import type { ComponentType } from 'react'

/**
 * Feature pages mounted under DashboardLayout, one chunk each.
 *
 * App.tsx wraps every entry in React.lazy, so a feature's code (and its CSS —
 * the AG Grid styles travel with Data Studio) only reaches the browser when
 * its route is visited. The login page, the layout and the Overview landing
 * stay in the entry chunk: they render on every sign-in, so splitting them
 * would add a round trip without saving anything.
 *
 * The loaders live here instead of inline in App.tsx so routes.test.ts can
 * resolve each one and catch a broken import path before a user navigates.
 */
export interface LazyRoute {
  /** Path relative to the panel root, as given to `<Route path>`. */
  path: string
  /** Dynamic import of the page module; the page is its default export. */
  load: () => Promise<{ default: ComponentType }>
}

export const lazyRoutes: readonly LazyRoute[] = [
  { path: 'data-studio', load: () => import('@/features/data-studio/pages/DataStudioPage') },
  { path: 'system', load: () => import('@/features/system/pages/SystemPulsePage') },
  { path: 'live', load: () => import('@/features/network/pages/NetworkInspectorPage') },
  { path: 'sessions', load: () => import('@/features/infra/pages/InfraManagerPage') },
  { path: 'health', load: () => import('@/features/health/pages/HealthPage') },
  { path: 'rbac', load: () => import('@/features/rbac/pages/RBACPage') },
  { path: 'audit', load: () => import('@/features/audit/pages/AuditLogPage') },
]
