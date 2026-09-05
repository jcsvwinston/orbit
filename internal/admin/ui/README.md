# Orbit admin UI

The single-page app behind Orbit's in-process admin panel (`orbit.Module`).
It is built here, committed as `dist/`, and embedded into the Go binary with
`//go:embed all:ui/dist` (see `internal/admin/ui_fallback.go`), so consumers
get the panel as a normal Go dependency with no Node toolchain.

## Stack

- Vite 8 (rolldown) + React 19 + TypeScript 5.9
- Tailwind CSS 3 (`src/index.css` holds the theme tokens); PostCSS is
  configured inline in `vite.config.ts`
- Base UI (`@base-ui/react`) for dialogs and buttons, `lucide-react` icons
- AG Grid Community (Data Studio grid), Recharts (System Pulse)
- Zustand (auth / theme stores), React Router 7
- ESLint 10 (flat config, `typescript-eslint` + `react-hooks`) and Vitest +
  Testing Library

## Commands

```bash
cd internal/admin/ui
npm ci            # reproducible install (package-lock.json is the source of truth)
npm run dev       # Vite dev server; point it at a running app with a proxy if needed
npm run typecheck # tsc -b
npm run lint      # eslint .
npm test          # vitest run
npm run build     # tsc -b && vite build  -> dist/ (COMMIT the result)
```

`dist/` is tracked on purpose. CI (`.github/workflows/ci.yml`, job
`admin-ui`) runs lint, tests and the build, then fails if the rebuilt `dist/`
differs from the committed one — after changing anything under `src/`,
rebuild and commit `dist/` in the same change.

## Code splitting

Feature pages are loaded on demand. `src/routes.ts` lists every route
mounted under `DashboardLayout` with a dynamic `import()` of its page;
`App.tsx` wraps each one in `React.lazy`, and the `Suspense` boundary around
the `<Outlet />` in `DashboardLayout` shows a spinner in the content area
while the chunk loads. The login page, the layout and the Overview landing
stay in the entry chunk because they render on every sign-in.

A chunk can also fail to load, which a single bundle never could: a tab
opened before the binary was upgraded still holds the old `index.html`, and
its first visit to a page it has not loaded yet asks for a file the new
binary does not ship (404). `RouteErrorBoundary` (`components/layout/`)
wraps the `Suspense` so the failure replaces only the content area — without
it React 19 unmounts the whole tree and the panel goes blank. The first such
failure in a browser session reloads the page, which fetches the current
`index.html` (`lib/chunk-recovery.ts`; `main.tsx` applies the same rule to
Vite's `vite:preloadError` event); a later one shows an error with a
**Reload** button, since `React.lazy` remembers a rejected loader for good.
A page that throws while rendering lands in the same boundary with **Try
again**. The boundary is keyed by pathname, so navigating elsewhere starts
clean.

Consequences to keep in mind:

- A new page goes into `src/routes.ts` (with a `default` export), not as a
  static import in `App.tsx`. `src/routes.test.ts` resolves every loader and
  `internal/admin/ui_embed_test.go` keeps the assets `index.html` loads
  directly under a size budget, so a page imported eagerly by mistake fails
  the Go tests. `src/App.test.tsx` drives the layout with mocked loaders
  (pending, rejected, throwing page) and pins where the boundaries sit.
- Vite emits one JS (and, for Data Studio, one CSS) file per page under
  `dist/assets/`; all of them are embedded and served under the mount prefix
  by the same handlers as the entry, and the entry resolves them relative to
  its own URL, so no prefix-specific work is needed.
- `vite.config.ts` pins React, the router and the lucide icons to the
  `vendor` and `icons` chunks. Rolldown groups capture a module's
  dependencies too, so do not add a group for a library the entry does not
  use (a Recharts group used to drag `clsx` and a React helper into a chunk
  the entry then had to load).
- The AG Grid stylesheet ships the `ag-theme-quartz`, `-dark` and
  `-auto-dark` variants sharing one selector list; the panel only ever
  applies the first two, so `tools/postcss-strip-unused-grid-themes.ts`
  drops the third at build time. Replacing the stylesheet imports with AG
  Grid's Theming API waits for the major upgrade (the API is a preview on
  the 32.x line the panel pins).

While iterating on the SPA against a running app you can bypass the embedded
copy: set `NUCLEUS_ADMIN_UI_DIR=/path/to/internal/admin/ui/dist` and the panel
serves that directory instead.

## Runtime configuration

The Go side injects two `<meta>` tags into every served page:

- `nucleus-admin-prefix` — the mount prefix (`orbit.Config.Prefix`, default
  `/admin`). `src/config.ts` reads it and every API call and router path is
  built from it, so the SPA works at any prefix without a rebuild.
- `nucleus-admin-title` — the panel title (`orbit.Config.Title`).

Login feedback (`nucleus-admin-login-error` / `-info`) travels the same way.

## Layout

```
src/
├── App.tsx                 # routes; ProtectedRoute gates on the session check (+ test)
├── routes.ts               # lazy feature pages (path -> dynamic import) (+ test)
├── config.ts               # prefix/title from the injected meta tags
├── components/
│   ├── layout/             # DashboardLayout (sidebar, theme, sign out, Suspense), RouteErrorBoundary
│   └── ui/                 # button, dialog, toast, table, error-state, route-fallback, ...
├── features/
│   ├── auth/               # /login
│   ├── overview/           # /            models + runtime summary
│   ├── data-studio/        # /data-studio grid, record form, import, field config
│   │   └── lib/            # loader hook, field value codecs, constants (+ tests)
│   ├── system/             # /system     runtime snapshot + charts
│   ├── network/            # /live       HTTP/SQL feed over WebSocket
│   ├── infra/              # /sessions   active admin sessions
│   ├── health/             # /health
│   ├── rbac/               # /rbac       policies with allow/deny effect
│   └── audit/              # /audit      filtered, paginated audit log
├── lib/                    # utils, datetime codecs, chunk recovery (one reload per session) (+ tests)
├── services/api.ts         # every backend call; throws ApiError{status, body}
├── stores/                 # zustand: auth (session known, identity NOT known), theme
├── types/                  # backend contracts
└── test/                   # vitest setup (jest-dom, storage shim), location.reload stub
tools/
└── postcss-strip-unused-grid-themes.ts   # build-time CSS pass (+ test)
```

## Backend contract notes

The SPA talks only to `/api/*` under the admin prefix (routes are registered
in `internal/admin/panel.go`, `mountAPIRoutes`). Things worth knowing when
touching `services/api.ts`:

- Errors arrive as `{"error": {"code", "message"}}` or `{"error": "..."}`;
  `fetchAPI` turns both into `ApiError` so pages can show the message and
  treat 403 as "no permission" (`components/ui/error-state.tsx`).
- There is no identity endpoint: the auth store only knows whether the
  session cookie is accepted (`GET /api/models`), never who is signed in.
- `GET /api/models/{name}` caps `page_size` at 200 and sorts with
  `order_by=<column> <asc|desc>`; the grid sorts server-side and appends
  pages on "Load more".
- Bulk delete (`POST /api/models/{name}/bulk`) takes ids as strings (numbers
  are still accepted); a UUID key travels like an integer one, and the ids
  the backend cannot narrow or reach come back per id in `errors[]`
  (`{id: string, error}`), not as a failed request.
- `GET /api/models/{name}?search=` answers 400 on a model with no
  searchable field; the grid reads `is_search` from the schema and disables
  the search box for those models.
- Import is three calls: `POST /api/imports` (multipart, returns `key` and
  detected `format`), `POST /api/import/validate?key=` and
  `POST /api/import/execute?key=`, both with `{model, format}` — nothing is
  written until execute returns.
- RBAC policies carry `eft` (`allow` | `deny`); the audit log filters by
  `user_id`, `model`, `action` and pages with `page` / `page_size`.

## Debugging a login loop

1. Make sure an admin user exists (`ADMIN_BOOTSTRAP_PASSWORD` on first run of
   the example, or `nucleus createuser`).
2. In DevTools → Network, `POST <prefix>/login` should answer `303` with a
   `Set-Cookie`; a `Secure` cookie over plain HTTP is silently dropped by the
   browser (`session_cookie_secure: false` in dev).
3. `fetchAPI` redirects to `<prefix>/login` on any 401 or on a response whose
   final URL is the login page; that redirect is the usual source of loops.
