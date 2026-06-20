# Nucleus Admin UI

Modern admin panel built with React + Vite + Tailwind CSS + shadcn/ui.

## Tech Stack

- **Bundler**: Vite
- **Framework**: React 19 (TypeScript)
- **Styling**: Tailwind CSS 3
- **Components**: shadcn/ui
- **State Management**: Zustand
- **Routing**: React Router 7
- **Charts**: Recharts
- **Icons**: Lucide React

## Configuration

### Customizable Admin Prefix

By default, the admin panel is served at `/admin`. Applications can override this path to use `/admin` for their own panels and move the Nucleus admin panel to a different route.

**In your `nucleus.yml`:**

```yaml
# Move admin panel to /nucleus-admin
admin_prefix: /nucleus-admin
```

Or via environment variable:

```bash
NUCLEUS_ADMIN_PREFIX=/nucleus-admin
```

The frontend automatically reads the admin prefix from the backend injection and adjusts all API calls and routing accordingly.

## Development

```bash
# Install dependencies
npm install

# Start dev server
npm run dev

# Build for production
npm run build
```

## Project Structure

```
src/
├── app/                    # App configuration and routing
├── components/
│   ├── ui/                 # shadcn/ui components
│   └── layout/             # Layout components
├── features/               # Feature-based modules
│   ├── auth/               # Authentication
│   ├── overview/           # Dashboard overview
│   ├── data-studio/        # Data export/import
│   ├── system/             # System metrics
│   ├── network/            # Network inspector
│   ├── infra/              # Session management
│   ├── health/             # Health checks
│   ├── rbac/               # Access control
│   └── audit/              # Audit log
├── hooks/                  # Custom hooks
├── lib/                    # Utilities
├── services/               # API services
├── stores/                 # Zustand stores
├── types/                  # TypeScript types
└── main.tsx                # Entry point
```

## Features

- 🔐 **Authentication**: Login page with session management
- 📊 **Overview**: Dashboard with model statistics
- 💾 **Data Studio**: Export/Import data (CSV, JSON, SQL)
- 🖥️ **System Pulse**: Real-time Go runtime metrics
- 🌐 **Network Inspector**: Live HTTP traffic monitoring
- 👥 **Session Manager**: Active user session management
- ❤️ **Health Checks**: Service health monitoring
- 🛡️ **Access Control**: RBAC policy management
- 📝 **Audit Log**: Administrative action tracking

## Building for Production

The admin UI is automatically embedded into the Go binary. To rebuild:

```bash
# From the project root
./pkg/admin/build-ui.sh

# Or manually
cd pkg/admin/ui
npm install
npm run build
```

The built files will be placed in `ui/dist/` and embedded via Go's `//go:embed` directive.

## Customization

This admin panel is fully customizable. Modify:

- **Theme**: Edit `tailwind.config.js` and `src/index.css`
- **Components**: Modify `src/components/ui/` (shadcn/ui components)
- **Features**: Add new pages in `src/features/`
- **API**: Extend `src/services/api.ts`

## Architecture

Follows feature-based architecture:
- Each feature is self-contained with its own components, pages, and logic
- Global state managed via Zustand stores
- API calls centralized in services
- UI components use shadcn/ui for consistency

---

## 🔍 Debugging Guide

### Common Issues

#### Login Loop (No Error Shown)

If you're stuck in a login loop without seeing any error messages, follow these steps:

**1. Verify Admin User Exists**

The most common cause is a missing admin user. Create one:

```bash
go run . createuser --username admin --password YourPassword123! --email admin@localhost --no-input
```

To update an existing user's password:

```bash
go run . changepassword --username admin --password NewPassword123! --no-input
```

**2. Check Browser DevTools**

Open DevTools (F12) and inspect:

**Console Tab:**
- Look for JavaScript errors
- Check for failed API calls

**Network Tab:**
- Filter by `XHR` or `Fetch` requests
- Look at POST `/admin/login` request:
  - **Expected success:** Status `303 See Other` with `Set-Cookie` header
  - **Failed auth:** Status `401 Unauthorized` or `200` with error message
- Check if session cookie is being set:
  - Response headers should include `Set-Cookie: session=...; Path=/; HttpOnly`
  - If `Secure` flag is present but you're on HTTP, browser will reject the cookie

**Application Tab → Cookies:**
- Verify session cookie exists for `localhost:8080`
- Check cookie attributes:
  - `Secure` should be **unchecked** for HTTP (development)
  - `HttpOnly` should be **checked**
  - `SameSite` should be `Lax` or `Strict`

**3. Check Session Store Configuration**

If using database or Redis for sessions, verify the store is accessible:

```bash
# Check application health
go run . health
```

Look for:
- `deploy.session_cookie_secure`: Should be `error` (green/ok) for HTTP development
- `deploy.session_store`: Should show `memory` for development

**4. Enable Debug Logging**

Set environment variable to increase logging:

```bash
NUCLEUS_DEBUG=true go run . serve
```

Or use the `--config` flag with a custom config file:

```yaml
# nucleus-debug.yaml
env: development
debug: true
log_level: debug
```

```bash
go run . serve --config nucleus-debug.yaml
```

**5. Test Authentication Manually**

Use curl to test the login flow:

```bash
# Step 1: Login (should return 303 redirect)
curl -v -X POST http://localhost:8080/admin/login \
  -d "username=admin&password=YourPassword123!" \
  -c /tmp/cookies.txt \
  -L

# Step 2: Use the cookie to access protected route
curl -v http://localhost:8080/admin/api/models \
  -b /tmp/cookies.txt
```

Expected:
- First request: `HTTP/1.1 303 See Other` with `Set-Cookie` header
- Second request: `HTTP/1.1 200 OK` with JSON response

If second request returns `302` to `/admin/login`, the session is not persisting.

**6. Check Database for Admin User**

If using SQLite (default), query the database directly:

```bash
# If using sqlite3 CLI
sqlite3 nucleus.db "SELECT id, username, email, is_superuser FROM nucleus_admin_users;"
```

Expected output:
```
u_20260413120000_abc123|admin|admin@localhost|1
```

If the table or user is missing, run the `createuser` command above.

### Frontend Debugging

**Enable React DevTools:**
- Install [React Developer Tools](https://chromewebstore.google.com/detail/react-develop-tools) browser extension
- Inspect component state in DevTools → Components tab

**Check Auth State:**
- Open browser console and run:
```javascript
// Check authentication state (if using Zustand DevTools)
// This requires Zustand devtools middleware to be enabled
```

**Inspect Network Requests:**
In `src/services/api.ts`, the `fetchAPI()` function handles all API calls:
- Line 18-20: On 401 response, redirects to `/admin/login`
- This is the most common source of login loops

**Check Auth Store:**
In `src/stores/authStore.ts`:
- `checkAuth()` calls `getCurrentUser()` on app initialization
- If this returns `null`, sets `isAuthenticated: false`
- `ProtectedRoute` component then redirects to `/login`

### Backend Debugging

**Authentication Flow:**
1. `authMiddleware()` in `pkg/admin/panel.go` calls `Authenticate()`
2. `Authenticate()` in `pkg/admin/default_auth.go`:
   - Checks session for `__nucleus_admin_user_id`
   - If missing, returns error → redirects to `/admin/login`
   - If found, looks up user in database
   - If user not found, destroys session → redirects to `/admin/login`

**Session Handling:**
1. Login POST in `handleLoginPOST()`:
   - Validates credentials against `nucleus_admin_users` table
   - Calls `session.RenewToken()` to prevent session fixation
   - Stores user ID, username, email in session
   - Redirects to `next` URL (default: `/admin/`)

2. Session middleware in `pkg/auth/session.go`:
   - Uses `alexedwards/scs` for server-side sessions
   - Default store: in-memory (lost on server restart)
   - Cookie config: `HttpOnly=true`, `Secure=false` (development)

### Quick Diagnostic Commands

```bash
# 1. Check if server is running
curl -I http://localhost:8080/admin/login

# 2. Verify admin user exists
go run . createuser --username admin --password test --email test@test.com --no-input

# 3. Check health status
go run . health

# 4. View server logs
NUCLEUS_LOG_LEVEL=debug go run . serve

# 5. Test with fresh session (clear browser cookies first)
curl -c /tmp/gf_cookies.txt -X POST http://localhost:8080/admin/login \
  -d "username=admin&password=YourPassword"
```

### Common Fixes

| Symptom | Cause | Fix |
|---------|-------|-----|
| Login redirects back to `/login` with no error | No admin user exists | Run `go run . createuser ...` |
| Login works but session lost on refresh | Session store not persisting | Check `session_store` config; use `memory` for dev |
| Cookie not being set | `Secure` flag on HTTP | Set `session_cookie_secure: false` in config |
| 401 on all API calls | Session middleware not in chain | Verify `sessionManager.Middleware()` is applied |
| Blank page after login | SPA not building correctly | Run `npm run build` in `pkg/admin/ui/` |
| Admin panel not accessible at custom path | `admin_prefix` not configured | Set `admin_prefix` in `nucleus.yml` |
| API calls fail with 404 after custom prefix | Frontend not rebuilt | Run `npm run build` and restart server |

### Custom Admin Prefix

If you're using a custom admin prefix (e.g., `/nucleus-admin`):

1. **Configure in `nucleus.yml`:**
   ```yaml
   admin_prefix: /nucleus-admin
   ```

2. **Access the admin panel at the new path:**
   - Login: `http://localhost:8080/nucleus-admin/login`
   - Dashboard: `http://localhost:8080/nucleus-admin/`

3. **Rebuild the admin UI** (if making frontend changes):
   ```bash
   cd pkg/admin/ui
   npm run build
   ```

4. **The frontend automatically adjusts:**
   - All API calls use the configured prefix
   - React Router uses the prefix as `basename`
   - Login redirects use the correct path

### Reporting Issues

When reporting authentication issues, include:

1. **Browser console logs** (DevTools → Console)
2. **Network requests** (DevTools → Network → export as HAR)
3. **Server logs** with `log_level: debug`
4. **Health check output**: `go run . health`
5. **Admin user status**: `go run . createuser --username admin --email admin@localhost --no-input` (shows if user exists/updated)
