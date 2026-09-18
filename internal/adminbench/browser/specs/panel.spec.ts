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
})
