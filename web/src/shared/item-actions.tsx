// Human decisions on an item: approve, send back (reject) with a reason, merge the pull request,
// retry a blocked item. Send back and merge ask for confirmation; approve and retry do not, since
// both only move work forward and can be undone by the next decision.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Check, ExternalLink, GitMerge, RotateCcw, Undo2 } from 'lucide-react';
import { type FormEvent, useId, useState } from 'react';
import { toast } from 'sonner';
import { approveItem, mergeItem, rejectItem, retryItem } from '../api/client';
import { keys } from '../api/queries';
import type { Item, ItemState, Station } from '../api/types';
import { Button } from '../ui/button';
import { cn } from '../ui/cn';
import { Dialog, DialogClose, DialogContent } from '../ui/dialog';
import { humanize } from './format';
import { decisionsFor, pullRequestLabel } from './item-state';
import { useOrg } from './org';

export interface DecidableItem {
  /** The id or ref the API accepts. */
  key: string;
  ref: string;
  title: string;
  state: ItemState;
  /** The pull request a merge would land, when known. */
  pr_url?: string | null | undefined;
}

type Decision = { kind: 'approve' } | { kind: 'reject'; reason: string } | { kind: 'merge' } | { kind: 'retry' };

function useDecision(item: DecidableItem, factory: string) {
  const client = useQueryClient();
  const org = useOrg();
  return useMutation({
    // goald records the request's actor (the local user) as the decider.
    mutationFn: (d: Decision): Promise<Item> => {
      switch (d.kind) {
        case 'approve':
          return approveItem(org, item.key);
        case 'reject':
          return rejectItem(org, item.key, d.reason);
        case 'merge':
          return mergeItem(org, item.key);
        case 'retry':
          return retryItem(org, item.key);
      }
    },
    onSuccess: (updated, d) => {
      client.setQueryData(keys.item(org, item.key), updated);
      const where = updated.state === 'done' ? 'It is done.' : `It is now at ${humanize(updated.station)}.`;
      const done = {
        approve: `Approved ${item.ref}.`,
        reject: `Sent ${item.ref} back.`,
        merge: `Merged ${item.ref}.`,
        retry: `${item.ref} is back in the queue.`,
      }[d.kind];
      toast.success(`${done} ${d.kind === 'retry' ? '' : where}`.trim());
    },
    onError: (err, d) => {
      const verb = { approve: 'approve', reject: 'send back', merge: 'merge', retry: 'retry' }[d.kind];
      toast.error(`Could not ${verb} ${item.ref}`, { description: err.message });
    },
    onSettled: () =>
      Promise.all([
        client.invalidateQueries({ queryKey: keys.factory(org, factory) }),
        client.invalidateQueries({ queryKey: keys.item(org, item.key) }),
      ]),
  });
}

interface Props {
  item: DecidableItem;
  factory: string;
  /** The item's station: its kind and action decide what can be done. */
  station: Pick<Station, 'kind' | 'action'> | undefined;
  /** `stack` fills the width with 44px targets, for phones and narrow cards. */
  layout?: 'row' | 'stack';
  size?: 'md' | 'lg';
  className?: string;
}

/** The decisions available on an item, or nothing when there are none. */
export function ItemDecisions({ item, factory, station, layout = 'row', size = 'md', className }: Props) {
  const decide = useDecision(item, factory);
  const can = decisionsFor(item, station);
  const [open, setOpen] = useState<'reject' | 'merge' | null>(null);

  if (!can.approve && !can.reject && !can.merge && !can.retry) return null;

  const pending = decide.isPending ? decide.variables?.kind : undefined;
  const btn = layout === 'stack' ? 'w-full justify-center' : '';
  const s = layout === 'stack' ? 'lg' : size;

  return (
    <div className={cn('flex gap-2', layout === 'stack' ? 'flex-col' : 'flex-wrap items-center', className)}>
      {can.merge ? (
        <Button variant="primary" size={s} className={btn} disabled={decide.isPending} onClick={() => setOpen('merge')}>
          <GitMerge size={16} strokeWidth={1.75} aria-hidden="true" />
          {pending === 'merge' ? 'Merging…' : 'Merge'}
        </Button>
      ) : null}
      {can.approve ? (
        <Button
          variant="primary"
          size={s}
          className={btn}
          disabled={decide.isPending}
          onClick={() => decide.mutate({ kind: 'approve' })}
          aria-label={layout === 'row' && size === 'md' ? `Approve ${item.ref}` : undefined}
        >
          <Check size={16} strokeWidth={1.75} aria-hidden="true" />
          {pending === 'approve' ? 'Approving…' : 'Approve'}
        </Button>
      ) : null}
      {can.reject ? (
        <Button variant="rework" size={s} className={btn} disabled={decide.isPending} onClick={() => setOpen('reject')}>
          <Undo2 size={16} strokeWidth={1.75} aria-hidden="true" />
          {pending === 'reject' ? 'Sending back…' : 'Send back'}
        </Button>
      ) : null}
      {can.retry ? (
        <Button
          variant="primary"
          size={s}
          className={btn}
          disabled={decide.isPending}
          onClick={() => decide.mutate({ kind: 'retry' })}
        >
          <RotateCcw size={16} strokeWidth={1.75} aria-hidden="true" />
          {pending === 'retry' ? 'Retrying…' : 'Retry'}
        </Button>
      ) : null}

      <RejectDialog
        item={item}
        open={open === 'reject'}
        onOpenChange={(o) => setOpen(o ? 'reject' : null)}
        onConfirm={(reason) => decide.mutate({ kind: 'reject', reason }, { onSuccess: () => setOpen(null) })}
        pending={pending === 'reject'}
      />
      {can.merge ? (
        <MergeDialog
          item={item}
          open={open === 'merge'}
          onOpenChange={(o) => setOpen(o ? 'merge' : null)}
          onConfirm={() => decide.mutate({ kind: 'merge' }, { onSuccess: () => setOpen(null) })}
          pending={pending === 'merge'}
        />
      ) : null}
    </div>
  );
}

interface DialogProps<T> {
  item: DecidableItem;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (value: T) => void;
  pending: boolean;
}

function RejectDialog({ item, open, onOpenChange, onConfirm, pending }: DialogProps<string>) {
  const id = useId();
  const [reason, setReason] = useState('');
  const [touched, setTouched] = useState(false);
  const missing = reason.trim() === '';

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!missing) onConfirm(reason.trim());
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        onOpenChange(o);
        if (!o) setTouched(false);
      }}
    >
      <DialogContent
        title={`Send ${item.ref} back`}
        description="It goes back to the agent for rework. Say what must change; the agent reads your reason first."
      >
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          <div className="flex flex-col gap-1.5">
            <label htmlFor={id} className="text-sm text-muted">
              What must change
            </label>
            <textarea
              id={id}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={4}
              required
              aria-invalid={touched && missing}
              aria-describedby={touched && missing ? `${id}-error` : undefined}
              placeholder="The spec misses the empty-bucket case; cover it with a test."
              className="min-h-28 w-full resize-y rounded-control border border-rule bg-surface px-3 py-2.5 text-base text-ink placeholder:text-muted aria-[invalid=true]:border-rework"
            />
            {touched && missing ? (
              <p id={`${id}-error`} className="m-0 text-sm text-rework">
                Write a reason so the agent knows what to fix.
              </p>
            ) : null}
          </div>
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={pending}>
              {pending ? 'Sending back…' : 'Send back'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function MergeDialog({ item, open, onOpenChange, onConfirm, pending }: DialogProps<void>) {
  const pr = item.pr_url ? pullRequestLabel(item.pr_url) : 'the pull request';
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={`Merge ${pr}?`}
        description={`goald squash-merges the pull request of ${item.ref}, deletes its branch and approves the item. It refuses a pull request with conflicts or failing checks, and then nothing changes.`}
      >
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            onConfirm();
          }}
        >
          <p className="m-0 text-[15px] leading-snug">{item.title}</p>
          {item.pr_url ? (
            <a
              href={item.pr_url}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1.5 self-start text-sm hover:text-run"
            >
              Review {pr} first
              <ExternalLink size={14} strokeWidth={1.5} aria-hidden="true" />
              <span className="sr-only">(opens in a new tab)</span>
            </a>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={pending}>
              <GitMerge size={16} strokeWidth={1.75} aria-hidden="true" />
              {pending ? 'Merging…' : 'Squash and merge'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
