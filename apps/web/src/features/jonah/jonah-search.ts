// The Proposals tab's state filter, kept in the URL: ?state= filters it by a proposal's state. An unknown
// state reads as All, so a link never shows an empty view.
import type { ProposalState } from '../../api/types';

export const proposalStates = [
  'proposed',
  'published',
  'applied',
  'rejected',
  'superseded',
] as const satisfies readonly ProposalState[];

export interface JonahSearch {
  /** A proposal state; absent shows every state. */
  state?: ProposalState | undefined;
}

export function jonahSearchOf(search: Record<string, unknown>): JonahSearch {
  const state = (proposalStates as readonly unknown[]).includes(search.state)
    ? (search.state as ProposalState)
    : undefined;
  return { state };
}
