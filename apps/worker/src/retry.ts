import { readdir, rm } from 'node:fs/promises';
import { join } from 'node:path';
import type { Exec } from './prepare.js';

/** How many times a network git step runs at most, and the wait before each retry. */
export const attempts = 3;
export const backoff = [2_000, 8_000, 20_000];

/**
 * What in git's stderr marks a dropped or failed transport, worth another attempt. A rejected push,
 * a missing ref or a bad URL is a final answer and is not on the list.
 */
const transient = [
  /RPC failed/i,
  /early EOF/i,
  /unexpected disconnect while reading sideband packet/i,
  /the remote end hung up unexpectedly/i,
  /Could not resolve host/i,
  /Failed to connect/i,
  /Operation timed out/i,
  /Connection timed out/i,
  /Connection reset by peer/i,
  /The requested URL returned error: 5\d\d/i,
];

export function isTransient(stderr: string): boolean {
  return transient.some((re) => re.test(stderr));
}

/** Removes the user and password of any URL in s, so a log line or an error carries no credential. */
export function redact(s: string): string {
  return s.replace(/(\w+:\/\/)[^\s/@]+@/g, '$1***@');
}

/** The first non-empty line of git's stderr, redacted. */
export function firstLine(stderr: string): string {
  return redact(stderr.split('\n').find((l) => l.trim() !== '') ?? '').trim();
}

export interface RetryDeps {
  exec: Exec;
  sleep: (ms: number) => Promise<void>;
  /** One line to stderr, never stdout: stdout is prepare's protocol. */
  log: (line: string) => void;
  /** Each attempt's time limit. */
  timeoutMs: number;
}

/** Keeps the tail of git's stderr; a hung transfer may print progress for minutes. */
const maxStderr = 64 * 1024;

/**
 * Runs one network git step (git <args>) with a time limit per attempt, retrying a cut-off attempt or
 * a transport failure up to `attempts` times. Before each retry it empties `clean` (the directory a
 * clone writes into), never more. It resolves with the exit code of the last attempt when that one
 * succeeded or failed for a reason a retry does not fix, and rejects when every attempt failed on the
 * transport, naming the last attempt's first error line.
 */
export async function gitWithRetry(
  deps: RetryDeps,
  args: string[],
  opts: { cwd?: string; clean?: string } = {},
): Promise<number> {
  const step = `git ${args[0]}`;
  for (let attempt = 1; ; attempt++) {
    if (attempt > 1 && opts.clean !== undefined) await empty(opts.clean);
    const { code, stderr, timedOut } = await once(deps, args, opts.cwd);
    if (code === 0) return 0;
    if (!timedOut && !isTransient(stderr)) return code;
    const why = timedOut ? `timed out after ${deps.timeoutMs / 1000}s` : firstLine(stderr) || `exited ${code}`;
    if (attempt >= attempts) {
      throw new Error(`${step} failed after ${attempt} attempts: ${why}`);
    }
    const wait = backoff[attempt - 1] ?? backoff[backoff.length - 1] ?? 0;
    deps.log(
      `goal-agent prepare: ${step} attempt ${attempt} of ${attempts} failed: ${why}; retrying in ${wait / 1000}s`,
    );
    await deps.sleep(wait);
  }
}

async function once(
  deps: RetryDeps,
  args: string[],
  cwd: string | undefined,
): Promise<{ code: number; stderr: string; timedOut: boolean }> {
  const abort = new AbortController();
  let stderr = '';
  const onStderr = (chunk: string) => {
    stderr = (stderr + chunk).slice(-maxStderr);
  };
  let timer: NodeJS.Timeout | undefined;
  const cut = new Promise<'timeout'>((resolve) => {
    timer = setTimeout(() => {
      abort.abort();
      resolve('timeout');
    }, deps.timeoutMs);
  });
  const opts = { signal: abort.signal, stderr: onStderr, ...(cwd === undefined ? {} : { cwd }) };
  try {
    const res = await Promise.race([deps.exec('git', args, opts), cut]);
    if (res === 'timeout') return { code: 1, stderr, timedOut: true };
    return { code: res, stderr, timedOut: false };
  } finally {
    clearTimeout(timer);
  }
}

/** Removes what is in dir, keeping dir itself (it may be a mount point). A missing dir is empty. */
async function empty(dir: string): Promise<void> {
  const entries = await readdir(dir).catch((err: NodeJS.ErrnoException) => {
    if (err.code === 'ENOENT') return [];
    throw err;
  });
  for (const e of entries) await rm(join(dir, e), { recursive: true, force: true });
}
