import { Check } from 'lucide-react';
import { cn } from './cn';

export type StepState = 'done' | 'current' | 'todo';

export interface Step {
  name: string;
  /** A line under the name: "2 h 12 min, waiting on you". */
  detail?: string;
  state: StepState;
}

const stateText: Record<StepState, string> = {
  done: 'Done',
  current: 'Current step',
  todo: 'Not started',
};

/**
 * Progress through named steps: circles joined by a track. Done steps are run-coloured with a
 * check; the current one has a ring in violet when it waits on a person. Each step says its state
 * in text too, so colour is never the only signal.
 */
export function Stepper({
  label,
  steps,
  waiting = false,
}: {
  label: string;
  steps: readonly Step[];
  waiting?: boolean;
}) {
  return (
    <ol aria-label={label} className="m-0 flex list-none p-0">
      {steps.map((step, i) => {
        const current = step.state === 'current';
        const waits = current && waiting;
        const last = i === steps.length - 1;
        const state = waits ? 'Current step, waiting on you' : stateText[step.state];
        return (
          <li
            key={step.name}
            aria-current={current ? 'step' : undefined}
            className="flex min-w-0 flex-1 flex-col gap-2"
          >
            <div className="relative flex h-6 items-center">
              {last ? null : (
                <span
                  aria-hidden="true"
                  className="absolute top-1/2 right-0 left-6 h-1 -translate-y-1/2 rounded-full bg-rule"
                >
                  {step.state === 'done' ? <span className="block h-full rounded-full bg-run-soft" /> : null}
                  {current ? <span className="block h-full w-1/2 rounded-full bg-run" /> : null}
                </span>
              )}
              <span
                aria-hidden="true"
                className={cn(
                  'relative flex size-6 shrink-0 items-center justify-center rounded-full border-2 bg-shell',
                  step.state === 'done' && 'border-run bg-run text-white',
                  step.state === 'todo' && 'border-rule',
                  current && !waits && 'border-run',
                  waits && 'border-you ring-4 ring-you-tint',
                )}
              >
                {step.state === 'done' ? <Check className="size-3" strokeWidth={2.25} /> : null}
                {current ? <span className={cn('size-2 rounded-full', waits ? 'bg-you' : 'bg-run')} /> : null}
              </span>
            </div>
            <div className="flex min-w-0 flex-col gap-1 pr-2">
              <span className="text-secondary font-semibold text-ink">
                {step.name}
                <span className="sr-only">, {state}</span>
              </span>
              {step.detail ? (
                <span className={cn('text-meta text-muted', waits && 'text-you-ink')}>{step.detail}</span>
              ) : null}
            </div>
          </li>
        );
      })}
    </ol>
  );
}
