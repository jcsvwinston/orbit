import { defineConfig } from '@playwright/test'

/**
 * The browser half of the admin bench.
 *
 * It is driven from Go (browserbench_test.go), which boots the same
 * application the rest of the bench probes and passes its URL in
 * ORBIT_BENCH_URL. Playwright never starts a server of its own: the panel
 * under test has to be the panel the Go bench measures, or the two halves
 * would be reporting on different things.
 */
export default defineConfig({
  testDir: './specs',
  // One worker: the probes sign into the same application, and a bench that
  // raced itself would measure the race.
  workers: 1,
  fullyParallel: false,
  timeout: 30_000,
  retries: 0,
  use: {
    baseURL: process.env.ORBIT_BENCH_URL ?? 'http://127.0.0.1:8080',
    headless: true,
    // A viewport, because half of what this instrument exists to see —
    // focus rings, contrast, whether a dialog can be reached — has no
    // meaning without one.
    viewport: { width: 1280, height: 800 },
  },
  reporter: [['json', { outputFile: 'results.json' }]],
})
