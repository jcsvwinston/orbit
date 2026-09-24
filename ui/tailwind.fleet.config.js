/** @type {import('tailwindcss').Config} */

// The fleet entry's Tailwind theme (ADR-015). Its screens still read the
// numbered palette of the "Orbit Admin" handoff (bg-t5, text-t46, ...),
// exposed here as colors over the --tN custom properties; the shared
// tokens of src/shared/tokens.css are imported by its stylesheet as well,
// so a screen re-skinned onto the design system's names needs no config
// change. Content is the fleet tree only: the panel has its own config.
const tokens = Object.fromEntries(
  Array.from({ length: 54 }, (_, i) => [`t${i}`, `var(--t${i})`]),
)

export default {
  content: ['./fleet/index.html', './src/fleet/**/*.{ts,tsx}', './src/shared/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        ...tokens,
        accent: 'var(--accent)',
      },
      fontFamily: {
        sans: ['ui-sans-serif', 'system-ui', '-apple-system', "'Segoe UI'", 'sans-serif'],
        mono: ['ui-monospace', 'Menlo', 'monospace'],
      },
      animation: {
        'pulse-dot': 'pulse 2.4s infinite',
        'pulse-fast': 'pulse 1.6s infinite',
      },
    },
  },
  plugins: [],
}
