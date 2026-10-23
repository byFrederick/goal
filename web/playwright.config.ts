import { defineConfig } from '@playwright/test';

// The critical flows at the three widths the design targets, against a production build served
// by vite preview with the in-memory mock API (GOAL_API=mock). One worker: the mock keeps state,
// and every test resets it first.
const port = 4174;

export default defineConfig({
  testDir: 'e2e',
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
    { name: 'tablet-768', use: { viewport: { width: 768, height: 1024 } } },
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
