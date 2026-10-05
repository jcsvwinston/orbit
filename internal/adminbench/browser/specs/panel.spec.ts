import { expect, test, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'

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
   * is about. UIX-07 is present since A11 O1; its preconditions stay, so a
   * regression fails for the reason it is.
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

  test('UIX-08 the record view offers the action the application declared', async ({ page }) => {
    await signIn(page)
    // A row of its own to open: the bench's application starts empty. The
    // call is made from the page, the way the SPA makes it — the session
    // belongs to this browser, and a request from outside it (page.request)
    // is refused with a 401.
    const status = await page.evaluate(async () => {
      const r = await fetch('/admin/api/models/Note', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify({ title: 'uix-08 record', status: 'draft' }),
      })
      return r.status
    })
    expect(status < 300, `UIX-08 precondition: creating a note answered ${status}`).toBe(true)

    await page.goto('/admin/data-studio')
    await page.waitForLoadState('networkidle')
    await page.getByRole('button', { name: /^Notes/ }).first().click({ timeout: 15_000 })

    // The schema the screen loads offers the action to this operator —
    // DS-09's surface, and the precondition here: an action nobody offers
    // would make this control measure the declaration, not the record view.
    const offered = await page.evaluate(async () => {
      const r = await fetch('/admin/api/models/Note/schema', { credentials: 'same-origin' })
      const schema = await r.json()
      return (schema.actions ?? []).some((a: { name: string }) => a.name === 'publish')
    })
    expect(offered, 'UIX-08 precondition: the schema offers no publish action (DS-09 should be red too)').toBe(true)

    await page.getByRole('button', { name: /^Edit record/ }).first().waitFor({ timeout: 15_000 })
    await page.getByRole('button', { name: /^Edit record/ }).first().click()
    const dialog = page.getByRole('dialog').first()
    await expect(dialog, 'UIX-08 precondition: the record view did not open').toBeVisible({ timeout: 10_000 })

    await expect(
      dialog.getByRole('button', { name: /publish/i }),
      'UIX-08: the record view offers no action of the application — publish is declared on Note and the open record has no button for it',
    ).toHaveCount(1, { timeout: 5_000 })
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
})
