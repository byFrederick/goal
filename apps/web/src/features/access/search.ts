// The Access page's URL state (frontend.md: every filter lives in the URL): the view (?by=reach, factory or
// secret) and the search text. Unknown or malformed values are dropped, never thrown on.
import type { AccessView } from '../../api/types';

export const accessViews = ['reach', 'factory', 'secret'] as const satisfies readonly AccessView[];

export interface AccessSearch {
  /** The view; absent for reach, the default. */
  by?: Exclude<AccessView, 'reach'> | undefined;
  /** Text a row's name, its secrets or a factory contains. */
  q?: string | undefined;
}

export function accessSearch(search: Record<string, unknown>): AccessSearch {
  const by = search.by === 'factory' || search.by === 'secret' ? search.by : undefined;
  const q = typeof search.q === 'string' && search.q.trim() !== '' && search.q.length <= 100 ? search.q : undefined;
  return { ...(by ? { by } : {}), ...(q ? { q } : {}) };
}
