/**
 * Recovery for a feature page whose chunk fails to load.
 *
 * Feature pages are separate files under dist/assets with a content hash in
 * their name (src/routes.ts). A panel tab opened before the embedding binary
 * was upgraded still holds the old index.html, so its first navigation to a
 * page it has not visited yet asks for a chunk the new binary no longer
 * ships: the server answers 404, the dynamic import rejects and, left alone,
 * React 19 would unmount the whole panel. Reloading fetches the current
 * index.html, whose chunk names exist again.
 *
 * The reload waits for the server to answer. A chunk fails to load just as
 * well while the process serving the panel is restarting — the redeploy
 * window itself — and a reload then cannot fetch index.html: it would
 * replace the whole panel with the browser's connection-error page, where
 * the boundary's error state keeps the sidebar and the pages already loaded.
 * So reloadOnce first sends a HEAD request for the current document, the
 * very thing the reload will ask for, and reloads only when that comes back
 * with a status below 500. A request that fails means the server cannot be
 * reached; a 5xx is what a reverse proxy answers while its upstream
 * restarts. Either way the caller shows the error state, whose Reload button
 * is there for when the server is back, and nothing is spent: the next
 * failure probes again. While the browser reports itself offline the probe
 * is skipped, the outcome being known.
 *
 * The automatic reload is bounded: a sessionStorage flag records when the
 * last one started and counts as spent for RELOAD_SPENT_MS (a module
 * variable also remembers a reload already decided in this document), so a
 * chunk that keeps failing right after the reload — a proxy strips /assets,
 * the new build is broken — ends in an error state with a Reload button
 * rather than a reload loop. The flag ages out rather than living for the
 * tab's lifetime because the reloaded document often lands on a static page
 * (the upgrade invalidated the session: /rbac reloads into /login, then the
 * Overview), so no chunk ever loads to clear it, and a tab kept open across
 * a later upgrade would otherwise be denied its reload. A chunk that loads
 * clears the flag outright (markChunkLoaded).
 */

const RELOAD_FLAG = 'orbit-admin:chunk-reload'

/** How long after an automatic reload a further chunk failure shows the error instead of reloading again. */
export const RELOAD_SPENT_MS = 60_000

// Browsers word a failed dynamic import differently:
//   Chrome   TypeError: Failed to fetch dynamically imported module: <url>
//   Firefox  TypeError: error loading dynamically imported module: <url>
//   Safari   TypeError: Importing a module script failed.
// Vite's preload helper rejects a chunk whose stylesheet is missing with
//   Error: Unable to preload CSS for <url>
const CHUNK_LOAD_MESSAGE = /dynamically imported module|Importing a module script failed|Unable to preload CSS/i

/** True for the error a dynamic import rejects with when its chunk or stylesheet did not load. */
export function isChunkLoadError(error: unknown): boolean {
  return error instanceof Error && CHUNK_LOAD_MESSAGE.test(error.message)
}

/**
 * The decision under way — the probe, then the reload — shared by every
 * failure reported until it settles. Null again once a probe found the
 * server not answering, so the next failure asks again; a decision that
 * ended in a reload stays, since the document is being replaced.
 */
let decision: Promise<boolean> | null = null

function reloadedRecently(): boolean {
  try {
    const startedAt = Number(sessionStorage.getItem(RELOAD_FLAG))
    // A missing flag reads as 0 and an unparsable one as NaN; neither is a
    // recent reload. A clock that went backwards counts as recent, which
    // errs on the side of not reloading.
    return Number.isFinite(startedAt) && Date.now() - startedAt < RELOAD_SPENT_MS
  } catch {
    // Without storage there is no way to bound the reloads; behave as if the
    // one reload was spent so the failure surfaces as an error state.
    return true
  }
}

function offline(): boolean {
  return typeof navigator !== 'undefined' && navigator.onLine === false
}

/**
 * Whether the server can serve the document a reload would fetch: a HEAD
 * request for it comes back with a status below 500. A rejected request is
 * a server that cannot be reached; a 5xx one that answers for a server that
 * cannot serve yet (a reverse proxy during the restart).
 */
async function serverAnswers(): Promise<boolean> {
  try {
    const response = await fetch(window.location.href, {
      method: 'HEAD',
      cache: 'no-store',
      credentials: 'same-origin',
    })
    return response.status < 500
  } catch {
    return false
  }
}

/** Records the reload about to start; false when storage cannot, since the reload could not be bounded then. */
function spendReload(): boolean {
  try {
    sessionStorage.setItem(RELOAD_FLAG, String(Date.now()))
    return true
  } catch {
    return false
  }
}

/**
 * True when a reload is decided or being decided, or `error` warrants
 * deciding one. Reads only, so an error boundary can call it from
 * getDerivedStateFromError and keep its loading state up meanwhile;
 * reloadOnce settles to false if the probe then finds the server not
 * answering.
 */
export function reloadPending(error: unknown): boolean {
  return decision !== null || (isChunkLoadError(error) && !offline() && !reloadedRecently())
}

/**
 * Starts an automatic reload if `error` warrants one and the server
 * answers. Resolves to whether a reload is under way; false means the
 * caller should show the error.
 */
export function reloadOnce(error: unknown): Promise<boolean> {
  if (decision) return decision
  if (!reloadPending(error)) return Promise.resolve(false)
  decision = serverAnswers().then((answers) => {
    if (answers && spendReload()) {
      window.location.reload()
      return true
    }
    decision = null
    return false
  })
  return decision
}

/** A chunk loaded: the next failure is a new upgrade, not the same one, and gets its reload. */
export function markChunkLoaded(): void {
  try {
    sessionStorage.removeItem(RELOAD_FLAG)
  } catch {
    // Nothing to clear when storage is unavailable.
  }
}

/**
 * Vite's preload helper announces a failed chunk or stylesheet on `window`
 * before rejecting the import. The layout's error boundary handles that
 * rejection for the feature pages; this listener applies the same reload
 * rule to a dynamic import anywhere else in the tree. Returns a function
 * that removes the listener.
 */
export function installPreloadErrorReload(): () => void {
  const onPreloadError = (event: Event) => {
    void reloadOnce((event as Event & { payload?: unknown }).payload)
  }
  window.addEventListener('vite:preloadError', onPreloadError)
  return () => window.removeEventListener('vite:preloadError', onPreloadError)
}
