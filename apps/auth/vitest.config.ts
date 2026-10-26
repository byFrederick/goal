import { defineConfig } from 'vitest/config';

// unit: no database (task check). integration: PostgreSQL 18 from compose.yaml at
// GOAL_TEST_DATABASE_URL, a throwaway database per test file (task test-integration).
export default defineConfig({
  test: {
    projects: [
      {
        test: {
          name: 'unit',
          include: ['src/**/*.test.ts'],
          exclude: ['src/**/*.integration.test.ts'],
        },
      },
      {
        test: {
          name: 'integration',
          include: ['src/**/*.integration.test.ts'],
          testTimeout: 30_000,
          hookTimeout: 120_000,
        },
      },
    ],
  },
});
