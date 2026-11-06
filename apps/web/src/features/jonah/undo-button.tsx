// Undo this change (ADR 0039 decision 15): writes the proposal that reverses a published proposal and opens it
// for a person to approve. Used where a Worse watch shows: After publish on Jonah's Overview and the Needs you
// entry, which belongs to no item and may be about any factory, so the factory comes in by props.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { toast } from 'sonner';
import { ApiError, undoProposal } from '../../api/client';
import { keys } from '../../api/queries';
import { useOrg } from '../../shared/org';
import { Button } from '../../ui/button';

/** What Undo says when it is refused: goald's own sentence for a 409, else a line by its code. */
export function undoFailure(err: unknown): string {
  const code = err instanceof ApiError ? err.code : undefined;
  if (err instanceof ApiError && err.status === 409 && err.problem.detail) return err.problem.detail;
  if (code === 'undo-open') return 'An undo of this change is already waiting or was published.';
  if (code === 'undo-conflict')
    return 'The files changed since, so Jonah cannot reverse this change on its own. Edit them by hand.';
  if (code === 'proposal-not-published') return 'This proposal was never published, so there is nothing to undo.';
  return 'goald is not answering. Try again in a moment.';
}

export function UndoThisChange({
  number,
  factoryId,
  factoryName,
  size = 'sm',
}: {
  number: number;
  factoryId: string;
  factoryName: string;
  size?: 'sm' | 'md';
}) {
  const org = useOrg();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const undo = useMutation({
    mutationFn: () => undoProposal(org.id, factoryId, number),
    onSuccess: async (made) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: keys.jonah(org.id, factoryId) }),
        queryClient.invalidateQueries({ queryKey: keys.inboxes(org.id) }),
      ]);
      await navigate({
        to: '/o/$org/f/$factory/jonah/proposals/$number',
        params: { org: org.slug, factory: factoryName, number: String(made.number) },
      });
    },
    onError: (err) => toast.error(undoFailure(err)),
  });
  return (
    <Button variant="secondary" size={size} busy={undo.isPending} busyLabel="Undoing…" onClick={() => undo.mutate()}>
      Undo this change<span className="sr-only"> of proposal {number}</span>
    </Button>
  );
}
