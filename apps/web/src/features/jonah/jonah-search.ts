// Jonah's overview and proposals, kept in the URL: ?tab=proposals opens the Proposals tab, and ?state=
// filters it by a proposal's state. An unknown tab reads as Overview and an unknown state as All, so a
// link never shows an empty view.
import type { ProposalState } from '../../api/types';

export const proposalStates = [
  'proposed',
  'published',
  'applied',
  'rejected',
  'superseded',
] as const satisfies readonly ProposalState[];

export interface JonahSearch {
  /** Optional so that links need no search; the route always reads a tab. */
  tab?: 'overview' | 'proposals' | undefined;
  /** A proposal state; absent shows every state. */
  state?: ProposalState | undefined;
}

export function jonahSearchOf(search: Record<string, unknown>): JonahSearch {
  const tab = search.tab === 'proposals' ? 'proposals' : 'overview';
  const state = (proposalStates as readonly unknown[]).includes(search.state)
    ? (search.state as ProposalState)
    : undefined;
  return { tab, state };
}
