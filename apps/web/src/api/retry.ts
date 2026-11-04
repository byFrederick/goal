// The retry rule of every query. A 4xx will not fix itself, and neither will a 503 whose code says goald
// was built without the feature (jonah-unavailable): the page shows the feature's
// empty state at once. Any other 5xx or a network blip is tried again, twice.
import { ApiError } from './client';

/** Whether the answer says this goald has no such feature: a 503 with a code ending in "-unavailable". */
export function isUnavailable(err: unknown): boolean {
  return err instanceof ApiError && err.status === 503 && (err.code?.endsWith('-unavailable') ?? false);
}

export function shouldRetry(count: number, err: unknown): boolean {
  if (err instanceof ApiError && err.status < 500) return false;
  return !isUnavailable(err) && count < 2;
}
