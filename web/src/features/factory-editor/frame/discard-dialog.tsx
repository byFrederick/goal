// "Discard draft…" (D §10.3): asks first, then deletes the draft at the session's revision. The
// frame then shows the running version; the first edit opens a new draft. A 412 (someone saved
// since) keeps the draft and says so; the draft's queries are read again to show what changed.
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../../api/client';
import { defKeys } from '../../../api/queries';
import { ConfirmDialog } from '../../../ui/confirm-dialog';
import { useDraftSession } from '../draft/use-draft';

interface DiscardDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The number the draft would publish as ("v9"). */
  next: number;
  /** The version that runs, which keeps running; null before the first publish. */
  current: number | null;
  /** "3 changes in 2 sections"; null without changes. */
  changes: string | null;
  changeCount: number;
}

function why(err: unknown): string {
  if (err instanceof ApiError && err.status === 412) {
    return 'The draft changed since you opened this. Its newest changes are shown now; discard again to drop them.';
  }
  if (err instanceof ApiError) return `Not discarded: ${err.detail ?? err.title}`;
  return 'Not discarded: goald can’t be reached. Try again once it answers.';
}

export function DiscardDialog({ open, onOpenChange, next, current, changes, changeCount }: DiscardDialogProps) {
  const session = useDraftSession();
  const client = useQueryClient();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = () =>
    Promise.all([
      client.invalidateQueries({ queryKey: defKeys.draft(session.org, session.factory) }),
      client.invalidateQueries({ queryKey: defKeys.factory(session.org, session.factory) }),
      client.invalidateQueries({ queryKey: defKeys.factories(session.org) }),
    ]);

  const discard = async () => {
    setPending(true);
    setError(null);
    try {
      await session.discard();
      onOpenChange(false);
      toast.success('Draft discarded');
    } catch (err) {
      setError(why(err));
    } finally {
      setPending(false);
      await refresh();
    }
  };

  const lost = changes
    ? `${changes} ${changeCount === 1 ? 'is' : 'are'} lost.`
    : 'Nothing in it differs from the version it is based on.';
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={(o) => {
        if (!o) setError(null);
        onOpenChange(o);
      }}
      title={`Discard the draft of v${next}?`}
      description={`${lost} ${current !== null ? `v${current} keeps running.` : ''}`.trim()}
      confirmLabel="Discard draft"
      pendingLabel="Discarding…"
      tone="danger"
      pending={pending}
      onConfirm={() => void discard()}
    >
      {error ? (
        <p role="alert" className="m-0 rounded-control border border-rework-line bg-rework-tint px-3 py-2.5 text-sm">
          {error}
        </p>
      ) : null}
    </ConfirmDialog>
  );
}
