import { spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  type BuildCacheState,
  buildCacheKey,
  detectTools,
  type RestoreDeps,
  restoreBuildCache,
  type SaveDeps,
  saveBuildCache,
} from './cache.js';
import type { Exec, Output } from './prepare.js';

const gateway = 'http://goal-egress:3128/mcp';
const token = 'task-token-123';

let root: string;

beforeEach(async () => {
  root = await mkdtemp(join(tmpdir(), 'goal-cache-'));
});

afterEach(async () => {
  // Go's module cache is read-only, as Go leaves it.
  await new Promise((resolve) => spawn('chmod', ['-R', 'u+w', root]).once('close', resolve));
  await rm(root, { recursive: true, force: true });
});

interface Entry {
  state: 'saving' | 'ready';
  object?: Buffer;
  size?: number;
}

/**
 * The gateway's build-cache routes over a map, as internal/gateway serves them without signed URLs:
 * GET and POST /run/build-cache/<key>, GET and PUT <key>/archive, POST <key>/done.
 */
function fakeBucket(opts: { archiveStatus?: number; putStatus?: number; doneStatus?: number } = {}) {
  const entries = new Map<string, Entry>();
  const requests: { method: string; path: string; auth: string | null; body?: string }[] = [];
  const fake = (async (input: string | URL | Request, init?: RequestInit) => {
    const url = new URL(String(input));
    const method = init?.method ?? 'GET';
    const auth = new Headers(init?.headers).get('Authorization');
    const m = /^\/run\/build-cache\/([0-9a-f]{64})(?:\/(archive|done))?$/.exec(url.pathname);
    if (!m || url.origin !== new URL(gateway).origin) return new Response('not found', { status: 404 });
    const [, key = '', sub] = m;
    const entry = entries.get(key);
    if (sub === 'archive' && method === 'PUT') {
      const body = Buffer.from(
        await new Response(init?.body as ConstructorParameters<typeof Response>[0]).arrayBuffer(),
      );
      requests.push({ method, path: url.pathname, auth });
      if (opts.putStatus) return new Response('', { status: opts.putStatus });
      if (entry) entry.object = body;
      return new Response('', { status: 200 });
    }
    requests.push({ method, path: url.pathname, auth, ...(typeof init?.body === 'string' ? { body: init.body } : {}) });
    if (auth !== `Bearer ${token}`) return new Response('', { status: 401 });
    if (sub === 'archive') {
      if (opts.archiveStatus) return new Response('', { status: opts.archiveStatus });
      return entry?.object ? new Response(entry.object) : new Response('', { status: 404 });
    }
    if (sub === 'done') {
      if (opts.doneStatus) return new Response('', { status: opts.doneStatus });
      if (!entry?.object) return new Response('', { status: 409 });
      entry.state = 'ready';
      entry.size = (JSON.parse(String(init?.body)) as { size_bytes: number }).size_bytes;
      return new Response(null, { status: 204 });
    }
    if (method === 'POST') {
      if (entry) return new Response('', { status: 409 });
      entries.set(key, { state: 'saving' });
      return Response.json({ url: `/run/build-cache/${key}/archive`, method: 'PUT' });
    }
    if (entry?.state !== 'ready') return new Response('', { status: 404 });
    return Response.json({ url: `/run/build-cache/${key}/archive` });
  }) as typeof fetch;
  return { fake, entries, requests };
}

/** tar for real; signal ends it. */
const exec: Exec = (command, args, opts = {}) =>
  new Promise((resolve) => {
    const child = spawn(command, args, {
      cwd: opts.cwd,
      stdio: 'ignore',
      ...(opts.signal ? { signal: opts.signal } : {}),
    });
    child.once('error', () => resolve(1));
    child.once('close', (code) => resolve(code ?? 1));
  });

/** A box: its workspace, its cache directories and how mise, go and pnpm answer in it. */
interface Box {
  ws: string;
  goal: string;
  dirs: { gomod: string; gocache: string; store: string };
  output: Output;
  calls: string[];
}

async function box(
  name: string,
  files: Record<string, string>,
  caches: string = join(root, 'caches'),
  versions: Record<string, string> = { go: '1.26.10', node: '24.11.0', pnpm: '12.15.0' },
): Promise<Box> {
  const ws = join(root, name, 'workspace');
  await mkdir(ws, { recursive: true });
  for (const [path, content] of Object.entries(files)) {
    await mkdir(join(ws, path, '..'), { recursive: true });
    await writeFile(join(ws, path), content);
  }
  const dirs = { gomod: join(caches, 'gomod'), gocache: join(caches, 'gocache'), store: join(caches, 'pnpm-store') };
  const calls: string[] = [];
  const output: Output = async (command, args, opts = {}) => {
    calls.push([command, ...args].join(' '));
    expect(opts.cwd).toBe(ws);
    if (command === 'mise' && args[0] === 'current') {
      const v = versions[args[1] ?? ''];
      if (v === undefined) throw new Error('mise current exited 1');
      return `${v}\n`;
    }
    if (command === 'go') return `${dirs.gomod}\n${dirs.gocache}\n`;
    if (command === 'pnpm') return `${dirs.store}\n`;
    throw new Error(`unexpected ${command}`);
  };
  return { ws, goal: join(root, name, 'goal'), dirs, output, calls };
}

const both = {
  'go.mod': 'module example.com/acme\n',
  'go.sum': 'example.com/x v1.0.0 h1:abc=\n',
  'pnpm-lock.yaml': "lockfileVersion: '9.0'\n",
};

function restoreDeps(b: Box, fake: typeof fetch, logs: string[] = []): RestoreDeps {
  return {
    fetch: fake,
    exec,
    output: b.output,
    gateway,
    token,
    archive: join(b.goal, 'build-cache.tar.zst'),
    log: (l) => logs.push(l),
    workspace: b.ws,
  };
}

function saveDeps(b: Box, fake: typeof fetch, logs: string[] = [], maxBytes?: number): SaveDeps {
  return {
    fetch: fake,
    exec,
    gateway,
    token,
    archive: join(b.goal, 'build-cache.tar.zst'),
    log: (l) => logs.push(l),
    ...(maxBytes === undefined ? {} : { maxBytes }),
  };
}

/** Fills the box's cache directories as a cold build would. */
async function build(b: Box): Promise<void> {
  await mkdir(join(b.dirs.gomod, 'example.com', 'x@v1.0.0'), { recursive: true });
  await writeFile(join(b.dirs.gomod, 'example.com', 'x@v1.0.0', 'x.go'), 'package x\n');
  await mkdir(join(b.dirs.gocache, '00'), { recursive: true });
  await writeFile(join(b.dirs.gocache, '00', 'a-d'), 'compiled');
  await mkdir(join(b.dirs.store, 'files'), { recursive: true });
  await writeFile(join(b.dirs.store, 'files', 'pkg'), 'tarball');
}

describe('build cache', () => {
  it('misses, saves, then hits on a second box with the same inputs', async () => {
    const bucket = fakeBucket();
    const first = await box('one', both);
    const missed = await restoreBuildCache(restoreDeps(first, bucket.fake), true);
    expect(missed?.outcome).toBe('miss');
    expect(missed?.key).toMatch(/^[0-9a-f]{64}$/);
    expect(missed?.dirs).toEqual([first.dirs.gomod, first.dirs.gocache, first.dirs.store]);
    expect(first.calls).toEqual([
      'mise current go',
      'mise current node',
      'mise current pnpm',
      'go env GOMODCACHE GOCACHE',
      'pnpm store path',
    ]);

    await build(first);
    expect(await saveBuildCache(saveDeps(first, bucket.fake), missed as BuildCacheState)).toBe(true);
    const key = missed?.key as string;
    const entry = bucket.entries.get(key);
    expect(entry?.state).toBe('ready');
    expect(entry?.size).toBe(entry?.object?.length);
    // A zstd frame.
    expect(entry?.object?.subarray(0, 4).toString('hex')).toBe('28b52ffd');
    expect(bucket.requests.map((r) => `${r.method} ${r.path.replace(key, '<key>')}`)).toEqual([
      'GET /run/build-cache/<key>',
      'POST /run/build-cache/<key>',
      'PUT /run/build-cache/<key>/archive',
      'POST /run/build-cache/<key>/done',
    ]);
    expect(bucket.requests.every((r) => r.auth === `Bearer ${token}`)).toBe(true);
    expect(bucket.requests.at(-1)?.body).toBe(JSON.stringify({ size_bytes: entry?.size }));
    expect(existsSync(join(first.goal, 'build-cache.tar.zst'))).toBe(false);

    // The second box starts empty: the same image has the same cache directories.
    await rm(join(root, 'caches'), { recursive: true, force: true });
    const second = await box('two', both);
    const hit = await restoreBuildCache(restoreDeps(second, bucket.fake), true);
    expect(hit).toEqual({ outcome: 'hit', key, dirs: missed?.dirs });
    expect(await readFile(join(second.dirs.gomod, 'example.com', 'x@v1.0.0', 'x.go'), 'utf8')).toBe('package x\n');
    expect(await readFile(join(second.dirs.gocache, '00', 'a-d'), 'utf8')).toBe('compiled');
    expect(await readFile(join(second.dirs.store, 'files', 'pkg'), 'utf8')).toBe('tarball');
    // After a hit nothing is saved.
    expect(await saveBuildCache(saveDeps(second, bucket.fake), hit as BuildCacheState)).toBe(false);
  });

  it('skips the upload and done when another box holds the key', async () => {
    const bucket = fakeBucket();
    const b = await box('one', both);
    const state = (await restoreBuildCache(restoreDeps(b, bucket.fake), true)) as BuildCacheState;
    bucket.entries.set(state.key as string, { state: 'saving' });
    await build(b);
    const logs: string[] = [];
    expect(await saveBuildCache(saveDeps(b, bucket.fake, logs), state)).toBe(false);
    expect(bucket.requests.map((r) => r.method)).toEqual(['GET', 'POST']);
    expect(logs.join('\n')).toContain('skipped');
  });

  it('skips an archive over the limit: no upload and no done', async () => {
    const bucket = fakeBucket();
    const b = await box('one', both);
    const state = (await restoreBuildCache(restoreDeps(b, bucket.fake), true)) as BuildCacheState;
    await build(b);
    const logs: string[] = [];
    expect(await saveBuildCache(saveDeps(b, bucket.fake, logs, 16), state)).toBe(false);
    expect(bucket.requests.map((r) => r.method)).toEqual(['GET', 'POST']);
    expect(logs.join('\n')).toMatch(/over the limit/);
    expect(existsSync(join(b.goal, 'build-cache.tar.zst'))).toBe(false);
  });

  it('logs a failed upload or done and never throws', async () => {
    for (const opts of [{ putStatus: 500 }, { doneStatus: 409 }]) {
      const bucket = fakeBucket(opts);
      const b = await box(`box-${Object.keys(opts)[0]}`, both, join(root, `caches-${Object.keys(opts)[0]}`));
      const state = (await restoreBuildCache(restoreDeps(b, bucket.fake), true)) as BuildCacheState;
      await build(b);
      const logs: string[] = [];
      expect(await saveBuildCache(saveDeps(b, bucket.fake, logs), state)).toBe(false);
      expect(logs).toHaveLength(1);
      expect(logs[0]).toMatch(/build cache save: (upload: 500|POST \/run\/build-cache\/<key>\/done: 409)/);
      expect(logs[0]).not.toContain(token);
    }
  });

  it('reports error when the download fails or the archive does not unpack, and goes on', async () => {
    const corrupt = fakeBucket();
    const b = await box('one', both);
    const { key } = (await buildCacheKey(restoreDeps(b, corrupt.fake))) as { key: string };
    corrupt.entries.set(key, { state: 'ready', object: Buffer.from('not a tar.zst') });
    const logs: string[] = [];
    expect((await restoreBuildCache(restoreDeps(b, corrupt.fake, logs), true))?.outcome).toBe('error');
    expect(logs.join('\n')).toMatch(/restore: tar exited/);
    expect(existsSync(join(b.goal, 'build-cache.tar.zst'))).toBe(false);

    const failing = fakeBucket({ archiveStatus: 500 });
    failing.entries.set(key, { state: 'ready', object: Buffer.from('x') });
    expect((await restoreBuildCache(restoreDeps(b, failing.fake, logs), true))?.outcome).toBe('error');
    expect(logs.join('\n')).toMatch(/restore: download: 500/);
  });

  it('reports error when the restore runs past its time limit', async () => {
    const b = await box('one', both);
    const hanging = ((_: unknown, init?: RequestInit) =>
      new Promise((_resolve, reject) => {
        init?.signal?.addEventListener('abort', () => reject(new Error('aborted')));
      })) as typeof fetch;
    const state = await restoreBuildCache({ ...restoreDeps(b, hanging), timeoutMs: 20 }, true);
    expect(state?.outcome).toBe('error');
  });

  it('is off without fetching when the run turns the cache off', async () => {
    const bucket = fakeBucket();
    const b = await box('one', both);
    expect(await restoreBuildCache(restoreDeps(b, bucket.fake), false)).toEqual({ outcome: 'off' });
    expect(bucket.requests).toEqual([]);
    expect(b.calls).toEqual([]);
  });

  it('has no cache for a repository that uses neither Go nor pnpm', async () => {
    const bucket = fakeBucket();
    const b = await box('one', { 'README.md': '# acme\n', 'mise.toml': '[tools]\nnode = "24"\n' });
    expect(await restoreBuildCache(restoreDeps(b, bucket.fake), true)).toBeUndefined();
    expect(bucket.requests).toEqual([]);
    expect(b.calls).toEqual([]);
  });
});

describe('detectTools', () => {
  it('reads go.mod, pnpm-lock.yaml and the tools a mise config names', async () => {
    const cases: [Record<string, string>, { go: boolean; pnpm: boolean }][] = [
      [{ 'go.mod': 'module x\n' }, { go: true, pnpm: false }],
      [{ 'pnpm-lock.yaml': '' }, { go: false, pnpm: true }],
      [{ 'mise.toml': '[tools]\ngo = "1.26"\n"npm:pnpm" = "12"\n' }, { go: true, pnpm: true }],
      [{ '.mise.toml': '[env]\ngo = "x"\n[tools]\nnode = "24"\n' }, { go: false, pnpm: false }],
      [{ '.config/mise/config.toml': 'tools.pnpm = "12"\n' }, { go: false, pnpm: true }],
      [{ '.tool-versions': 'golang 1.26\ngo 1.26.10\n' }, { go: true, pnpm: false }],
      [{ '.tool-versions': '# go 1.26\nnodejs 24\n' }, { go: false, pnpm: false }],
    ];
    for (const [i, [files, want]] of cases.entries()) {
      const b = await box(`case-${i}`, files);
      expect(await detectTools(b.ws), JSON.stringify(files)).toEqual(want);
    }
  });
});

describe('buildCacheKey', () => {
  it('is the same for the same inputs and changes with a lockfile or a version', async () => {
    const keyOf = async (name: string, files: Record<string, string>, versions?: Record<string, string>) => {
      const b = await box(name, files, undefined, versions);
      return (await buildCacheKey(restoreDeps(b, fakeBucket().fake)))?.key;
    };
    const base = await keyOf('a', both);
    expect(base).toMatch(/^[0-9a-f]{64}$/);
    expect(await keyOf('b', both)).toBe(base);
    expect(await keyOf('c', { ...both, 'go.sum': 'example.com/x v1.0.1 h1:def=\n' })).not.toBe(base);
    expect(await keyOf('d', { ...both, 'pnpm-lock.yaml': "lockfileVersion: '9.1'\n" })).not.toBe(base);
    expect(await keyOf('e', { ...both, 'go.work.sum': 'example.com/y v1.0.0 h1:x=\n' })).not.toBe(base);
    expect(await keyOf('f', both, { go: '1.26.11', node: '24.11.0', pnpm: '12.15.0' })).not.toBe(base);
    // A version mise does not resolve is empty, the same on every box.
    const noPnpm = { go: '1.26.10', node: '24.11.0' };
    expect(await keyOf('g', both, noPnpm)).toBe(await keyOf('h', both, noPnpm));
    expect(await keyOf('g2', both, noPnpm)).not.toBe(base);
  });

  it('keys on the OS and architecture', async () => {
    const b = await box('a', both);
    const deps = restoreDeps(b, fakeBucket().fake);
    const amd = await buildCacheKey({ ...deps, platform: 'linux', arch: 'x64' });
    const arm = await buildCacheKey({ ...deps, platform: 'linux', arch: 'arm64' });
    expect(amd?.key).not.toBe(arm?.key);
  });
});
