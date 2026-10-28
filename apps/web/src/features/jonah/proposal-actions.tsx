// The decisions on one proposal (ADR 0017, decisions 7 to 9): Approve publishes it as a new version, Edit
// writes it into the draft and opens the builder, and Reject records the reason Jonah reads next time. Each
// is a mutation that refreshes the Jonah keys and the definition's keys. Each refusal the API gives is said
// in plain words, by the code it carries, never by the message.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { type FormEvent, useState } from 'react';
import { ApiError, approveProposal, editProposal, rejectProposal } from '../../api/client';
import { defKeys, keys } from '../../api/queries';
import { useFactoryParams } from '../../shared/factory';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { Dialog, DialogContent, DialogTrigger } from '../../ui/dialog';
import { Field, Textarea } from '../../ui/field';

/** The longest reason the API takes (ProposalReject). */
export const reasonMaxLength = 2000;

/** The sentence a refused decision shows. The codes are the API's (openapi.yaml, the proposal operations). */
function decisionFailure(err: unknown, codes: Record<string, string>): string {
  const message = err instanceof ApiError && err.code ? codes[err.code] : undefined;
  return message ?? 'goald is not answering. Try again in a moment.';
}

const decidedMessage = 'This proposal was already decided. The page shows its state now.';

/** What Approve says when the publish is refused: an open draft, a stale proposal, or the publish's own gates. */
export function approveFailure(err: unknown): string {
  return decisionFailure(err, {
    'draft-open':
      'The definition already has an open draft. Publish it or discard it, then approve this proposal again.',
    'proposal-stale':
      'The files changed since Jonah read them, so nothing was published. Jonah has to propose the change again.',
    'proposal-decided': decidedMessage,
    'proposal-not-found': 'This proposal no longer exists.',
    'definition-invalid': 'The change does not validate, so it was not published. The draft is open to fix it.',
    'evals-pending': 'Evals are still running for this change. Approve it again once they finish.',
    'evals-failing': 'An eval case fails with this change, so it was not published. The draft is open to look at it.',
  });
}

/** What Edit says when the draft has changed under the proposal, so nothing was written. */
export function editFailure(err: unknown): string {
  return decisionFailure(err, {
    'draft-conflict':
      'The draft changed these files since Jonah read them, so nothing was written. Open the draft to see its changes.',
    'proposal-decided': decidedMessage,
    'proposal-not-found': 'This proposal no longer exists.',
  });
}

/** What Reject says when it is refused. The reason itself is checked before the request is sent. */
export function rejectFailure(err: unknown): string {
  return decisionFailure(err, {
    'proposal-decided': decidedMessage,
    'proposal-not-found': 'This proposal no longer exists.',
    'invalid-request': 'The reason is missing or longer than 2000 characters.',
  });
}

/**
 * The page's three decisions. Each refreshes the Jonah keys and the definition keys it changes; Approve and
 * Edit then open the definition, at its versions (published) or its review (the draft).
 */
export function useProposalDecisions(org: string, factory: string, number: number) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const params = useFactoryParams();

  const refresh = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: keys.jonah(org, factory) }),
      queryClient.invalidateQueries({ queryKey: defKeys.factory(org, factory) }),
      queryClient.invalidateQueries({ queryKey: defKeys.draft(org, factory) }),
      queryClient.invalidateQueries({ queryKey: defKeys.versions(org, factory) }),
    ]);

  const approve = useMutation({
    mutationFn: () => approveProposal(org, factory, number),
    onSuccess: async () => {
      await refresh();
      await navigate({ to: '/o/$org/f/$factory/definition/$section', params: { ...params, section: 'versions' } });
    },
    onError: refresh,
  });

  const edit = useMutation({
    mutationFn: () => editProposal(org, factory, number),
    onSuccess: async () => {
      await refresh();
      await navigate({ to: '/o/$org/f/$factory/definition/$section', params: { ...params, section: 'review' } });
    },
    onError: refresh,
  });

  const reject = useMutation({
    mutationFn: (reason: string) => rejectProposal(org, factory, number, reason),
    onSuccess: refresh,
  });

  return { approve, edit, reject };
}

export type ProposalDecisions = ReturnType<typeof useProposalDecisions>;

/** The reject dialog: a required reason, and Reject stays disabled until one that is not blank is typed. */
export function RejectDialog({
  disabled,
  onReject,
}: {
  disabled: boolean;
  onReject: (reason: string) => Promise<unknown>;
}) {
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState('');
  const [failure, setFailure] = useState<string | null>(null);
  const blank = reason.trim() === '';

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (blank) return;
    setFailure(null);
    try {
      await onReject(reason.trim());
      setOpen(false);
      setReason('');
    } catch (err) {
      setFailure(rejectFailure(err));
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="danger" disabled={disabled}>
          Reject with a reason
        </Button>
      </DialogTrigger>
      <DialogContent
        title="Reject with a reason"
        description="Jonah reads the reason before it proposes again, so it does not propose this change again."
      >
        <form onSubmit={submit} noValidate className="flex flex-col gap-5">
          <Field label="Reason" help="Up to 2000 characters.">
            <Textarea
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              maxLength={reasonMaxLength}
              rows={4}
              required
            />
          </Field>
          <div aria-live="polite">{failure ? <Callout tone="rework">{failure}</Callout> : null}</div>
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="white" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" variant="danger-fill" disabled={blank}>
              Reject proposal
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
