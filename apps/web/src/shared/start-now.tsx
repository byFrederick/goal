// "Start now" (ADR 0040 decision 4): clears an item's start time so the first station may release it on its
// next tick. The label swaps to a check once goald answered (MOTION.md, Approve, publish, merge succeed), and
// `onStarted` runs after the hold, so the row that holds the button can leave then. Used by Intake's Coming
// up and by the item page's start banner.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError, clearItemStart } from '../api/client';
import { Button } from '../ui/button';
import { SuccessLabel, successHoldMs } from '../ui/success-label';
import { factoryWorkInvalidations } from './invalidation';
import { useOrg } from './org';

/** The mutation behind Start now: `done` once goald answered, and `onStarted` after the hold. */
export function useStartNow(itemId: string, factoryId: string, onStarted?: () => void) {
  const org = useOrg();
  const client = useQueryClient();
  const [done, setDone] = useState(false);
  const start = useMutation({
    mutationFn: () => clearItemStart(org.id, itemId),
    onSuccess: () => {
      setDone(true);
      setTimeout(() => {
        for (const queryKey of factoryWorkInvalidations(org.id, factoryId)) void client.invalidateQueries({ queryKey });
        void client.invalidateQueries({ queryKey: ['org', org.id, 'item'] });
        onStarted?.();
      }, successHoldMs);
    },
    onError: (err) =>
      toast.error('Could not start the item', { description: err instanceof ApiError ? err.message : '' }),
  });
  return { done, pending: start.isPending, start: () => start.mutate() };
}

export function StartNowButton({
  itemId,
  factoryId,
  label,
  onStarted,
  className,
}: {
  itemId: string;
  factoryId: string;
  /** What the button's name says about the item it starts. */
  label: string;
  onStarted?: () => void;
  className?: string;
}) {
  const { done, pending, start } = useStartNow(itemId, factoryId, onStarted);
  return (
    <Button
      variant="secondary"
      size="sm"
      className={className}
      aria-label={done ? undefined : `Start now: ${label}`}
      disabled={done}
      busy={pending}
      busyLabel="Starting…"
      onClick={start}
    >
      <SuccessLabel done={done} doneLabel="Started">
        Start now
      </SuccessLabel>
    </Button>
  );
}
