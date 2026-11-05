// Try with a past delivery or event (ADR 0028, ADR 0040 decision 20): a dry run of the saved trigger against one
// of its recent events. The dialog picks the event, runs the try and shows what the trigger would do: matched
// or not, each step and the item it would create. Nothing is created; the result says so. A check trigger has
// Try a check instead: it queues a check that files nothing, and the log shows what it would have done.
// Every string the event shapes (its title, the item's title and brief, the reasons) is shown as text.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Play } from 'lucide-react';
import { useEffect, useId, useRef, useState } from 'react';
import { ApiError } from '../../api/client';
import { intakeKeys, intakeTryMutation, tryCheckMutation } from '../../api/queries';
import type { IntakeEvent, TriggerTry, TriggerTryStep } from '../../api/types';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { Dialog, DialogContent } from '../../ui/dialog';
import { Field, NativeSelect } from '../../ui/field';
import { Spinner } from '../../ui/spinner';
import { eventWhen } from './trigger-events';


/** The copy for each step the try answers, in the order the trigger runs them. */
const stepLabel: Partial<Record<TriggerTryStep['step'], string>> = {
  when: 'The event matches When',
  'only.failuresInARow': 'The same check failed in a row',
  'only.dedupe': 'No open item already covers it',
  create: 'Create the item',
  'limits.perDay': 'Under the daily limit',
  'limits.pauseOnBudget': 'Budget is not used up',
};

const outcomeText: Record<TriggerTryStep['outcome'], string> = {
  passed: 'Passed',
  skipped: 'Skipped',
  not_reached: 'Not reached',
};

/** The event is not stored: a missed event, or a delivery whose body is no longer kept (the API's event-not-stored). */
export const notStoredReason = 'This event is no longer stored, so it cannot be tried. A delivery is kept for 30 days.';

/** A step the contract still lists but the page no longer draws, such as the removed model check. */
const labelOf = (step: TriggerTryStep['step']) => stepLabel[step] ?? 'A removed check';

interface TryProps {
  org: string;
  factory: string;
  trigger: string;
  /** The saved trigger's 20 newest events, newest first. */
  events: IntakeEvent[];
  /** Why Try is unavailable, when it is; the button is disabled with this reason shown on the page. */
  disabled: boolean;
  /** The button's words: a delivery for a webhook or GitHub trigger, a past event for a schedule. */
  title?: string;
}

/** The header button and its dialog. The page computes the reason it is disabled. */
export function TryWithPastEvent({ org, factory, trigger, events, disabled, title = 'Try with a past event' }: TryProps) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="white" size="md" disabled={disabled} onClick={() => setOpen(true)}>
        <Play size={15} strokeWidth={1.5} aria-hidden="true" />
        {title}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        {open ? (
          <TryDialog
            org={org}
            factory={factory}
            trigger={trigger}
            events={events}
            title={title}
          />
        ) : null}
      </Dialog>
    </>
  );
}

function TryDialog({
  org,
  factory,
  trigger,
  events,
  title,
}: Omit<TryProps, 'disabled' | 'title'> & { title: string }) {
  const [firing, setFiring] = useState(events[0]?.id ?? '');
  const run = useMutation(intakeTryMutation(org, factory, trigger));
  const resultId = useId();
  const chosen = events.find((e) => e.id === firing);
  const selectRef = useRef<HTMLSelectElement>(null);

  return (
    <DialogContent
      title={title}
      description="Runs the saved trigger against one of its recent events. Nothing is created."
      initialFocus={selectRef}
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (firing) run.mutate(firing);
        }}
      >
        <Field label="Event">
          <NativeSelect
            ref={selectRef}
            value={firing}
            onChange={(e) => setFiring(e.target.value)}
            className="h-10 rounded-control border-rule bg-card px-3 text-body"
          >
            {events.map((event) => (
              <option key={event.id} value={event.id}>
                {`${event.title}, ${eventWhen(event.at)}`}
              </option>
            ))}
          </NativeSelect>
        </Field>
        <div>
          <Button type="submit" variant="primary" disabled={!chosen || run.isPending}>
            {run.isPending ? 'Trying…' : 'Run the try'}
          </Button>
        </div>
      </form>

      <div aria-busy={run.isPending} className="flex flex-col gap-3">
        {run.isPending ? (
          <p className="m-0 flex items-center gap-2 text-secondary text-muted">
            <Spinner tone="dark" />
            <span className="sr-only">Loading</span>
            Trying the event
          </p>
        ) : null}
        {run.isError ? <TryFailure error={run.error} /> : null}
        {run.data ? <TryResult id={resultId} result={run.data} /> : null}
      </div>
    </DialogContent>
  );
}

function TryFailure({ error }: { error: unknown }) {
  if (error instanceof ApiError && error.problem.code === 'event-not-stored') {
    return <Callout tone="rework">{notStoredReason}</Callout>;
  }
  const text = error instanceof ApiError ? (error.problem.detail ?? error.problem.title) : 'Try again in a moment.';
  return <Callout tone="rework">{`Could not try the trigger. ${text}`}</Callout>;
}

function TryResult({ id, result }: { id: string; result: TriggerTry }) {
  const wouldCreate = result.outcome === 'would_create';
  return (
    <div role="status" className="flex flex-col gap-3">
      <h3 id={id} className="m-0 text-heading">
        {result.matched ? 'The event matched' : 'The event did not match'}
      </h3>
      <p className="m-0 text-body">
        {wouldCreate
          ? 'The trigger would create an item.'
          : `The trigger would skip it${result.reason ? `. ${result.reason}` : '.'}`}
      </p>
      <ol className="m-0 flex list-none flex-col gap-1 p-0">
        {result.steps.map((step) => (
          <li key={step.step} className="flex flex-wrap items-baseline gap-x-2 text-body">
            <span>{labelOf(step.step)}</span>
            <span className="text-secondary font-semibold">{outcomeText[step.outcome]}</span>
            {step.reason ? <span className="w-full text-meta text-muted break-words">{step.reason}</span> : null}
          </li>
        ))}
      </ol>
      {result.item ? (
        <dl className="m-0 flex flex-col gap-3 rounded-cell border border-line p-3 text-body">
          <Pair term="Type">{result.item.type}</Pair>
          <Pair term="Station">{result.item.station}</Pair>
          <Pair term="Lane">{result.item.expedite ? 'Expedite lane' : (result.item.lane ?? 'Default lane')}</Pair>
          <Pair term="Title">{result.item.title}</Pair>
          <Pair term="Brief" pre>
            {result.item.brief}
          </Pair>
        </dl>
      ) : null}
      <p className="m-0 text-meta text-muted">Nothing was created. The try changed no item, event or limit.</p>
    </div>
  );
}

function Pair({ term, pre, children }: { term: string; pre?: boolean; children: string }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-meta text-muted">{term}</dt>
      <dd className={cn('m-0 break-words', pre && 'whitespace-pre-wrap')}>{children}</dd>
    </div>
  );
}

export interface TryCheckProps {
  org: string;
  factory: string;
  trigger: string;
  /** Why the button is off before it is pressed: unsaved edits, or a check that is queued or running. */
  disabled: boolean;
  /** A check of the trigger is queued or running: a try waits for it (the API's 409 still-checking). */
  checking: boolean;
  /** Changes whenever the log is read again, so a still-checking answer clears once the log shows the check done. */
  readAt: number;
  /** The server's words when a try is refused, or null. */
  onProblem: (text: string | null) => void;
}

/** Try a check: queues a check that files, notes and saves nothing; its row in the log is marked as a try. */
export function TryCheck({ org, factory, trigger, disabled, checking, readAt, onProblem }: TryCheckProps) {
  const queryClient = useQueryClient();
  const run = useMutation(tryCheckMutation(org, factory, trigger));
  const [stillChecking, setStillChecking] = useState(false);
  // A 409 holds the button off until the log is read again and shows no check queued or running.
  // biome-ignore lint/correctness/useExhaustiveDependencies: readAt is the signal that the log was read again.
  useEffect(() => {
    if (!checking) setStillChecking(false);
  }, [readAt, checking]);
  const tryIt = () => {
    onProblem(null);
    run.mutate(undefined, {
      onSuccess: () => void queryClient.invalidateQueries({ queryKey: intakeKeys.trigger(org, factory, trigger) }),
      onError: (error) => {
        if (error instanceof ApiError && error.problem.code === 'still-checking') {
          setStillChecking(true);
          onProblem('A check is queued or running. Try again when it is done.');
          void queryClient.invalidateQueries({ queryKey: intakeKeys.trigger(org, factory, trigger) });
        } else if (error instanceof ApiError && error.problem.code === 'no-check') {
          onProblem(error.problem.detail ?? 'This trigger has no check to try. Save it with a check first.');
        } else {
          onProblem(`Could not try a check. ${error instanceof ApiError ? (error.problem.detail ?? error.problem.title) : 'Try again in a moment.'}`);
        }
      },
    });
  };
  return (
    <Button variant="white" size="md" disabled={disabled || checking || stillChecking || run.isPending} onClick={tryIt}>
      <Play size={15} strokeWidth={1.5} aria-hidden="true" />
      {run.isPending ? 'Trying…' : 'Try a check'}
    </Button>
  );
}
