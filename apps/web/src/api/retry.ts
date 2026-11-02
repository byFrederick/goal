import { ApiError } from './client';

/**
 * Whether a failed read is asked again. An answer that will not change is not: a 4xx, a 501 and any
 * `*-unavailable` code (judges, evals, Jonah) show their screen at once. A network blip or a
 * 502/503 may pass, so it is asked up to twice more.
 */
export function shouldRetry(count: number, err: unknown): boolean {
  if (count >= 2) return false;
  if (err instanceof ApiError) {
    if (err.status < 500 || err.status === 501) return false;
    if (err.code?.endsWith('-unavailable')) return false;
  }
  return true;
}
