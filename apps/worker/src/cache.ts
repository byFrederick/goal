import { createHash } from 'node:crypto';
import { createReadStream, createWriteStream } from 'node:fs';
import { mkdir, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { dirname, join, relative } from 'node:path';
import { Readable } from 'node:stream';
import { pipeline } from 'node:stream/promises';
import type { ReadableStream } from 'node:stream/web';
import type { Exec, Output } from './prepare.js';

/**
 * The build cache of a run box (ADR 0045): Go's module and build caches and the pnpm store of the
 * workspace's repository, restored by prepare after the repository's mise install and saved by
 * run after the agent, through the gateway's /run/build-cache/<key> routes.
 */

/** What the run's finished line reports as build_cache (domain.WorkerFinished). */
export type BuildCacheOutcome = 'hit' | 'miss' | 'off' | 'error';

/** What prepare hands to run in stateFile: the outcome and, after a restore was tried, the key and directories. */
export interface BuildCacheState {
  outcome: BuildCacheOutcome;
  key?: string;
  dirs?: string[];
}

/** The state file's name under /goal, beside spec.json. */
export const stateFile = 'build-cache.json';

/** The archive's name under /goal while it downloads or uploads; removed after. */
export const archiveFile = 'build-cache.tar.zst';

/** Changes whenever what a cache holds or how it is packed changes, so older caches are never restored. */
export const cacheFormat = 'goal-build-cache-1';

/** How long a restore may take, download and unpack together, before the run goes on cold. */
export const restoreTimeoutMs = 5 * 60_000;

/** The largest archive a box uploads: 2 GiB, the gateway's own cap (Config.MaxBuildCacheBytes). */
export const maxArchiveBytes = 2 * 1024 ** 3;

/** The gateway's folder of build caches (internal/gateway, BuildCachePath). */
const routes = '/run/build-cache/';

/** Where a mise config can live at a repository's root, as mise reads them. */
const miseConfigs = [
  'mise.toml',
  '.mise.toml',
  'mise/config.toml',
  '.mise/config.toml',
  '.config/mise.toml',
  '.config/mise/config.toml',
];

/** The lockfiles whose contents are part of the key, where they exist. */
const lockfiles = ['go.sum', 'go.work.sum', 'pnpm-lock.yaml'];

/** How the box reaches the gateway's build-cache routes. */
export interface CacheIO {
  fetch: typeof fetch;
  exec: Exec;
  /** The gateway's URL (GOAL_GATEWAY_URL); the routes resolve against its origin. */
  gateway: string;
  /** The run's task token, sent only to the gateway's origin. */
  token: string;
  /** Where the archive is written while it downloads or uploads. */
  archive: string;
  /** Where a failure is logged; never a URL or a token. */
  log: (line: string) => void;
}

export interface RestoreDeps extends CacheIO {
  output: Output;
  /** The workspace's repository, whose root decides the tools and the key. */
  workspace: string;
  /** The OS and architecture in the key; the process's own when absent. */
  platform?: string;
  arch?: string;
  /** restoreTimeoutMs when absent. */
  timeoutMs?: number;
}

export interface SaveDeps extends CacheIO {
  /** maxArchiveBytes when absent. */
  maxBytes?: number;
}

/** Which of the cached tools the repository at ws uses, from its root's files and its mise config. */
export async function detectTools(ws: string): Promise<{ go: boolean; pnpm: boolean }> {
  const named = new Set<string>();
  for (const path of miseConfigs) {
    for (const tool of tomlTools(await readIfExists(join(ws, path)))) named.add(tool);
  }
  for (const line of (await readIfExists(join(ws, '.tool-versions'))).split('\n')) {
    const name = line.trim().split(/\s+/)[0];
    if (name && !name.startsWith('#')) named.add(toolName(name));
  }
  return {
    go: named.has('go') || (await exists(join(ws, 'go.mod'))),
    pnpm: named.has('pnpm') || (await exists(join(ws, 'pnpm-lock.yaml'))),
  };
}

/** The tool names a mise TOML config lists: the keys of its [tools] table, and tools.<name> keys. */
function tomlTools(toml: string): string[] {
  const names: string[] = [];
  let section = '';
  for (const raw of toml.split('\n')) {
    const line = raw.trim();
    const header = /^\[([^\]]+)\]/.exec(line);
    if (header) {
      section = (header[1] ?? '').trim();
      continue;
    }
    const key = /^(?:"([^"]+)"|'([^']+)'|([^\s=#]+))\s*=/.exec(line);
    const name = key?.[1] ?? key?.[2] ?? key?.[3];
    if (!name) continue;
    if (section === 'tools') names.push(toolName(name));
    else if (section === '' && name.startsWith('tools.')) names.push(toolName(name.slice('tools.'.length)));
  }
  return names;
}

/** A mise tool's short name: npm:pnpm and aqua:pnpm/pnpm are pnpm. */
function toolName(spec: string): string {
  const name = spec.slice(spec.lastIndexOf(':') + 1);
  return name.slice(name.lastIndexOf('/') + 1);
}

/**
 * The cache key of the workspace and the directories its cache holds; undefined when the repository
 * uses neither Go nor pnpm. The key is the lowercase hex SHA-256 of the cache format, the OS and
 * architecture, the go, node and pnpm versions mise resolves in the workspace (empty when it resolves
 * none) and the contents of go.sum, go.work.sum and pnpm-lock.yaml where they exist. The directories
 * are what `go env GOMODCACHE GOCACHE` and `pnpm store path` report. A tool whose directories cannot
 * be read throws.
 */
export async function buildCacheKey(deps: RestoreDeps): Promise<{ key: string; dirs: string[] } | undefined> {
  const ws = deps.workspace;
  const tools = await detectTools(ws);
  if (!tools.go && !tools.pnpm) return undefined;

  const hash = createHash('sha256');
  const part = (s: string) => hash.update(`${s}\n`);
  part(cacheFormat);
  part(`${deps.platform ?? process.platform}/${deps.arch ?? process.arch}`);
  for (const tool of ['go', 'node', 'pnpm']) {
    const version = await deps.output('mise', ['current', tool], { cwd: ws }).then(
      (out) => out.trim(),
      () => '',
    );
    part(`${tool}=${version}`);
  }
  for (const name of lockfiles) {
    const content = await readFile(join(ws, name)).catch((err: NodeJS.ErrnoException) => {
      if (err.code === 'ENOENT') return undefined;
      throw err;
    });
    part(`${name}=${content ? createHash('sha256').update(content).digest('hex') : '-'}`);
  }

  const dirs: string[] = [];
  if (tools.go) dirs.push(...lines(await deps.output('go', ['env', 'GOMODCACHE', 'GOCACHE'], { cwd: ws })));
  if (tools.pnpm) dirs.push(...lines(await deps.output('pnpm', ['store', 'path'], { cwd: ws })));
  if (dirs.some((d) => !d.startsWith('/'))) throw new Error(`a cache directory is not absolute: ${dirs.join(' ')}`);
  return { key: hash.digest('hex'), dirs };
}

/**
 * Restores the workspace's build cache, for prepare after the repository's mise install: off when
 * enabled is false (nothing is fetched), undefined when the repository uses neither tool. A 404 is a
 * miss; a download or unpack that fails or runs past timeoutMs is error, logged, and the run goes on
 * cold. It never throws.
 */
export async function restoreBuildCache(
  deps: RestoreDeps,
  enabled: boolean,
): Promise<BuildCacheState | undefined> {
  if (!enabled) return { outcome: 'off' };
  let found: { key: string; dirs: string[] } | undefined;
  try {
    found = await buildCacheKey(deps);
  } catch (err) {
    deps.log(`goal-agent prepare: build cache: ${message(err)}`);
    return { outcome: 'error' };
  }
  if (!found) return undefined;
  const { key, dirs } = found;
  const signal = AbortSignal.timeout(deps.timeoutMs ?? restoreTimeoutMs);
  try {
    const res = await deps.fetch(gatewayRoute(deps, key), { headers: authFor(deps, gatewayRoute(deps, key)), signal });
    if (res.status === 404) {
      await res.body?.cancel();
      return { outcome: 'miss', key, dirs };
    }
    if (!res.ok) throw new Error(`GET ${routes}<key>: ${res.status}`);
    const { url } = (await res.json()) as { url?: unknown };
    if (typeof url !== 'string' || url === '') throw new Error(`GET ${routes}<key>: no url`);
    const from = new URL(url, deps.gateway);
    const download = await deps.fetch(from, { headers: authFor(deps, from), signal });
    if (!download.ok || !download.body) throw new Error(`download: ${download.status}`);
    await mkdir(dirname(deps.archive), { recursive: true });
    await pipeline(Readable.fromWeb(download.body as ReadableStream<Uint8Array>), createWriteStream(deps.archive), {
      signal,
    });
    const code = await deps.exec('tar', ['--zstd', '-xf', deps.archive, '-C', '/'], { signal });
    if (code !== 0) throw new Error(`tar exited ${code}`);
    return { outcome: 'hit', key, dirs };
  } catch (err) {
    deps.log(`goal-agent prepare: build cache restore: ${message(err)}`);
    return { outcome: 'error', key, dirs };
  } finally {
    await rm(deps.archive, { force: true });
  }
}

/**
 * Saves the cache after a miss, for run after the agent and before its finished line: it reserves
 * the key (a 409, another box's reservation or a ready cache, skips), packs the directories that
 * exist into a tar.zst, skips one over maxBytes, uploads it with PUT and calls done with its size.
 * It returns whether the cache was saved; a failure is logged and never thrown.
 */
export async function saveBuildCache(deps: SaveDeps, state: BuildCacheState): Promise<boolean> {
  const { key, dirs } = state;
  if (state.outcome !== 'miss' || !key || !dirs) return false;
  try {
    const present: string[] = [];
    for (const d of dirs) if (await exists(d)) present.push(relative('/', d));
    if (present.length === 0) return false;

    const reserve = gatewayRoute(deps, key);
    const res = await deps.fetch(reserve, { method: 'POST', headers: authFor(deps, reserve) });
    if (res.status === 409) {
      await res.body?.cancel();
      deps.log('goal-agent run: build cache: another box saves this key; skipped');
      return false;
    }
    if (!res.ok) throw new Error(`POST ${routes}<key>: ${res.status}`);
    const upload = (await res.json()) as { url?: unknown; method?: unknown };
    if (typeof upload.url !== 'string' || upload.url === '') throw new Error(`POST ${routes}<key>: no url`);

    await mkdir(dirname(deps.archive), { recursive: true });
    const code = await deps.exec('tar', ['-I', 'zstd -T0', '-cf', deps.archive, '-C', '/', '--', ...present]);
    if (code !== 0) throw new Error(`tar exited ${code}`);
    const size = (await stat(deps.archive)).size;
    if (size > (deps.maxBytes ?? maxArchiveBytes)) {
      deps.log(`goal-agent run: build cache: the archive is ${size} bytes, over the limit; skipped`);
      return false;
    }

    const to = new URL(upload.url, deps.gateway);
    const put = await deps.fetch(to, {
      method: typeof upload.method === 'string' && upload.method !== '' ? upload.method : 'PUT',
      headers: { ...authFor(deps, to), 'Content-Length': String(size), 'Content-Type': 'application/zstd' },
      body: Readable.toWeb(createReadStream(deps.archive)) as unknown as RequestInit['body'],
      duplex: 'half',
    } as RequestInit);
    await put.body?.cancel();
    if (!put.ok) throw new Error(`upload: ${put.status}`);

    const done = gatewayRoute(deps, key, 'done');
    const committed = await deps.fetch(done, {
      method: 'POST',
      headers: { ...authFor(deps, done), 'Content-Type': 'application/json' },
      body: JSON.stringify({ size_bytes: size }),
    });
    await committed.body?.cancel();
    if (!committed.ok) throw new Error(`POST ${routes}<key>/done: ${committed.status}`);
    return true;
  } catch (err) {
    deps.log(`goal-agent run: build cache save: ${message(err)}`);
    return false;
  } finally {
    await rm(deps.archive, { force: true });
  }
}

/** The state prepare wrote at path; undefined when it wrote none or it does not read. */
export async function readBuildCacheState(path: string): Promise<BuildCacheState | undefined> {
  const text = await readIfExists(path);
  if (text === '') return undefined;
  try {
    const s = JSON.parse(text) as BuildCacheState;
    return ['hit', 'miss', 'off', 'error'].includes(s.outcome) ? s : undefined;
  } catch {
    return undefined;
  }
}

export async function writeBuildCacheState(path: string, state: BuildCacheState): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, JSON.stringify(state));
}

/** The route of key, or of one of its sub-paths, on the gateway's origin. */
function gatewayRoute(deps: CacheIO, key: string, sub?: string): URL {
  return new URL(`${routes}${key}${sub ? `/${sub}` : ''}`, deps.gateway);
}

/** The task token for a request to the gateway's origin, nothing for a signed bucket URL. */
function authFor(deps: CacheIO, url: URL): Record<string, string> {
  return url.origin === new URL(deps.gateway).origin ? { Authorization: `Bearer ${deps.token}` } : {};
}

function lines(out: string): string[] {
  return out
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => l !== '');
}

async function readIfExists(path: string): Promise<string> {
  return readFile(path, 'utf8').catch((err: NodeJS.ErrnoException) => {
    if (err.code === 'ENOENT' || err.code === 'ENOTDIR') return '';
    throw err;
  });
}

async function exists(path: string): Promise<boolean> {
  return stat(path).then(
    () => true,
    () => false,
  );
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
