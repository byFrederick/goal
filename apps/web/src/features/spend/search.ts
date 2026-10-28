// The spend screen's URL state (frontend.md: every filter and selection lives in the URL): the window
// the figures cover, ?window=week|month|90d. Week is the default, and any other value falls back to it.
import type { SpendWindow } from '../../api/types';

export interface SpendSearch {
  /** Absent means week: the link to the screen needs no window. */
  window?: SpendWindow | undefined;
}

export const spendWindows: readonly SpendWindow[] = ['week', 'month', '90d'];

export function spendSearch(search: Record<string, unknown>): SpendSearch {
  const window = spendWindows.find((w) => w === search.window);
  return window ? { window } : {};
}
