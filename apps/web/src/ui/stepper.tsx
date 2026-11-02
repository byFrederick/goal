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
 * Progress through named steps (MASTER.md, Components: stepper, v3): 8 px dots on a 1 px track. A done step is an
 * ink dot and the track before it is ink; the current step is an ink ring, or an orange ring when it waits on a
 * person; a step not started is a grey dot. Each step says its state in text too, so colour is never the only signal.
 */
export function Stepper({
  label,
  steps,
  waiting = false,
  compact = false,
}: {
  label: string;
  steps: readonly Step[];
  waiting?: boolean;
  /** Phones: each name stays on one line and only the current step shows its detail. */
  compact?: boolean;
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
            <div className="relative flex h-4 items-center">
              {last ? null : (
                <span aria-hidden="true" className="absolute top-1/2 right-0 left-1.5 h-px -translate-y-1/2 bg-rule">
                  {step.state === 'done' ? <span className="block h-full bg-ink" /> : null}
                </span>
              )}
              <span
                aria-hidden="true"
                className={cn(
                  'relative block size-2 shrink-0 rounded-full',
                  step.state === 'done' && 'bg-ink',
                  step.state === 'todo' && 'bg-rule',
                  current && 'border-[1.5px] border-ink bg-shell',
                  waits && 'border-you',
                )}
              />
            </div>
            <div className={cn('flex min-w-0 flex-col gap-1 pr-2', compact && 'pr-1')}>
              <span className={cn('text-secondary font-medium text-ink', compact && 'truncate text-meta')}>
                {step.name}
                <span className="sr-only">, {state}</span>
              </span>
              {step.detail && (!compact || current) ? (
                <span className={cn('text-meta text-muted', compact && 'truncate', waits && 'text-you-ink')}>
                  {step.detail}
                </span>
              ) : null}
            </div>
          </li>
        );
      })}
    </ol>
  );
}
