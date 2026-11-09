// The retry rule of every query. A 4xx will not fix itself, and neither will a 503 whose code says goald
// was built without the feature (jonah-unavailable): the page shows the feature's
// empty state at once. Any other 5xx or a network blip is tried again, twice.
import { ApiError } from './client';

/** Whether the answer says this goald has no such feature: a 503 with a code ending in "-unavailable". */
export function isUnavailable(err: unknown): boolean {
  return err instanceof ApiError && err.status === 503 && (err.code?.endsWith('-unavailable') ?? false);
}

/**
 * Whether goald is restarting: a 503 with code goald-starting, or a 503 with no code or any code that
 * does not end in "-unavailable" (a proxy's bare 503). The route boundary shows "Goal is restarting"
 * and loads again by itself (restartDelay).
 */
export function isStarting(err: unknown): boolean {
  return err instanceof ApiError && err.status === 503 && (err.code === 'goald-starting' || !isUnavailable(err));
}

export function shouldRetry(count: number, err: unknown): boolean {
  if (err instanceof ApiError && err.status < 500) return false;
  if (isStarting(err)) return false;
  return !isUnavailable(err) && count < 2;
}

const restartBackoffMs = [2_000, 4_000, 8_000];
const restartCapMs = 15_000;
const retryAfterCapMs = 30_000;

/** How long to wait before loading again: Retry-After, capped at 30 s; 2, 4, 8, then 15 s when it is missing. */
export function restartDelay(attempt: number, retryAfterSeconds?: number): number {
  if (retryAfterSeconds !== undefined) return Math.min(retryAfterSeconds * 1000, retryAfterCapMs);
  return restartBackoffMs[attempt] ?? restartCapMs;
}

const sameRunMs = 10_000;
let lastRestart: { path: string; attempt: number; at: number } | undefined;

/** The attempt number for a restarting screen that just appeared: it goes on from the retry that loaded this page a moment ago, else starts at 0. */
export function restartAttempt(path: string, now = Date.now()): number {
  return lastRestart && lastRestart.path === path && now - lastRestart.at < sameRunMs ? lastRestart.attempt : 0;
}

/** Records that the screen loaded the page again by itself, so the next failure waits longer. */
export function noteRestartRetry(path: string, attempt: number, now = Date.now()): void {
  lastRestart = { path, attempt: attempt + 1, at: now };
}
