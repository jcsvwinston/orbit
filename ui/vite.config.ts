import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import autoprefixer from 'autoprefixer'
import tailwindcss from 'tailwindcss'
import path from 'path'
import { fileURLToPath } from 'url'
import { precompress } from './tools/precompress.ts'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

// https://vitejs.dev/config/
export default defineConfig({
  // precompress (tools/): .gz and .br beside every text file of the dist,
  // for the Go servers to answer Accept-Encoding with.
  plugins: [react(), precompress()],
  base: './',
  resolve: {
    alias: {
      '@': path.join(__dirname, './src'),
    },
  },
  css: {
    // Inline PostCSS setup (Vite skips postcss.config.js when this is set).
    // The grid's styles are not here: AG Grid's Theming API injects them
    // from Data Studio's chunk (src/features/data-studio/lib/grid.ts).
    postcss: {
      plugins: [tailwindcss(), autoprefixer()],
    },
  },
  build: {
    // The panel's entry of the one frontend project (ADR-015): dist/panel,
    // next to the fleet entry's dist/fleet; both Go binaries embed the
    // whole dist through the ui module and serve their subtree.
    outDir: 'dist/panel',
    emptyOutDir: true,
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
            // AG Grid, in a file of its own that only Data Studio loads: the
            // largest dependency the panel has changes once per upgrade, and
            // a change to the screen around it no longer makes the browser
            // fetch it again. React, which ag-grid-react imports, is taken by
            // `vendor` first.
            { name: 'grid', test: /node_modules[\\/](ag-grid-community|ag-grid-react|ag-stack)[\\/]/, priority: 1 },
            // What every screen of the panel reads — the prefix, the API
            // client, the stores and the class helpers — in one file. Left to
            // itself the bundler cuts it into one chunk per combination of
            // the lazy chunks that share it: the dashboards' grid (loaded by
            // the overview and by the dashboard route) turned the one shared
            // chunk of the first load into five.
            {
              name: 'app',
              test: /src[\\/](config\.ts|lib[\\/]utils\.ts|services[\\/]api\.ts|stores[\\/][^\\/]+\.ts)$|node_modules[\\/](clsx|class-variance-authority|tailwind-merge|zustand)[\\/]/,
              priority: 0,
            },
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
