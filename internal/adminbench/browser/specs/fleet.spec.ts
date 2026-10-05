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
  /**
   * UIF-06 the same screens in the dark theme. UIF-02 reads the theme the
   * fleet opens in (light); the palette is held to AA as a whole in both
   * (OR-59, ui/tools/fleet-palette.test.ts), and this reads the dark one off
   * the screens.
   */
  test('UIF-06 the fleet screens are legible in the dark theme too', async ({ page }) => {
    await page.goto('/')
    await page.evaluate(() => window.localStorage.setItem('orbit.theme', 'dark'))
    for (const path of ['/', '/#/nodes', '/#/data-studio', '/#/audit']) {
      await page.goto(path)
      await expect(page.locator('#root')).not.toBeEmpty()
      await expect(page.locator('html'), 'UIF-06 precondition: the fleet did not open in the dark theme').toHaveAttribute('data-theme', 'dark')
      const violations = await axeViolations(page, ['color-contrast'])
      expect(violations, `${path}: ${JSON.stringify(violations)}`).toEqual([])
    }
  })

  /**
   * UIF-07 the fleet's scripts and stylesheets reach the browser compressed
   * (OR-61): an encoding the browser asked for, an answer that says it
   * varies, and nothing of 1 KiB or more as it is.
   */
  test("UIF-07 the fleet's scripts and stylesheets reach the browser compressed", async ({ page }) => {
    const fetched: { path: string; encoding: string; vary: string; length: number; size: number; asked: string }[] = []
    page.on('response', async (res) => {
      const url = new URL(res.url())
      if (!/\/assets\/[^/]+\.(js|css)$/.test(url.pathname)) return
      const headers = await res.allHeaders()
      fetched.push({
        path: url.pathname,
        encoding: headers['content-encoding'] ?? '',
        vary: headers['vary'] ?? '',
        length: Number(headers['content-length'] ?? -1),
        size: (await res.body().catch(() => Buffer.alloc(0))).length,
        asked: (await res.request().allHeaders())['accept-encoding'] ?? '',
      })
    })
    await page.goto('/')
    await expect(page.locator('#root')).not.toBeEmpty()
    // The overview streams for as long as it is open, so the network never
    // goes idle: wait for the document's own script and stylesheet instead.
    await expect
      .poll(() => fetched.map((f) => f.path.split('.').pop()).sort().join(','), {
        message: "UIF-07 precondition: the browser fetched the fleet's script and stylesheet",
      })
      .toBe('css,js')
    // What was negotiated, for the report: the instrument's browser decides.
    test.info().annotations.push({ type: 'encodings', description: [...new Set(fetched.map((f) => f.encoding || 'identity'))].sort().join(', ') })
    const built = fetched.filter((f) => f.size >= 1024)
    expect(built.length, 'UIF-07 precondition: no file the browser fetched measured 1 KiB or more').toBeGreaterThan(1)
    const plain = built.filter((f) => f.encoding === '')
    expect(plain, `UIF-07: files of 1 KiB or more that travelled uncompressed: ${JSON.stringify(plain)}`).toEqual([])
    const wrong = fetched.filter((f) => f.encoding !== '' && (!f.asked.includes(f.encoding) || !/accept-encoding/i.test(f.vary)))
    expect(wrong, `UIF-07: an encoding not asked for, or an answer that does not vary: ${JSON.stringify(wrong)}`).toEqual([])
    // Compressed by the build: Brotli to a browser that accepts it, with a length.
    const once = built.filter((f) => (/\bbr\b/.test(f.asked) && f.encoding !== 'br') || !(f.length > 0))
    expect(once, `UIF-07: not the build's encoding (Brotli when accepted, with a length): ${JSON.stringify(once)}`).toEqual([])
  })
})
