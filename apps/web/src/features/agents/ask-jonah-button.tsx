// "Ask Jonah for a fix" on the Agent page (build/agent, ADR 0021): one draft request about this agent,
// its text built from the reasons "Why review sent work back" shows. Not built yet: goald does not run
// draft requests yet, so the button stays "Not available yet" (ADR 0016, decision 4). fixRequestText is
// kept for the request that the runner will send.

import type { AgentStats } from '../../api/types';
import { Button } from '../../ui/button';
import { NotAvailable } from '../../ui/not-available';

/** goald's limit on a request's text, once trimmed (422 draft-request-text-invalid past it). */
export const requestTextLimit = 2000;

/**
 * The text of the fix request: the reasons review gave, most frequent first. A reason that would
 * pass the limit is left out and counted in a closing line, so the text always fits.
 */
export function fixRequestText(stats: AgentStats): string {
  const head = `Fix the agent ${stats.agent}, so review sends less of its work back. In the last 7 days review sent ${stats.sent_back.count} of its tasks back, for these reasons:`;
  const tail = 'Change its prompt or its skills in the definition. Leave the other agents as they are.';
  const lines = stats.reasons.map((r) => `- ${r.label} (${r.count} ${r.count === 1 ? 'time' : 'times'})`);
  // The text with the first `kept` reasons and a closing line for the rest.
  const textWith = (kept: number) => {
    const rest = lines.length - kept;
    const body = [head, ...lines.slice(0, kept), ...(rest > 0 ? [`- and ${rest} more`] : []), tail];
    return body.join('\n');
  };
  let best = textWith(0);
  for (let kept = 1; kept <= lines.length; kept += 1) {
    const candidate = textWith(kept);
    if (candidate.length > requestTextLimit) break;
    best = candidate;
  }
  return best;
}

export function AskJonahButton() {
  return (
    <NotAvailable>
      <Button variant="secondary" size="sm">
        Ask Jonah for a fix
      </Button>
    </NotAvailable>
  );
}
