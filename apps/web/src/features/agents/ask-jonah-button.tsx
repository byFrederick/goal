// "Ask Jonah for a fix" on the Agent page (build/agent, ADR 0021): one draft request about this agent,
// its text built from the reasons "Why review sent work back" shows. It needs definition:edit, as the API
// does, and without the reasons there is nothing to ask about, so the button stays "Not available yet".
// On success the definition screen opens on the request (?request=), where the Ask bar shows it.
import { useNavigate } from '@tanstack/react-router';
import { useId } from 'react';
import type { AgentStats } from '../../api/types';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { holds, reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { NotAvailable } from '../../ui/not-available';
import { askFailure, useAskFactory } from '../factory-editor/draft-requests';

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

export function AskJonahButton({ stats }: { stats: AgentStats }) {
  const factory = useFactory();
  const params = useFactoryParams();
  const navigate = useNavigate();
  const reasonId = useId();
  const askFactory = useAskFactory();

  if (stats.reasons.length === 0) {
    return (
      <NotAvailable>
        <Button variant="secondary" size="sm">
          Ask Jonah for a fix
        </Button>
      </NotAvailable>
    );
  }

  // Asking changes the definition, so it needs definition:edit (the API's scope).
  const canEdit = holds(factory, 'definition:edit');

  return (
    <div className="flex max-w-96 flex-col items-end gap-2">
      <Button
        variant="secondary"
        size="sm"
        disabled={!canEdit || askFactory.isPending}
        aria-busy={askFactory.isPending || undefined}
        aria-describedby={canEdit ? undefined : reasonId}
        onClick={() =>
          askFactory.mutate(
            { text: fixRequestText(stats), agent: stats.agent },
            {
              onSuccess: (created) =>
                navigate({
                  to: '/o/$org/f/$factory/definition/$section',
                  params: { ...params, section: 'agents' },
                  search: { agent: stats.agent, request: created.id },
                }),
            },
          )
        }
      >
        Ask Jonah for a fix
      </Button>
      {canEdit ? null : (
        <p id={reasonId} className="m-0 text-right text-meta text-muted">
          {reasons.definition(factory.name)}
        </p>
      )}
      {askFactory.isError ? (
        <div aria-live="polite">
          <Callout tone="rework">{askFailure(askFactory.error)}</Callout>
        </div>
      ) : null}
    </div>
  );
}
