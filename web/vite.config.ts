/// <reference types="vitest/config" />
import tailwindcss from '@tailwindcss/vite';
import { tanstackRouter } from '@tanstack/router-plugin/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';
import { mockApi } from './mock/plugin.ts';

// GOAL_API=mock serves the in-memory mock API (mock/); otherwise /v1 is proxied to goald
// (GOAL_API_URL, default the local goald). GOAL_MOCK_LIVE=1 makes the mock's agents keep working.
const useMock = process.env.GOAL_API === 'mock';
const target = process.env.GOAL_API_URL ?? 'http://127.0.0.1:8080';
// The proxy keeps the browser's Host, so goald's cross-origin protection sees the page's own
// origin (Origin = Host) as well as Sec-Fetch-Site: same-origin.
const proxy = useMock ? undefined : { '/v1': { target, changeOrigin: false } };

export default defineConfig({
  plugins: [
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    react(),
    tailwindcss(),
    ...(useMock ? [mockApi({ live: process.env.GOAL_MOCK_LIVE === '1' })] : []),
  ],
  server: { port: 5173, strictPort: true, ...(proxy ? { proxy } : {}) },
  preview: { port: 4173, strictPort: true, ...(proxy ? { proxy } : {}) },
  build: { target: 'es2023' },
  test: {
    include: ['src/**/*.test.ts'],
    environment: 'node',
  },
});
