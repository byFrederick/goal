import { redact } from './retry.js';

/** The problem code goald and the control Worker answer with, on a 503, while goald starts. */
export const startingCode = 'goald-starting';

/** The wait before another attempt: Retry-After clamped to these bounds, or the fallback without it. */
export const minWaitSeconds = 1;
export const maxWaitSeconds = 30;
export const fallbackWaitSeconds = 5;

/** The longest a call waits out a restart in total; a deploy's outage lasts about 47 s. */
export const maxTotalWaitMs = 120_000;

export interface GatewayDeps {
  fetch: typeof globalThis.fetch;
  /** The wait between attempts; setTimeout when absent. */
  sleep?: (ms: number) => Promise<void>;
  /** The clock in milliseconds; Date.now when absent. */
  now?: () => number;
  /** One line per retry, to stderr when absent. */
  log?: (line: string) => void;
}

export interface GatewayOptions {
  /** Each attempt's own time limit, joined to init.signal, so one attempt's timeout does not end the retry. */
  attemptTimeoutMs?: number;
}

/**
 * fetch for a call to the gateway that waits out a goald restart. On a 503 whose JSON body has code
 * goald-starting it waits Retry-After seconds (clamped to 1–30 s, 5 s without the header) and sends
 * the request again, until the total wait would pass 120 s; then it returns that last 503. Any other
 * answer comes back at once, and so does the first answer to a request whose body cannot be sent
 * again (a stream). An abort of init.signal ends the wait and rejects with its reason.
 */
export async function gatewayFetch(
  deps: GatewayDeps,
  input: string | URL,
  init: RequestInit = {},
  opts: GatewayOptions = {},
): Promise<Response> {
  const sleep = deps.sleep ?? ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)));
  const now = deps.now ?? Date.now;
  const log = deps.log ?? ((line: string) => process.stderr.write(`${line}\n`));
  const replayable = canReplay(init.body);
  const started = now();
  let slept = 0;
  for (let attempt = 1; ; attempt++) {
    const res = await deps.fetch(input, withAttemptSignal(init, opts.attemptTimeoutMs));
    if (!replayable || !(await isStarting(res))) return res;
    const wait = waitSeconds(res.headers.get('Retry-After')) * 1000;
    if (Math.max(now() - started, slept) + wait > maxTotalWaitMs) return res;
    await res.body?.cancel();
    log(
      `goal-agent: gateway ${redact(new URL(input).pathname)} is starting (503 ${startingCode}); attempt ${attempt} failed, retrying in ${wait / 1000}s`,
    );
    await abortable(sleep(wait), init.signal);
    slept += wait;
  }
}

/** Whether res is a 503 whose JSON body has code goald-starting; it reads a clone, so res stays whole. */
async function isStarting(res: Response): Promise<boolean> {
  if (res.status !== 503 || !res.body) return false;
  try {
    const body = (await res.clone().json()) as { code?: unknown } | null;
    return body?.code === startingCode;
  } catch {
    return false;
  }
}

/** The seconds a Retry-After header asks for, clamped; the fallback when it is missing or not a number. */
export function waitSeconds(header: string | null): number {
  const n = header === null || header.trim() === '' ? Number.NaN : Number(header);
  if (!Number.isFinite(n)) return fallbackWaitSeconds;
  return Math.min(maxWaitSeconds, Math.max(minWaitSeconds, Math.ceil(n)));
}

/** Whether fetch can send body again: no body, or one held in memory. A stream is read once. */
function canReplay(body: RequestInit['body']): boolean {
  return (
    body === undefined ||
    body === null ||
    typeof body === 'string' ||
    body instanceof URLSearchParams ||
    body instanceof Blob ||
    body instanceof ArrayBuffer ||
    ArrayBuffer.isView(body) ||
    body instanceof FormData
  );
}

function withAttemptSignal(init: RequestInit, timeoutMs: number | undefined): RequestInit {
  if (timeoutMs === undefined) return init;
  const timeout = AbortSignal.timeout(timeoutMs);
  return { ...init, signal: init.signal ? AbortSignal.any([init.signal, timeout]) : timeout };
}

/** wait, ended early by signal with its reason. */
function abortable(wait: Promise<void>, signal: AbortSignal | null | undefined): Promise<void> {
  if (!signal) return wait;
  if (signal.aborted) return Promise.reject(signal.reason);
  return new Promise<void>((resolve, reject) => {
    const onAbort = () => reject(signal.reason);
    signal.addEventListener('abort', onAbort, { once: true });
    wait.then(
      () => {
        signal.removeEventListener('abort', onAbort);
        resolve();
      },
      (err: unknown) => {
        signal.removeEventListener('abort', onAbort);
        reject(err);
      },
    );
  });
}
