// Human decisions on an item: approve, send back (reject) with a reason, merge the pull request,
// retry a blocked item; and, where the Item page passes its controls, pause, resume and move it
// (E10), which stop a run in progress. Send back, merge, pause and move ask first; approve, retry
// and resume do not, since they only move work forward and the next decision can undo them.
// What the caller may do comes from the factory's `can` (spec 3 §10.2): without item:decide the
// decisions show disabled with who decides ("Approvers of sre decide here."); without
// item:operate pause and move are not offered. goald refuses them anyway.
import { type QueryKey, useMutation, useQueryClient } from '@tanstack/react-query';
import { ArrowRight, Ban, Check, Ellipsis, ExternalLink, GitMerge, Pause, Play, RotateCcw, Undo2 } from 'lucide-react';
import { type FormEvent, useId, useState } from 'react';
import { toast } from 'sonner';
import { ApiError, approveItem, cancelItem, mergeItem, pauseItem, rejectItem, retryItem } from '../api/client';
import { keys } from '../api/queries';
import type { Factory, Item, ItemState, Station } from '../api/types';
import { Button } from '../ui/button';
import { cn } from '../ui/cn';
import { ConfirmDialog } from '../ui/confirm-dialog';
import { Dialog, DialogClose, DialogContent } from '../ui/dialog';
import { Field, Textarea } from '../ui/field';
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../ui/menu';
import { useFactoryById } from './factory';
import { humanize } from './format';
import { factoryWorkInvalidations } from './invalidation';
import { controlsFor, decisionsFor, pullRequestLabel } from './item-state';
import { MoveDialog } from './move-menu';
import { useOrg } from './org';
import { holds, reasons } from './permissions';

export interface DecidableItem {
  /** The id or ref the API accepts. */
  key: string;
  ref: string;
  title: string;
  state: ItemState;
  /** Every pull request a merge lands: the item's outputs', or a card's pull_requests. */
  pull_requests: string[];
}

type Decision =
  | { kind: 'approve' }
  | { kind: 'reject'; reason: string }
  | { kind: 'merge' }
  | { kind: 'retry' }
  | { kind: 'pause'; reason: string };

export function useDecision(item: DecidableItem, factory: string) {
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
      // A merge goald refuses (cannot-merge, no-code-host-connection) is said in the merge dialog.
      if (d.kind === 'merge' && mergeRefusal(err)) return;
      const verb = { approve: 'approve', reject: 'send back', merge: 'merge', retry: 'retry', pause: 'pause' }[d.kind];
      toast.error(`Could not ${verb} ${item.ref}`, { description: err.message });
    },
    onSettled: () =>
      Promise.all(
        itemChangeInvalidations(org.id, factory, item.key).map((queryKey) => client.invalidateQueries({ queryKey })),
      ),
  });
}

/**
 * Approves an item from outside its page (the sidebar's search): the same approve as useDecision, with
 * its toasts and invalidations, for a row that names the item rather than holding it.
 */
export function useApprove() {
  const client = useQueryClient();
  const org = useOrg();
  return useMutation({
    mutationFn: ({ key }: { key: string; ref: string; factory: string }): Promise<Item> => approveItem(org.id, key),
    onSuccess: (updated, { key, ref }) => {
      client.setQueryData(keys.item(org.id, key), updated);
      const where = updated.state === 'done' ? 'It is done.' : `It is now at ${humanize(updated.station)}.`;
      toast.success(`Approved ${ref}. ${where}`);
    },
    onError: (err, { ref }) => toast.error(`Could not approve ${ref}`, { description: err.message }),
    onSettled: (_updated, _err, { key, factory }) =>
      Promise.all(
        itemChangeInvalidations(org.id, factory, key).map((queryKey) => client.invalidateQueries({ queryKey })),
      ),
  });
}

/**
 * Cancels an open item (Cancel item, the item page's). A 409 (the item closed meanwhile) is said in the
 * dialog, so it raises no toast; the other errors do. The invalidations are the decisions' own.
 */
export function useCancelItem(item: DecidableItem, factory: string) {
  const client = useQueryClient();
  const org = useOrg();
  return useMutation({
    mutationFn: (reason: string) => cancelItem(org.id, item.key, reason || undefined),
    onSuccess: () => toast.success(`Cancelled ${item.ref}.`),
    onError: (err) => {
      if (err instanceof ApiError && err.status === 409) return;
      toast.error(`Could not cancel ${item.ref}`, { description: err.message });
    },
    onSettled: () =>
      Promise.all(
        itemChangeInvalidations(org.id, factory, item.key).map((queryKey) => client.invalidateQueries({ queryKey })),
      ),
  });
}

/**
 * The queries a change to one item moves: its factory's work (the board), the organization's
 * attention and inboxes, and the item with its parts (an assembly's pause, retry or cancel changes them).
 */
export function itemChangeInvalidations(org: string, factory: string, item: string): QueryKey[] {
  return [
    ...factoryWorkInvalidations(org, factory),
    keys.attention(org),
    keys.inboxes(org),
    keys.item(org, item),
    keys.parts(org, item),
  ];
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
  /** Move goes into a "More actions" menu beside the other controls (the Item page's top bar). */
  moveInMenu?: boolean;
  /**
   * Phones: `decisions` renders only Approve, Merge and Retry, and `controls` only one "More actions" menu
   * with Pause, Send back and Move. Undefined renders both, as the desktop does.
   */
  split?: 'decisions' | 'controls';
  /**
   * The item's page offers Cancel item (a button, or a menu entry on a phone). It shows only while the
   * item is open and the caller holds item:decide; an assembly's page never passes it.
   */
  cancellable?: boolean;
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
  moveInMenu = false,
  split,
  cancellable = false,
  className,
}: Props) {
  const decide = useDecision(item, factory);
  const cancel = useCancelItem(item, factory);
  const grants = useFactoryById(factory);
  const mayDecide = holds(grants, 'item:decide');
  const mayOperate = holds(grants, 'item:operate');
  const reasonId = useId();
  const can = decisionsFor(item, station);
  const ctl =
    controls && mayOperate ? controlsFor(item, controls.reworkTarget) : { pause: false, sendBack: false, move: false };
  const [open, setOpen] = useState<'reject' | 'merge' | 'pause' | 'move' | null>(null);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [cancelReason, setCancelReason] = useState('');
  const reject = can.reject || ctl.sendBack;
  const decisions = can.approve || reject || can.merge || can.retry;
  const cancelHere = cancellable && mayDecide && item.state !== 'done' && item.state !== 'cancelled';

  const menuHas = (Boolean(controls) && (ctl.pause || reject || ctl.move)) || cancelHere;
  if (
    split === 'controls'
      ? !menuHas
      : split === 'decisions'
        ? !decisions
        : !decisions && !ctl.pause && !ctl.move && !cancelHere
  ) {
    return null;
  }

  // Closing the cancel dialog drops a refusal and the typed reason with it.
  const closeCancel = (next: boolean) => {
    setCancelOpen(next);
    if (!next) {
      cancel.reset();
      setCancelReason('');
    }
  };
  const cancelRefusal =
    cancel.error instanceof ApiError && cancel.error.status === 409
      ? (cancel.error.detail ?? cancel.error.title)
      : undefined;

  const pending = decide.isPending ? decide.variables?.kind : undefined;
  const btn = layout === 'stack' ? 'w-full justify-center' : '';
  const s = layout === 'stack' ? 'lg' : size;
  const backTo = controls?.reworkTarget ? `Send back to ${humanize(controls.reworkTarget)}` : sendBackLabel;
  const resume = controls?.paused ?? false;
  // A decision the caller may not make stays in view, disabled and focusable, saying who decides.
  const decision = (act: () => void) =>
    mayDecide
      ? { disabled: decide.isPending, onClick: act }
      : { 'aria-disabled': true, 'aria-describedby': reasonId, onClick: undefined };

  return (
    <div className={cn('flex gap-2', layout === 'stack' ? 'flex-col' : 'flex-wrap items-center', className)}>
      {split !== 'controls' && can.merge ? (
        <Button variant="primary" size={s} className={btn} {...decision(() => setOpen('merge'))}>
          <GitMerge size={16} strokeWidth={1.75} aria-hidden="true" />
          {pending === 'merge' ? 'Merging…' : 'Merge'}
        </Button>
      ) : null}
      {split !== 'controls' && can.approve ? (
        <Button
          variant="primary"
          size={s}
          className={btn}
          {...decision(() => decide.mutate({ kind: 'approve' }))}
          aria-label={layout === 'row' && size === 'md' ? `Approve ${item.ref}` : undefined}
        >
          <Check size={16} strokeWidth={1.75} aria-hidden="true" />
          {pending === 'approve' ? 'Approving…' : 'Approve'}
        </Button>
      ) : null}
      {split !== 'controls' && can.retry ? (
        <Button variant="primary" size={s} className={btn} {...decision(() => decide.mutate({ kind: 'retry' }))}>
          {resume ? (
            <Play size={16} strokeWidth={1.75} aria-hidden="true" />
          ) : (
            <RotateCcw size={16} strokeWidth={1.75} aria-hidden="true" />
          )}
          {pending === 'retry' ? (resume ? 'Resuming…' : 'Retrying…') : resume ? 'Resume' : 'Retry'}
        </Button>
      ) : null}
      {split === undefined && ctl.pause ? (
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
      {split === undefined && reject ? (
        <Button variant="rework" size={s} className={btn} {...decision(() => setOpen('reject'))}>
          <Undo2 size={16} strokeWidth={1.75} aria-hidden="true" />
          {pending === 'reject' ? 'Sending back…' : backTo}
        </Button>
      ) : null}
      {split === undefined && ctl.move && controls && !moveInMenu ? (
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
      {split === 'controls' && menuHas ? (
        <Menu>
          <MenuTrigger asChild>
            <Button variant="secondary" size="icon" aria-label="More actions" className="size-11">
              <Ellipsis size={16} strokeWidth={1.75} aria-hidden="true" />
            </Button>
          </MenuTrigger>
          {/* An action's dialog opens from the menu: focus goes to it, not back to the trigger. */}
          <MenuContent
            align="end"
            className="w-52"
            onCloseAutoFocus={(event) => {
              if (open !== null || cancelOpen) event.preventDefault();
            }}
          >
            {ctl.pause ? (
              <MenuItem disabled={decide.isPending} onSelect={() => setOpen('pause')}>
                <Pause size={16} strokeWidth={1.75} aria-hidden="true" />
                Pause
              </MenuItem>
            ) : null}
            {reject ? (
              <MenuItem disabled={decide.isPending} onSelect={() => setOpen('reject')}>
                <Undo2 size={16} strokeWidth={1.75} aria-hidden="true" />
                {backTo}
              </MenuItem>
            ) : null}
            {ctl.move ? (
              <MenuItem disabled={decide.isPending} onSelect={() => setOpen('move')}>
                <ArrowRight size={16} strokeWidth={1.75} aria-hidden="true" />
                Move
              </MenuItem>
            ) : null}
            {cancelHere ? (
              <MenuItem disabled={cancel.isPending} className="text-rework" onSelect={() => setCancelOpen(true)}>
                <Ban size={16} strokeWidth={1.75} aria-hidden="true" />
                Cancel item
              </MenuItem>
            ) : null}
          </MenuContent>
        </Menu>
      ) : null}
      {split === undefined && ctl.move && controls && moveInMenu ? (
        <Menu>
          <MenuTrigger asChild>
            <Button variant="secondary" size="icon" aria-label="More actions">
              <Ellipsis size={16} strokeWidth={1.75} aria-hidden="true" />
            </Button>
          </MenuTrigger>
          {/* Move's dialog opens from the menu: focus goes to it, not back to the trigger. */}
          <MenuContent
            align="end"
            className="w-52"
            onCloseAutoFocus={(event) => {
              if (open === 'move') event.preventDefault();
            }}
          >
            <MenuItem onSelect={() => setOpen('move')}>
              <ArrowRight size={16} strokeWidth={1.75} aria-hidden="true" />
              Move
            </MenuItem>
          </MenuContent>
        </Menu>
      ) : null}
      {split === undefined && cancelHere ? (
        <Button variant="rework" disabled={cancel.isPending} onClick={() => setCancelOpen(true)}>
          <Ban size={16} strokeWidth={1.75} aria-hidden="true" />
          {cancel.isPending ? 'Cancelling…' : 'Cancel item'}
        </Button>
      ) : null}
      {split !== 'controls' && decisions && !mayDecide ? (
        <p id={reasonId} className={cn('m-0 text-sm text-muted', layout === 'row' && 'basis-full')}>
          {reasons.decide(grants?.name ?? 'this factory')}
        </p>
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
          items={[{ ...item, id: controls.id, station: controls.station }]}
          factory={controls.factory}
          open={open === 'move'}
          onOpenChange={(o) => setOpen(o ? 'move' : null)}
        />
      ) : null}
      {can.merge ? (
        <MergeDialog
          item={item}
          open={open === 'merge'}
          onOpenChange={(o) => {
            setOpen(o ? 'merge' : null);
            // A refusal said in the dialog goes with it.
            if (!o) decide.reset();
          }}
          onConfirm={() => decide.mutate({ kind: 'merge' }, { onSuccess: () => setOpen(null) })}
          pending={pending === 'merge'}
          refusal={decide.variables?.kind === 'merge' ? mergeRefusal(decide.error) : undefined}
        />
      ) : null}
      {cancelHere ? (
        <CancelItemDialog
          itemRef={item.ref}
          open={cancelOpen}
          onOpenChange={closeCancel}
          reason={cancelReason}
          onReasonChange={setCancelReason}
          onConfirm={() => cancel.mutate(cancelReason, { onSuccess: () => closeCancel(false) })}
          pending={cancel.isPending}
          refusal={cancelRefusal}
        />
      ) : null}
    </div>
  );
}

interface CancelItemDialogProps {
  itemRef: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  reason: string;
  onReasonChange: (reason: string) => void;
  onConfirm: () => void;
  pending: boolean;
  /** goald's refusal (409), said inline; the dialog stays open. */
  refusal: string | undefined;
}

/** Cancel item: says the pull request stays open, takes an optional reason and confirms in red. */
function CancelItemDialog({
  itemRef,
  open,
  onOpenChange,
  reason,
  onReasonChange,
  onConfirm,
  pending,
  refusal,
}: CancelItemDialogProps) {
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Cancel ${itemRef}?`}
      description="The factory stops working on it and its pull request stays open. You can't reopen a cancelled item."
      confirmLabel="Cancel item"
      pendingLabel="Cancelling…"
      tone="danger"
      pending={pending}
      onConfirm={onConfirm}
    >
      <Field label="Reason" help="Optional.">
        <Textarea rows={3} value={reason} onChange={(e) => onReasonChange(e.target.value)} />
      </Field>
      {refusal ? (
        <p
          role="alert"
          className="m-0 rounded-control border border-rework-tint bg-rework-tint px-3 py-2 text-sm leading-snug text-rework-ink"
        >
          {refusal}
        </p>
      ) : null}
    </ConfirmDialog>
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

export function RejectDialog({
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
          <div className="flex flex-col gap-2">
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
              className="min-h-28 w-full resize-y rounded-control border border-rule bg-card px-3 py-3 text-base text-ink placeholder:text-muted aria-[invalid=true]:border-rework"
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
          <div className="flex flex-col gap-2">
            <label htmlFor={id} className="text-sm text-muted">
              Why (optional)
            </label>
            <textarea
              id={id}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={3}
              maxLength={500}
              className="min-h-20 w-full resize-y rounded-control border border-rule bg-card px-3 py-3 text-base text-ink"
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

/**
 * Why goald refused a merge, in the dialog's words (spec 4 §4.7): a pull request it could not
 * merge (409 cannot-merge, naming it; the ones before it stay merged and are skipped next time) or
 * a host no connection of the factory serves (409 no-code-host-connection); undefined otherwise.
 */
export function mergeRefusal(err: unknown): string | undefined {
  if (!(err instanceof ApiError) || err.status !== 409) return undefined;
  const detail = err.detail ?? err.title;
  if (err.code === 'cannot-merge') {
    return `goald stopped at a pull request it could not merge: ${detail}. Pull requests merged before it stay merged; fix this one and merge again.`;
  }
  if (err.code === 'no-code-host-connection') {
    return `No connection of this factory signs in to that host, so nothing was merged: ${detail}. Add the host’s connection to the factory’s Connections, then merge again.`;
  }
  return undefined;
}

export function MergeDialog({
  item,
  open,
  onOpenChange,
  onConfirm,
  pending,
  refusal,
}: DialogProps<void> & { refusal: string | undefined }) {
  const prs = item.pull_requests;
  const one = prs.length === 1 ? prs[0] : undefined;
  const what = one ? pullRequestLabel(one) : prs.length > 1 ? `${prs.length} pull requests` : 'the pull request';
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={`Merge ${what}?`}
        description={`goald squash-merges ${prs.length > 1 ? 'every pull request' : 'the pull request'} of ${item.ref}, deletes ${prs.length > 1 ? 'their branches' : 'its branch'} and approves the item. It refuses a pull request with conflicts or failing checks, and then the item stays here.`}
      >
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            onConfirm();
          }}
        >
          <p className="m-0 text-body leading-snug">{item.title}</p>
          {prs.length > 0 ? (
            <ul className="m-0 flex list-none flex-col gap-1 p-0">
              {prs.map((pr) => (
                <li key={pr}>
                  <a
                    href={pr}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-2 text-sm hover:text-run"
                  >
                    Review {pullRequestLabel(pr, prs.length > 1)} first
                    <ExternalLink size={14} strokeWidth={1.5} aria-hidden="true" />
                    <span className="sr-only">(opens in a new tab)</span>
                  </a>
                </li>
              ))}
            </ul>
          ) : null}
          {refusal ? (
            <p
              role="alert"
              className="m-0 rounded-control border border-rework-tint bg-rework-tint px-3 py-3 text-sm leading-snug text-rework-ink"
            >
              {refusal}
            </p>
          ) : null}
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={pending}>
              <GitMerge size={16} strokeWidth={1.75} aria-hidden="true" />
              {pending ? 'Merging…' : refusal ? 'Merge again' : 'Squash and merge'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
