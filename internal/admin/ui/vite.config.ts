import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import autoprefixer from 'autoprefixer'
import tailwindcss from 'tailwindcss'
import path from 'path'
import { fileURLToPath } from 'url'
import { stripUnusedGridThemes } from './tools/postcss-strip-unused-grid-themes.ts'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  base: './',
  resolve: {
    alias: {
      '@': path.join(__dirname, './src'),
    },
  },
  css: {
    // Inline PostCSS setup (Vite skips postcss.config.js when this is set):
    // Tailwind and autoprefixer as before, plus the pass that drops the AG
    // Grid theme variant the panel never applies (tools/).
    postcss: {
      plugins: [tailwindcss(), autoprefixer(), stripUnusedGridThemes()],
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    rolldownOptions: {
      output: {
        // Feature pages are dynamic imports (src/routes.ts), so each one is
        // already its own chunk and Recharts lands in System Pulse's without
        // a group. Groups capture a module's dependencies too, which is why
        // there is no `charts` group any more: it used to take `clsx` and a
        // React helper along and the entry then imported the whole chunk
        // for them. `vendor` keeps React and the router in one stable file;
        // `icons` folds the lucide glyphs shared by several pages into one
        // request instead of a 200-byte chunk per icon.
        codeSplitting: {
          groups: [
            {
              name: 'vendor',
              test: /node_modules[\\/](react|react-dom|react-router|react-router-dom|scheduler)[\\/]/,
              priority: 2,
            },
            { name: 'icons', test: /node_modules[\\/]lucide-react[\\/]/, priority: 1 },
          ],
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.{ts,tsx}', 'tools/**/*.test.ts'],
    css: false,
  },
})
