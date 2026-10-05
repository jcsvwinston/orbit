import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App.tsx'
import { getAdminTitle } from './config'
import { applyBranding } from './lib/branding'
import { useMessages } from './stores/messagesStore'
import { installPreloadErrorReload } from './lib/chunk-recovery'
import { applyInitialTheme, useTheme } from './stores/themeStore'
import './index.css'

// Reflect the configured panel title (injected by the backend as a meta tag)
// in the browser tab. The static <title> only covers a build served without
// the backend injection.
document.title = getAdminTitle();

// The application's logo, colour and favicon, painted before React renders so
// the first frame is already the product's (src/lib/branding.ts).
applyBranding()

// The chrome's phrases in the declared language. The fetch is not awaited:
// an English panel renders immediately and re-renders translated when the
// catalogue lands (src/stores/messagesStore.ts).
void useMessages.getState().load();

// The theme of the first frame, before React renders. When the application
// configured one, the document already applied it — or the operator's own
// choice — ahead of this bundle, and this leaves it alone; otherwise it is
// decided here as it always was (src/stores/themeStore.ts).
applyInitialTheme()
useTheme.getState().initTheme()

// A chunk or stylesheet that fails to load (the tab predates the binary now
// serving it) reloads the page once, when the server answers; see
// src/lib/chunk-recovery.ts.
installPreloadErrorReload()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
