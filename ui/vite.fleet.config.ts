import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import autoprefixer from 'autoprefixer'
import tailwindcss from 'tailwindcss'
import path from 'path'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

// The fleet plane's entry of the one frontend project (ADR-015). It shares
// the sources, the tokens, the tests, the lint and the budget with the
// panel's entry and builds into dist/fleet, next to dist/panel, so both Go
// binaries embed one dist. Base './' keeps it prefix-agnostic like the
// panel: the admin server serves it at its UI root.
//
// `npm run dev:fleet` starts Vite on :5173 with a proxy to the admin
// server on :8080 that injects the trusted-proxy identity headers, so the
// UI listener (which trusts 127.0.0.1) accepts the requests.
export default defineConfig({
  root: path.join(__dirname, 'fleet'),
  plugins: [react()],
  base: './',
  resolve: {
    alias: {
      '@': path.join(__dirname, 'src'),
    },
  },
  css: {
    postcss: {
      plugins: [tailwindcss({ config: path.join(__dirname, 'tailwind.fleet.config.js') }), autoprefixer()],
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/nucleus.admin.v1.': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: false,
        configure: (proxy) => {
          proxy.on('proxyReq', (proxyReq) => {
            proxyReq.setHeader('X-Auth-User', 'dev')
            proxyReq.setHeader('X-Auth-Email', 'dev@local')
          })
        },
      },
      '/healthz': { target: 'http://127.0.0.1:8080', changeOrigin: false },
    },
  },
  build: {
    outDir: path.join(__dirname, 'dist', 'fleet'),
    emptyOutDir: true,
    sourcemap: false,
  },
})
