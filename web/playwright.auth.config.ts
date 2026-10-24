import { defineConfig } from '@playwright/test';

// `task web:e2e-auth` (spec 3 §13): the sign-in flow against a real goald under GOAL_AUTH=session,
// its identity service and Mailpit, not the mock. Start them first (GOAL_AUTH=session goal up);
// GOAL_E2E_AUTH_URL names goald (default the local one), GOAL_MAILPIT_URL Mailpit's API and
// GOAL_MAILPIT_AUTH its user:password (~/.config/goal/mailpit-ui-auth, which goal up writes). Nothing
// is started or stopped here.
process.env.GOAL_E2E_RUN ??= `auth-${new Date().toISOString().replace(/[:.]/g, '-')}-${process.pid}`;

export default defineConfig({
  testDir: 'e2e-auth',
  outputDir: `test-results/${process.env.GOAL_E2E_RUN}`,
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: process.env.GOAL_E2E_AUTH_URL ?? 'http://127.0.0.1:8080',
    browserName: 'chromium',
    viewport: { width: 1440, height: 1000 },
    reducedMotion: 'reduce',
    trace: 'retain-on-failure',
  },
});
