import { existsSync } from 'node:fs';
import { mkdir, mkdtemp, readdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { Exec } from './prepare.js';
import { gitWithRetry, isTransient, type RetryDeps, redact } from './retry.js';

const dropped = 'error: RPC failed; curl 92 HTTP/2 stream 5 was not closed cleanly: CANCEL (err 8)\nfatal: early EOF\n';

let root: string;
let ws: string;

beforeEach(async () => {
  root = await mkdtemp(join(tmpdir(), 'goal-retry-'));
  ws = join(root, 'workspace');
});

afterEach(async () => {
  await rm(root, { recursive: true, force: true });
});

/** A fake git that runs answer(attempt) per call and records the waits and log lines. */
function fake(answer: (attempt: number, opts: Parameters<Exec>[2]) => Promise<number>, timeoutMs = 1_000) {
  const calls: { args: string[]; signal?: AbortSignal; leftover: string[] }[] = [];
  const waits: number[] = [];
  const lines: string[] = [];
  const exec: Exec = async (_command, args, opts) => {
    const leftover = existsSync(ws) ? await readdir(ws) : [];
    calls.push({ args, leftover, ...(opts?.signal ? { signal: opts.signal } : {}) });
    return answer(calls.length, opts);
  };
  const deps: RetryDeps = {
    exec,
    sleep: async (ms) => {
      waits.push(ms);
    },
    log: (line) => lines.push(line),
    timeoutMs,
  };
  return { deps, calls, waits, lines };
}

/** A clone attempt that writes a partial workspace, then fails on the transport with stderr. */
async function partialClone(opts: Parameters<Exec>[2], stderr: string): Promise<number> {
  await mkdir(join(ws, '.git'), { recursive: true });
  await writeFile(join(ws, '.git', 'half'), 'x');
  opts?.stderr?.(stderr);
  return 128;
}

const clone = ['clone', '--quiet', '--filter=blob:none', '--', 'https://example.com/acme/repo.git', ''];

describe('gitWithRetry', () => {
  it('retries a clone that fails twice on the transport and then succeeds, from an empty workspace', async () => {
    const { deps, calls, waits, lines } = fake((n, opts) => (n < 3 ? partialClone(opts, dropped) : Promise.resolve(0)));

    await expect(gitWithRetry(deps, clone, { clean: ws })).resolves.toBe(0);

    expect(calls).toHaveLength(3);
    expect(calls.map((c) => c.leftover)).toEqual([[], [], []]);
    expect(existsSync(ws)).toBe(true);
    expect(waits).toEqual([2_000, 8_000]);
    expect(lines).toEqual([
      'goal-agent prepare: git clone attempt 1 of 3 failed: error: RPC failed; curl 92 HTTP/2 stream 5 was not closed cleanly: CANCEL (err 8); retrying in 2s',
      'goal-agent prepare: git clone attempt 2 of 3 failed: error: RPC failed; curl 92 HTTP/2 stream 5 was not closed cleanly: CANCEL (err 8); retrying in 8s',
    ]);
  });

  it('rejects after 3 attempts with the last error when the clone always fails', async () => {
    const { deps, calls, waits } = fake((n, opts) =>
      partialClone(opts, n < 3 ? dropped : 'fatal: the remote end hung up unexpectedly\nfatal: early EOF\n'),
    );

    await expect(gitWithRetry(deps, clone, { clean: ws })).rejects.toThrow(
      'git clone failed after 3 attempts: fatal: the remote end hung up unexpectedly',
    );
    expect(calls).toHaveLength(3);
    expect(waits).toEqual([2_000, 8_000]);
  });

  it('cuts a hung attempt at the time limit, ends it and runs the next one', async () => {
    const { deps, calls, lines } = fake((n) => (n === 1 ? new Promise<number>(() => {}) : Promise.resolve(0)), 20);

    await expect(gitWithRetry(deps, ['fetch', '--quiet', 'origin'], { cwd: ws })).resolves.toBe(0);

    expect(calls).toHaveLength(2);
    expect(calls[0]?.signal?.aborted).toBe(true);
    expect(calls[1]?.signal?.aborted).toBe(false);
    expect(lines).toEqual([
      'goal-agent prepare: git fetch attempt 1 of 3 failed: timed out after 0.02s; retrying in 2s',
    ]);
  });

  it.each([
    ' ! [rejected]        goal/base -> goal/base (fetch first)\nerror: failed to push some refs\n',
    "fatal: couldn't find remote ref refs/heads/goal/base\n",
    "fatal: '/tmp/no-such.git' does not appear to be a git repository\n",
    '',
  ])('does not retry a failure that is not the transport: %j', async (stderr) => {
    const { deps, calls, lines } = fake(async (_n, opts) => {
      opts?.stderr?.(stderr);
      return 1;
    });

    await expect(gitWithRetry(deps, ['push', '--quiet', 'origin', 'goal/base'])).resolves.toBe(1);
    expect(calls).toHaveLength(1);
    expect(lines).toEqual([]);
  });

  it('logs a retry without the credentials of a URL in git’s error', async () => {
    const leak =
      "fatal: unable to access 'https://x-access-token:test-token@example.com/acme/repo.git/': Could not resolve host: example.com\n";
    const { deps, lines } = fake(async (n, opts) => {
      if (n === 1) opts?.stderr?.(leak);
      return n === 1 ? 128 : 0;
    });
    const stdout: string[] = [];
    const write = process.stdout.write;
    process.stdout.write = ((chunk: string) => {
      stdout.push(chunk);
      return true;
    }) as typeof process.stdout.write;
    try {
      await gitWithRetry(deps, clone, { clean: ws });
    } finally {
      process.stdout.write = write;
    }

    expect(lines).toHaveLength(1);
    expect(lines[0]).not.toContain('test-token');
    expect(lines[0]).toContain("'https://***@example.com/acme/repo.git/'");
    expect(stdout).toEqual([]);
  });

  it('knows the transport errors of the incident and leaves credentials out of a URL', () => {
    for (const e of [
      'error: RPC failed; curl 92 HTTP/2 stream 5 was not closed cleanly: CANCEL (err 8)',
      'fetch-pack: unexpected disconnect while reading sideband packet',
      'fatal: early EOF',
      'error: The requested URL returned error: 502',
    ]) {
      expect(isTransient(e)).toBe(true);
    }
    expect(isTransient('error: The requested URL returned error: 403')).toBe(false);
    expect(redact('https://user:pw@example.com/a and http://tok@example.com')).toBe(
      'https://***@example.com/a and http://***@example.com',
    );
  });
});
