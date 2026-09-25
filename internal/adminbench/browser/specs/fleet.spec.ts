import { expect, test } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'

/**
 * What the FLEET plane does in a browser — the half the Go bench cannot
 * measure, over the admin server's own UI (ADR-015: the fleet entry of the
 * one frontend project). Driven from Go (internal/fleettest), which boots an
 * admin server with a credential-less loopback operator and one connected
 * agent, and passes the UI listener's URL in ORBIT_BENCH_URL.
 *
 * Every test here is one control, named UIF-NN, and asserts by effect on the
 * rendered page, like the panel's UIX controls. The verdicts are recorded in
 * Go (internal/fleettest/browserbench_test.go).
 */

const NODE_ID = process.env.ORBIT_BENCH_NODE_ID ?? ''

async function axeViolations(page: import('@playwright/test').Page, rules: string[]) {
  const results = await new AxeBuilder({ page }).withRules(rules).analyze()
  return results.violations.map((v) => ({
    id: v.id,
    impact: v.impact,
    nodes: v.nodes.slice(0, 3).map((n) => n.target.join(' ')),
  }))
}

test.describe('UIF', () => {
  /** UIF-00 measures the instrument: a planted violation must be caught. */
  test('UIF-00 the instrument bites: a planted violation is caught', async ({ page }) => {
    await page.goto('/')
    await page.evaluate(() => {
      const planted = document.createElement('div')
      planted.id = 'planted-violation'
      planted.innerHTML =
        '<button id="planted-button"></button>' +
        '<p id="planted-text" style="color:#777;background:#888">unreadable</p>'
      document.body.appendChild(planted)
    })
    const violations = await axeViolations(page, ['button-name', 'color-contrast'])
    expect(violations.map((v) => v.id).sort()).toEqual(['button-name', 'color-contrast'])
  })

  /** UIF-01 the fleet UI loads and lists the connected node. */
  test('UIF-01 the overview loads and lists the connected agent', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('#root')).not.toBeEmpty()
    await page.goto('/#/nodes')
    await expect(page.getByText(NODE_ID, { exact: false }).first()).toBeVisible({ timeout: 15_000 })
  })

  /** UIF-02 the screens an operator opens are legible: text meets contrast. */
  test('UIF-02 the fleet screens are legible: text meets contrast', async ({ page }) => {
    for (const path of ['/', '/#/nodes', '/#/data-studio', '/#/audit']) {
      await page.goto(path)
      await expect(page.locator('#root')).not.toBeEmpty()
      const violations = await axeViolations(page, ['color-contrast'])
      expect(violations, `${path}: ${JSON.stringify(violations)}`).toEqual([])
    }
  })

  /** UIF-03 every control says what it is: names, roles and labels. */
  test('UIF-03 every control says what it is: names, roles and labels', async ({ page }) => {
    for (const path of ['/', '/#/nodes', '/#/data-studio']) {
      await page.goto(path)
      await expect(page.locator('#root')).not.toBeEmpty()
      const violations = await axeViolations(page, ['button-name', 'link-name', 'label', 'aria-allowed-attr', 'aria-valid-attr-value'])
      expect(violations, `${path}: ${JSON.stringify(violations)}`).toEqual([])
    }
  })

  /** UIF-04 the document says what it is: language, landmarks, one main heading. */
  test('UIF-04 the document says what it is: language, landmarks, one main heading', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('#root')).not.toBeEmpty()
    await expect(page.locator('html')).toHaveAttribute('lang', /.+/)
    const violations = await axeViolations(page, ['landmark-one-main', 'page-has-heading-one', 'region'])
    expect(violations, JSON.stringify(violations)).toEqual([])
  })

  /** UIF-05 the keyboard reaches the navigation, and the focus is visible. */
  test('UIF-05 the keyboard reaches the navigation, and the focus is visible', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('#root')).not.toBeEmpty()
    const nav = page.locator('nav a, nav button').first()
    await expect(nav).toBeVisible()
    // Tab until something inside the navigation has focus, bounded.
    let reached = false
    for (let i = 0; i < 40 && !reached; i++) {
      await page.keyboard.press('Tab')
      reached = await page.evaluate(() => {
        const el = document.activeElement
        return !!el && !!el.closest('nav')
      })
    }
    expect(reached, 'Tab never reached the navigation').toBe(true)
    const visible = await page.evaluate(() => {
      const el = document.activeElement as HTMLElement | null
      if (!el) return false
      const cs = getComputedStyle(el)
      return cs.outlineStyle !== 'none' || cs.boxShadow !== 'none'
    })
    expect(visible, 'the focused navigation item has no visible focus').toBe(true)
  })
})
