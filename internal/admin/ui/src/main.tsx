import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App.tsx'
import { getAdminTitle } from './config'
import './index.css'

// Reflect the configured panel title (injected by the backend as a meta tag)
// in the browser tab. The static <title> only covers a build served without
// the backend injection.
document.title = getAdminTitle();

// Initialize theme before React renders (avoids CSP inline script issue)
(function () {
  var theme = localStorage.getItem('gf-theme')
  if (!theme) {
    var prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
    theme = prefersDark ? 'dark' : 'light'
  }
  localStorage.setItem('gf-theme', theme)
  if (theme === 'dark') document.documentElement.classList.add('dark')
  else document.documentElement.classList.remove('dark')
})()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
