// The andon cord of a factory's line (E §4.4, Board): whether the line runs, and the button that
// stops it (with a reason) or restarts it (with an optional note). While the cord is pulled goald
// releases and claims nothing on the line; runs in progress finish and people still decide. Both
// read the line from the board (Board.line.status and .andon). Pulling and clearing need
// andon:manage (approvers and editors); without it the button stays in view, disabled, with who
// may pull it beside it (canvas Board-Viewer).
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Hand, Play } from 'lucide-react';
import { type FormEvent, type ReactNode, useId, useState } from 'react';
import { toast } from 'sonner';
import { clearAndon, pullAndon } from '../api/client';
import { keys } from '../api/queries';
import type { AndonPull, Board, Factory } from '../api/types';
import { Button } from '../ui/button';
import { cn } from '../ui/cn';
import { Dialog, DialogClose, DialogContent } from '../ui/dialog';
import { factoryTitle } from './factory';
import { factoryWorkInvalidations } from './invalidation';
import { useOrg } from './org';
import { holds, reasons } from './permissions';
import { useStreamStatus } from './stream';

/** The longest reason goald takes, in characters. */
const maxReason = 500;
/** The longest clearing note goald takes, in characters. */
const maxNote = 2000;

/**
 * "Line running" or "Line stopped: <reason>", with a dot that is never the only signal; while the
 * live stream is not connected it says so, since the state shown may be behind. One line: a long
 * reason is cut, with the full text in `title` (the reason by default).
 */
export function LineStatus({
  andon,
  suffix,
  title,
  className,
}: {
  /** The line's open pull, or null while it runs. */
  andon: AndonPull | null;
  /** After the status, on the same line when it fits: ", WIP 1 of 2" (keep a phrase whole with a nowrap span). */
  suffix?: ReactNode;
  /** The full text of the line, for a title attribute; the reason when the line is stopped. */
  title?: string;
  className?: string;
}) {
  const stream = useStreamStatus();
  const stale = { connecting: ', connecting…', live: '', reconnecting: ', reconnecting…' }[stream];

  return (
    <p role="status" className={cn('m-0 flex min-w-0 items-center gap-2', className)}>
      <span aria-hidden="true" className={cn('size-2 shrink-0 rounded-full', andon ? 'bg-rework' : 'bg-ok-dot')} />
      <span className={cn('min-w-0 truncate', andon && 'text-rework-ink')} title={title ?? andon?.reason}>
        {andon ? `Line stopped: ${andon.reason}` : 'Line running'}
        {suffix}
        {stale}
      </span>
    </p>
  );
}

type Action = { kind: 'pull'; reason: string } | { kind: 'clear'; note: string };

function useAndon(factory: Factory) {
  const client = useQueryClient();
  const org = useOrg();
  return useMutation({
    mutationFn: (a: Action) =>
      a.kind === 'pull' ? pullAndon(org.id, factory.id, a.reason) : clearAndon(org.id, factory.id, a.note),
    onSuccess: (pull, a) => {
      // The pull answers what the line is now; the stream's factory event refreshes the rest.
      const open = pull.cleared_at ? null : pull;
      client.setQueryData<Board>(keys.board(org.id, factory.id), (b) =>
        b ? { ...b, line: { ...b.line, status: open ? 'stopped' : 'running', andon: open } } : b,
      );
      toast.success(
        a.kind === 'pull'
          ? `Andon pulled. ${factoryTitle(factory)} takes no new work until it is cleared.`
          : `Andon cleared. ${factoryTitle(factory)} runs again.`,
      );
    },
    onError: (err, a) =>
      toast.error(a.kind === 'pull' ? 'Could not pull the andon cord' : 'Could not clear the andon', {
        description: err.message,
      }),
    onSettled: () =>
      Promise.all([
        ...factoryWorkInvalidations(org.id, factory.id).map((queryKey) => client.invalidateQueries({ queryKey })),
        client.invalidateQueries({ queryKey: keys.attention(org.id) }),
        client.invalidateQueries({ queryKey: keys.overviews(org.id) }),
      ]),
  });
}

/** "Pull the andon cord" while the line runs, "Clear the andon" while it is stopped; each opens its dialog. */
export function AndonButton({
  factory,
  andon,
  className,
}: {
  factory: Factory;
  /** The line's open pull, or null while it runs. */
  andon: AndonPull | null;
  className?: string;
}) {
  const act = useAndon(factory);
  const [dialog, setDialog] = useState(false);
  const reasonId = useId();
  const label = (
    <>
      {andon ? (
        <Play size={16} strokeWidth={1.75} aria-hidden="true" />
      ) : (
        <Hand size={16} strokeWidth={1.75} aria-hidden="true" />
      )}
      {andon ? 'Clear the andon' : 'Pull the andon cord'}
    </>
  );

  if (!holds(factory, 'andon:manage')) {
    // In view and focusable, so the reason is heard as well as seen; it does nothing.
    return (
      <span className={cn('inline-flex items-center gap-3', className)}>
        <span id={reasonId} className="max-w-[150px] text-[13px] leading-tight text-muted md:text-right">
          {reasons.andon(andon !== null)}
        </span>
        <Button variant={andon ? 'secondary' : 'rework'} aria-disabled="true" aria-describedby={reasonId}>
          {label}
        </Button>
      </span>
    );
  }

  return (
    <>
      <Button variant={andon ? 'secondary' : 'rework'} className={className} onClick={() => setDialog(true)}>
        {label}
      </Button>
      {andon ? (
        <ClearDialog
          factory={factory}
          pull={andon}
          open={dialog}
          onOpenChange={setDialog}
          pending={act.isPending}
          onConfirm={(note) => act.mutate({ kind: 'clear', note }, { onSuccess: () => setDialog(false) })}
        />
      ) : (
        <PullDialog
          factory={factory}
          open={dialog}
          onOpenChange={setDialog}
          pending={act.isPending}
          onConfirm={(reason) => act.mutate({ kind: 'pull', reason }, { onSuccess: () => setDialog(false) })}
        />
      )}
    </>
  );
}

interface DialogProps {
  factory: Factory;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (text: string) => void;
  pending: boolean;
}

const textarea =
  'min-h-28 w-full resize-y rounded-control border border-rule bg-surface px-3 py-2.5 text-base text-ink placeholder:text-muted aria-[invalid=true]:border-rework';

function PullDialog({ factory, open, onOpenChange, onConfirm, pending }: DialogProps) {
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
        title="Pull the andon cord"
        description={`${factoryTitle(factory)} stops taking new work: nothing is released or claimed until someone clears the andon. Runs in progress finish, and decisions waiting on people still go ahead.`}
      >
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          <div className="flex flex-col gap-1.5">
            <label htmlFor={id} className="text-sm text-muted">
              Why the line must stop
            </label>
            <textarea
              id={id}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={3}
              required
              maxLength={maxReason}
              aria-invalid={touched && missing}
              aria-describedby={touched && missing ? `${id}-error` : undefined}
              placeholder="Answers cite the wrong runbook; stop until the prompt is fixed."
              className={textarea}
            />
            {touched && missing ? (
              <p id={`${id}-error`} className="m-0 text-sm text-rework">
                Write why, so whoever clears it knows what to fix.
              </p>
            ) : null}
          </div>
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={pending}>
              {pending ? 'Stopping the line…' : 'Stop the line'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function ClearDialog({ factory, pull, open, onOpenChange, onConfirm, pending }: DialogProps & { pull: AndonPull }) {
  const id = useId();
  const [note, setNote] = useState('');

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="Clear the andon"
        description={`${factoryTitle(factory)} takes new work again. It was stopped because: ${pull.reason}`}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            onConfirm(note.trim());
          }}
          className="flex flex-col gap-4"
        >
          <div className="flex flex-col gap-1.5">
            <label htmlFor={id} className="text-sm text-muted">
              What was done about it (optional)
            </label>
            <textarea
              id={id}
              value={note}
              onChange={(e) => setNote(e.target.value)}
              rows={3}
              maxLength={maxNote}
              placeholder="Fixed the prompt and re-ran the evals."
              className={textarea}
            />
          </div>
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={pending}>
              {pending ? 'Restarting the line…' : 'Restart the line'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
