/** @type {import('tailwindcss').Config} */

// Token aliases for the redesign (design handoff "Orbit Admin"): every --tN
// CSS variable from src/index.css is exposed as a Tailwind color, so classes
// read as bg-t5 / text-t46 / border-t18 and both themes resolve at runtime.
const tokens = Object.fromEntries(
  Array.from({ length: 54 }, (_, i) => [`t${i}`, `var(--t${i})`]),
)

export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
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
