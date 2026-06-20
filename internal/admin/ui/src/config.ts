/**
 * Nucleus Admin UI Configuration
 * 
 * This module reads runtime configuration injected by the Go backend.
 * The admin prefix is set via `admin_prefix` in nucleus.yml (default: "/admin").
 * 
 * Applications can override the prefix to use /admin for their own panels
 * and move the Nucleus admin panel to a different path.
 */

function normalizeAdminPrefix(prefix: string): string {
  const trimmed = prefix.trim()
  if (!trimmed) {
    return '/admin'
  }

  const withLeadingSlash = trimmed.startsWith('/') ? trimmed : `/${trimmed}`
  return withLeadingSlash.replace(/\/+$/, '') || '/admin'
}

/**
 * Get the admin panel prefix from the injected <meta> tag.
 * Falls back to '/admin' if not set (backward compatibility).
 */
export function getAdminPrefix(): string {
  if (typeof document !== 'undefined') {
    const meta = document.querySelector<HTMLMetaElement>('meta[name="nucleus-admin-prefix"]')
    if (meta && meta.content) {
      return normalizeAdminPrefix(meta.content)
    }
  }
  return '/admin'
}

/**
 * Build a full admin path by prepending the admin prefix.
 * 
 * @param path - The path relative to the admin panel root (e.g., '/api/models', '/login')
 * @returns The full path including admin prefix
 * 
 * @example
 * ```ts
 * // If admin prefix is '/nucleus-admin':
 * buildAdminPath('/api/models') // => '/nucleus-admin/api/models'
 * buildAdminPath('/login')      // => '/nucleus-admin/login'
 * 
 * // If admin prefix is '/admin' (default):
 * buildAdminPath('/api/models') // => '/admin/api/models'
 * ```
 */
export function buildAdminPath(path: string): string {
  const prefix = getAdminPrefix()

  // Ensure prefix doesn't double-slash
  const cleanPrefix = prefix.endsWith('/') ? prefix.slice(0, -1) : prefix

  // Ensure path starts with /
  const cleanPath = path.startsWith('/') ? path : `/${path}`

  return `${cleanPrefix}${cleanPath}`
}
