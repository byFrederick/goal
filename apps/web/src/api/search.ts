// The shell's search (shell.jpg s2) over client.ts's goald client: one read per keystroke for the
// organization's items, agents, factories and connections, each group best first (openapi.yaml,
// searchOrg). The pages are not here: the shell matches them from app/screens.ts.
import { orgPath, unwrap } from '@goal/api-client';
import { cancel, goal } from './client';
import type { OrgSearch } from './types';

export interface SearchQuery {
  /** What to find, trimmed by the caller: 1 to 200 characters. */
  q: string;
  /** How many results each group holds at most (1 to 20). */
  limit: number;
}

/** 400 for a query that is empty or too long; the caller never sends one. */
export function searchOrg(org: string, { q, limit }: SearchQuery, signal?: AbortSignal): Promise<OrgSearch> {
  return unwrap(goal.GET(orgPath('/search'), { params: { path: { org }, query: { q, limit } }, ...cancel(signal) }));
}
