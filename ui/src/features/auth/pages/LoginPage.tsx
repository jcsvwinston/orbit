import { useEffect, useState } from 'react'
import { useTheme } from '@/stores/themeStore'
import { buildAdminPath, getAdminTitle } from '@/config'
import { readBranding } from '@/lib/branding'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { useSearchParams } from 'react-router-dom'
import { Shield, Sun, Moon } from 'lucide-react'

// The server surfaces login feedback by injecting meta tags into the served
// document (same mechanism as nucleus-admin-prefix) — e.g. after a rejected
// credentials POST re-serves this page with a 401.
function serverLoginMessage(name: string): string {
  return document.querySelector(`meta[name="${name}"]`)?.getAttribute('content')?.trim() ?? ''
}

export default function LoginPage() {
  const { theme, toggleTheme } = useTheme()
  const [searchParams] = useSearchParams()
  const next = searchParams.get('next')?.trim() ?? ''
  const loginError = serverLoginMessage('nucleus-admin-login-error')
  const loginInfo = serverLoginMessage('nucleus-admin-login-info')
  // The application's logo, declared as branding and carried on this
  // document like the title: the login screen renders before any API call
  // could say whose product this is. One that fails to load gives the
  // panel's own mark back rather than leaving a hole above the title.
  const logo = readBranding().logo
  const [logoFailed, setLogoFailed] = useState(false)

  // Consume the injected metas so a later client-side navigation back to the
  // login route (e.g. session expiry) does not replay a stale banner.
  useEffect(() => {
    ;['nucleus-admin-login-error', 'nucleus-admin-login-info'].forEach((name) => {
      document.querySelector(`meta[name="${name}"]`)?.remove()
    })
  }, [])

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-background to-muted p-4">
      <button
        type="button"
        onClick={toggleTheme}
        aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
        className="fixed top-4 right-4 p-2 rounded-full bg-background/80 backdrop-blur-sm border border-border hover:bg-accent transition-colors"
      >
        {theme === 'dark' ? <Sun className="h-5 w-5" /> : <Moon className="h-5 w-5" />}
      </button>

      <Card className="w-full max-w-md shadow-2xl">
        <CardHeader className="space-y-1 text-center">
          <div className="flex justify-center mb-4">
            {logo && !logoFailed ? (
              // The title below names the product in text, so the image is
              // not announced a second time (an empty alt, not a missing one).
              <img
                src={logo}
                alt=""
                className="h-16 max-w-[12rem] object-contain"
                onError={() => setLogoFailed(true)}
              />
            ) : (
              <div className="h-16 w-16 rounded-full bg-primary/10 flex items-center justify-center">
                <Shield className="h-8 w-8 text-primary" />
              </div>
            )}
          </div>
          <CardTitle className="text-2xl font-bold">{getAdminTitle()}</CardTitle>
          <CardDescription>
            Enter your credentials to access the admin panel
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loginError && (
            <div role="alert" className="mb-4 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive-text">
              {loginError}
            </div>
          )}
          {!loginError && loginInfo && (
            <div className="mb-4 rounded-md border border-border bg-muted px-3 py-2 text-sm text-muted-foreground">
              {loginInfo}
            </div>
          )}
          <form action={buildAdminPath('/login')} method="POST" className="space-y-4">
            {next && <input type="hidden" name="next" value={next} />}
            <div className="space-y-2">
              <Label htmlFor="username">Username or Email</Label>
              <Input
                id="username"
                name="username"
                type="text"
                placeholder="admin@example.com"
                required
                autoComplete="username"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                name="password"
                type="password"
                required
                autoComplete="current-password"
              />
            </div>
            <Button type="submit" className="w-full">
              Sign In
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
