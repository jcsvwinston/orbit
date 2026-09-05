import { Suspense, useState } from 'react'
import { Outlet, Link, useLocation } from 'react-router-dom'
import { useAuth } from '@/stores/authStore'
import { useTheme } from '@/stores/themeStore'
import { getAdminTitle } from '@/config'
import { RouteFallback } from '@/components/ui/route-fallback'
import { RouteErrorBoundary } from '@/components/layout/route-error-boundary'
import {
  LayoutDashboard,
  Database,
  Activity,
  Network,
  Users,
  HeartPulse,
  Shield,
  FileText,
  LogOut,
  Sun,
  Moon,
  Menu,
  X,
  ChevronLeft,
  ChevronRight,
} from 'lucide-react'

const navItems = [
  { icon: LayoutDashboard, label: 'Overview', path: '/' },
  { icon: Database, label: 'Data Studio', path: '/data-studio' },
  { icon: Activity, label: 'System Pulse', path: '/system' },
  { icon: Network, label: 'Network Inspector', path: '/live' },
  { icon: Users, label: 'Sessions', path: '/sessions' },
  { icon: HeartPulse, label: 'Health', path: '/health' },
  { icon: Shield, label: 'Access Control', path: '/rbac' },
  { icon: FileText, label: 'Audit Log', path: '/audit' },
]

export default function DashboardLayout() {
  const location = useLocation()
  const { logout } = useAuth()
  const { theme, toggleTheme } = useTheme()
  const [sidebarOpen, setSidebarOpen] = useState(true)
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)

  const handleLogout = async () => {
    await logout()
  }

  return (
    <div className="min-h-screen bg-background">
      {/* Mobile menu button */}
      <div className="lg:hidden fixed top-0 left-0 right-0 z-50 bg-background border-b border-border px-4 py-2 flex items-center justify-between">
        <button
          type="button"
          onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
          aria-label={mobileMenuOpen ? 'Close navigation' : 'Open navigation'}
          aria-expanded={mobileMenuOpen}
          className="p-2 rounded-md hover:bg-accent"
        >
          {mobileMenuOpen ? <X className="h-6 w-6" /> : <Menu className="h-6 w-6" />}
        </button>
        <span className="font-bold text-lg">{getAdminTitle()}</span>
        <button
          type="button"
          onClick={toggleTheme}
          aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
          className="p-2 rounded-md hover:bg-accent"
        >
          {theme === 'dark' ? <Sun className="h-5 w-5" /> : <Moon className="h-5 w-5" />}
        </button>
      </div>

      {/* Mobile menu overlay */}
      {mobileMenuOpen && (
        <div
          className="lg:hidden fixed inset-0 bg-black/50 z-40"
          onClick={() => setMobileMenuOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside
        className={`fixed top-0 left-0 h-full bg-card border-r border-border z-40 transition-all duration-300 ${mobileMenuOpen ? 'translate-x-0' : '-translate-x-full'
          } lg:translate-x-0 ${sidebarOpen ? 'w-64' : 'w-16'}`}
      >
        <div className="flex flex-col h-full">
          {/* Logo */}
          <div className="p-4 border-b border-border flex items-center justify-between">
            {sidebarOpen && (
              <h1 className="text-xl font-bold truncate">{getAdminTitle()}</h1>
            )}
            <button
              type="button"
              onClick={() => setSidebarOpen(!sidebarOpen)}
              aria-label={sidebarOpen ? 'Collapse sidebar' : 'Expand sidebar'}
              aria-expanded={sidebarOpen}
              className="hidden lg:block p-1 rounded-md hover:bg-accent"
            >
              {sidebarOpen ? (
                <ChevronLeft className="h-5 w-5" />
              ) : (
                <ChevronRight className="h-5 w-5" />
              )}
            </button>
          </div>

          {/* Navigation */}
          <nav className="flex-1 overflow-y-auto p-2" aria-label="Main navigation">
            <ul className="space-y-1">
              {navItems.map((item) => {
                const isActive = location.pathname === item.path
                return (
                  <li key={item.path}>
                    <Link
                      to={item.path}
                      aria-current={isActive ? 'page' : undefined}
                      aria-label={sidebarOpen ? undefined : item.label}
                      title={sidebarOpen ? undefined : item.label}
                      className={`flex items-center gap-3 px-3 py-2 rounded-md transition-colors ${isActive
                          ? 'bg-primary text-primary-foreground'
                          : 'hover:bg-accent'
                        }`}
                      onClick={() => setMobileMenuOpen(false)}
                    >
                      <item.icon className="h-5 w-5 shrink-0" />
                      {sidebarOpen && <span className="truncate">{item.label}</span>}
                    </Link>
                  </li>
                )
              })}
            </ul>
          </nav>

          {/* User section */}
          <div className="p-4 border-t border-border">
            {sidebarOpen ? (
              <div className="space-y-3">
                {/* The panel has no identity endpoint, so the SPA only knows
                    the session is accepted — it must not invent a name. */}
                <div className="text-sm">
                  <p className="font-medium truncate">Signed in</p>
                  <p className="text-muted-foreground text-xs truncate">Admin session active</p>
                </div>
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={toggleTheme}
                    aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
                    className="flex-1 p-2 rounded-md border border-border hover:bg-accent"
                  >
                    {theme === 'dark' ? (
                      <Sun className="h-4 w-4" />
                    ) : (
                      <Moon className="h-4 w-4" />
                    )}
                  </button>
                  <button
                    type="button"
                    onClick={handleLogout}
                    aria-label="Sign out"
                    className="flex-1 p-2 rounded-md border border-border hover:bg-destructive/10 hover:text-destructive"
                  >
                    <LogOut className="h-4 w-4" />
                  </button>
                </div>
              </div>
            ) : (
              <div className="space-y-2">
                <button
                  type="button"
                  onClick={toggleTheme}
                  aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
                  className="w-full p-2 rounded-md border border-border hover:bg-accent"
                >
                  {theme === 'dark' ? (
                    <Sun className="h-4 w-4" />
                  ) : (
                    <Moon className="h-4 w-4" />
                  )}
                </button>
                <button
                  type="button"
                  onClick={handleLogout}
                  aria-label="Sign out"
                  className="w-full p-2 rounded-md border border-border hover:bg-destructive/10 hover:text-destructive"
                >
                  <LogOut className="h-4 w-4" />
                </button>
              </div>
            )}
          </div>
        </div>
      </aside>

      {/* Main content */}
      <main className={`transition-all duration-300 ${sidebarOpen ? 'lg:ml-64' : 'lg:ml-16'}`}>
        <div className="p-4 lg:p-8 mt-14 lg:mt-0">
          {/* Feature pages are lazy (src/routes.ts): the fallback and, if the
              chunk or the page fails, the error state replace only the content
              area, so the sidebar stays. The key remounts both on navigation:
              a new page never inherits the previous one's error, and a page
              still loading shows the spinner right away instead of holding
              the old page (router navigations run in a transition). */}
          <RouteErrorBoundary key={location.pathname}>
            <Suspense fallback={<RouteFallback className="h-[60vh]" />}>
              <Outlet />
            </Suspense>
          </RouteErrorBoundary>
        </div>
      </main>
    </div>
  )
}
