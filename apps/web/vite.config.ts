/// <reference types="vitest/config" />
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import tailwindcss from '@tailwindcss/vite';
import { tanstackRouter } from '@tanstack/router-plugin/vite';
import react from '@vitejs/plugin-react';
import { build, defineConfig, type Plugin } from 'vite';
import { mockApi } from './mock/plugin.ts';

// The service worker is a classic script of its own at the root of the build (sw.js, unhashed, so
// goald serves it no-cache at scope /). A nested build, with no plugins and no shared chunks,
// makes it: its __BUILD_ID__ is a hash of every file of this build, so each build has its own
// cache (src/sw/sw.ts).
function serviceWorker(): Plugin {
  return {
    name: 'goal-service-worker',
    apply: 'build',
    async writeBundle(options, bundle) {
      const hash = createHash('sha256');
      for (const name of Object.keys(bundle).sort()) {
        const file = bundle[name];
        if (!file) continue;
        hash.update(name);
        hash.update(file.type === 'chunk' ? file.code : file.source);
      }
      const buildId = hash.digest('hex').slice(0, 16);
      const root = fileURLToPath(new URL('.', import.meta.url));
      const result = await build({
        root,
        configFile: false,
        publicDir: false,
        logLevel: 'warn',
        define: { __BUILD_ID__: JSON.stringify(buildId) },
        build: {
          write: false,
          target: 'es2023',
          lib: {
            entry: join(root, 'src/sw/sw.ts'),
            formats: ['iife'],
            name: 'goalServiceWorker',
            fileName: () => 'sw.js',
          },
        },
      });
      const outputs = (Array.isArray(result) ? result : [result]).flatMap((out) => ('output' in out ? out.output : []));
      const chunk = outputs.find((item) => item.type === 'chunk');
      if (!chunk) throw new Error('the service worker build emitted no chunk');
      if (!options.dir) throw new Error('the service worker needs an output directory');
      await writeFile(join(options.dir, 'sw.js'), chunk.code);
    },
  };
}

// GOAL_API=mock serves the in-memory mock API (mock/); otherwise /v1 and the identity service's
// /api/auth are proxied to goald (GOAL_API_URL, default the local goald), so the session cookie is
// the page's own. GOAL_MOCK_LIVE=1 makes the mock's agents keep working.
const useMock = process.env.GOAL_API === 'mock';
const target = process.env.GOAL_API_URL ?? 'http://127.0.0.1:8080';
// The proxy keeps the browser's Host, so goald's cross-origin protection sees the page's own
// origin (Origin = Host) as well as Sec-Fetch-Site: same-origin.
const proxy = useMock
  ? undefined
  : { '/v1': { target, changeOrigin: false }, '/api/auth': { target, changeOrigin: false } };
// The Content-Security-Policy and the other headers goald serves the build with (webui.go; HSTS
// only over https): preview sends them too, so the e2e tests run the build under them. Not in
// dev, whose HMR client needs inline scripts and a socket.
const csp = readFileSync(new URL('./csp.txt', import.meta.url), 'utf8').trim();
const securityHeaders = {
  'Content-Security-Policy': csp,
  'X-Frame-Options': 'DENY',
  'Cross-Origin-Opener-Policy': 'same-origin',
  'Referrer-Policy': 'same-origin',
  'X-Content-Type-Options': 'nosniff',
};

export default defineConfig({
  plugins: [
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    react(),
    tailwindcss(),
    serviceWorker(),
    ...(useMock ? [mockApi({ live: process.env.GOAL_MOCK_LIVE === '1' })] : []),
  ],
  resolve: {
    alias: [
      // monaco-yaml's worker imports Monaco's editor worker by its pre-0.57 path, which Monaco's
      // package exports now name without the esm/vs prefix.
      { find: 'monaco-editor/esm/vs/editor/editor.worker.js', replacement: 'monaco-editor/editor/editor.worker' },
      // Monaco's sanitizer imports its own copy of DOMPurify (3.4.15 in 0.57.0, which has
      // advisories); this makes it import the dompurify package Monaco depends on, which
      // pnpm-workspace.yaml's override pins to a fixed release (SECURITY.md). Drop both once a
      // Monaco release vendors a fixed DOMPurify.
      { find: /^\.\/dompurify\/dompurify\.js$/, replacement: 'dompurify' },
    ],
  },
  server: { port: 5173, strictPort: true, ...(proxy ? { proxy } : {}) },
  preview: {
    port: 4173,
    strictPort: true,
    headers: securityHeaders,
    ...(proxy ? { proxy } : {}),
  },
  build: {
    target: 'es2023',
    // Fonts stay files: the CSP's default-src 'self' allows no data: fonts (Vite inlines files
    // under 4 KB, some of Archivo's and JetBrains Mono's unicode-range subsets among them).
    assetsInlineLimit: (file) => (/\.(woff2?|ttf|otf)$/.test(file) ? false : undefined),
  },
  test: {
    // Component tests opt into jsdom with a `@vitest-environment jsdom` comment at the top of the file.
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx', 'mock/**/*.test.ts'],
    environment: 'node',
    // React's act (what Testing Library waits on) is in its development build only; a production
    // NODE_ENV in the shell would load the build without it.
    env: { NODE_ENV: 'test' },
  },
});
