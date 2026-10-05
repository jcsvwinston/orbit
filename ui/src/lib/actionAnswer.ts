import { buildAdminPath } from '@/config'

// What an application action answered with (EXT-03), as the SPA follows it:
// a page of the panel to go to, or a file to save.

// A destination the SPA can reach: a screen of its own, which the router
// draws without reloading the document, or a page the server serves inside
// the panel (an application's /x/ screen), which needs a document load.
export type ActionDestination =
  | { router: true; to: string }
  | { router: false; href: string }

// redirectDestination decides how to follow a redirect an action answered
// with. The server has already refused anything that is not a path inside
// the panel; this refuses it again (returns null) rather than navigate the
// operator off the panel if a server ever let one through. spaRoots are the
// first path segments the SPA's router draws ("data-studio", "audit", …).
export function redirectDestination(path: string, spaRoots: readonly string[]): ActionDestination | null {
  // One leading slash, no backslash, no control character: the same rule
  // the server applies, for the same reason — a browser reads "//host" and
  // "/\host" as another site.
  // eslint-disable-next-line no-control-regex
  if (!/^\/(?![/\\])/.test(path) || /[\\\u0000-\u001f\u007f]/.test(path)) return null
  const pathname = path.split(/[?#]/, 1)[0]
  if (pathname.split('/').some((segment) => segment === '.' || segment === '..')) return null
  const root = pathname.split('/')[1] ?? ''
  if (root === '' || spaRoots.includes(root)) return { router: true, to: path }
  return { router: false, href: buildAdminPath(path) }
}

// isAttachment reports whether an answer is a file to save rather than a
// JSON answer to read.
export function isAttachment(disposition: string | null): boolean {
  return disposition !== null && /^\s*attachment\b/i.test(disposition)
}

// filenameFromDisposition reads the name a file is to be saved under: the
// exact name from filename*= (RFC 5987) when it is there, the ASCII fallback
// from filename= otherwise. Whatever it reads, it keeps only the last path
// element — a name is never a place to write to.
export function filenameFromDisposition(disposition: string | null): string | null {
  if (!disposition) return null
  let name: string | null = null
  const extended = /filename\*\s*=\s*([^']*)'[^']*'([^;]+)/i.exec(disposition)
  if (extended) {
    try {
      name = decodeURIComponent(extended[2].trim())
    } catch {
      name = null
    }
  }
  if (name === null) {
    const quoted = /filename\s*=\s*"((?:[^"\\]|\\.)*)"/i.exec(disposition)
    if (quoted) name = quoted[1].replace(/\\(.)/g, '$1')
  }
  if (name === null) {
    const bare = /filename\s*=\s*([^;]+)/i.exec(disposition)
    if (bare) name = bare[1].trim()
  }
  if (name === null) return null
  const base = name.split(/[/\\]/).pop()?.trim() ?? ''
  return base === '' || base === '.' || base === '..' ? null : base
}

// saveFile hands a file to the browser to save, under the name given. The
// object URL lives long enough for the download to start and is then
// released.
export function saveFile(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.rel = 'noopener'
  link.style.display = 'none'
  document.body.appendChild(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
}
