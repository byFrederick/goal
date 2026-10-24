import { defineConfig } from '@playwright/test';

// The critical flows at the three widths the design targets, against a production build served
// by vite preview with the in-memory mock API (GOAL_API=mock). One worker: the mock keeps state,
// and every test resets it first. GOAL_E2E_PORT serves it elsewhere when 4174 is taken.
const port = Number(process.env.GOAL_E2E_PORT ?? 4174);

// Each run keeps its artifacts in its own folder, so two runs in one checkout never empty each
// other's: the runner names it once and its workers inherit the name.
process.env.GOAL_E2E_RUN ??= `${new Date().toISOString().replace(/[:.]/g, '-')}-${process.pid}`;

export default defineConfig({
  testDir: 'e2e',
  outputDir: `test-results/${process.env.GOAL_E2E_RUN}`,
  fullyParallel: false,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    browserName: 'chromium',
    reducedMotion: 'reduce',
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'phone-375', use: { viewport: { width: 375, height: 812 }, hasTouch: true } },
    // The mock answers in no time; this project waits 200 ms per answer (longer than the router's
    // 150 ms before a pending screen), so loads overlap navigations as they do against goald.
    {
      name: 'tablet-768',
      use: { viewport: { width: 768, height: 1024 }, extraHTTPHeaders: { 'X-Mock-Latency-Ms': '200' } },
    },
    { name: 'desktop-1440', use: { viewport: { width: 1440, height: 1000 } } },
  ],
  webServer: {
    command: `pnpm exec vite build && pnpm exec vite preview --host 127.0.0.1 --port ${port} --strictPort`,
    url: `http://127.0.0.1:${port}/v1/health`,
    env: { GOAL_API: 'mock' },
    reuseExistingServer: false,
    timeout: 120_000,
  },
});
