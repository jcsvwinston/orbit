/**
 * The panel wearing the application's clothes.
 *
 * The backend injects what the application declared as <meta> tags — the same
 * channel the prefix and the title travel on, which is what makes branding
 * available on the LOGIN screen, before any API call could carry it.
 *
 * The colour has to become a Tailwind custom property (`H S% L%`, no
 * `hsl()`), and the text drawn ON that colour has to stay readable: a brand
 * colour is chosen for a logo, not for contrast against white.
 */

export interface Branding {
  logo: string
  favicon: string
  primaryColor: string
}

function metaContent(name: string): string {
  if (typeof document === 'undefined') return ''
  const meta = document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)
  return meta?.content?.trim() ?? ''
}

export function readBranding(): Branding {
  return {
    logo: metaContent('nucleus-admin-logo'),
    favicon: metaContent('nucleus-admin-favicon'),
    primaryColor: metaContent('nucleus-admin-primary-color'),
  }
}

/** hexToHsl turns #rgb or #rrggbb into the `H S% L%` triple the stylesheet's
 * custom properties are written in. Returns null for anything else — the
 * backend validates the value at startup, so a bad one here means the page
 * was served by something else, and the panel keeps its own colour. */
export function hexToHsl(hex: string): string | null {
  const match = /^#(?:([0-9a-f]{3})|([0-9a-f]{6}))$/i.exec(hex.trim())
  if (!match) return null
  const full = match[1]
    ? match[1].split('').map((c) => c + c).join('')
    : match[2]
  const r = parseInt(full.slice(0, 2), 16) / 255
  const g = parseInt(full.slice(2, 4), 16) / 255
  const b = parseInt(full.slice(4, 6), 16) / 255

  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const l = (max + min) / 2
  let h = 0
  let s = 0
  if (max !== min) {
    const d = max - min
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
    switch (max) {
      case r: h = ((g - b) / d + (g < b ? 6 : 0)); break
      case g: h = (b - r) / d + 2; break
      default: h = (r - g) / d + 4
    }
    h /= 6
  }
  const round = (n: number) => Math.round(n * 10) / 10
  return `${round(h * 360)} ${round(s * 100)}% ${round(l * 100)}%`
}

/** foregroundFor picks the text colour drawn on the brand colour. A brand is
 * chosen to look like a brand, not to contrast with white: a pale yellow
 * button with white text is unreadable, and that is a contrast failure the
 * panel would have introduced on the application's behalf. */
export function foregroundFor(hsl: string): string {
  const lightness = Number(hsl.split(' ')[2]?.replace('%', '') ?? '0')
  return lightness > 60 ? '222.2 47.4% 11.2%' : '0 0% 100%'
}

/** applyBranding paints what the application declared. It is called before
 * React renders, so the first frame is already the product's. */
export function applyBranding(branding: Branding = readBranding()): void {
  if (typeof document === 'undefined') return

  if (branding.primaryColor) {
    const hsl = hexToHsl(branding.primaryColor)
    if (hsl) {
      const root = document.documentElement
      root.style.setProperty('--primary', hsl)
      root.style.setProperty('--primary-foreground', foregroundFor(hsl))
      // The focus ring follows the brand: leaving it on the default would
      // draw the one element a keyboard user relies on in another colour.
      root.style.setProperty('--ring', hsl)
    }
  }

  if (branding.favicon) {
    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    if (!link) {
      link = document.createElement('link')
      link.rel = 'icon'
      document.head.appendChild(link)
    }
    link.href = branding.favicon
  }
}
