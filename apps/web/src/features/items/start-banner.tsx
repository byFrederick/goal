// What the item's page says about when it starts and how it closed. An item that has not started shows its
// start with Start now and Change (ADR 0040 decisions 3 and 4); an item an agent closed shows "Closed by
// <agent>" with the reason (decision 11), read from the cancelled event the engine wrote.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { CalendarClock, CircleSlash } from 'lucide-react';
import { type FormEvent, useId, useState } from 'react';
import { toast } from 'sonner';
import { ApiError, clearItemStart, setItemStart } from '../../api/client';
import type { Item, ItemEvent } from '../../api/types';
import { useFactory } from '../../shared/factory';
import { formatStart } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { type StartChoice, StartField, startOf } from '../../shared/start-field';
import { StartNowButton } from '../../shared/start-now';
import { Button } from '../../ui/button';
import { Dialog, DialogClose, DialogContent, DialogTrigger } from '../../ui/dialog';

/** The agent and reason of an item an agent closed, from its cancelled event; none for a person's cancel. */
export function closedBy(
  item: Pick<Item, 'state'>,
  events: readonly ItemEvent[],
): { agent: string; reason: string } | null {
  if (item.state !== 'cancelled') return null;
  const event = events.findLast((e) => e.kind === 'cancelled');
  if (event?.data.closed !== true) return null;
  const agent = typeof event.data.agent === 'string' ? event.data.agent : event.actor;
  const reason = typeof event.data.reason === 'string' ? event.data.reason : '';
  return { agent, reason };
}

const banner =
  'flex flex-wrap items-center gap-x-3 gap-y-2 rounded-card border border-line bg-panel px-4 py-3 text-body';

export function ClosedBanner({ closed }: { closed: { agent: string; reason: string } }) {
  return (
    <div role="status" className={`${banner} motion-safe:animate-fade-in`}>
      <CircleSlash size={16} strokeWidth={1.75} aria-hidden="true" className="flex-none text-muted" />
      <span className="min-w-0 flex-1 basis-60 break-words">
        <b className="font-semibold">Closed by {closed.agent}.</b>
        {closed.reason ? <span className="text-ink-soft"> {closed.reason}</span> : null}
      </span>
    </div>
  );
}

export function StartBanner({ item }: { item: Item }) {
  const factory = useFactory();
  if (!item.start || item.state === 'done' || item.state === 'cancelled') return null;
  const when = formatStart(item.start.at);
  const canOperate = holds(factory, 'item:operate');
  return (
    <div role="status" className={`${banner} motion-safe:animate-fade-in`}>
      <CalendarClock size={16} strokeWidth={1.75} aria-hidden="true" className="flex-none text-muted" />
      <span className="min-w-0 flex-1 basis-60">
        <b className="font-semibold">
          Starts {when.day} at {when.time}.
        </b>{' '}
        <span className="text-ink-soft">Waiting in the inbox.</span>
      </span>
      {canOperate ? (
        <span className="flex items-center gap-2">
          <StartNowButton itemId={item.id} factoryId={factory.id} label={item.title} />
          <ChangeStart item={item} />
        </span>
      ) : null}
    </div>
  );
}

const pad = (n: number) => String(n).padStart(2, '0');

/** The choice the dialog opens with: Later, at the item's current start. */
function choiceOf(item: Item): StartChoice {
  const d = new Date(item.start?.at ?? Date.now());
  return {
    mode: 'later',
    date: `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`,
    time: `${pad(d.getHours())}:${pad(d.getMinutes())}`,
    after: '',
  };
}

/** Change: a new time or items for an item that has not started; Now clears the start instead. */
function ChangeStart({ item }: { item: Item }) {
  const id = useId();
  const org = useOrg();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [choice, setChoice] = useState(() => choiceOf(item));
  const [problem, setProblem] = useState<string>();
  const when = startOf(choice);
  const save = useMutation({
    mutationFn: () => (when.start ? setItemStart(org.id, item.id, when.start) : clearItemStart(org.id, item.id)),
    onSuccess: () => {
      setOpen(false);
      void client.invalidateQueries({ queryKey: ['org', org.id, 'item'] });
      void client.invalidateQueries({ queryKey: ['org', org.id, 'factory'] });
    },
    onError: (err) => {
      if (err instanceof ApiError && (err.status === 400 || err.status === 409)) setProblem(err.detail ?? err.title);
      else toast.error('Could not change the start', { description: err instanceof Error ? err.message : '' });
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setProblem(undefined);
    if (!when.missing) save.mutate();
    else setProblem(when.missing);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) {
          setChoice(choiceOf(item));
          setProblem(undefined);
        }
        setOpen(next);
      }}
    >
      <DialogTrigger asChild>
        <Button variant="secondary" size="sm" aria-label={`Change the start of ${item.title}`}>
          Change
        </Button>
      </DialogTrigger>
      <DialogContent title="Change when it starts" description={`${item.ref} waits in the inbox until then.`}>
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          <StartField id={id} value={choice} onChange={setChoice} error={problem} />
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={save.isPending}>
              {save.isPending ? 'Saving…' : choice.mode === 'now' ? 'Start now' : 'Save'}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
