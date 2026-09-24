import postcss from 'postcss'
import { describe, expect, it } from 'vitest'
import { stripUnusedGridThemes } from './postcss-strip-unused-grid-themes'

// Shape of ag-theme-quartz.css: shared selector lists across the three
// variants, one auto-dark-only rule, one media block that only serves
// auto-dark and one that mixes it with a used variant.
const sample = `
.ag-theme-quartz,
.ag-theme-quartz-dark,
.ag-theme-quartz-auto-dark {
  --ag-row-height: 42px;
}
.ag-theme-quartz .ag-header,
.ag-theme-quartz-dark .ag-header,
.ag-theme-quartz-auto-dark .ag-header {
  font-weight: 500;
}
.ag-theme-quartz-auto-dark {
  color-scheme: light;
}
@media (prefers-color-scheme: dark) {
  .ag-theme-quartz-auto-dark {
    color-scheme: dark;
  }
}
@media (max-resolution: 1.5x) {
  .ag-theme-quartz .ag-label,
  .ag-theme-quartz-auto-dark .ag-label {
    font-size: 12px;
  }
}
`

async function run(css: string, variants?: readonly string[]): Promise<string> {
  const result = await postcss([stripUnusedGridThemes(variants ? { variants } : {})]).process(css, {
    from: undefined,
  })
  return result.css
}

describe('stripUnusedGridThemes', () => {
  it('drops the auto-dark selectors and keeps the light and dark variants', async () => {
    const out = await run(sample)
    expect(out).not.toContain('ag-theme-quartz-auto-dark')
    expect(out).toContain('.ag-theme-quartz,\n.ag-theme-quartz-dark {')
    expect(out).toContain('.ag-theme-quartz .ag-header,\n.ag-theme-quartz-dark .ag-header {')
    expect(out).toContain('--ag-row-height: 42px')
    expect(out).toContain('font-weight: 500')
  })

  it('removes rules and media blocks that only targeted the unused variant', async () => {
    const out = await run(sample)
    expect(out).not.toContain('color-scheme')
    expect(out).not.toContain('prefers-color-scheme')
    // The mixed media block survives with the used selector only.
    expect(out).toContain('@media (max-resolution: 1.5x) {\n  .ag-theme-quartz .ag-label {')
    expect(out).toContain('font-size: 12px')
  })

  it('leaves CSS that never mentions the variant untouched', async () => {
    const css = '.ag-root { display: flex; }\n@media (max-width: 600px) { .ag-root { display: block; } }\n'
    expect(await run(css)).toBe(css)
  })

  it('matches the variant as a whole class, not as a prefix of another theme', async () => {
    const css = '.ag-theme-quartz,\n.ag-theme-quartz-dark {\n  color: red;\n}\n'
    // Asking to strip the light theme must not take the dark one with it.
    expect(await run(css, ['ag-theme-quartz'])).toBe('.ag-theme-quartz-dark {\n  color: red;\n}\n')
  })
})
