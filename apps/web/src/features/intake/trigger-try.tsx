// Try with a past event (ADR 0028): a dry run of the saved trigger against one of its recent events. The
// dialog picks the event, runs the try and shows what the trigger would do: matched or not, each step, the
// item it would create and the model check's answer and cost. Nothing is created; the result says so.
// Every string the event shapes (its title, the item's title and brief, the reasons) is shown as text.
import { useMutation } from '@tanstack/react-query';
import { Play } from 'lucide-react';
import { useId, useRef, useState } from 'react';
import { ApiError } from '../../api/client';
import { intakeTryMutation } from '../../api/queries';
import type { IntakeEvent, TriggerTry, TriggerTryStep } from '../../api/types';
import { Button } from '../../ui/button';
import { Callout } from '../../ui/callout';
import { cn } from '../../ui/cn';
import { Dialog, DialogContent } from '../../ui/dialog';
import { Field, NativeSelect } from '../../ui/field';
import { Spinner } from '../../ui/spinner';
import { eventWhen } from './trigger-events';

const money = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 4 });
const modelCost = 'This try runs the model check. It costs about $0.01 each time.';

/** The copy for each step the try answers, in the order the trigger runs them. */
const stepLabel: Record<TriggerTryStep['step'], string> = {
  when: 'The event matches When',
  'only.failuresInARow': 'The same check failed in a row',
  'only.dedupe': 'No open item already covers it',
  'only.modelAgrees': 'A model agrees it is worth an item',
  create: 'Create the item',
  'limits.perDay': 'Under the daily limit',
  'limits.pauseOnBudget': 'Budget is not used up',
};

const outcomeText: Record<TriggerTryStep['outcome'], string> = {
  passed: 'Passed',
  skipped: 'Skipped',
  not_reached: 'Not reached',
};

/** The event is not stored: a webhook or GitHub event cannot be tried (the API's event-not-stored). */
export const notStoredReason = "Only a schedule's events are stored, so this trigger's events cannot be tried.";

interface TryProps {
  org: string;
  factory: string;
  trigger: string;
  /** The saved trigger's 20 newest events, newest first. */
  events: IntakeEvent[];
  /** The saved trigger's Only if has the model check: the try spends its cost. */
  modelCheck: boolean;
  /** Why Try is unavailable, when it is; the button is disabled with this reason shown on the page. */
  disabled: boolean;
}

/** The header button and its dialog. The page computes the reason it is disabled. */
export function TryWithPastEvent({ org, factory, trigger, events, modelCheck, disabled }: TryProps) {
  const [open, setOpen] = useState(false);
  const title = 'Try with a past event';
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
            modelCheck={modelCheck}
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
  modelCheck,
  title,
}: Omit<TryProps, 'disabled'> & { title: string }) {
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
        <Field label="Event" help={modelCheck ? modelCost : undefined}>
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
            <span>{stepLabel[step.step]}</span>
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
      {result.model_check ? (
        <p className="m-0 text-body">
          {`The model said ${result.model_check.worth ? 'yes' : 'no'}: ${result.model_check.reason}`}
          <span className="block text-meta text-muted">
            {`${result.model_check.model}, cost ${money.format(result.model_check.cost_usd)}`}
          </span>
        </p>
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
