import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { lazy, useEffect } from 'react'
import { useAuth } from '@/stores/authStore'
import { useTheme } from '@/stores/themeStore'
import { Toaster } from '@/components/ui/toaster'
import { RouteFallback } from '@/components/ui/route-fallback'
import { getAdminPrefix } from '@/config'
import { lazyRoutes } from '@/routes'
import { markChunkLoaded } from '@/lib/chunk-recovery'
import LoginPage from '@/features/auth/pages/LoginPage'
import DashboardLayout from '@/components/layout/DashboardLayout'
import OverviewPage from '@/features/overview/pages/OverviewPage'

// Built once at module load: lazy() inside render would create a new
// component type on every render and remount the page. The Suspense boundary
// that shows the fallback while a chunk loads, and the error boundary that
// handles a chunk that fails to load, live in DashboardLayout, so the sidebar
// stays put either way. A chunk that lands clears the one-reload flag of
// src/lib/chunk-recovery.ts.
const lazyPages = lazyRoutes.map((route) => ({
  path: route.path,
  Page: lazy(() =>
    route.load().then((mod) => {
      markChunkLoaded()
      return mod
    }),
  ),
}))

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, isLoading } = useAuth()

  if (isLoading) {
    return <RouteFallback />
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  return <>{children}</>
}

function App() {
  const { checkAuth } = useAuth()
  const { initTheme } = useTheme()

  useEffect(() => {
    initTheme()
    checkAuth()
  }, [checkAuth, initTheme])

  const adminPrefix = getAdminPrefix()

  return (
    <BrowserRouter basename={adminPrefix}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          path="/"
          element={
            <ProtectedRoute>
              <DashboardLayout />
            </ProtectedRoute>
          }
        >
          <Route index element={<OverviewPage />} />
          {lazyPages.map(({ path, Page }) => (
            <Route key={path} path={path} element={<Page />} />
          ))}
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
      <Toaster />
    </BrowserRouter>
  )
}

export default App
