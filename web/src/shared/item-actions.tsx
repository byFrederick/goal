// Human decisions on an item: approve, send back (reject) with a reason, merge the pull request,
// retry a blocked item; and, where the Item page passes its controls, pause, resume and move it
// (E10), which stop a run in progress. Send back, merge, pause and move ask first; approve, retry
// and resume do not, since they only move work forward and the next decision can undo them.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { ArrowRight, Check, ExternalLink, GitMerge, Pause, Play, RotateCcw, Undo2 } from 'lucide-react';
import { type FormEvent, useId, useState } from 'react';
import { toast } from 'sonner';
import { approveItem, mergeItem, pauseItem, rejectItem, retryItem } from '../api/client';
import { keys } from '../api/queries';
import type { Factory, Item, ItemState, Station } from '../api/types';
import { Button } from '../ui/button';
import { cn } from '../ui/cn';
import { Dialog, DialogClose, DialogContent } from '../ui/dialog';
import { humanize } from './format';
import { factoryWorkInvalidations } from './invalidation';
import { controlsFor, decisionsFor, pullRequestLabel } from './item-state';
import { MoveDialog } from './move-menu';
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

type Decision =
  | { kind: 'approve' }
  | { kind: 'reject'; reason: string }
  | { kind: 'merge' }
  | { kind: 'retry' }
  | { kind: 'pause'; reason: string };

function useDecision(item: DecidableItem, factory: string) {
  const client = useQueryClient();
  const org = useOrg();
  return useMutation({
    // goald records the request's actor (the local user) as the decider.
    mutationFn: (d: Decision): Promise<Item> => {
      switch (d.kind) {
        case 'approve':
          return approveItem(org.id, item.key);
        case 'reject':
          return rejectItem(org.id, item.key, d.reason);
        case 'merge':
          return mergeItem(org.id, item.key);
        case 'retry':
          return retryItem(org.id, item.key);
        case 'pause':
          return pauseItem(org.id, item.key, d.reason || undefined);
      }
    },
    onSuccess: (updated, d) => {
      client.setQueryData(keys.item(org.id, item.key), updated);
      const where = updated.state === 'done' ? 'It is done.' : `It is now at ${humanize(updated.station)}.`;
      const done = {
        approve: `Approved ${item.ref}.`,
        reject: `Sent ${item.ref} back.`,
        merge: `Merged ${item.ref}.`,
        retry: `${item.ref} is back in the queue.`,
        pause: `Paused ${item.ref} at ${humanize(updated.station)}.`,
      }[d.kind];
      toast.success(`${done} ${d.kind === 'retry' || d.kind === 'pause' ? '' : where}`.trim());
    },
    onError: (err, d) => {
      const verb = { approve: 'approve', reject: 'send back', merge: 'merge', retry: 'retry', pause: 'pause' }[d.kind];
      toast.error(`Could not ${verb} ${item.ref}`, { description: err.message });
    },
    onSettled: () =>
      Promise.all([
        ...factoryWorkInvalidations(org.id, factory).map((queryKey) => client.invalidateQueries({ queryKey })),
        client.invalidateQueries({ queryKey: keys.attention(org.id) }),
        client.invalidateQueries({ queryKey: keys.item(org.id, item.key) }),
      ]),
  });
}

/** What the Item page knows to offer pause, resume, move and send back mid-run. */
export interface ItemControls {
  /** The item's id and station, for the Move menu and the board. */
  id: string;
  station: string;
  /** Where a send-back goes from the item's station (Item.flow.rework_target). */
  reworkTarget: string | null;
  /** A person paused it: its retry is Resume. */
  paused: boolean;
  /** Its factory, for the Move menu's stations. */
  factory: Factory;
}

interface Props {
  item: DecidableItem;
  /** The factory's id. */
  factory: string;
  /** Pause, resume, move and send back at any open state (the Item page). */
  controls?: ItemControls | undefined;
  /** The item's station: its kind and action decide what can be done. */
  station: Pick<Station, 'kind' | 'action'> | undefined;
  /** `stack` fills the width with 44px targets, for phones and narrow cards. */
  layout?: 'row' | 'stack';
  size?: 'md' | 'lg';
  /** The send back button's words where there are no controls ("Send back" by default). */
  sendBackLabel?: string;
  className?: string;
}

/** The decisions available on an item, or nothing when there are none. */
export function ItemDecisions({
  item,
  factory,
  station,
  controls,
  layout = 'row',
  size = 'md',
  sendBackLabel = 'Send back',
  className,
}: Props) {
  const decide = useDecision(item, factory);
  const can = decisionsFor(item, station);
  const ctl = controls ? controlsFor(item, controls.reworkTarget) : { pause: false, sendBack: false, move: false };
  const [open, setOpen] = useState<'reject' | 'merge' | 'pause' | 'move' | null>(null);
  const reject = can.reject || ctl.sendBack;

  if (!can.approve && !reject && !can.merge && !can.retry && !ctl.pause && !ctl.move) return null;

  const pending = decide.isPending ? decide.variables?.kind : undefined;
  const btn = layout === 'stack' ? 'w-full justify-center' : '';
  const s = layout === 'stack' ? 'lg' : size;
  const backTo = controls?.reworkTarget ? `Send back to ${humanize(controls.reworkTarget)}` : sendBackLabel;
  const resume = controls?.paused ?? false;

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
      {can.retry ? (
        <Button
          variant="primary"
          size={s}
          className={btn}
          disabled={decide.isPending}
          onClick={() => decide.mutate({ kind: 'retry' })}
        >
          {resume ? (
            <Play size={16} strokeWidth={1.75} aria-hidden="true" />
          ) : (
            <RotateCcw size={16} strokeWidth={1.75} aria-hidden="true" />
          )}
          {pending === 'retry' ? (resume ? 'Resuming…' : 'Retrying…') : resume ? 'Resume' : 'Retry'}
        </Button>
      ) : null}
      {ctl.pause ? (
        <Button
          variant="secondary"
          size={s}
          className={btn}
          disabled={decide.isPending}
          onClick={() => setOpen('pause')}
        >
          <Pause size={16} strokeWidth={1.75} aria-hidden="true" />
          {pending === 'pause' ? 'Pausing…' : 'Pause'}
        </Button>
      ) : null}
      {reject ? (
        <Button variant="rework" size={s} className={btn} disabled={decide.isPending} onClick={() => setOpen('reject')}>
          <Undo2 size={16} strokeWidth={1.75} aria-hidden="true" />
          {pending === 'reject' ? 'Sending back…' : backTo}
        </Button>
      ) : null}
      {ctl.move && controls ? (
        <Button
          variant="secondary"
          size={s}
          className={btn}
          disabled={decide.isPending}
          onClick={() => setOpen('move')}
        >
          <ArrowRight size={16} strokeWidth={1.75} aria-hidden="true" />
          Move
        </Button>
      ) : null}

      <RejectDialog
        item={item}
        to={controls?.reworkTarget ?? null}
        open={open === 'reject'}
        onOpenChange={(o) => setOpen(o ? 'reject' : null)}
        onConfirm={(reason) => decide.mutate({ kind: 'reject', reason }, { onSuccess: () => setOpen(null) })}
        pending={pending === 'reject'}
      />
      {ctl.pause ? (
        <PauseDialog
          item={item}
          open={open === 'pause'}
          onOpenChange={(o) => setOpen(o ? 'pause' : null)}
          onConfirm={(reason) => decide.mutate({ kind: 'pause', reason }, { onSuccess: () => setOpen(null) })}
          pending={pending === 'pause'}
        />
      ) : null}
      {ctl.move && controls ? (
        <MoveDialog
          item={{ ...item, id: controls.id, station: controls.station }}
          factory={controls.factory}
          open={open === 'move'}
          onOpenChange={(o) => setOpen(o ? 'move' : null)}
        />
      ) : null}
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

/** What stopping a run means, for the dialogs of the controls that do it. */
function runStops(item: DecidableItem): string {
  return item.state === 'in_progress' ? ' The agent working on it now stops.' : '';
}

function RejectDialog({
  item,
  to,
  open,
  onOpenChange,
  onConfirm,
  pending,
}: DialogProps<string> & { to: string | null }) {
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
        title={to ? `Send ${item.ref} back to ${humanize(to)}` : `Send ${item.ref} back`}
        description={`It goes back to the agent for rework.${runStops(item)} Say what must change; the agent reads your reason first.`}
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

function PauseDialog({ item, open, onOpenChange, onConfirm, pending }: DialogProps<string>) {
  const id = useId();
  const [reason, setReason] = useState('');

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={`Pause ${item.ref}?`}
        description={`It stays where it is until you resume it; no agent picks it up meanwhile.${runStops(item)}`}
      >
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            onConfirm(reason.trim());
          }}
        >
          <div className="flex flex-col gap-1.5">
            <label htmlFor={id} className="text-sm text-muted">
              Why (optional)
            </label>
            <textarea
              id={id}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={3}
              maxLength={500}
              className="min-h-20 w-full resize-y rounded-control border border-rule bg-surface px-3 py-2.5 text-base text-ink"
            />
          </div>
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={pending}>
              <Pause size={16} strokeWidth={1.75} aria-hidden="true" />
              {pending ? 'Pausing…' : 'Pause'}
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
