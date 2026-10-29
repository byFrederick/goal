// Jonah's copy and numbers, from the overview and the proposals (ADR 0034). Pure, so the rules are tested
// without a page.
import type { JonahPolicy, ProposalEstimate, ProposalState } from '../../api/types';
import { formatPercent, formatUsd, plural } from '../../shared/format';

/** A proposal's state in words, for the Proposals tab's filter and a card's chip. */
export const proposalStateLabels: Record<ProposalState, string> = {
  proposed: 'Waiting',
  published: 'Published',
  applied: 'Applied',
  rejected: 'Rejected',
  superseded: 'Superseded',
};

const roleNames: Record<string, string> = {
  owner: 'owners',
  admin: 'admins',
  editor: 'editors',
  viewer: 'viewers',
  member: 'members',
};

/** "Owners and admins", "Owners, admins and editors": the roles that may approve, in the order the policy gives. */
export function reviewersText(roles: readonly string[]): string {
  const names = roles.map((role) => roleNames[role] ?? role);
  const last = names.pop();
  if (last === undefined) return 'Nobody yet';
  const text = names.length === 0 ? last : `${names.join(', ')} and ${last}`;
  return text.replace(/^./, (first) => first.toUpperCase());
}

/** The model the review runs on, or "Not set" when neither the policy nor the runtime sets one. */
export function modelText(policy: JonahPolicy): string {
  return policy.model ?? 'Not set';
}

/** "Proposes after" in the policy's words: "5 failures with the same cause". */
export function proposeAfterText(policy: JonahPolicy): string {
  return `${plural(policy.propose_after, 'failure', 'failures')} with the same cause`;
}

/** Hours with no decimals, as the estimate shows them: "31 h". */
function hours(h: number): string {
  return `${Math.round(h)} h`;
}

/** The estimate's metric in words and its before and after, as the card's line shows them. */
export function estimateOf(estimate: ProposalEstimate): { label: string; before: string; after: string } {
  if (estimate.metric === 'sent_back_rate') {
    return {
      label: estimate.station ? `Sent back, ${estimate.station} lane` : 'Sent back',
      before: formatPercent(estimate.before),
      after: formatPercent(estimate.after),
    };
  }
  return { label: 'Lead time, p85', before: hours(estimate.before), after: hours(estimate.after) };
}

/** The cost per item the change adds: "+$0.04", "-$0.02", or "no change". */
export function costPerItemText(usd: number): string {
  if (usd === 0) return 'no change';
  return usd > 0 ? `+${formatUsd(usd)}` : formatUsd(usd);
}

/** "Based on 4 runs sent back": the evidence a proposal rests on. */
export function basedOnText(runs: number): string {
  return `Based on ${plural(runs, 'run', 'runs')} sent back`;
}

/** An observation's chip: Proposed once it became a proposal, else how far it has come. */
export function observationChip(state: 'watching' | 'proposed', count: number, proposeAfter: number) {
  return state === 'proposed' ? 'Proposed' : `Watching, ${count} of ${proposeAfter}`;
}
