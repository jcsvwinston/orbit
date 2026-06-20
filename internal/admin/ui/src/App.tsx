import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { useEffect } from 'react'
import { useAuth } from '@/stores/authStore'
import { useTheme } from '@/stores/themeStore'
import { Toaster } from '@/components/ui/toaster'
import { getAdminPrefix } from '@/config'
import LoginPage from '@/features/auth/pages/LoginPage'
import DashboardLayout from '@/components/layout/DashboardLayout'
import OverviewPage from '@/features/overview/pages/OverviewPage'
import DataStudioPage from '@/features/data-studio/pages/DataStudioPage'
import SystemPulsePage from '@/features/system/pages/SystemPulsePage'
import NetworkInspectorPage from '@/features/network/pages/NetworkInspectorPage'
import InfraManagerPage from '@/features/infra/pages/InfraManagerPage'
import HealthPage from '@/features/health/pages/HealthPage'
import RBACPage from '@/features/rbac/pages/RBACPage'
import AuditLogPage from '@/features/audit/pages/AuditLogPage'

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, isLoading } = useAuth()

  if (isLoading) {
    return (
      <div className="flex h-screen items-center justify-center">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
      </div>
    )
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
          <Route path="data-studio" element={<DataStudioPage />} />
          <Route path="system" element={<SystemPulsePage />} />
          <Route path="live" element={<NetworkInspectorPage />} />
          <Route path="sessions" element={<InfraManagerPage />} />
          <Route path="health" element={<HealthPage />} />
          <Route path="rbac" element={<RBACPage />} />
          <Route path="audit" element={<AuditLogPage />} />
        </Route>
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
      <Toaster />
    </BrowserRouter>
  )
}

export default App
