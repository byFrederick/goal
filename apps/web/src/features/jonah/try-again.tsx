// Try again on a proposal's Tried on past items card (ADR 0039 decision 11): ask goald for the estimate, show it
// in a confirm dialog, and queue the tries only when the person confirms. The proposal that comes back is written
// into the page's cache, so each item's new attempts show after the earlier ones. A refusal is said by its code.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { ApiError, tryProposal } from '../../api/client';
import { proposalQuery } from '../../api/queries';
import type { TryProposalEstimate } from '../../api/types';
import { useFactory } from '../../shared/factory';
import { formatUsd, plural } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { InfoTip } from './info-tip';
import { tryRefusals } from './jonah-format';

/** What a refused or failed Try again says: the sentence of the API's code, or that goald is not answering. */
export function tryFailure(err: unknown): string {
  const known = err instanceof ApiError && err.code ? tryRefusals[err.code] : undefined;
  return known ?? 'goald is not answering. Try again in a moment.';
}

/** "Adds 2 tries for about $8.40. $31.20 left of Jonah's budget this week." */
export function estimateText(e: TryProposalEstimate): string {
  return `Adds ${plural(e.tries, 'try', 'tries')} for about ${formatUsd(e.estimate_usd)}. ${formatUsd(e.budget_left_usd)} left of Jonah's budget this week.`;
}

/** The Try again button and its confirm dialog, for a person with definition:edit on a proposal that waits. */
export function TryAgain({ number }: { number: number }) {
  const org = useOrg();
  const factory = useFactory();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);

  const estimate = useMutation({ mutationFn: () => tryProposal(org.id, factory.id, number, false) });
  const confirm = useMutation({
    mutationFn: () => tryProposal(org.id, factory.id, number, true),
    onSuccess: (result) => {
      if (result.proposal)
        queryClient.setQueryData(proposalQuery(org.id, factory.id, number).queryKey, result.proposal);
      setOpen(false);
    },
  });

  const show = (next: boolean) => {
    setOpen(next);
    if (next) {
      confirm.reset();
      estimate.mutate();
    }
  };
  const failure = estimate.error ?? confirm.error;
  const ready = estimate.data !== undefined && !estimate.isPending;

  return (
    <>
      <Button size="sm" variant="secondary" onClick={() => show(true)}>
        Try again
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={(next) => (next ? show(true) : setOpen(false))}
        title="Try again"
        description="Adds one more try for each past item."
        confirmLabel="Queue the tries"
        pendingLabel="Queueing"
        pending={confirm.isPending}
        disabled={!ready || estimate.isError}
        onConfirm={() => {
          if (ready) confirm.mutate();
        }}
      >
        <div aria-live="polite" className="flex flex-col gap-3">
          {estimate.isPending ? <p className="m-0 text-secondary text-muted">Estimating</p> : null}
          {estimate.data ? (
            <p className="m-0 flex items-center gap-1 text-body text-ink">
              <span>{estimateText(estimate.data.estimate)}</span>
              <InfoTip label="How the estimate is priced">
                Each new try is priced at what the run it reruns cost, summed over the items.
              </InfoTip>
            </p>
          ) : null}
          {failure ? <Callout tone="rework">{tryFailure(failure)}</Callout> : null}
        </div>
      </ConfirmDialog>
    </>
  );
}
