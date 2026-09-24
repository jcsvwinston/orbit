import type { AtRule, Plugin, Rule } from 'postcss'

/**
 * AG Grid ships `ag-theme-quartz.css` with three variants sharing one
 * selector list per rule: `.ag-theme-quartz`, `.ag-theme-quartz-dark` and
 * `.ag-theme-quartz-auto-dark` (the last one follows `prefers-color-scheme`).
 * The panel picks light or dark itself from its theme store
 * (AGGridTable.tsx), so the auto-dark variant is never applied. This pass
 * removes its selectors, the rules that only targeted it and the media block
 * that became empty, before Vite minifies the chunk. The structural
 * `ag-grid.css` mentions none of the variants and passes through untouched.
 *
 * Wired in vite.config.ts (`css.postcss.plugins`). Switching to AG Grid's
 * Theming API would remove the stylesheet imports altogether, but that API
 * is only a preview on the 32.x line the panel pins; it belongs with the
 * major upgrade.
 */
export const DEFAULT_UNUSED_VARIANTS: readonly string[] = ['ag-theme-quartz-auto-dark']

export interface StripUnusedGridThemesOptions {
  /** Theme class names (without the leading dot) the panel never applies. */
  variants?: readonly string[]
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

export function stripUnusedGridThemes(options: StripUnusedGridThemesOptions = {}): Plugin {
  const variants = options.variants ?? DEFAULT_UNUSED_VARIANTS
  // `.ag-theme-quartz` must not match `.ag-theme-quartz-dark`: the class has
  // to end where the selector token ends.
  const matchers = variants.map((name) => new RegExp(`\\.${escapeRegExp(name)}(?![\\w-])`))
  const mentions = (selector: string) => matchers.some((matcher) => matcher.test(selector))

  return {
    postcssPlugin: 'orbit-strip-unused-grid-themes',
    Once(root) {
      const emptied = new Set<AtRule>()
      root.walkRules((rule: Rule) => {
        if (!mentions(rule.selector)) return
        const kept = rule.selectors.filter((selector) => !mentions(selector))
        if (kept.length > 0) {
          rule.selectors = kept
          return
        }
        const parent = rule.parent
        rule.remove()
        if (parent?.type === 'atrule') emptied.add(parent as AtRule)
      })
      for (const atRule of emptied) {
        if (atRule.nodes?.length === 0) atRule.remove()
      }
    },
  }
}

stripUnusedGridThemes.postcss = true
