import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App.tsx'
import { getAdminTitle } from './config'
import { installPreloadErrorReload } from './lib/chunk-recovery'
import './index.css'

// Reflect the configured panel title (injected by the backend as a meta tag)
// in the browser tab. The static <title> only covers a build served without
// the backend injection.
document.title = getAdminTitle();

// Initialize theme before React renders (avoids CSP inline script issue)
(function () {
  let theme = localStorage.getItem('gf-theme')
  if (!theme) {
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
    theme = prefersDark ? 'dark' : 'light'
  }
  localStorage.setItem('gf-theme', theme)
  if (theme === 'dark') document.documentElement.classList.add('dark')
  else document.documentElement.classList.remove('dark')
})()

// A chunk or stylesheet that fails to load (the tab predates the binary now
// serving it) reloads the page once; see src/lib/chunk-recovery.ts.
installPreloadErrorReload()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
