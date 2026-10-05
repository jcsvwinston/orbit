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

/** The two inks the panel draws on an accent: white, and the dark ink of its
 * light theme. The backend checks a palette with the same two
 * (internal/admin/appearance.go). */
const INK_ON_DARK = '0 0% 100%'
const INK_ON_LIGHT = '222.2 47.4% 11.2%'

/** hslToRgb reads a `H S% L%` triple back into channels in [0, 1]. */
function hslToRgb(hsl: string): [number, number, number] {
  const [h, s, l] = hsl.split(' ').map((part) => Number(part.replace('%', '')))
  const sat = s / 100
  const light = l / 100
  const k = (n: number) => (n + h / 30) % 12
  const a = sat * Math.min(light, 1 - light)
  const f = (n: number) => light - a * Math.max(-1, Math.min(k(n) - 3, 9 - k(n), 1))
  return [f(0), f(8), f(4)]
}

/** luminance is WCAG's relative luminance of a `H S% L%` triple. */
function luminance(hsl: string): number {
  const lin = (v: number) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4)
  const [r, g, b] = hslToRgb(hsl).map(lin)
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

/** foregroundFor picks the text colour drawn on the brand colour: whichever
 * of the panel's two inks reads better on it. A brand is chosen to look like
 * a brand, not to contrast with white: a pale yellow button with white text
 * is unreadable, and that is a contrast failure the panel would have
 * introduced on the application's behalf. Chosen by lightness alone, a
 * saturated yellow (lightness 50%) still got white text at 1.07:1. */
export function foregroundFor(hsl: string): string {
  return contrast(hsl, INK_ON_LIGHT) > contrast(hsl, INK_ON_DARK) ? INK_ON_LIGHT : INK_ON_DARK
}

/** applyBranding paints what the application declared. It is called before
 * React renders, so the first frame is already the product's. */
export function applyBranding(branding: Branding = readBranding()): void {
  if (typeof document === 'undefined') return

  // When the application declared a palette per theme, the document already
  // carries the accent of each theme (a <style id="orbit-palette"> the
  // backend wrote); an inline property here would paint one accent over
  // both themes again.
  const perTheme = document.getElementById('orbit-palette') !== null
  if (branding.primaryColor && !perTheme) {
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
