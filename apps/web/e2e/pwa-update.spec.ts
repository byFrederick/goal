import { randomBytes } from 'node:crypto';
import { cpSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { createServer, request, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { tmpdir } from 'node:os';
import { extname, join, normalize } from 'node:path';
import { expect, test } from '@playwright/test';

// The update toast (docs/engineering/frontend.md, PWA): it shows only when the waiting worker
// carries a newer build than the page runs. The test serves copies of the production build under
// other build ids from a server of its own, and proxies the API to the preview server.
test.use({ serviceWorkers: 'allow' });

const TOAST = 'A new version of Goal is ready';
const types: Record<string, string> = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.woff2': 'font/woff2',
  '.webmanifest': 'application/manifest+json',
  '.json': 'application/json',
};

const dist = join(import.meta.dirname, '..', 'dist');
const buildOf = (html: string) => /name="goal-build" content="([0-9a-f]{16})"/.exec(html)?.[1] ?? '';

// A copy of the build under a new id: index.html's meta and the worker's cache name change.
function makeBuild(root: string, name: string, from: string, silent = false): string {
  const dir = join(root, name);
  cpSync(dist, dir, { recursive: true });
  const id = randomBytes(8).toString('hex');
  for (const file of ['index.html', 'sw.js']) {
    const path = join(dir, file);
    writeFileSync(path, readFileSync(path, 'utf8').split(from).join(id));
  }
  // A worker from before the GET_BUILD check: it never answers.
  if (silent) {
    const sw = join(dir, 'sw.js');
    writeFileSync(sw, readFileSync(sw, 'utf8').split('GET_BUILD').join('NO_BUILD'));
  }
  return id;
}

function listFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((n) => (statSync(join(dir, n)).isDirectory() ? listFiles(join(dir, n)) : [n]));
}

let root = '';
let server: Server;
let origin = '';
let current = '';
const ids: Record<string, string> = {};

test.beforeAll(async () => {
  const info = test.info();
  test.skip(!['phone-375', 'desktop-1440'].includes(info.project.name), 'runs at 375 and 1440 px only');
  expect(listFiles(dist).length).toBeGreaterThan(0);
  root = mkdtempSync(join(tmpdir(), 'goal-pwa-update-'));
  const a = buildOf(readFileSync(join(dist, 'index.html'), 'utf8'));
  expect(a).toMatch(/^[0-9a-f]{16}$/);
  ids.base = makeBuild(root, 'base', a);
  ids.a = makeBuild(root, 'a', a, true);
  ids.b = makeBuild(root, 'b', a);
  ids.c = makeBuild(root, 'c', a);
  current = 'base';
  const preview = new URL(info.project.use.baseURL ?? 'http://127.0.0.1:4174');
  server = createServer((req, res) => {
    const url = new URL(req.url ?? '/', 'http://x');
    if (url.pathname.startsWith('/v1') || url.pathname.startsWith('/api')) {
      const upstream = request(
        {
          host: preview.hostname,
          port: preview.port,
          path: req.url,
          method: req.method,
          headers: { ...req.headers, host: preview.host },
        },
        (r) => {
          res.writeHead(r.statusCode ?? 502, r.headers);
          r.pipe(res);
        },
      );
      upstream.on('error', () => res.writeHead(502).end());
      req.pipe(upstream);
      return;
    }
    const dir = join(root, current);
    let file = join(dir, normalize(url.pathname));
    if (!file.startsWith(dir) || !statSync(dir).isDirectory()) return void res.writeHead(400).end();
    try {
      if (statSync(file).isDirectory()) file = join(file, 'index.html');
      statSync(file);
    } catch {
      file = join(dir, 'index.html');
    }
    const noCache = file.endsWith('index.html') || file.endsWith('sw.js');
    res.writeHead(200, {
      'Content-Type': types[extname(file)] ?? 'application/octet-stream',
      'Cache-Control': noCache ? 'no-cache' : 'public, max-age=31536000, immutable',
    });
    res.end(readFileSync(file));
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

test.afterAll(async () => {
  await new Promise<void>((resolve) => (server ? server.close(() => resolve()) : resolve()));
  if (root) rmSync(root, { recursive: true, force: true });
});

test('a stale toast does not stay, and Reload loads the newest build', async ({ page }, info) => {
  test.skip(!['phone-375', 'desktop-1440'].includes(info.project.name), 'runs at 375 and 1440 px only');
  // The base build: the first visit installs the worker, which claims the page.
  await page.goto(`${origin}/`);
  expect(await page.locator('meta[name="goal-build"]').getAttribute('content')).toBe(ids.base);
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);

  // Build A, whose worker never answers GET_BUILD, is left waiting and its toast ignored.
  current = 'a';
  await page.goto(`${origin}/`);
  expect(await page.locator('meta[name="goal-build"]').getAttribute('content')).toBe(ids.a);
  await page.waitForFunction(async () => (await navigator.serviceWorker.getRegistration())?.waiting != null);

  // Build B, then a navigation: the page runs B, A's worker is replaced by B's, no toast remains.
  current = 'b';
  await page.goto(`${origin}/`);
  expect(await page.locator('meta[name="goal-build"]').getAttribute('content')).toBe(ids.b);
  await page.waitForFunction(async (id) => {
    const keys = await caches.keys();
    return keys.length === 1 && keys[0] === `goal-shell-${id}`;
  }, ids.b);
  await expect(page.getByText(TOAST)).toHaveCount(0);

  // Build C without navigating: the update check finds a newer build, and Reload loads it.
  current = 'c';
  await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
  await expect(page.getByText(TOAST)).toBeVisible();
  await page.getByRole('button', { name: 'Reload' }).click();
  await expect(page.locator('meta[name="goal-build"]')).toHaveAttribute('content', ids.c ?? '');
});
