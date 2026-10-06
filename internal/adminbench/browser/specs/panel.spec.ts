import { expect, test, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { readFileSync } from 'node:fs'

/**
 * What the panel does in a browser — the half the Go bench cannot measure.
 *
 * Every test here is one control of the bench, named the way the Go cases are
 * (UIX-NN), and asserts the SAME way: by effect, on the rendered page. A
 * check that only asked whether an attribute exists would be the config-key
 * trap the Go bench recorded, one layer up.
 *
 * The verdicts are recorded in Go (browserbench_test.go): a control that
 * gains or loses ground turns the suite red until the recorded verdict moves
 * with it.
 */

const USERNAME = process.env.ORBIT_BENCH_USER ?? 'admin'

/** Data Studio's grid, by its role: AG Grid 36 says `grid`, the 32 line the
 * panel drew before A12 O1 said `treegrid`. */
const GRID = ':is([role="grid"], [role="treegrid"])'
/** Its data rows: AG Grid marks each with the id the panel gives it
 * (getRowId). */
const GRID_ROWS = `${GRID} [row-id]`
const PASSWORD = process.env.ORBIT_BENCH_PASSWORD ?? ''

/** signIn goes through the panel's own login form, because that is how an
 * operator arrives and because the login screen is itself under test. */
async function signIn(page: Page): Promise<void> {
  await page.goto('/admin/login')
  await page.getByLabel(/user/i).or(page.locator('input[name="username"]')).first().fill(USERNAME)
  await page.locator('input[type="password"]').first().fill(PASSWORD)
  await page.locator('button[type="submit"], input[type="submit"]').first().click()
  await page.waitForURL(/\/admin\/?$/, { timeout: 15_000 })
}

/** signInAs signs in to the bench's application as another operator than
 * the bootstrap admin, through the same form. */
async function signInAs(page: Page, username: string, password: string): Promise<void> {
  await page.goto('/admin/login')
  await page.locator('input[name="username"]').first().fill(username)
  await page.locator('input[type="password"]').first().fill(password)
  await page.locator('button[type="submit"], input[type="submit"]').first().click()
  await page.waitForURL(/\/admin\/?$/, { timeout: 15_000 })
}

/** signInAt signs in to another application than the bench's own, through
 * its login form. */
async function signInAt(page: Page, base: string): Promise<void> {
  await page.goto(`${base}/admin/login`)
  await page.locator('input[name="username"]').first().fill(USERNAME)
  await page.locator('input[type="password"]').first().fill(PASSWORD)
  await page.locator('button[type="submit"], input[type="submit"]').first().click()
  await page.waitForURL(`${base}/admin/`, { timeout: 15_000 })
}

/** The operators UIX-16 to UIX-18 sign in as, created by the driver: the
 * viewer may list and open a note and nothing more; the actor also holds
 * delete (not bulk_delete), schedule, duplicate, the panel's export
 * (export_data) and import (import_data) and create on Note, may not read a
 * note's meta or views, and may list their own articles and no other; the
 * lister may list the notes and not open one, and holds the panel's import
 * with no write of Note. None holds update or update_schema. */
function partialOperators(control: string): { viewer: string; actor: string; lister: string; password: string } {
  const viewer = process.env.ORBIT_BENCH_VIEWER_USER ?? ''
  const actor = process.env.ORBIT_BENCH_ACTOR_USER ?? ''
  const lister = process.env.ORBIT_BENCH_LISTER_USER ?? ''
  const password = process.env.ORBIT_BENCH_OPERATOR_PASSWORD ?? ''
  if (!viewer || !actor || !lister || !password) {
    throw new Error(`${control} precondition: the driver passed no operators (ORBIT_BENCH_VIEWER_USER, ORBIT_BENCH_ACTOR_USER, ORBIT_BENCH_LISTER_USER, ORBIT_BENCH_OPERATOR_PASSWORD)`)
  }
  return { viewer, actor, lister, password }
}

/** noteAsAdmin creates one note as the bootstrap admin — the operators
 * under test may not — and signs out again. */
async function noteAsAdmin(page: Page, fields: { title: string; body: string }, control: string): Promise<string> {
  await signIn(page)
  const created = await page.evaluate(async (note) => {
    const r = await fetch('/admin/api/models/Note', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify({ ...note, status: 'draft' }),
    })
    const out = await r.json().catch(() => ({}))
    return { status: r.status, id: String(out?.data?.id ?? out?.id ?? '') }
  }, fields)
  expect(created.status < 300 && created.id !== '', `${control} precondition: creating a note answered ${created.status}`).toBe(true)
  await signOut(page)
  return created.id
}

/** signOut leaves the panel before its session goes: a screen still open
 * when the cookie is cleared answers its next 401 by navigating to the
 * login page itself, and that navigation aborts the one the next sign-in
 * starts. */
async function signOut(page: Page): Promise<void> {
  await page.goto('about:blank')
  await page.context().clearCookies()
}

/** heldOnNote is what the schema a screen loads says this operator holds on
 * Note, and the actions it offers them: the hints the grid draws from. */
async function heldOnNote(page: Page): Promise<{ permissions: Record<string, boolean>; actions: string[] }> {
  return page.evaluate(async () => {
    const r = await fetch('/admin/api/models/Note/schema', { credentials: 'same-origin' })
    const schema = r.ok ? await r.json() : {}
    return {
      permissions: schema.permissions ?? {},
      actions: (schema.actions ?? []).map((a: { name: string }) => a.name).sort(),
    }
  })
}

/** forced makes, from the page, a call the screen did not offer: the
 * operator's own session, the way a request typed into the console or
 * replayed from another tab arrives. */
async function forced(page: Page, method: string, path: string, body?: unknown): Promise<number> {
  return page.evaluate(async ({ method, path, body }) => {
    const r = await fetch(path, {
      method,
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    return r.status
  }, { method, path, body })
}

/** importAs uploads rows as a JSON file and runs one step of the import of
 * Note (validate or execute) with it, from the page: the operator's own
 * session, the way the import dialog makes the same three calls. */
async function importAs(page: Page, step: 'validate' | 'execute', rows: unknown[], onConflict = ''): Promise<{ status: number; message: string }> {
  return page.evaluate(async ({ step, rows, onConflict }) => {
    const form = new FormData()
    form.append('file', new Blob([JSON.stringify(rows)], { type: 'application/json' }), 'uix-18.json')
    const up = await fetch('/admin/api/imports', { method: 'POST', body: form, credentials: 'same-origin' })
    const uploaded = await up.json().catch(() => ({}))
    if (up.status !== 201 || !uploaded?.key) return { status: up.status, message: `upload answered ${up.status}` }
    const r = await fetch(`/admin/api/import/${step}?key=${encodeURIComponent(uploaded.key)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify({ model: 'Note', format: 'json', on_conflict: onConflict }),
    })
    const out = await r.json().catch(() => ({}))
    return { status: r.status, message: String(out?.error?.message ?? '') }
  }, { step, rows, onConflict })
}

/** notesTitled counts, through the operator's own list, the notes whose
 * title is title. */
async function notesTitled(page: Page, title: string): Promise<number> {
  return page.evaluate(async (title) => {
    const r = await fetch(`/admin/api/models/Note?search=${encodeURIComponent(title)}`, { credentials: 'same-origin' })
    const out = r.ok ? await r.json() : {}
    return ((out.items ?? []) as Array<{ title?: string }>).filter((n) => n.title === title).length
  }, title)
}

/** narrowToNote opens Data Studio on Notes narrowed to one note, as the
 * operator signed in. */
async function narrowToNote(page: Page, title: string, who: string, control: string): Promise<void> {
  await page.goto('/admin/data-studio')
  await page.waitForLoadState('networkidle')
  await page.getByRole('button', { name: /^Notes/ }).first().click({ timeout: 15_000 })
  await page.getByRole('textbox', { name: 'Search records' }).fill(title)
  await page.keyboard.press('Enter')
  await expect(page.locator(GRID_ROWS), `${control} precondition: the search did not narrow the ${who}'s grid to the note`)
    .toHaveCount(1, { timeout: 10_000 })
}

/** openNote is narrowToNote for an operator who may open the record. The
 * row's History button is the one every such operator is offered (it asks
 * the same verb, retrieve): seeing it is what says the actions column was
 * drawn, so a button missing beside it is missing, not unrendered. */
async function openNote(page: Page, title: string, id: string, who: string, control: string): Promise<void> {
  await narrowToNote(page, title, who, control)
  await expect(page.getByRole('button', { name: `History of record ${id}` }), `${control} precondition: the ${who}'s row draws no actions column`)
    .toBeVisible({ timeout: 10_000 })
  await page.waitForFunction(() => document.getAnimations().length === 0, undefined, { timeout: 10_000 })
}

/** The rules an operator's own view is held to: the ones UIX-02 and UIX-03
 * hold the panel's screens to, on the screen this operator is shown. */
const OPERATOR_VIEW_RULES = [
  'color-contrast',
  'button-name',
  'link-name',
  'image-alt',
  'label',
  'aria-allowed-attr',
  'aria-required-attr',
  'aria-required-children',
  'aria-valid-attr-value',
  'aria-hidden-focus',
]

/** axeIn runs the accessibility engine over one part of the page. */
async function axeIn(page: Page, selector: string, rules: string[]) {
  const results = await new AxeBuilder({ page }).include(selector).withRules(rules).analyze()
  return results.violations.map((v) => ({ id: v.id, nodes: v.nodes.slice(0, 3).map((n) => `${n.target.join(' ')} — ${n.any[0]?.message ?? ''}`) }))
}

/** axeViolations runs the accessibility engine over the current page and
 * returns the violations of the rules this control is about. */
async function axeViolations(page: Page, rules: string[]) {
  const results = await new AxeBuilder({ page }).withRules(rules).analyze()
  return results.violations.map((v) => ({
    id: v.id,
    impact: v.impact,
    nodes: v.nodes.slice(0, 3).map((n) => n.target.join(' ')),
  }))
}

type Painted = { dark: boolean; background: string; rendered: number; at: number }
type ThemeChange = { t: number; dark: boolean }
type Frame = { held: number; before: Painted; after: Painted; changes: ThemeChange[]; firstPaint: number | null }

/** recordThemeChanges keeps, from before each document exists, every change
 * of the root between dark and light, with the time it happened. */
async function recordThemeChanges(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const changes: { t: number; dark: boolean }[] = []
    ;(window as unknown as { __themeChanges: typeof changes }).__themeChanges = changes
    let last: boolean | null = null
    const record = () => {
      const root = document.documentElement
      if (!root) return
      const dark = root.classList.contains('dark')
      if (dark !== last) {
        changes.push({ t: performance.now(), dark })
        last = dark
      }
    }
    new MutationObserver(record).observe(document, { subtree: true, childList: true, attributes: true, attributeFilter: ['class'] })
    record()
  })
}

/** firstFrame opens url with the panel's bundle held at the network, reads
 * what the browser can paint before it runs, lets it through, and reads what
 * the panel shows once it has rendered. */
async function firstFrame(page: Page, url: string, rendered: () => Promise<unknown>): Promise<Frame> {
  let held = 0
  let release: () => void = () => {}
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  const bundle = /\/assets\/index-[^/]+\.js$/
  await page.route(bundle, async (route) => {
    held++
    await gate
    await route.continue()
  })
  // Every other script the document loads — anything that is not the
  // bundle and its chunks — arrives late. One that stops the parser keeps
  // the browser from painting until it has run; one that does not (a module,
  // async, defer) loses the race to the first paint, and the frame shows it.
  const late = (url: URL) => url.pathname.endsWith('.js') && !url.pathname.includes('/assets/')
  await page.route(late, async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 400))
    await route.continue()
  })
  const painted = () => ({
    dark: document.documentElement.classList.contains('dark'),
    background: getComputedStyle(document.body).backgroundColor,
    rendered: document.getElementById('root')?.childElementCount ?? 0,
    at: performance.now(),
  })
  await page.goto(url, { waitUntil: 'commit' })
  // Parsed and styled: the body exists and the panel's stylesheet applies.
  await page.waitForFunction(
    () =>
      document.readyState !== 'loading' &&
      Array.from(document.styleSheets).some((sheet) => (sheet.href ?? '').includes('/assets/index-')),
    undefined,
    { polling: 50, timeout: 15_000 },
  )
  const before = await page.evaluate(painted)
  release()
  await rendered()
  await page.unroute(bundle)
  await page.unroute(late)
  const after = await page.evaluate(painted)
  const changes = await page.evaluate(() => (window as unknown as { __themeChanges: { t: number; dark: boolean }[] }).__themeChanges ?? [])
  const firstPaint = await page.evaluate(
    () => performance.getEntriesByType('paint').find((entry) => entry.name === 'first-paint')?.startTime ?? null,
  )
  return { held, before, after, changes, firstPaint }
}

function rgbOf(css: string): number[] {
  if (css.startsWith('#')) {
    return [1, 3, 5].map((i) => parseInt(css.slice(i, i + 2), 16))
  }
  return (css.match(/\d+(\.\d+)?/g) ?? []).slice(0, 3).map(Number)
}

function sameColour(a: string, b: string): boolean {
  const [x, y] = [rgbOf(a), rgbOf(b)]
  return x.length === 3 && y.length === 3 && x.every((v, i) => Math.abs(v - y[i]) <= 2)
}

/** expectFrame: the frame the browser could paint before the bundle ran is
 * in the theme wanted and on its surface, the panel the bundle rendered is
 * still in it, and nothing switched it in between. */
function expectFrame(frame: Frame, want: { dark: boolean; surface: string }, where: string): void {
  const name = (dark: boolean) => (dark ? 'dark' : 'light')
  expect(frame.held, `UIX-10 precondition: ${where}: the bundle was never held, so nothing was measured before it ran`).toBeGreaterThan(0)
  expect(frame.before.rendered, `UIX-10 precondition: ${where}: the panel rendered while its bundle was held`).toBe(0)
  expect(frame.firstPaint, `UIX-10 precondition: ${where}: the browser reported no first paint`).not.toBeNull()

  expect(frame.before.dark, `UIX-10: ${where}: before the bundle ran the document is ${name(frame.before.dark)}, and should be ${name(want.dark)}`).toBe(want.dark)
  expect(
    sameColour(frame.before.background, want.surface),
    `UIX-10: ${where}: before the bundle ran the browser paints ${frame.before.background}, the ${name(want.dark)} surface is ${want.surface}`,
  ).toBe(true)
  const atFirstPaint = frame.changes.filter((c) => c.t <= (frame.firstPaint ?? 0)).pop()
  expect(atFirstPaint?.dark, `UIX-10: ${where}: the first paint was ${name(atFirstPaint?.dark ?? false)}, and should be ${name(want.dark)}`).toBe(want.dark)
  const switched = frame.changes.filter((c) => c.t >= (frame.firstPaint ?? 0) && c.dark !== want.dark)
  expect(switched, `UIX-10: ${where}: the theme switched after the first paint: ${JSON.stringify(frame.changes)}`).toEqual([])
  expect(frame.after.dark, `UIX-10: ${where}: once the panel rendered it is ${name(frame.after.dark)}, and should be ${name(want.dark)}`).toBe(want.dark)
  expect(
    sameColour(frame.after.background, want.surface),
    `UIX-10: ${where}: once the panel rendered the browser paints ${frame.after.background}, the surface is ${want.surface}`,
  ).toBe(true)
}

test.describe('UIX', () => {
  /**
   * UIX-00 measures the INSTRUMENT, not the panel.
   *
   * An accessibility engine that is misconfigured — a rule name that no
   * longer exists, an analyze() that never ran — reports zero violations,
   * and every control below then passes by measuring nothing. That is the
   * same failure the umbrella's guard-of-guards exists for: a check nobody
   * checked. So this one injects a violation the engine must catch, and
   * fails if it does not.
   */
  test('UIX-00 the instrument bites: a planted violation is caught', async ({ page }) => {
    await page.goto('/admin/login')
    await page.evaluate(() => {
      const planted = document.createElement('div')
      planted.id = 'planted-violation'
      // A button with no accessible name and text nobody can read: two
      // rules, so a single renamed rule does not silence this test.
      planted.innerHTML =
        '<button id="planted-button"></button>' +
        '<p id="planted-text" style="color:#eeeeee;background:#ffffff">unreadable</p>'
      document.body.appendChild(planted)
    })

    const violations = await axeViolations(page, ['button-name', 'color-contrast'])
    expect(
      violations.map((v) => v.id).sort(),
      'the accessibility engine found nothing wrong with a button that has no name and text at 1.1:1 — it is not measuring',
    ).toEqual(['button-name', 'color-contrast'])

    await page.evaluate(() => document.getElementById('planted-violation')?.remove())
  })

  test('UIX-01 the login screen is legible: text meets contrast', async ({ page }) => {
    await page.goto('/admin/login')
    const violations = await axeViolations(page, ['color-contrast'])
    expect(violations, JSON.stringify(violations, null, 2)).toEqual([])
  })

  test('UIX-02 the panel is legible: text meets contrast on the screens an operator opens', async ({ page }) => {
    await signIn(page)
    for (const path of ['/admin/', '/admin/data-studio', '/admin/audit']) {
      await page.goto(path)
      await page.waitForLoadState('networkidle')
      const violations = await axeViolations(page, ['color-contrast'])
      expect(violations, `${path}: ${JSON.stringify(violations, null, 2)}`).toEqual([])
    }
  })

  test('UIX-03 every control says what it is: names, roles and labels', async ({ page }) => {
    await signIn(page)
    const violations = await axeViolations(page, [
      'button-name',
      'link-name',
      'image-alt',
      'label',
      'aria-allowed-attr',
      'aria-required-attr',
      'aria-valid-attr-value',
      'aria-hidden-focus',
    ])
    expect(violations, JSON.stringify(violations, null, 2)).toEqual([])
  })

  test('UIX-04 the document says what it is: language, landmarks and one main heading', async ({ page }) => {
    await signIn(page)
    const violations = await axeViolations(page, ['html-has-lang', 'html-lang-valid', 'landmark-one-main', 'page-has-heading-one', 'region'])
    expect(violations, JSON.stringify(violations, null, 2)).toEqual([])
  })

  test('UIX-05 the keyboard reaches the navigation, and the focus is visible', async ({ page }) => {
    await signIn(page)
    await page.waitForLoadState('networkidle')

    // Walk in from the top of the document. Twenty tabs is more than the
    // chrome has; if the navigation is not reachable within them it is not
    // reachable by keyboard at all.
    let reached: string | null = null
    for (let i = 0; i < 20 && !reached; i++) {
      await page.keyboard.press('Tab')
      reached = await page.evaluate(() => {
        const active = document.activeElement as HTMLElement | null
        if (!active) return null
        const link = active.closest('a[href]') as HTMLAnchorElement | null
        return link && link.getAttribute('href')?.includes('data-studio') ? link.getAttribute('href') : null
      })
    }
    expect(reached, 'Tab never reached the Data Studio link').not.toBeNull()

    // A focused control has to LOOK focused: a keyboard user who cannot see
    // where they are is navigating blind, and the panel draws its own
    // focus ring rather than relying on the one it suppresses.
    const visible = await page.evaluate(() => {
      const active = document.activeElement as HTMLElement | null
      if (!active) return false
      const style = getComputedStyle(active)
      const ring = style.getPropertyValue('box-shadow')
      const outline = style.getPropertyValue('outline-style')
      const outlineWidth = parseFloat(style.getPropertyValue('outline-width') || '0')
      return (outline !== 'none' && outlineWidth > 0) || (ring !== 'none' && ring.trim() !== '')
    })
    expect(visible, 'the focused element draws no visible focus indicator').toBe(true)

    // And Enter on it navigates: focus that cannot act is not reach.
    await page.keyboard.press('Enter')
    await page.waitForURL(/data-studio/, { timeout: 10_000 })
  })

  test('UIX-06 a dialog can be opened and dismissed from the keyboard', async ({ page }) => {
    await signIn(page)
    await page.goto('/admin/data-studio')
    await page.waitForLoadState('networkidle')

    // Pick a model — the grid only exists once one is chosen — and then
    // open the create form, the dialog every screen of this panel is built
    // on.
    await page.getByRole('button', { name: /^Notes/ }).first().click({ timeout: 15_000 })
    await page.getByRole('button', { name: /new record/i }).first().click({ timeout: 15_000 })
    const dialog = page.getByRole('dialog').first()
    await expect(dialog).toBeVisible()

    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden({ timeout: 5_000 })
  })

  /*
   * UIX-07 and UIX-08 are the browser half of the extension family (A11).
   * Both were recorded ABSENT at the arc's baseline, and an absent browser
   * control has a weakness the present ones do not: it fails, and a spec
   * that broke for any other reason fails too. So each one checks its own
   * precondition first, with a message of its own, and the Go side
   * (browserbench_test.go, failsWith) only accepts the failure the control
   * is about. UIX-07 is present since A11 O1 and UIX-08 since its session
   * O4; both keep their preconditions, so a regression fails for the reason
   * it is.
   */

  test('UIX-07 the login screen draws the logo the application declared', async ({ page }) => {
    // What the login screen draws, before anyone has signed in.
    await page.goto('/admin/login')
    await page.locator('input[type="password"]').first().waitFor({ timeout: 15_000 })
    const logo = await page.locator('meta[name="nucleus-admin-logo"]').getAttribute('content')
    if (!logo) throw new Error('UIX-07 precondition: the document declares no logo (CUST-02 should be red too)')
    const onLogin = page.locator(`img[src="${logo}"]`)
    const drawn = await onLogin.count()
    // Drawn means LOADED: an <img> whose fetch failed — a 404, or a source
    // the page's own Content-Security-Policy refuses — is a broken image,
    // not a logo. The bench's application serves the file it declares.
    const loaded = drawn > 0
      ? await onLogin.first().evaluate(async (img: HTMLImageElement) => {
          if (!img.complete) await new Promise((done) => { img.onload = img.onerror = done })
          return img.naturalWidth > 0
        })
      : false

    // The instrument can see a logo where the panel does draw one: the
    // sidebar. Without this, a locator that matched nothing anywhere would
    // record the login screen as logo-less.
    await signIn(page)
    await expect(
      page.locator(`img[src="${logo}"]`),
      'UIX-07 precondition: the sidebar draws no logo either, so this locator sees nothing',
    ).toHaveCount(1, { timeout: 10_000 })

    expect(drawn, `UIX-07: the login screen draws no logo — ${logo} travels on the document and nothing renders it`).toBeGreaterThan(0)
    expect(loaded, `UIX-07: the login screen draws ${logo} and the browser did not load it`).toBe(true)
  })

  /*
   * UIX-08 is the browser half of EXT-02 and EXT-03: an action offered on
   * one record is drawn where the record is — its record view and its row's
   * menu — and what it answers with is followed. The bench's "duplicate"
   * answers with the copy's record view, which the SPA reaches through its
   * router without reloading the document; its "download_text" answers with
   * a file, which the browser saves under the name the action gave.
   */
  test('UIX-08 an action on one record is offered where the record is, and its page and its file arrive', async ({ page }) => {
    const title = `uix-08 note ${Date.now()}`
    await signIn(page)
    // A row of its own to open: the bench's application starts empty. The
    // call is made from the page, the way the SPA makes it — the session
    // belongs to this browser, and a request from outside it (page.request)
    // is refused with a 401.
    const created = await page.evaluate(async (noteTitle) => {
      const r = await fetch('/admin/api/models/Note', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ title: noteTitle, body: 'uix-08 body', status: 'draft' }),
      })
      const body = await r.json().catch(() => ({}))
      return { status: r.status, id: String(body?.data?.id ?? body?.id ?? '') }
    }, title)
    expect(created.status < 300 && created.id !== '', `UIX-08 precondition: creating a note answered ${created.status}`).toBe(true)
    const id = created.id

    // The schema the screen loads offers both actions on a record to this
    // operator — the precondition: an action nobody offers would make this
    // control measure the declaration, not the record view.
    const placements = await page.evaluate(async () => {
      const r = await fetch('/admin/api/models/Note/schema', { credentials: 'same-origin' })
      const schema = await r.json()
      return Object.fromEntries((schema.actions ?? []).map((a: { name: string; placement?: string }) => [a.name, a.placement ?? '']))
    })
    expect(placements.duplicate === 'record' && placements.download_text === 'selection_and_record',
      `UIX-08 precondition: the schema does not offer the record actions (${JSON.stringify(placements)}); EXT-02 should be red too`).toBe(true)

    await page.goto('/admin/data-studio')
    await page.waitForLoadState('networkidle')
    await page.getByRole('button', { name: /^Notes/ }).first().click({ timeout: 15_000 })
    await page.getByRole('textbox', { name: 'Search records' }).fill(title)
    await page.keyboard.press('Enter')
    await expect(page.getByRole('button', { name: `Edit record ${id}` }), 'UIX-08 precondition: the search did not narrow the grid to the new note')
      .toBeVisible({ timeout: 10_000 })

    // 1. The record view offers the record's actions.
    await page.getByRole('button', { name: `Edit record ${id}` }).click()
    const view = page.getByRole('dialog').first()
    await expect(view, 'UIX-08 precondition: the record view did not open').toBeVisible({ timeout: 10_000 })
    const offered = view.getByRole('group', { name: 'Actions on this record' })
    await expect(
      offered.getByRole('button', { name: 'Duplicate' }),
      'UIX-08: the record view offers no action of the application — duplicate is declared on one record and the open record has no button for it',
    ).toHaveCount(1, { timeout: 5_000 })

    // 2. Pressed, it runs on this record, and the page it answers with is
    // reached through the router: the document is the same one.
    await page.evaluate(() => { (window as unknown as { uix08?: string }).uix08 = 'same document' })
    await offered.getByRole('button', { name: 'Duplicate' }).click()
    await expect(page, 'UIX-08: the redirect the action answered with was not followed')
      .toHaveURL(/\/admin\/data-studio\?model=Note&record=\d+$/, { timeout: 10_000 })
    const copyId = new URL(page.url()).searchParams.get('record')
    expect(copyId, 'UIX-08: the redirect opened the record the action ran on, not the copy it made').not.toBe(id)
    const copy = page.getByRole('dialog').first()
    await expect(copy.getByLabel(/^Title/), 'UIX-08: the page the action named does not show the copy').toHaveValue(`Copy of ${title}`, { timeout: 10_000 })
    expect(
      await page.evaluate(() => (window as unknown as { uix08?: string }).uix08),
      'UIX-08: the redirect reloaded the document instead of going through the router',
    ).toBe('same document')
    await page.keyboard.press('Escape')
    await expect(copy).toBeHidden({ timeout: 5_000 })
    await expect(page, 'UIX-08: the closed record view is still named by the URL').toHaveURL(/model=Note$/)

    // 3. From the row's own menu, the file — saved under the name the
    // action gave, holding what it wrote.
    const rowMenu = page.getByRole('button', { name: `Actions for record ${id}` })
    await expect(rowMenu, 'UIX-08: the row offers no menu of its record actions').toBeVisible({ timeout: 5_000 })
    await rowMenu.click()
    const menu = page.getByRole('menu')
    await expect(menu, 'UIX-08: the row\'s menu did not open').toBeVisible({ timeout: 5_000 })
    const menuViolations = (await new AxeBuilder({ page }).include('[role="menu"]')
      .withRules(['color-contrast', 'aria-required-children', 'aria-valid-attr-value']).analyze()).violations
      .map((v) => ({ id: v.id, nodes: v.nodes.slice(0, 3).map((n) => n.target.join(' ')) }))
    expect(menuViolations, JSON.stringify(menuViolations, null, 2)).toEqual([])
    const downloaded = page.waitForEvent('download', { timeout: 10_000 })
      .catch(() => { throw new Error('UIX-08: the action answered with a file and the browser saved none') })
    await menu.getByRole('menuitem', { name: 'Download as text' }).click()
    const file = await downloaded
    expect(file.suggestedFilename(), 'UIX-08: the file was not saved under the name the action gave').toBe(`note-${id}.txt`)
    const path = await file.path()
    expect(readFileSync(path, 'utf8'), 'UIX-08: the file does not hold what the action wrote').toContain(`${title}\nuix-08 body`)
  })

  /*
   * UIX-09 is the browser half of EXT-01: an action that declared fields
   * (the bench's "schedule" on Note) opens a form instead of the plain
   * confirmation, a submit the server refuses lands on the field it names,
   * and a submit it accepts runs the action and shows what it said. The
   * form does no checking of its own, so the refusal this control reads is
   * the server's: break the server's check and the action runs on the
   * first, empty submit, and this control fails.
   */
  test('UIX-09 an action that asks first draws its form and shows the refusal on the field', async ({ page }) => {
    const title = `uix-09 note ${Date.now()}`
    const marker = 'uix-09 typed this reason'
    await signIn(page)
    const status = await page.evaluate(async (noteTitle) => {
      const r = await fetch('/admin/api/models/Note', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ title: noteTitle, status: 'draft' }),
      })
      return r.status
    }, title)
    expect(status < 300, `UIX-09 precondition: creating a note answered ${status}`).toBe(true)

    await page.goto('/admin/data-studio')
    await page.waitForLoadState('networkidle')
    await page.getByRole('button', { name: /^Notes/ }).first().click({ timeout: 15_000 })

    // One row on screen, the one this control created, and it selected.
    await page.getByRole('textbox', { name: 'Search records' }).fill(title)
    await page.keyboard.press('Enter')
    const rowBox = page.getByRole('checkbox', { name: /toggle row selection/i })
    await expect(rowBox, 'UIX-09 precondition: the search did not narrow the grid to the new note').toHaveCount(1, { timeout: 10_000 })
    await rowBox.check()

    await page.getByRole('button', { name: /^Schedule/ }).click({ timeout: 10_000 })
    const dialog = page.getByRole('dialog', { name: 'Schedule' })
    await expect(dialog, 'UIX-09: the action declares fields and no form opened').toBeVisible({ timeout: 5_000 })
    const reason = dialog.getByLabel(/^Reason/)
    await expect(reason).toBeVisible()
    await expect(dialog.getByLabel(/^Channel/)).toBeVisible()
    await expect(dialog.getByLabel(/^Publish on/)).toHaveAttribute('type', 'date')

    // Submitted empty: the server refuses, naming the fields, and the
    // refusal is on the input — announced as its description — while the
    // dialog stays open with what was entered.
    await dialog.getByRole('button', { name: 'Schedule', exact: true }).click()
    await expect(reason, 'UIX-09: an empty required field was not marked invalid after the submit').toHaveAttribute('aria-invalid', 'true', { timeout: 5_000 })
    await expect(reason).toHaveAccessibleDescription(/is required/)
    await expect(dialog).toBeVisible()

    // The form with its errors showing is still a form everybody can read.
    const violations = (await new AxeBuilder({ page }).include('[role="dialog"]')
      .withRules(['label', 'aria-valid-attr-value', 'color-contrast']).analyze()).violations
      .map((v) => ({ id: v.id, nodes: v.nodes.slice(0, 3).map((n) => n.target.join(' ')) }))
    expect(violations, JSON.stringify(violations, null, 2)).toEqual([])

    // Filled in: it runs, the dialog closes, and the action's own message
    // says what it did with what was typed.
    await reason.fill(marker)
    await dialog.getByLabel(/^Priority/).fill('3')
    await dialog.getByLabel(/^Notify subscribers/).check()
    await dialog.getByLabel(/^Channel/).selectOption('email')
    await dialog.getByLabel(/^Publish on/).fill('2026-10-05')
    await dialog.getByRole('button', { name: 'Schedule', exact: true }).click()
    await expect(dialog).toBeHidden({ timeout: 10_000 })
    await expect(
      page.getByRole('status').filter({ hasText: marker }),
      'UIX-09: the valid submit did not show the action\'s result',
    ).toContainText(`1 note(s) scheduled on email for 2026-10-05: ${marker}`, { timeout: 10_000 })
  })

  /*
   * UIX-10 is the browser half of EXT-09 (A11 O2): the server says what the
   * document carries, and this says what the browser paints with it.
   *
   * "The first frame" is measured, not inferred from a tag. The panel's
   * bundle is held at the network while the document is parsed and styled,
   * so whatever the browser can paint before the bundle runs is decided by
   * the document and what the document loads ahead of it — and that is read
   * off the page: the root's class and the body's painted background. Then
   * the bundle is let through, and a record of every change to the theme,
   * kept from before the document existed, shows whether anything switched
   * it afterwards.
   */
  test("UIX-10 the first frame wears the theme the application configured, and the operator's own choice wins on reload", async ({ page }) => {
    const base = process.env.ORBIT_BENCH_THEMED_URL ?? ''
    const surface = process.env.ORBIT_BENCH_THEMED_SURFACE ?? ''
    if (!base || !surface) throw new Error('UIX-10 precondition: the driver passed no themed application (ORBIT_BENCH_THEMED_URL, ORBIT_BENCH_THEMED_SURFACE)')

    // The browser says light; the application says dark. A first frame that
    // followed the browser would be light.
    await page.emulateMedia({ colorScheme: 'light' })
    await recordThemeChanges(page)
    const toggle = page.getByRole('button', { name: /switch to (light|dark) theme/i }).first()
    const password = page.locator('input[type="password"]').first()

    // The login screen: the first screen, before anyone is anybody.
    const login = await firstFrame(page, `${base}/admin/login`, () => password.waitFor({ timeout: 15_000 }))
    expectFrame(login, { dark: true, surface }, 'the login screen')

    // The panel's own document, which the server writes on another path.
    await signInAt(page, base)
    const panel = await firstFrame(page, `${base}/admin/`, () => toggle.waitFor({ timeout: 15_000 }))
    expectFrame(panel, { dark: true, surface }, 'the panel')

    // The operator chooses light, and it is theirs to keep: the reload opens
    // light, over the application's dark.
    await toggle.click()
    await expect
      .poll(() => page.evaluate(() => document.documentElement.classList.contains('dark')), {
        message: 'UIX-10 precondition: the toggle did not switch the panel to light',
      })
      .toBe(false)
    const reloaded = await firstFrame(page, `${base}/admin/`, () => toggle.waitFor({ timeout: 15_000 }))
    expectFrame(reloaded, { dark: false, surface: '#ffffff' }, 'the panel, reloaded after the operator chose light')
  })

  /*
   * UIX-11 is the browser half of EXT-04 and EXT-05 (A11 O5): the server
   * says a second dashboard exists, carries a series and answers only the
   * operator granted it; this says what each operator is SHOWN.
   *
   * "Drawn" is measured against what the API served, not against the
   * existence of an <svg>: the chart must mark one point per label, and the
   * point of the highest reading — today, where the driver created notes —
   * must sit above the point of the lowest, which no chart drawn from
   * nothing would do.
   */
  test('UIX-11 a second dashboard draws a series to the operator granted it, and is neither listed nor served to one who is not', async ({ page }) => {
    const reader = process.env.ORBIT_BENCH_READER_USER ?? ''
    const outsider = process.env.ORBIT_BENCH_OUTSIDER_USER ?? ''
    const password = process.env.ORBIT_BENCH_OPERATOR_PASSWORD ?? ''
    const board = process.env.ORBIT_BENCH_DASHBOARD ?? ''
    if (!reader || !outsider || !password || !board) {
      throw new Error('UIX-11 precondition: the driver passed no operators or dashboard (ORBIT_BENCH_READER_USER, ORBIT_BENCH_OUTSIDER_USER, ORBIT_BENCH_OPERATOR_PASSWORD, ORBIT_BENCH_DASHBOARD)')
    }
    const nav = page.getByRole('navigation', { name: /main navigation|navegación/i })
    const apiPath = `/admin/api/ui/dashboards/${board}`
    const askAPI = () =>
      page.evaluate(async (path) => {
        const r = await fetch(path, { credentials: 'same-origin' })
        return { status: r.status, body: r.ok ? await r.json() : null }
      }, apiPath)

    // The operator granted the dashboard.
    await signInAs(page, reader, password)
    const served = await askAPI()
    expect(served.status, `UIX-11 precondition: the dashboard's API answered the operator granted it ${served.status} (EXT-05 should be red too)`).toBe(200)
    const card = (served.body.widgets ?? []).find((w: { kind?: string }) => w.kind === 'line' || w.kind === 'bar')
    expect(card, 'UIX-11 precondition: the dashboard serves no series card (EXT-04 should be red too)').toBeTruthy()
    const values: number[] = card.series[0].values
    const highest = values.indexOf(Math.max(...values))
    const lowest = values.indexOf(Math.min(...values))
    expect(values[highest] > values[lowest], `UIX-11 precondition: the series is flat (${JSON.stringify(values)}), so its drawing cannot be told from nothing`).toBe(true)

    const link = nav.getByRole('link', { name: served.body.title })
    await expect(link, `UIX-11: the navigation of the operator granted the dashboard does not list "${served.body.title}"`).toHaveCount(1, { timeout: 10_000 })
    await link.click()
    await page.waitForURL(new RegExp(`/admin/dashboards/${board}$`), { timeout: 10_000 })
    await expect(page.getByRole('heading', { level: 1, name: served.body.title })).toBeVisible()

    const chart = page.locator(`[data-widget="${card.id}"] [data-chart]`)
    await expect(chart, `UIX-11: the dashboard draws no chart for ${card.id}`).toHaveCount(1, { timeout: 10_000 })
    const points = chart.locator('svg [data-series-point]')
    await expect(points, `UIX-11: the chart marks ${await points.count()} points for the ${card.labels.length} labels the API served`).toHaveCount(card.labels.length, { timeout: 10_000 })
    const heights = await points.evaluateAll((marks) => marks.map((m) => Number(m.getAttribute('cy'))))
    expect(
      heights[highest] < heights[lowest],
      `UIX-11: the highest reading (${values[highest]}) is drawn no higher than the lowest (${values[lowest]}): cy ${JSON.stringify(heights)}`,
    ).toBe(true)
    const box = await chart.locator('svg').first().boundingBox()
    expect(box && box.width > 200 && box.height > 100, `UIX-11: the chart is drawn at ${JSON.stringify(box)}`).toBe(true)

    // The screen is legible, and says what its parts are, by the rules
    // UIX-02 and UIX-03 hold the panel's own screens to.
    const violations = await axeViolations(page, [
      'color-contrast',
      'button-name',
      'link-name',
      'image-alt',
      'svg-img-alt',
      'role-img-alt',
      'aria-allowed-attr',
      'aria-required-attr',
      'aria-valid-attr-value',
      'aria-hidden-focus',
      'td-headers-attr',
      'th-has-data-cells',
    ])
    expect(violations, `UIX-11: the dashboard: ${JSON.stringify(violations, null, 2)}`).toEqual([])

    // The operator granted nothing.
    await page.context().clearCookies()
    await signInAs(page, outsider, password)
    await nav.waitFor({ timeout: 10_000 })
    await page.waitForLoadState('networkidle')
    await expect(nav.getByRole('link', { name: served.body.title }), 'UIX-11: the navigation of an operator not granted the dashboard lists it').toHaveCount(0)
    const refused = await askAPI()
    expect(refused.status, `UIX-11: the dashboard's API answered ${refused.status} to an operator not granted it`).toBe(403)
    // By its address, as a bookmark or a reload reaches it: the screen
    // loads, and says no.
    await page.goto(`/admin/dashboards/${board}`)
    await expect(page.getByText('You do not have permission to view this'), 'UIX-11: the dashboard reached by its address is not refused').toBeVisible({ timeout: 10_000 })
    await expect(page.locator('[data-widget]'), 'UIX-11: a refused dashboard still draws cards').toHaveCount(0)
  })

  /*
   * UIX-12 is the browser half of EXT-06 and EXT-07 (A11 O6): the server
   * says the document names the application's script and stylesheet with
   * their digests, after the bundle, under a script-src that is still
   * 'self'; this says the browser runs and applies them, with no violation,
   * and what the script registers draws a field.
   *
   * The bench's application draws Note.status with its own renderer
   * (note-status), which knows "draft" and throws on any status it does not
   * know. "Drawn" is the renderer's own element in the cell, styled by the
   * application's stylesheet; "falls back" is the panel's own text and the
   * renderer's failure, in that cell, with the rest of the grid and the
   * record view still there.
   */
  test("UIX-12 the application's own script loads under the policy and draws a field in the grid and the record view, and a renderer that throws falls back", async ({ page }) => {
    // Every violation of the policy, from before the document exists, and
    // every error the console reports — a script refused for its digest is
    // one, and fires no policy event.
    await page.addInitScript(() => {
      const seen: string[] = []
      ;(window as unknown as { __violations: string[] }).__violations = seen
      document.addEventListener('securitypolicyviolation', (e) => seen.push(`${e.violatedDirective} ${e.blockedURI}`))
    })
    const consoleErrors: string[] = []
    page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()) })
    const pageErrors: string[] = []
    page.on('pageerror', (err) => pageErrors.push(err.message))

    const stamp = Date.now()
    const drawnTitle = `uix-12 ${stamp} drawn`
    const brokenTitle = `uix-12 ${stamp} unknown`
    await signIn(page)

    // Preconditions: the schema names the renderer for the field, and the
    // document names the application's files.
    const renderer = await page.evaluate(async () => {
      const r = await fetch('/admin/api/models/Note/schema', { credentials: 'same-origin' })
      const schema = await r.json()
      return (schema.fields ?? []).find((f: { column: string }) => f.column === 'status')?.renderer ?? null
    })
    expect(renderer, 'UIX-12 precondition: the schema names no renderer for Note.status (EXT-07 should be red too)').toBe('note-status')
    expect(await page.locator('script[src^="/admin/client/"]').count(), 'UIX-12 precondition: the document loads no script of the application\'s (EXT-06 should be red too)').toBeGreaterThan(0)
    expect(await page.locator('link[rel="stylesheet"][href^="/admin/client/"]').count(), 'UIX-12 precondition: the document links no stylesheet of the application\'s (EXT-06 should be red too)').toBeGreaterThan(0)
    expect(await page.evaluate(() => (window as unknown as { orbit?: { version: number } }).orbit?.version), 'UIX-12: the panel offers no window.orbit (version 1) to the application\'s script').toBe(1)

    // One note the renderer knows and one it does not.
    const ids: string[] = []
    for (const note of [{ title: drawnTitle, status: 'draft' }, { title: brokenTitle, status: 'uix-12-unknown' }]) {
      const created = await page.evaluate(async (body) => {
        const r = await fetch('/admin/api/models/Note', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          credentials: 'same-origin',
          body: JSON.stringify(body),
        })
        const out = await r.json().catch(() => ({}))
        return { status: r.status, id: String(out?.data?.id ?? out?.id ?? '') }
      }, note)
      expect(created.status < 300 && created.id !== '', `UIX-12 precondition: creating a note answered ${created.status}`).toBe(true)
      ids.push(created.id)
    }
    const [drawnId, brokenId] = ids

    await page.goto('/admin/data-studio')
    await page.waitForLoadState('networkidle')
    await page.getByRole('button', { name: /^Notes/ }).first().click({ timeout: 15_000 })
    // The first page of notes already holds the one the renderer throws on:
    // a renderer's failure that escaped would take the screen with it.
    const search = page.getByRole('textbox', { name: 'Search records' })
    await expect(search, 'UIX-12: the Data Studio screen did not survive the application\'s renderer').toBeVisible({ timeout: 10_000 })
    await search.fill(`uix-12 ${stamp}`, { timeout: 10_000 }).catch(() => {
      throw new Error('UIX-12: the Data Studio screen stopped answering once the grid drew the application\'s renderer')
    })
    await page.keyboard.press('Enter')
    // The grid holds the two notes and nothing else — the search has
    // landed — and has stopped moving them: a row read mid-animation is
    // read at an opacity it does not keep.
    // Rows are read by the grid's role and the id the panel gives each row
    // (getRowId), not by AG Grid's own container classes: those changed
    // when A12 O1 moved the grid to AG Grid 36, and say nothing the control
    // is about.
    await expect(page.locator(GRID_ROWS), 'UIX-12 precondition: the search did not narrow the grid to the two notes')
      .toHaveCount(2, { timeout: 10_000 })
    await page.waitForFunction(() => document.getAnimations().length === 0, undefined, { timeout: 10_000 })
    const cell = (id: string, column: string) => page.locator(`${GRID} [row-id="${id}"] [col-id="${column}"]`)
    await expect(cell(drawnId, 'title'), 'UIX-12 precondition: the search did not bring the two notes to the grid').toHaveText(drawnTitle, { timeout: 10_000 })
    await expect(cell(brokenId, 'title'), 'UIX-12 precondition: the search did not bring the two notes to the grid').toHaveText(brokenTitle, { timeout: 10_000 })

    // 1. In the grid, the renderer draws the status, and the application's
    // stylesheet styles what it drew.
    const badge = cell(drawnId, 'status').locator('[data-bench-status="draft"]')
    await expect(badge, 'UIX-12: the grid does not draw Note.status with the application\'s renderer').toHaveText('Draft', { timeout: 10_000 })
    await expect(badge, 'UIX-12: the renderer was not told it draws in the list').toHaveAttribute('data-bench-where', 'list')
    expect(await badge.evaluate((el) => getComputedStyle(el).borderTopLeftRadius), 'UIX-12: the application\'s stylesheet did not apply to what its renderer drew').toBe('9999px')

    // 2. A value the renderer throws on is drawn the panel's way, and the
    // cell says the renderer failed; the row and the grid are still there.
    const broken = cell(brokenId, 'status')
    await expect(broken, 'UIX-12: a renderer that throws did not fall back to the panel\'s own drawing').toContainText('uix-12-unknown', { timeout: 10_000 })
    await expect(broken, 'UIX-12: the cell whose renderer threw does not say so').toContainText('note-status failed: no badge for the status "uix-12-unknown"')
    await expect(cell(brokenId, 'title')).toHaveText(brokenTitle)

    // Both are legible, by the rule the panel's own screens are held to.
    const gridViolations = (await new AxeBuilder({ page }).include(GRID).withRules(['color-contrast']).analyze()).violations
      .map((v) => ({ id: v.id, nodes: v.nodes.slice(0, 3).map((n) => n.target.join(' ')) }))
    expect(gridViolations, `UIX-12: the grid: ${JSON.stringify(gridViolations, null, 2)}`).toEqual([])

    // 3. The record view draws it too, and the record that throws keeps a
    // record view that works.
    await page.getByRole('button', { name: `Edit record ${drawnId}` }).click()
    let view = page.getByRole('dialog').first()
    await expect(view, 'UIX-12 precondition: the record view did not open').toBeVisible({ timeout: 10_000 })
    await expect(view.locator('[data-bench-status="draft"][data-bench-where="record"]'), 'UIX-12: the record view does not draw Note.status with the application\'s renderer').toHaveText('Draft', { timeout: 5_000 })
    await page.keyboard.press('Escape')
    await expect(view).toBeHidden({ timeout: 5_000 })

    await page.getByRole('button', { name: `Edit record ${brokenId}` }).click()
    view = page.getByRole('dialog').first()
    await expect(view, 'UIX-12: the record whose renderer throws has no record view').toBeVisible({ timeout: 10_000 })
    await expect(view, 'UIX-12: the record view of a value the renderer throws on does not say so').toContainText('note-status failed: no badge for the status "uix-12-unknown"', { timeout: 5_000 })
    await expect(view.getByLabel(/^Title/), 'UIX-12: the record view lost its form to the renderer that threw').toHaveValue(brokenTitle)
    await page.keyboard.press('Escape')
    await expect(view).toBeHidden({ timeout: 5_000 })

    // 4. And nothing of the application's was refused on the way: no
    // violation of the script or style policy and none naming its files, no
    // digest the browser rejected, no error the page did not catch. The
    // renderer's own failure is reported by the panel on purpose; nothing
    // else is. (The grid's own icon font, refused by font-src until A12 O1,
    // is UIX-14's to count.)
    const violations = (await page.evaluate(() => (window as unknown as { __violations: string[] }).__violations))
      .filter((v) => /^(script|style)-src/.test(v) || v.includes('/admin/client/'))
    expect(violations, `UIX-12: the policy refused something: ${JSON.stringify(violations)}`).toEqual([])
    const refused = consoleErrors.filter((m) => /integrity/i.test(m) || m.includes('/admin/client/') || /directive: "(script|style)-src/.test(m))
    expect(refused, `UIX-12: the browser refused the application's files: ${JSON.stringify(refused)}`).toEqual([])
    expect(pageErrors, `UIX-12: an error the page did not catch: ${JSON.stringify(pageErrors)}`).toEqual([])
  })
  /*
   * UIX-13 is the browser half of OR-62 (A12 O1): an error the panel writes
   * — a field's and the form's — is legible in both themes. UIX-09 reads
   * the form an ACTION declares; this one reads the record form every model
   * has, in the light theme and in the dark one, with its errors showing.
   * The errors are the form's own (a document field that is not JSON), so
   * what is read is the panel's drawing of them, not a server's message.
   */
  test('UIX-13 a form with its errors showing is legible, in the light theme and in the dark one', async ({ page }) => {
    await signIn(page)
    for (const theme of ['light', 'dark'] as const) {
      // The operator's own choice, the way the toggle records it (UIX-10).
      await page.evaluate((t) => {
        localStorage.setItem('gf-theme', t)
        localStorage.setItem('orbit-theme-choice', t)
      }, theme)
      await page.goto('/admin/data-studio')
      await page.waitForLoadState('networkidle')
      expect(
        await page.evaluate(() => document.documentElement.classList.contains('dark')),
        `UIX-13 precondition: the panel did not open in the ${theme} theme`,
      ).toBe(theme === 'dark')

      await page.getByRole('button', { name: /^Notes/ }).first().click({ timeout: 15_000 })
      await page.getByRole('button', { name: /new record/i }).first().click({ timeout: 15_000 })
      const dialog = page.getByRole('dialog').first()
      await expect(dialog, 'UIX-13 precondition: the record form did not open').toBeVisible()
      await dialog.locator('#field-title').fill(`uix-13 ${theme}`)
      const meta = dialog.locator('#field-meta')
      await expect(meta, 'UIX-13 precondition: the form has no document field to get wrong').toBeVisible()
      await meta.fill('{ "not": json')
      await dialog.getByRole('button', { name: 'Create', exact: true }).click()

      await expect(meta, `UIX-13 precondition (${theme}): the field that is not JSON was not marked invalid`).toHaveAttribute('aria-invalid', 'true', { timeout: 5_000 })
      await expect(dialog.locator('#field-meta-error'), `UIX-13 precondition (${theme}): the field shows no error`).toContainText('Invalid JSON')
      await expect(dialog.getByRole('alert'), `UIX-13 precondition (${theme}): the form shows no alert`).toContainText('Fix the highlighted fields')

      const violations = (await new AxeBuilder({ page }).include('[role="dialog"]').withRules(['color-contrast']).analyze()).violations
        .map((v) => ({ id: v.id, nodes: v.nodes.slice(0, 4).map((n) => `${n.target.join(' ')} — ${n.any[0]?.message ?? ''}`) }))
      expect(violations, `UIX-13: the form's errors in the ${theme} theme: ${JSON.stringify(violations, null, 2)}`).toEqual([])

      await page.keyboard.press('Escape')
      await expect(dialog).toBeHidden({ timeout: 5_000 })
    }
  })

  /*
   * UIX-14 is the browser half of OR-63 (A12 O1): Data Studio's grid draws
   * its icons, and nothing it loads is refused by the panel's own policy.
   * Until O1 the quartz stylesheet carried the grid's icon font as a data:
   * URL, font-src 'self' refused it, document.fonts reported the face in
   * error and every icon drawn in it was drawn in nothing — UIX-12 found it
   * and could only exclude it. Every violation is recorded from before the
   * document exists; an icon counts as drawn when an image paints it (a
   * mask, the way the grid's quartz icons are drawn now) or a font the
   * document LOADED does.
   */
  test("UIX-14 Data Studio's grid draws its icons, and the panel's own policy refuses nothing it loads", async ({ page }) => {
    await page.addInitScript(() => {
      const seen: string[] = []
      ;(window as unknown as { __violations: string[] }).__violations = seen
      document.addEventListener('securitypolicyviolation', (e) => seen.push(`${e.violatedDirective} ${e.blockedURI}`))
    })
    await signIn(page)
    const created = await page.evaluate(async () => {
      const r = await fetch('/admin/api/models/Note', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ title: `uix-14 ${Date.now()}`, status: 'draft' }),
      })
      return r.status
    })
    expect(created < 300, `UIX-14 precondition: creating a note answered ${created}`).toBe(true)

    await page.goto('/admin/data-studio')
    await page.waitForLoadState('networkidle')
    await page.getByRole('button', { name: /^Notes/ }).first().click({ timeout: 15_000 })
    await expect(page.locator(GRID_ROWS).first(), 'UIX-14 precondition: the grid drew no row').toBeVisible({ timeout: 10_000 })
    // A sorted column draws its sort icon beside the header's name.
    await page.getByRole('columnheader', { name: /^Title/ }).click()
    await page.waitForLoadState('networkidle')

    const icons = await page.evaluate(async (grid) => {
      await document.fonts.ready
      const loaded = new Set(Array.from(document.fonts).filter((f) => f.status === 'loaded').map((f) => f.family.replace(/["']/g, '')))
      return Array.from(document.querySelectorAll(`${grid} .ag-icon`))
        .filter((icon) => {
          const box = icon.getBoundingClientRect()
          return box.width > 0 && box.height > 0 && getComputedStyle(icon).visibility !== 'hidden'
        })
        .map((icon) => {
          const own = getComputedStyle(icon)
          const before = getComputedStyle(icon, '::before')
          const mask = (style: CSSStyleDeclaration) => style.maskImage || style.getPropertyValue('-webkit-mask-image')
          const image = [own, before].some((style) => /url\(/.test(mask(style) ?? '') || /url\(/.test(style.backgroundImage))
          const glyph = before.content !== 'none' && before.content !== 'normal' && before.content !== '""'
          const family = before.fontFamily.split(',')[0].trim().replace(/["']/g, '')
          return { icon: icon.className, image, glyph, family, fontLoaded: loaded.has(family) }
        })
    }, GRID)
    expect(icons.length, 'UIX-14 precondition: the grid shows no icon (the sort mark, the selection boxes)').toBeGreaterThan(0)
    const undrawn = icons.filter((i) => !i.image && !(i.glyph && i.fontLoaded))
    expect(undrawn, `UIX-14: icons the grid draws in nothing: ${JSON.stringify(undrawn, null, 2)}`).toEqual([])

    const failedFonts = await page.evaluate(() => Array.from(document.fonts).filter((f) => f.status === 'error').map((f) => f.family))
    expect(failedFonts, 'UIX-14: a font face the document could not load').toEqual([])
    const violations = await page.evaluate(() => (window as unknown as { __violations: string[] }).__violations)
    expect(violations.filter((v) => v.startsWith('font-src')), 'UIX-14: font-src refused a font').toEqual([])
    expect(violations, `UIX-14: the panel's policy refused something on Data Studio: ${JSON.stringify(violations)}`).toEqual([])
  })

  /*
   * UIX-15 is the browser half of OR-61 (A12 O1): the panel's own scripts
   * and stylesheets reach the browser compressed ONCE, by the build — in
   * Brotli when the browser accepts it, with their length, and saying the
   * answer varies on what was asked. Compressed on the way out is not the
   * same thing: an application on Nucleus's default middleware already
   * gzipped the panel per request (this bench's does), streamed without a
   * length and never in Brotli, while a panel mounted on any other router,
   * and the admin server, sent the bytes as they were. A file too small to
   * be worth an encoding (under 1 KiB) travels as it is.
   */
  test("UIX-15 the panel's own scripts and stylesheets reach the browser compressed once, by the build", async ({ page }) => {
    const fetched: { path: string; encoding: string; vary: string; length: number; size: number; asked: string }[] = []
    page.on('response', async (res) => {
      const url = new URL(res.url())
      if (!/\/admin\/assets\/[^/]+\.(js|css)$/.test(url.pathname)) return
      const headers = await res.allHeaders()
      const asked = (await res.request().allHeaders())['accept-encoding'] ?? ''
      fetched.push({
        path: url.pathname,
        encoding: headers['content-encoding'] ?? '',
        vary: headers['vary'] ?? '',
        length: Number(headers['content-length'] ?? -1),
        // The file's own size, decoded: what decides whether the build
        // encoded it (1 KiB or more).
        size: (await res.body().catch(() => Buffer.alloc(0))).length,
        asked,
      })
    })
    await signIn(page)
    await page.goto('/admin/data-studio')
    await page.waitForLoadState('networkidle')
    await page.getByRole('button', { name: /^Notes/ }).first().click({ timeout: 15_000 })
    await page.waitForLoadState('networkidle')

    expect(fetched.length, 'UIX-15 precondition: the browser fetched none of the panel\'s files').toBeGreaterThan(3)
    expect(fetched.some((f) => /\/DataStudioPage-[^/]+\.js$/.test(f.path)), `UIX-15 precondition: Data Studio's chunk was not fetched: ${JSON.stringify(fetched.map((f) => f.path))}`).toBe(true)
    // What was negotiated, for the report: the instrument's browser decides.
    test.info().annotations.push({ type: 'encodings', description: [...new Set(fetched.map((f) => f.encoding || 'identity'))].sort().join(', ') })
    const built = fetched.filter((f) => f.size >= 1024)
    expect(built.length, 'UIX-15 precondition: no file the browser fetched measured 1 KiB or more').toBeGreaterThan(3)
    const plain = built.filter((f) => f.encoding === '')
    expect(plain, `UIX-15: files of 1 KiB or more that travelled uncompressed: ${JSON.stringify(plain, null, 2)}`).toEqual([])
    const unasked = fetched.filter((f) => f.encoding !== '' && !f.asked.includes(f.encoding))
    expect(unasked, `UIX-15: an encoding the browser did not ask for: ${JSON.stringify(unasked, null, 2)}`).toEqual([])
    const unvaried = fetched.filter((f) => f.encoding !== '' && !/accept-encoding/i.test(f.vary))
    expect(unvaried, `UIX-15: an encoded answer that does not say it varies: ${JSON.stringify(unvaried, null, 2)}`).toEqual([])
    // Compressed by the build, not on the way out: Brotli to a browser that
    // accepts it, and an encoded answer whose length is known before it is
    // sent (a per-request compressor streams it without one).
    const notBrotli = built.filter((f) => /\bbr\b/.test(f.asked) && f.encoding !== 'br')
    expect(notBrotli, `UIX-15: the browser accepts Brotli and was sent another encoding: ${JSON.stringify(notBrotli, null, 2)}`).toEqual([])
    const streamed = built.filter((f) => f.encoding !== '' && !(f.length > 0))
    expect(streamed, `UIX-15: encoded answers without a length, compressed as they were sent: ${JSON.stringify(streamed, null, 2)}`).toEqual([])
  })

  /*
   * UIX-16 and UIX-17 are the screens of an operator who is not a superuser
   * (OR-64). Every other control but UIX-11 signs in as the bootstrap admin,
   * and every permission question is answered yes for a superuser before
   * any policy is read — so the grid drawn for an operator without delete
   * (A11 O3) and the record's menu for one without update (A11 O4) had been
   * seen only by whoever wrote them. The driver creates two operators, the
   * viewer and the actor (partialOperators), and each control reads what
   * each is shown, what they are NOT shown, that the screen they are shown
   * meets the rules the panel's own screens are held to, and that the
   * server refuses what the screen leaves out when it is asked anyway.
   *
   * Each "not shown" is read beside something the same operator IS shown
   * in the same place, so a locator that matched nothing anywhere cannot
   * pass for an absence.
   */
  test('UIX-16 an operator is offered a delete only where they hold one: no selection or Delete without it, and no batch Delete for one who may delete a record but not a batch', async ({ page }) => {
    const { viewer, actor, password } = partialOperators('UIX-16')
    const title = `uix-16 note ${Date.now()}`
    const id = await noteAsAdmin(page, { title, body: 'uix-16 body' }, 'UIX-16')
    const grid = page.locator(GRID)
    const rowBoxes = grid.getByRole('checkbox')
    const batchDelete = page.getByRole('button', { name: /^Delete \d+$/ })

    // 1. The viewer may not delete and holds no action over a selection:
    // the grid offers no selection, the row no Delete, and there is no
    // batch to delete.
    await signInAs(page, viewer, password)
    const viewerHolds = await heldOnNote(page)
    expect(
      viewerHolds.permissions.list === true && viewerHolds.permissions.delete === false && viewerHolds.permissions.bulk_delete === false && viewerHolds.actions.length === 0,
      `UIX-16 precondition: the viewer's schema does not say list and nothing else: ${JSON.stringify(viewerHolds)}`,
    ).toBe(true)
    await openNote(page, title, id, 'viewer', 'UIX-16')
    await expect(rowBoxes, 'UIX-16: the grid offers a selection to an operator who may not delete and holds no action over one').toHaveCount(0)
    await expect(page.getByRole('button', { name: `Delete record ${id}` }), 'UIX-16: the row offers a Delete to an operator who may not delete').toHaveCount(0)
    await expect(batchDelete, 'UIX-16: the toolbar offers a batch Delete to an operator who may not delete').toHaveCount(0)
    const viewerScreen = await axeIn(page, 'main', OPERATOR_VIEW_RULES)
    expect(viewerScreen, `UIX-16: the viewer's Data Studio: ${JSON.stringify(viewerScreen, null, 2)}`).toEqual([])
    // Asked anyway, the server says no to both, and the note is still there.
    expect(await forced(page, 'DELETE', `/admin/api/models/Note/${id}`), 'UIX-16: the server deleted a record for an operator who may not delete').toBe(403)
    expect(await forced(page, 'POST', '/admin/api/models/Note/bulk', { action: 'delete', ids: [id] }), 'UIX-16: the server ran a batch delete for an operator who may not delete').toBe(403)

    // 2. The actor may delete one record and holds schedule, an action over
    // a selection — and not bulk_delete, which is what the server asks of a
    // batch delete. The grid keeps its selection for the action (O3), the
    // row offers its Delete, and a selection offers Schedule and no Delete.
    await signOut(page)
    await signInAs(page, actor, password)
    const actorHolds = await heldOnNote(page)
    expect(
      actorHolds.permissions.delete === true && actorHolds.permissions.bulk_delete === false && actorHolds.actions.includes('schedule'),
      `UIX-16 precondition: the actor's schema does not say delete, schedule and no bulk_delete: ${JSON.stringify(actorHolds)}`,
    ).toBe(true)
    await openNote(page, title, id, 'actor', 'UIX-16')
    await expect(page.getByRole('button', { name: `Delete record ${id}` }), 'UIX-16: the row offers no Delete to an operator who may delete it').toBeVisible()
    const rowBox = grid.getByRole('checkbox', { name: /toggle row selection/i })
    await expect(rowBox, 'UIX-16: the grid offers no selection to an operator who holds an action over one').toHaveCount(1, { timeout: 5_000 })
    await rowBox.check()
    await expect(page.getByRole('button', { name: /^Schedule 1$/ }), 'UIX-16: a selection offers the actor no Schedule').toBeVisible({ timeout: 5_000 })
    await expect(batchDelete, 'UIX-16: a selection offers a batch Delete to an operator the server refuses one (no bulk_delete)').toHaveCount(0)
    const actorScreen = await axeIn(page, 'main', OPERATOR_VIEW_RULES)
    expect(actorScreen, `UIX-16: the actor's Data Studio, with a selection: ${JSON.stringify(actorScreen, null, 2)}`).toEqual([])
    expect(await forced(page, 'POST', '/admin/api/models/Note/bulk', { action: 'delete', ids: [id] }), 'UIX-16: the server ran a batch delete for an operator without bulk_delete').toBe(403)
    expect(await forced(page, 'GET', `/admin/api/models/Note/${id}`), 'UIX-16: the note is gone after the refused deletes').toBe(200)
  })

  test('UIX-17 an operator who may not update is offered no edit: the row opens the record read-only, and its menu and the record view hold only the actions granted', async ({ page }) => {
    const { viewer, actor, password } = partialOperators('UIX-17')
    const stamp = Date.now()
    const title = `uix-17 note ${stamp}`
    // The body is not a column of the grid: the record view is where an
    // operator reads it.
    const body = `uix-17 body ${stamp}`
    const id = await noteAsAdmin(page, { title, body }, 'UIX-17')
    const view = page.getByRole('dialog').first()
    const offered = view.getByRole('group', { name: 'Actions on this record' })

    // readOnlyView opens the record from its row and reads it as a view:
    // every value, no input, no save.
    const readOnlyView = async (who: string) => {
      await expect(page.getByRole('button', { name: `Edit record ${id}` }), `UIX-17: the row offers the ${who} an Edit, and the ${who} may not update`).toHaveCount(0)
      const open = page.getByRole('button', { name: `View record ${id}` })
      await expect(open, `UIX-17: the row offers the ${who} no way to open the record, which the ${who} may read (retrieve) and not update`).toBeVisible({ timeout: 5_000 })
      await open.click()
      await expect(view, `UIX-17: the ${who}'s record view did not open`).toBeVisible({ timeout: 10_000 })
      await expect(view, `UIX-17: the ${who}'s record view does not say it is read-only`).toContainText('You may view this record, not change it.')
      await expect(view, `UIX-17: the ${who}'s record view does not show the record's body`).toContainText(body, { timeout: 10_000 })
      await expect(view.locator('input, textarea, select'), `UIX-17: the ${who}'s record view draws an input`).toHaveCount(0)
      await expect(view.getByRole('button', { name: /^(Update|Create|Save)/ }), `UIX-17: the ${who}'s record view offers a save`).toHaveCount(0)
    }

    // 1. The actor holds duplicate (on a record) and schedule (on a
    // selection and on a record), not download_text and not update.
    await signInAs(page, actor, password)
    const actorHolds = await heldOnNote(page)
    expect(
      actorHolds.permissions.update === false && actorHolds.permissions.retrieve === true && JSON.stringify(actorHolds.actions) === JSON.stringify(['duplicate', 'schedule']),
      `UIX-17 precondition: the actor's schema does not say retrieve, duplicate and schedule, and no update: ${JSON.stringify(actorHolds)}`,
    ).toBe(true)
    await openNote(page, title, id, 'actor', 'UIX-17')
    await readOnlyView('actor')
    const inView = (await offered.getByRole('button').allTextContents()).map((t) => t.trim()).sort()
    expect(inView, 'UIX-17: the actor\'s record view does not offer exactly the record actions granted').toEqual(['Duplicate', 'Schedule'])
    const viewViolations = await axeIn(page, '[role="dialog"]', OPERATOR_VIEW_RULES)
    expect(viewViolations, `UIX-17: the actor's read-only record view: ${JSON.stringify(viewViolations, null, 2)}`).toEqual([])
    await page.keyboard.press('Escape')
    await expect(view).toBeHidden({ timeout: 5_000 })

    // The row's menu — the way to a record action without opening the
    // record (O4) — holds the same two, and not the one never granted.
    const rowMenu = page.getByRole('button', { name: `Actions for record ${id}` })
    await expect(rowMenu, 'UIX-17: the row offers the actor no menu of the record actions granted').toBeVisible()
    await rowMenu.click()
    const menu = page.getByRole('menu')
    await expect(menu, 'UIX-17: the row\'s menu did not open').toBeVisible({ timeout: 5_000 })
    const inMenu = (await menu.getByRole('menuitem').allTextContents()).map((t) => t.trim()).sort()
    expect(inMenu, 'UIX-17: the row\'s menu does not hold exactly the record actions granted').toEqual(['Duplicate', 'Schedule'])
    const menuViolations = await axeIn(page, '[role="menu"]', ['color-contrast', 'aria-required-children', 'aria-valid-attr-value'])
    expect(menuViolations, `UIX-17: the actor's row menu: ${JSON.stringify(menuViolations, null, 2)}`).toEqual([])
    await page.keyboard.press('Escape')
    await expect(menu).toBeHidden({ timeout: 5_000 })

    // Asked anyway: the write and the action never granted are refused, and
    // the note still says what it said.
    expect(await forced(page, 'PUT', `/admin/api/models/Note/${id}`, { title: 'uix-17 forced' }), 'UIX-17: the server wrote a record for an operator who may not update').toBe(403)
    expect(await forced(page, 'POST', `/admin/api/models/Note/actions/download_text/${id}`, {}), 'UIX-17: the server ran a record action the operator was never granted').toBe(403)

    // 2. The viewer holds no action: the record still opens read-only, and
    // neither the row nor the view offers one.
    await signOut(page)
    await signInAs(page, viewer, password)
    await openNote(page, title, id, 'viewer', 'UIX-17')
    await expect(page.getByRole('button', { name: `Actions for record ${id}` }), 'UIX-17: the row offers a menu of actions to an operator granted none').toHaveCount(0)
    await readOnlyView('viewer')
    await expect(offered, 'UIX-17: the viewer\'s record view offers actions, and the viewer was granted none').toHaveCount(0)
    await page.keyboard.press('Escape')
    await expect(view).toBeHidden({ timeout: 5_000 })
    expect(await forced(page, 'PUT', `/admin/api/models/Note/${id}`, { title: 'uix-17 forced' }), 'UIX-17: the server wrote a record for the viewer').toBe(403)
    const kept = await page.evaluate(async (path) => {
      const r = await fetch(path, { credentials: 'same-origin' })
      const out = await r.json().catch(() => ({}))
      return String(out?.data?.title ?? out?.title ?? '')
    }, `/admin/api/models/Note/${id}`)
    expect(kept, 'UIX-17: the note no longer says what it said after the refused writes').toBe(title)
  })

  /*
   * UIX-18 is the rest of Data Studio's doors (OR-65). UIX-16 and UIX-17
   * read the deletes and the edit; every other door asks the server for a
   * verb of its own, and the screen drew each of them for anybody: the
   * sidebar every model (get_schema and list), a row its history
   * (retrieve), the toolbar an export and an import (export_data and
   * import_data, both of admin:*), the model's header its field settings
   * (update_schema), and a shared view its removal (its owner's alone).
   * For each operator the control reads what is offered beside what is
   * not, holds the screen to the panel's rules, and asks the server for
   * each door the screen left out. The actor's export is then read for what
   * it holds (OR-66): the export is granted on admin:*, and what it carries
   * is what the actor may list — the notes without the field kept from
   * them, their own article and not another's, and no model they may not
   * list. And a field kept from the actor is not one they can ask about
   * (OR-69): the grid offers no sort or filter on it, a view somebody
   * shared that filters by it is not listed to them, and the list asked
   * anyway refuses the filter and the sort — the rows a filter leaves, and
   * the order a sort puts them in, are what the field holds. Last, the
   * import (OR-67): it is granted on admin:* too, and each row it writes
   * asks what the record form asks. The actor, who may create a note and
   * not update one, is offered the import; a file with a field kept from
   * them is refused in the dialog, with the row and the field named and
   * nothing written, the same file without it lands, and a file that would
   * update a note is refused at both steps. The lister holds the import and
   * no write of Note, and is offered none.
   */
  test('UIX-18 an operator is offered a model, a record\'s history, an export, an import, the field settings and a saved view\'s removal only where they hold them, each one asked anyway is refused, an export holds only what the operator may list, a field the operator may not read is neither offered nor answered as a sort, a filter or a saved view, and an import writes only what the operator could write by hand', async ({ page }) => {
    // Four sign-ins: the admin who sets the stage and three operators.
    test.setTimeout(120_000)
    const { viewer, actor, lister, password } = partialOperators('UIX-18')
    const stamp = Date.now()
    const title = `uix-18 note ${stamp}`
    const id = await noteAsAdmin(page, { title, body: 'uix-18 body' }, 'UIX-18')

    // A view of Notes the admin shares: every operator who may list notes
    // is shown it, and only its owner (or a superuser) may remove it.
    const shared = `uix-18 shared ${stamp}`
    await signIn(page)
    const sharedId = await page.evaluate(async (name) => {
      const r = await fetch('/admin/api/views', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ model: 'Note', name, query: '', is_shared: true }),
      })
      const out = await r.json().catch(() => ({}))
      return r.status < 300 ? String(out?.id ?? '') : ''
    }, shared)
    expect(sharedId, 'UIX-18 precondition: the admin could not share a view of Notes').not.toBe('')
    // A second shared view, filtered by a field the actor may not read
    // (OR-69): the viewer, who reads it, is shown it; the actor is not.
    const byViews = `uix-18 by views ${stamp}`
    const byViewsId = await page.evaluate(async (name) => {
      const r = await fetch('/admin/api/views', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ model: 'Note', name, query: 'views__gte=0', is_shared: true }),
      })
      const out = await r.json().catch(() => ({}))
      return r.status < 300 ? String(out?.id ?? '') : ''
    }, byViews)
    expect(byViewsId, 'UIX-18 precondition: the admin could not share a view of Notes filtered by views').not.toBe('')
    // What the actor's export is read against (OR-66): two articles, one
    // the actor's — the actor may list their own and no other
    // (admin:Article#own) — and a credential, of a model the actor may not
    // list at all.
    const actorArticle = `uix-18 the actor's article ${stamp}`
    const otherArticle = `uix-18 another's article ${stamp}`
    const seeded = await page.evaluate(async (rows) => {
      const statuses: number[] = []
      for (const [model, row] of rows) {
        const r = await fetch(`/admin/api/models/${model}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          credentials: 'same-origin',
          body: JSON.stringify(row),
        })
        statuses.push(r.status)
      }
      return statuses
    }, [
      ['Article', { title: actorArticle, owner: actor }],
      ['Article', { title: otherArticle, owner: viewer }],
      ['Credential', { label: `uix-18 credential ${stamp}`, password: 'uix-18-password', api_token: 'uix-18-token' }],
    ] as Array<[string, Record<string, unknown>]>)
    expect(seeded.every((status) => status < 300), `UIX-18 precondition: creating the articles and the credential answered ${seeded}`).toBe(true)
    await signOut(page)

    const exportBody = { format: 'json', models: ['Note'] }
    const importPath = '/admin/api/import/validate?key=_tmp/uix-18.json'
    const importBody = { model: 'Note', format: 'json' }
    const toolbarTransfer = page.getByRole('button', { name: /^(Export|Import)/ })

    // 1. The viewer may list and open a note, and holds none of the panel's
    // transfer, the field settings or any other model.
    await signInAs(page, viewer, password)
    const viewerHolds = await heldOnNote(page)
    expect(
      viewerHolds.permissions.retrieve === true && viewerHolds.permissions.export_data === false &&
        viewerHolds.permissions.import_data === false && viewerHolds.permissions.update_schema === false,
      `UIX-18 precondition: the viewer's schema does not say retrieve, and no export, import or field settings: ${JSON.stringify(viewerHolds)}`,
    ).toBe(true)
    // The models the panel lists and the viewer may not open, by the
    // payload the sidebar is drawn from.
    const closed = await page.evaluate(async () => {
      const r = await fetch('/admin/api/models', { credentials: 'same-origin' })
      const out = r.ok ? await r.json() : {}
      return ((out.models ?? []) as Array<{ name: string; plural: string; permissions?: Record<string, boolean> }>)
        .filter((m) => m.permissions?.get_schema === false || m.permissions?.list === false)
        .map((m) => ({ name: m.name, label: m.plural || m.name }))
    })
    expect(closed.length, 'UIX-18 precondition: the model list names no model the viewer may not open').toBeGreaterThan(0)
    await openNote(page, title, id, 'viewer', 'UIX-18')
    const escaped = (text: string) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    const sidebarModel = (label: string) => page.getByRole('button', { name: new RegExp('^' + escaped(label)) })
    await expect(sidebarModel('Notes'), 'UIX-18 precondition: the viewer\'s sidebar does not offer Notes').toBeVisible()
    for (const model of closed) {
      await expect(sidebarModel(model.label), `UIX-18: the sidebar offers the viewer ${model.label}, whose schema or list the server refuses them`).toHaveCount(0)
    }
    await expect(page.getByRole('heading', { name: 'Notes' }), 'UIX-18 precondition: the viewer\'s model header is not drawn').toBeVisible()
    await expect(page.getByRole('button', { name: 'Configure fields' }), 'UIX-18: the header offers the viewer the field settings, which the server asks update_schema for').toHaveCount(0)
    await expect(page.getByRole('button', { name: /^Filters/ }), 'UIX-18 precondition: the viewer\'s toolbar is not drawn').toBeVisible()
    await expect(toolbarTransfer, 'UIX-18: the toolbar offers the viewer an export or an import, and the viewer holds neither').toHaveCount(0)
    await expect(page.getByRole('button', { name: shared, exact: true }), 'UIX-18 precondition: the viewer is not shown the shared view').toBeVisible({ timeout: 5_000 })
    // The viewer reads a note's views: the grid sorts by it, and the view
    // filtered by it is theirs to see — what the actor's screen is read
    // against below.
    await expect(page.getByRole('columnheader', { name: 'Views' }), 'UIX-18 precondition: the viewer\'s grid draws no Views column').toBeVisible()
    await expect(page.getByRole('button', { name: byViews, exact: true }), 'UIX-18 precondition: the viewer, who reads views, is not shown the view filtered by it').toBeVisible({ timeout: 5_000 })
    await expect(page.getByRole('button', { name: `Remove the view ${shared}` }), 'UIX-18: the viewer is offered the removal of a view somebody else shared').toHaveCount(0)
    // Their own view, saved beside it, is theirs to remove.
    const own = `uix-18 own ${stamp}`
    await page.getByRole('textbox', { name: 'Name for the current view' }).fill(own)
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByRole('button', { name: `Remove the view ${own}` }), 'UIX-18: the viewer is not offered the removal of the view they saved').toBeVisible({ timeout: 5_000 })
    const viewerScreen = await axeIn(page, 'main', OPERATOR_VIEW_RULES)
    expect(viewerScreen, `UIX-18: the viewer's Data Studio: ${JSON.stringify(viewerScreen, null, 2)}`).toEqual([])
    // Asked anyway, the server says no to each.
    expect(await forced(page, 'POST', '/admin/api/exports', exportBody), 'UIX-18: the server exported for an operator without export_data').toBe(403)
    expect(await forced(page, 'POST', importPath, importBody), 'UIX-18: the server took an import from an operator without import_data').toBe(403)
    expect(await forced(page, 'PUT', '/admin/api/models/Note/schema/fields', { fields: { Title: { label: 'uix-18 forced' } } }), 'UIX-18: the server changed the field settings for an operator without update_schema').toBe(403)
    expect(await forced(page, 'GET', `/admin/api/models/${closed[0].name}/schema`), `UIX-18: the server opened ${closed[0].name} for an operator the sidebar did not offer it to`).toBe(403)
    expect(await forced(page, 'DELETE', `/admin/api/views/${sharedId}`), 'UIX-18: the server removed a shared view for an operator who does not own it').toBe(403)

    // 2. The actor holds the export, and the import with create on Note
    // and no update: the toolbar offers both.
    await signOut(page)
    await signInAs(page, actor, password)
    const actorHolds = await heldOnNote(page)
    expect(
      actorHolds.permissions.export_data === true && actorHolds.permissions.import_data === true &&
        actorHolds.permissions.create === true && actorHolds.permissions.update === false,
      `UIX-18 precondition: the actor's schema does not say export, import and create, and no update: ${JSON.stringify(actorHolds)}`,
    ).toBe(true)
    await openNote(page, title, id, 'actor', 'UIX-18')
    // A field kept from the actor (OR-69): no column to sort by, no filter,
    // and no view somebody shared that filters by it.
    await expect(page.getByRole('columnheader', { name: 'Title' }), 'UIX-18 precondition: the actor\'s grid draws no header').toBeVisible()
    await expect(page.getByRole('columnheader', { name: 'Views' }), 'UIX-18: the actor\'s grid offers a sort by views, which the actor may not read').toHaveCount(0)
    const filtersToggle = page.getByRole('button', { name: /^Filters/ })
    await filtersToggle.click()
    await expect(page.getByLabel('Status', { exact: true }), 'UIX-18 precondition: the actor\'s filters did not open').toBeVisible({ timeout: 5_000 })
    await expect(page.getByLabel('Views', { exact: true }), 'UIX-18: the actor is offered a filter on views, which the actor may not read').toHaveCount(0)
    await filtersToggle.click()
    await expect(page.getByLabel('Status', { exact: true })).toBeHidden({ timeout: 5_000 })
    await expect(page.getByRole('button', { name: shared, exact: true }), 'UIX-18 precondition: the actor is not shown the shared view').toBeVisible({ timeout: 5_000 })
    await expect(page.getByRole('button', { name: byViews, exact: true }), 'UIX-18: the actor is shown a view filtered by views, which the actor may not read').toHaveCount(0)
    const exportToggle = page.getByRole('button', { name: 'Export / Import', exact: true })
    await expect(exportToggle, 'UIX-18: the toolbar offers the actor no export and import, which the actor holds').toBeVisible()
    await exportToggle.click()
    await expect(page.getByRole('button', { name: /^Export (CSV|JSON|SQL)$/ }), 'UIX-18: the actor\'s export panel offers no export').toBeVisible({ timeout: 5_000 })
    const importButton = page.getByRole('button', { name: 'Import…', exact: true })
    await expect(importButton, 'UIX-18: the actor\'s export panel offers no import, and the actor holds import_data and create').toBeVisible()
    const actorScreen = await axeIn(page, 'main', OPERATOR_VIEW_RULES)
    expect(actorScreen, `UIX-18: the actor's Data Studio, with the export open: ${JSON.stringify(actorScreen, null, 2)}`).toEqual([])
    expect(await forced(page, 'POST', '/admin/api/exports', exportBody), 'UIX-18: the server refused the actor the export it offered').toBe(200)

    // What the actor's import writes (OR-67). In the dialog, a file with a
    // field kept from the actor is refused whole, with the row and the
    // field named, and nothing is written.
    const refusedTitle = `uix-18 refused import ${stamp}`
    const importedTitle = `uix-18 imported ${stamp}`
    const asFile = (rows: unknown[]) => ({ name: 'uix-18.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(rows)) })
    await importButton.click()
    const importDialog = page.getByRole('dialog').filter({ hasText: 'Import into' })
    await expect(importDialog, 'UIX-18 precondition: the import dialog did not open').toBeVisible({ timeout: 5_000 })
    await importDialog.locator('#import-file').setInputFiles(asFile([{ title: refusedTitle, status: 'draft' }, { title: refusedTitle, status: 'draft', meta: 'uix-18 meta' }]))
    await importDialog.getByRole('button', { name: 'Validate' }).click()
    const refusal = importDialog.getByRole('alert')
    await expect(refusal, 'UIX-18: the dialog does not say why the actor\'s file with a field kept from them was refused').toContainText('row 2', { timeout: 10_000 })
    await expect(refusal, 'UIX-18: the refusal does not name the field kept from the actor').toContainText('meta')
    await expect(refusal, 'UIX-18: the refusal does not say nothing was written').toContainText('nothing in the file was written')
    await expect(importDialog.getByRole('button', { name: /^Import \d+ row/ }), 'UIX-18: the dialog offers to import a file the server refused').toHaveCount(0)
    const refusedDialog = await axeIn(page, '[role="dialog"]', OPERATOR_VIEW_RULES)
    expect(refusedDialog, `UIX-18: the import dialog with the refusal showing: ${JSON.stringify(refusedDialog, null, 2)}`).toEqual([])
    expect(await notesTitled(page, refusedTitle), 'UIX-18: a refused file wrote a note').toBe(0)
    // The same rows without the field land, created by the actor.
    await importDialog.locator('#import-file').setInputFiles(asFile([{ title: importedTitle, status: 'draft' }]))
    await importDialog.getByRole('button', { name: 'Validate' }).click()
    const importRows = importDialog.getByRole('button', { name: 'Import 1 row' })
    await expect(importRows, 'UIX-18: the dialog does not let the actor import a file of rows they may create').toBeVisible({ timeout: 10_000 })
    await importRows.click()
    await expect(importDialog, 'UIX-18: the actor\'s import of rows they may create did not finish').toBeHidden({ timeout: 10_000 })
    expect(await notesTitled(page, importedTitle), 'UIX-18: the actor\'s import of rows they may create wrote no note').toBe(1)
    // A file that would update a note is refused at both steps: the actor
    // may not update one by hand. The note keeps its title.
    for (const step of ['validate', 'execute'] as const) {
      const updated = await importAs(page, step, [{ id: Number(id), title: `${title} overwritten` }], 'update')
      expect(updated.status, `UIX-18: the ${step} step took from the actor a file that updates a note: ${updated.message}`).toBe(403)
      expect(updated.message, `UIX-18: the ${step} step's refusal does not name the update the actor does not hold`).toContain('update')
    }
    expect(await notesTitled(page, title), 'UIX-18: a refused file updated the note').toBe(1)
    // Asked anyway (OR-69): a filter or a sort on views is refused, with the
    // answer a field the model does not have gets; one on a field the actor
    // reads is answered.
    for (const query of ['views=0', 'views__gte=0', 'order_by=views%20desc']) {
      expect(await forced(page, 'GET', `/admin/api/models/Note?${query}`), `UIX-18: the server answered ?${query} to the actor, who may not read views`).toBe(400)
    }
    expect(await forced(page, 'GET', '/admin/api/models/Note?order_by=title%20desc'), 'UIX-18: the server refused the actor a sort by a field the actor reads').toBe(200)
    const actorViews = await page.evaluate(async () => {
      const r = await fetch('/admin/api/views?model=Note', { credentials: 'same-origin' })
      return r.ok ? JSON.stringify(await r.json()) : `status ${r.status}`
    })
    expect(actorViews, 'UIX-18: the views listed to the actor hold the one filtered by views').not.toContain(byViewsId)

    // What the actor's export holds (OR-66). An export of every model, cut
    // and downloaded as the actor: the notes, without the field the actor
    // may not read; the actor's own article, not another's; and nothing of
    // a model the actor may not list.
    const exported = await page.evaluate(async () => {
      const made = await fetch('/admin/api/exports', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ format: 'json', models: [] }),
      })
      const out = await made.json().catch(() => ({}))
      const key = String(out?.storage_key ?? '')
      if (made.status !== 200 || key === '') return { status: made.status, records: [] as Array<Record<string, unknown>> }
      const got = await fetch(`/admin/api/exports/download?key=${encodeURIComponent(key)}`, { credentials: 'same-origin' })
      return { status: got.status, records: (got.ok ? await got.json() : []) as Array<Record<string, unknown>> }
    })
    expect(exported.status, 'UIX-18: the actor could not cut and download an export of every model').toBe(200)
    const exportedModels = [...new Set(exported.records.map((r) => String(r._model)))].sort()
    expect(exportedModels, 'UIX-18: the actor\'s export of every model holds a model the actor may not list (a credential), or lost one they may').toEqual(['Article', 'Note'])
    const exportedArticles = exported.records.filter((r) => r._model === 'Article').map((r) => String(r.title))
    expect(exportedArticles, 'UIX-18: the actor\'s export lost the article the actor owns').toContain(actorArticle)
    expect(exportedArticles, 'UIX-18: the actor\'s export holds an article another operator owns').not.toContain(otherArticle)
    const exportedNotes = exported.records.filter((r) => r._model === 'Note')
    expect(exportedNotes.some((r) => r.title === title), 'UIX-18 precondition: the actor\'s export holds no note of this control').toBe(true)
    expect(exportedNotes.filter((r) => 'meta' in r).length, 'UIX-18: the actor\'s export holds a field of Note the actor may not read (meta)').toBe(0)
    expect(exportedNotes.filter((r) => 'views' in r).length, 'UIX-18: the actor\'s export holds a field of Note the actor may not read (views)').toBe(0)
    // The model the actor may not list, asked for by name, is refused —
    // and the payload the screens draw from says so.
    const credential = await page.evaluate(async () => {
      const r = await fetch('/admin/api/models', { credentials: 'same-origin' })
      const out = r.ok ? await r.json() : {}
      const model = ((out.models ?? []) as Array<{ name: string; permissions?: Record<string, boolean> }>).find((m) => m.name === 'Credential')
      return model?.permissions ?? {}
    })
    expect(credential.list === false && credential.export_data === false, `UIX-18: the model list offers the actor an export of Credential, which the actor may not list: ${JSON.stringify(credential)}`).toBe(true)
    expect(await forced(page, 'POST', '/admin/api/exports', { format: 'json', models: ['Credential'] }), 'UIX-18: the server exported Credential for an operator who may not list it').toBe(403)

    // 3. The lister may list the notes and not open one: no history, no
    // view, and no Actions column with nothing in it.
    await signOut(page)
    await signInAs(page, lister, password)
    const listerHolds = await heldOnNote(page)
    expect(
      listerHolds.permissions.list === true && listerHolds.permissions.retrieve === false && listerHolds.actions.length === 0,
      `UIX-18 precondition: the lister's schema does not say list and no retrieve: ${JSON.stringify(listerHolds)}`,
    ).toBe(true)
    await narrowToNote(page, title, 'lister', 'UIX-18')
    await expect(page.locator(GRID_ROWS).first().getByText(title), 'UIX-18 precondition: the lister\'s row does not show the note').toBeVisible()
    await expect(page.getByRole('columnheader', { name: 'Title' }), 'UIX-18 precondition: the lister\'s grid draws no header').toBeVisible()
    await expect(page.getByRole('button', { name: `History of record ${id}` }), 'UIX-18: the row offers the lister a history, which the server asks retrieve for').toHaveCount(0)
    await expect(page.getByRole('button', { name: `View record ${id}` }), 'UIX-18: the row offers the lister a record they may not open').toHaveCount(0)
    await expect(page.getByRole('columnheader', { name: 'Actions' }), 'UIX-18: the grid draws the lister an Actions column with nothing to offer in it').toHaveCount(0)
    // The lister holds the import and no write of Note (OR-67): the schema
    // does not say import, the toolbar offers none, and a file of rows to
    // create, asked anyway, is refused for the create the lister lacks.
    expect(listerHolds.permissions.import_data, 'UIX-18: the lister\'s schema offers an import of Note, which the lister may neither create nor update rows of').toBe(false)
    await expect(toolbarTransfer, 'UIX-18: the toolbar offers the lister an import of Note, which the lister may not write').toHaveCount(0)
    const listerImport = await importAs(page, 'validate', [{ title: `uix-18 lister import ${stamp}`, status: 'draft' }])
    expect(listerImport.status, `UIX-18: the server took from the lister a file of notes to create: ${listerImport.message}`).toBe(403)
    expect(listerImport.message, 'UIX-18: the refusal of the lister\'s file does not name the create the lister lacks').toContain('create')
    const listerScreen = await axeIn(page, 'main', OPERATOR_VIEW_RULES)
    expect(listerScreen, `UIX-18: the lister's Data Studio: ${JSON.stringify(listerScreen, null, 2)}`).toEqual([])
    expect(await forced(page, 'GET', `/admin/api/models/Note/${id}/history`), 'UIX-18: the server answered a record\'s history to an operator who may not open it').toBe(403)
  })
})
