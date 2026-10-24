/// <reference types="vitest/config" />
import { readFileSync } from 'node:fs';
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
// The Content-Security-Policy goald serves the build with (webui.go): preview sends it too, so the
// e2e tests run the build under it. Not in dev, whose HMR client needs inline scripts and a socket.
const csp = readFileSync(new URL('./csp.txt', import.meta.url), 'utf8').trim();

export default defineConfig({
  plugins: [
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    react(),
    tailwindcss(),
    ...(useMock ? [mockApi({ live: process.env.GOAL_MOCK_LIVE === '1' })] : []),
  ],
  resolve: {
    alias: [
      // monaco-yaml's worker imports Monaco's editor worker by its pre-0.57 path, which Monaco's
      // package exports now name without the esm/vs prefix.
      { find: 'monaco-editor/esm/vs/editor/editor.worker.js', replacement: 'monaco-editor/editor/editor.worker' },
    ],
  },
  server: { port: 5173, strictPort: true, ...(proxy ? { proxy } : {}) },
  preview: {
    port: 4173,
    strictPort: true,
    headers: { 'Content-Security-Policy': csp, 'X-Frame-Options': 'DENY' },
    ...(proxy ? { proxy } : {}),
  },
  build: {
    target: 'es2023',
    // Fonts stay files: the CSP's default-src 'self' allows no data: fonts (Vite inlines files
    // under 4 KB, some of Inter's and Fira Code's unicode-range subsets among them).
    assetsInlineLimit: (file) => (/\.(woff2?|ttf|otf)$/.test(file) ? false : undefined),
  },
  test: {
    include: ['src/**/*.test.ts', 'mock/**/*.test.ts'],
    environment: 'node',
  },
});
