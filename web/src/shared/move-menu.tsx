// Moving an item to another station (E10): the Move menu every card and the Item page open, by the
// Move button, the M shortcut on a focused card, or a drop on the board. Upstream stations count as
// rework and ask why; an item in progress has its run stopped. The board moves the card at once and
// puts it back if goald refuses.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { ArrowRight } from 'lucide-react';
import { type FormEvent, type KeyboardEvent, useId, useRef, useState } from 'react';
import { toast } from 'sonner';
import { moveItem } from '../api/client';
import { keys } from '../api/queries';
import type { Board, Factory, Item, ItemState } from '../api/types';
import { Button } from '../ui/button';
import { cn } from '../ui/cn';
import { Dialog, DialogClose, DialogContent } from '../ui/dialog';
import { humanize } from './format';
import { factoryWorkInvalidations } from './invalidation';
import { type MoveChoice, moveChoices, movedOnBoard } from './move-choices';
import { useOrg } from './org';

/** The item being moved, as a card or the Item page knows it. */
export interface Movable {
  /** The id or ref the API accepts. */
  key: string;
  id: string;
  ref: string;
  title: string;
  station: string;
  state: ItemState;
}

/** M on a focused card opens its Move menu; the card's link announces it (aria-keyshortcuts). */
export const moveShortcut = 'M';

/** Whether a keydown is the Move shortcut (M without modifiers). */
export function isMoveShortcut(e: KeyboardEvent): boolean {
  return e.key.toUpperCase() === moveShortcut && !e.metaKey && !e.ctrlKey && !e.altKey && !e.repeat;
}

const hint: Record<MoveChoice['kind'], string> = {
  queue: 'Waits there to be released',
  agent: 'Agents pull it from the queue',
  human: 'Waits for a person to decide',
  done: '',
};

function useMove(item: Movable, factory: Factory, onMoved: (updated: Item) => void) {
  const client = useQueryClient();
  const org = useOrg();
  const board = keys.board(org.id, factory.id);
  const moveKey = keys.move(org.id, factory.id);

  return useMutation({
    mutationKey: moveKey,
    mutationFn: ({ to, reason }: { to: string; reason: string }) => moveItem(org.id, item.key, to, reason || undefined),
    onMutate: async ({ to }) => {
      await client.cancelQueries({ queryKey: board });
      const before = client.getQueryData<Board>(board);
      if (before) client.setQueryData(board, movedOnBoard(before, item.id, to, factory.stations));
      return { before };
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.before) client.setQueryData(board, ctx.before);
      toast.error(`Could not move ${item.ref}`, { description: err.message });
    },
    onSuccess: (updated) => {
      client.setQueryData(keys.item(org.id, item.key), updated);
      toast.success(`Moved ${item.ref} to ${humanize(updated.station)}.`);
      onMoved(updated);
    },
    onSettled: () =>
      Promise.all([
        ...factoryWorkInvalidations(org.id, factory.id)
          // Another move still in flight refetches the board when it settles (TanStack's
          // concurrent optimistic updates): this one would put that card back meanwhile.
          .filter((queryKey) => queryKey.at(-1) !== 'board' || client.isMutating({ mutationKey: moveKey }) <= 1)
          .map((queryKey) => client.invalidateQueries({ queryKey })),
        client.invalidateQueries({ queryKey: keys.attention(org.id) }),
        client.invalidateQueries({ queryKey: keys.inboxes(org.id) }),
        client.invalidateQueries({ queryKey: keys.item(org.id, item.key) }),
      ]),
  });
}

interface DialogProps {
  item: Movable;
  factory: Factory;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The station a drop chose, preselected. */
  initialTo?: string | undefined;
  /** After goald moved it (the board focuses the card again). */
  onMoved?: (updated: Item) => void;
}

/** The Move menu: a dialog with the line's stations to choose from (Radix: focus trapped, Escape closes). */
export function MoveDialog({ item, factory, open, onOpenChange, initialTo, onMoved }: DialogProps) {
  const choices = moveChoices(factory.stations, item.station);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {open ? (
        <MoveForm
          item={item}
          factory={factory}
          choices={choices}
          initialTo={initialTo}
          onDone={(updated) => {
            onOpenChange(false);
            onMoved?.(updated);
          }}
        />
      ) : null}
    </Dialog>
  );
}

function MoveForm({
  item,
  factory,
  choices,
  initialTo,
  onDone,
}: {
  item: Movable;
  factory: Factory;
  choices: MoveChoice[];
  initialTo: string | undefined;
  onDone: (updated: Item) => void;
}) {
  const id = useId();
  // A drop's station, else the next one down the line: the usual move.
  const [to, setTo] = useState(
    () => (choices.find((c) => c.id === initialTo) ?? choices.find((c) => !c.upstream) ?? choices[0])?.id ?? '',
  );
  const [reason, setReason] = useState('');
  const [touched, setTouched] = useState(false);
  const first = useRef<HTMLInputElement>(null);
  const move = useMove(item, factory, onDone);
  const choice = choices.find((c) => c.id === to);
  const missing = Boolean(choice?.upstream) && reason.trim() === '';

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!choice || missing) return;
    move.mutate({ to: choice.id, reason: reason.trim() });
  };

  return (
    <DialogContent
      title={`Move ${item.ref}`}
      description={
        item.state === 'in_progress'
          ? 'Choose the station it goes to. The agent working on it now stops.'
          : 'Choose the station it goes to.'
      }
      initialFocus={first}
    >
      {choices.length === 0 ? (
        <p className="m-0 text-sm text-muted">This line has no other station to move it to.</p>
      ) : (
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          <fieldset className="m-0 flex flex-col gap-1.5 border-0 p-0">
            <legend className="mb-1.5 p-0 text-sm text-muted">Station</legend>
            {choices.map((c) => (
              <label
                key={c.id}
                className={cn(
                  'flex min-h-12 cursor-pointer items-center gap-3 rounded-control border px-3 py-2 transition-colors duration-200',
                  'has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-run',
                  c.id === to ? 'border-ink bg-surface' : 'border-line bg-surface hover:border-faint',
                )}
              >
                <input
                  ref={c.id === to ? first : undefined}
                  type="radio"
                  name={`${id}-to`}
                  value={c.id}
                  checked={c.id === to}
                  onChange={() => setTo(c.id)}
                  className="size-4 shrink-0 accent-ink outline-none"
                />
                <span className="flex min-w-0 flex-col">
                  <span className="text-[15px] font-medium">{humanize(c.id)}</span>
                  <span className={cn('text-[13px]', c.upstream ? 'text-rework-ink' : 'text-muted')}>
                    {c.upstream ? 'Sends it back: counts as rework' : hint[c.kind]}
                  </span>
                </span>
              </label>
            ))}
          </fieldset>
          <div className="flex flex-col gap-1.5">
            <label htmlFor={`${id}-reason`} className="text-sm text-muted">
              {choice?.upstream ? 'Why it goes back' : 'Why (optional)'}
            </label>
            <textarea
              id={`${id}-reason`}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={3}
              maxLength={500}
              required={choice?.upstream}
              aria-invalid={touched && missing}
              aria-describedby={touched && missing ? `${id}-error` : undefined}
              className="min-h-20 w-full resize-y rounded-control border border-rule bg-surface px-3 py-2.5 text-base text-ink placeholder:text-muted aria-[invalid=true]:border-rework"
            />
            {touched && missing ? (
              <p id={`${id}-error`} className="m-0 text-sm text-rework">
                Say why it goes back; the agent reads it first.
              </p>
            ) : null}
          </div>
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={move.isPending}>
              {move.isPending ? 'Moving…' : `Move to ${choice ? humanize(choice.id) : '…'}`}
            </Button>
          </div>
        </form>
      )}
    </DialogContent>
  );
}

/** The Move button of a card: 32 px inside desktop columns, 44 px on phones. */
export function MoveButton({
  item,
  onMove,
  size = 'sm',
  className,
}: {
  item: Pick<Movable, 'ref'>;
  onMove: () => void;
  size?: 'sm' | 'lg';
  className?: string;
}) {
  return (
    <button
      type="button"
      onClick={onMove}
      aria-label={`Move ${item.ref} to another station`}
      className={cn(
        'relative z-10 inline-flex shrink-0 items-center justify-center rounded-control border border-line bg-surface text-ink transition-colors duration-200 hover:border-faint hover:bg-shell',
        size === 'sm' ? 'size-8' : 'size-11',
        className,
      )}
    >
      <ArrowRight size={16} strokeWidth={1.5} aria-hidden="true" />
    </button>
  );
}
