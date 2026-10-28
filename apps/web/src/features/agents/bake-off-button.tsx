// "Bake off this agent" on the Agent page (build/agent). Pressing it queues a bake-off of the agent on the
// current version and opens the new bake-off's screen. It needs definition:edit, as the API does: without
// it the button is disabled and says why. A refusal shows in place, under the button.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { ChartColumn } from 'lucide-react';
import { useId } from 'react';
import { ApiError, startBakeOff } from '../../api/client';
import { bakeOffQuery, keys } from '../../api/queries';
import { useFactory, useFactoryParams } from '../../shared/factory';
import { formatUsd } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { holds, reasons } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { runFailure } from '../jonah/evals-format';

/** Why a start was refused. A learning-budget 409 names the estimate it refused; every other error reads as runFailure says. */
export function startFailure(err: unknown): string {
  if (err instanceof ApiError && err.code === 'learning-budget-exhausted') {
    const usd = err.problem.estimate?.usd;
    return usd === undefined
      ? 'The learning budget has run out. Nothing was started.'
      : `This bake-off would cost about ${formatUsd(usd)}, more than the learning budget has left. Nothing was started.`;
  }
  return runFailure(err);
}

export function BakeOffButton({ agent }: { agent: string }) {
  const factory = useFactory();
  const org = useOrg();
  const params = useFactoryParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const reasonId = useId();

  // A bake-off is a run of evals, so it needs definition:edit (the API's scope).
  const canEdit = holds(factory, 'definition:edit');

  // The new bake-off is shown at once, from the answer, and the screen moves to it.
  const start = useMutation({
    mutationFn: () => startBakeOff(org.id, factory.id, agent),
    onSuccess: ({ bake_off }) => {
      queryClient.setQueryData(bakeOffQuery(org.id, factory.id, bake_off.id).queryKey, bake_off);
      void queryClient.invalidateQueries({ queryKey: keys.bakeOffs(org.id, factory.id) });
      void navigate({
        to: '/o/$org/f/$factory/jonah/bake-offs/$bakeOff',
        params: { ...params, bakeOff: bake_off.id },
      });
    },
  });

  return (
    <div className="flex max-w-96 flex-col items-end gap-2">
      <Button
        variant="secondary"
        size="sm"
        disabled={!canEdit || start.isPending}
        aria-busy={start.isPending || undefined}
        aria-describedby={canEdit ? undefined : reasonId}
        onClick={() => start.mutate()}
      >
        <ChartColumn size={15} strokeWidth={1.5} aria-hidden="true" />
        Bake off this agent
      </Button>
      {canEdit ? null : (
        <p id={reasonId} className="m-0 text-right text-meta text-muted">
          {reasons.definition(factory.name)}
        </p>
      )}
      {start.isError ? (
        <div aria-live="polite">
          <Callout tone="rework">{startFailure(start.error)}</Callout>
        </div>
      ) : null}
    </div>
  );
}
