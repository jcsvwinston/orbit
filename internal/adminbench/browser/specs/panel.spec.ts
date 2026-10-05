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
})
