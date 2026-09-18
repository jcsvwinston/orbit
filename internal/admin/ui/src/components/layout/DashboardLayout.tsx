import { Suspense, useEffect, useState } from 'react'
import { Outlet, Link, useLocation } from 'react-router-dom'
import { useAuth } from '@/stores/authStore'
import { useTheme } from '@/stores/themeStore'
import { getAdminTitle } from '@/config'
import * as api from '@/services/api'
import { readBranding } from '@/lib/branding'
import { useTranslate } from '@/stores/messagesStore'
import type { UIExtensionPage } from '@/services/api'
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
  UserCog,
  X,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
} from 'lucide-react'

// The English label is written here and travels with the key: it is the last
// fallback, so a catalogue that is missing a phrase reads as a sentence
// rather than as `nav.audit` (src/stores/messagesStore.ts).
const navItems = [
  { icon: LayoutDashboard, key: 'nav.overview', label: 'Overview', path: '/' },
  { icon: Database, key: 'nav.data_studio', label: 'Data Studio', path: '/data-studio' },
  { icon: Activity, key: 'nav.system', label: 'System Pulse', path: '/system' },
  { icon: Network, key: 'nav.live', label: 'Network Inspector', path: '/live' },
  { icon: Users, key: 'nav.sessions', label: 'Sessions', path: '/sessions' },
  { icon: HeartPulse, key: 'nav.health', label: 'Health', path: '/health' },
  { icon: UserCog, key: 'nav.operators', label: 'Operators', path: '/operators' },
  { icon: Shield, key: 'nav.rbac', label: 'Access Control', path: '/rbac' },
  { icon: FileText, key: 'nav.audit', label: 'Audit Log', path: '/audit' },
]

export default function DashboardLayout() {
  const location = useLocation()
  const { logout } = useAuth()
  const { theme, toggleTheme } = useTheme()
  const [sidebarOpen, setSidebarOpen] = useState(true)
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false)
  // The screens this application added to the panel, if any. They are
  // ordinary links and not SPA routes: the page is served by the
  // application, under the panel's prefix and its session, and the panel
  // refuses to be framed — so it is navigated to, not embedded.
  const [extensions, setExtensions] = useState<UIExtensionPage[]>([])
  const t = useTranslate()
  const branding = readBranding()

  useEffect(() => {
    let cancelled = false
    api
      .getUIExtensions()
      .then((pages) => {
        if (!cancelled) setExtensions(pages)
      })
      .catch(() => {
        // An application with no screens of its own is the common case,
        // and a panel whose extension list fails to load still works:
        // the section simply does not appear.
        if (!cancelled) setExtensions([])
      })
    return () => {
      cancelled = true
    }
  }, [])

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
          aria-label={mobileMenuOpen ? t('nav.close', 'Close navigation') : t('nav.open', 'Open navigation')}
          aria-expanded={mobileMenuOpen}
          className="p-2 rounded-md hover:bg-accent"
        >
          {mobileMenuOpen ? <X className="h-6 w-6" /> : <Menu className="h-6 w-6" />}
        </button>
        <span className="font-bold text-lg">{getAdminTitle()}</span>
        <button
          type="button"
          onClick={toggleTheme}
          aria-label={theme === 'dark' ? t('theme.light', 'Switch to light theme') : t('theme.dark', 'Switch to dark theme')}
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
              // The application's logo replaces the wordmark when it declared
              // one; the title stays as its alt text, so the panel is still
              // named for anyone who cannot see the image.
              branding.logo ? (
                <img src={branding.logo} alt={getAdminTitle()} className="h-8 max-w-[10rem] object-contain" />
              ) : (
                <h1 className="text-xl font-bold truncate">{getAdminTitle()}</h1>
              )
            )}
            <button
              type="button"
              onClick={() => setSidebarOpen(!sidebarOpen)}
              aria-label={sidebarOpen ? t('nav.collapse', 'Collapse sidebar') : t('nav.expand', 'Expand sidebar')}
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
          <nav className="flex-1 overflow-y-auto p-2" aria-label={t('nav.main', 'Main navigation')}>
            <ul className="space-y-1">
              {navItems.map((item) => {
                const isActive = location.pathname === item.path
                const label = t(item.key, item.label)
                return (
                  <li key={item.path}>
                    <Link
                      to={item.path}
                      aria-current={isActive ? 'page' : undefined}
                      aria-label={sidebarOpen ? undefined : label}
                      title={sidebarOpen ? undefined : label}
                      className={`flex items-center gap-3 px-3 py-2 rounded-md transition-colors ${isActive
                          ? 'bg-primary text-primary-foreground'
                          : 'hover:bg-accent'
                        }`}
                      onClick={() => setMobileMenuOpen(false)}
                    >
                      <item.icon className="h-5 w-5 shrink-0" />
                      {sidebarOpen && <span className="truncate">{label}</span>}
                    </Link>
                  </li>
                )
              })}
              {extensions.map((page) => (
                <li key={page.id}>
                  <a
                    href={page.url}
                    aria-label={sidebarOpen ? undefined : page.title}
                    title={sidebarOpen ? page.description : page.title}
                    className="flex items-center gap-3 px-3 py-2 rounded-md transition-colors hover:bg-accent"
                    onClick={() => setMobileMenuOpen(false)}
                  >
                    <ExternalLink className="h-5 w-5 shrink-0" />
                    {sidebarOpen && <span className="truncate">{page.title}</span>}
                  </a>
                </li>
              ))}
            </ul>
          </nav>

          {/* User section */}
          <div className="p-4 border-t border-border">
            {sidebarOpen ? (
              <div className="space-y-3">
                {/* The panel has no identity endpoint, so the SPA only knows
                    the session is accepted — it must not invent a name. */}
                <div className="text-sm">
                  <p className="font-medium truncate">{t('session.signed_in', 'Signed in')}</p>
                  <p className="text-muted-foreground text-xs truncate">{t('session.active', 'Admin session active')}</p>
                </div>
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={toggleTheme}
                    aria-label={theme === 'dark' ? t('theme.light', 'Switch to light theme') : t('theme.dark', 'Switch to dark theme')}
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
                    aria-label={t('session.sign_out', 'Sign out')}
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
                  aria-label={theme === 'dark' ? t('theme.light', 'Switch to light theme') : t('theme.dark', 'Switch to dark theme')}
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
                  aria-label={t('session.sign_out', 'Sign out')}
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
