import { readFileSync, readdirSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

/**
 * The fleet's numbered palette (src/fleet/index.css) held to WCAG AA as a whole, in
 * both themes (OR-59). The browser bench (UIF-02) reads contrast off the
 * screens it opens; this reads it off the palette, for every pair the
 * sources can put together, so a step darkened for one screen cannot leave
 * another below the line, and a screen that is not opened is covered too.
 *
 * What counts is derived from the sources, not listed here: a step is TEXT
 * when a component writes it as `text-tN` or `color: var(--tN)`, a SURFACE
 * when one paints a resting background with it (`bg-tN`, not `hover:bg-tN`,
 * or `background: var(--tN)`), and a STATUS colour when lib/colors.ts maps
 * one to it — those are drawn as text on a tint of themselves, as strong as
 * the strongest `color-mix` tint a component paints.
 */

// This file sits in tools/ for Node's file API; what it reads is the fleet.
const fleet = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'src', 'fleet')
const css = readFileSync(path.join(fleet, 'index.css'), 'utf8')
const colorsSource = readFileSync(path.join(fleet, 'lib', 'colors.ts'), 'utf8')

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) return entry.name === 'gen' ? [] : sources(full)
    return /\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name) ? [readFileSync(full, 'utf8')] : []
  })
}
const src = sources(fleet).join('\n')

function theme(selector: string): Record<string, string> {
  const start = css.indexOf(selector)
  if (start < 0) throw new Error(`index.css has no ${selector} block`)
  const block = css.slice(start, css.indexOf('}', start))
  const out: Record<string, string> = {}
  for (const m of block.matchAll(/--(t\d+|accent):\s*(#[0-9a-fA-F]{6})\s*;/g)) out[m[1]] = m[2]
  return out
}

function all(re: RegExp): Set<string> {
  return new Set(Array.from(src.matchAll(re), (m) => m[1]))
}

const statusTokens = new Set(Array.from(colorsSource.matchAll(/'var\(--(t\d+)\)'/g), (m) => m[1]))
const textTokens = new Set([...all(/(?<![:\w-])text-(t\d+)\b/g), ...all(/color:\s*'?var\(--(t\d+)\)/g)])
const surfaceTokens = new Set([...all(/(?<![:\w-])bg-(t\d+)\b/g), ...all(/background:\s*'?var\(--(t\d+)\)/g)])
const tintPercent = Math.max(...Array.from(src.matchAll(/background:\s*`color-mix\(in srgb, \$\{[^}]+\} (\d+)%, transparent\)`/g), (m) => Number(m[1])))
// --t53 is the text drawn ON a fill (the accent, the status red); the status
// colours are judged on their tints; the rest are the neutral steps.
const onFill = 't53'
const neutralText = [...textTokens].filter((t) => !statusTokens.has(t) && t !== onFill)
const neutralSurfaces = [...surfaceTokens].filter((t) => !statusTokens.has(t) && t !== onFill)

type RGB = [number, number, number]
const rgb = (hex: string): RGB => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255) as RGB
const channel = (c: number) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4)
const luminance = ([r, g, b]: RGB) => 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)
const ratio = (a: RGB, b: RGB) => {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}
const tint = (colour: RGB, over: RGB, share: number): RGB => colour.map((c, i) => c * share + over[i] * (1 - share)) as RGB

const AA = 4.5

describe.each([
  ['dark', ":root,\n[data-theme='dark'] {"],
  ['light', "[data-theme='light'] {"],
])('the fleet palette, %s theme', (_name, selector) => {
  const palette = theme(selector)
  const colour = (token: string) => {
    const hex = palette[token]
    if (!hex) throw new Error(`index.css defines no --${token} in this theme`)
    return rgb(hex)
  }

  it('finds what it measures in the sources', () => {
    expect(neutralText.length).toBeGreaterThan(10)
    expect(neutralSurfaces.length).toBeGreaterThan(4)
    expect(statusTokens.size).toBe(5)
    expect(tintPercent).toBeGreaterThan(0)
  })

  it('every step written as text reads at 4.5:1 on every surface', () => {
    const below = neutralText.flatMap((text) =>
      neutralSurfaces
        .map((surface) => ({ text, surface, ratio: ratio(colour(text), colour(surface)) }))
        .filter((pair) => pair.ratio < AA)
        .map((pair) => `--${pair.text} ${palette[pair.text]} on --${pair.surface} ${palette[pair.surface]}: ${pair.ratio.toFixed(2)}`),
    )
    expect(below).toEqual([])
  })

  it('every status colour reads at 4.5:1 on every surface and on its own tint', () => {
    const below = [...statusTokens].flatMap((status) =>
      neutralSurfaces.flatMap((surface) => {
        const s = colour(surface)
        const c = colour(status)
        return [
          { on: `--${surface}`, ratio: ratio(c, s) },
          { on: `its ${tintPercent}% tint over --${surface}`, ratio: ratio(c, tint(c, s, tintPercent / 100)) },
        ]
          .filter((pair) => pair.ratio < AA)
          .map((pair) => `--${status} ${palette[status]} on ${pair.on}: ${pair.ratio.toFixed(2)}`)
      }),
    )
    expect(below).toEqual([])
  })

  it('the accent reads as text on every surface, and the text on a fill reads on it', () => {
    const below = neutralSurfaces
      .map((surface) => ({ surface, ratio: ratio(colour('accent'), colour(surface)) }))
      .filter((pair) => pair.ratio < AA)
      .map((pair) => `--accent on --${pair.surface}: ${pair.ratio.toFixed(2)}`)
    for (const fill of ['accent', 't51']) {
      const r = ratio(colour(onFill), colour(fill))
      if (r < AA) below.push(`--${onFill} on --${fill}: ${r.toFixed(2)}`)
    }
    expect(below).toEqual([])
  })
})
