import { describe, expect, it } from 'vitest'

/**
 * `--destructive` is a fill: the colour of a dangerous button, with
 * `--destructive-foreground` on it. Written as text on the background it
 * reads at about 3.8:1 in the light theme and 2:1 in the dark one, below the
 * 4.5:1 small text needs; a message in red — a field's error, an alert —
 * uses `text-destructive-text` (src/shared/tokens.css). Nine places wrote
 * errors in the fill colour until A12 O1 (OR-62), and no screen the browser
 * bench opened showed one; this keeps the class out of the panel's sources,
 * hover states included. The browser bench reads the result in both themes
 * (UIX-13).
 */
const panelSources = import.meta.glob<string>(['../**/*.{ts,tsx}', '!../**/*.test.{ts,tsx}', '!../fleet/**'], {
  query: '?raw',
  import: 'default',
  eager: true,
})

describe('the destructive colour as text', () => {
  it('is written with the text token, never with the fill', () => {
    const offenders = Object.entries(panelSources).flatMap(([file, source]) =>
      source
        .split('\n')
        .map((line, i) => ({ line, at: `${file.replace(/^\.\.\//, '')}:${i + 1}` }))
        .filter(({ line }) => /(?<![\w-])text-destructive(?![\w-])/.test(line))
        .map(({ at, line }) => `${at}: ${line.trim()}`),
    )
    expect(Object.keys(panelSources).length, 'the glob found no panel source').toBeGreaterThan(50)
    expect(offenders).toEqual([])
  })
})
