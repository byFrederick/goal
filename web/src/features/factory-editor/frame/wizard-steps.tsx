// The New factory wizard's steps (D §10.3, canvas New-*): Start plus the editor's five sections,
// numbered 1–6. A done step shows a check and says ", done"; the current one is ink and
// aria-current="step". Once the factory exists every step after Start is a link (Start cannot be
// redone). On a phone they become "Step 3 of 6 · Line" with a meter.
import { Link } from '@tanstack/react-router';
import { Check } from 'lucide-react';
import { useOrg } from '../../../shared/org';
import { cn } from '../../../ui/cn';
import { Meter } from '../../../ui/hatch';
import { stepLabel, type WizardStep, wizardSteps } from '../wizard-progress';

interface StepsProps {
  current: WizardStep;
  done: ReadonlySet<WizardStep>;
  /** The new factory's id, once it exists. */
  factory?: string | undefined;
}

export function WizardSteps({ current, done, factory }: StepsProps) {
  const org = useOrg();
  const index = wizardSteps.indexOf(current);

  return (
    <nav aria-label="New factory steps">
      <div className="flex flex-col gap-2 md:hidden">
        <p className="m-0 text-sm font-medium">
          Step {index + 1} of {wizardSteps.length} · {stepLabel(current)}
        </p>
        <Meter value={index + 1} max={wizardSteps.length} className="h-1.5 bg-control" />
      </div>
      <ol className="m-0 hidden list-none grid-cols-3 gap-2 p-0 md:grid @[720px]:grid-cols-6">
        {wizardSteps.map((step, i) => {
          const isCurrent = step === current;
          const isDone = done.has(step) && !isCurrent;
          const body = (
            <>
              <span
                aria-hidden="true"
                className={cn(
                  'flex size-[22px] shrink-0 items-center justify-center rounded-chip text-xs font-medium',
                  isCurrent ? 'bg-surface text-ink' : isDone ? 'bg-ok-tint text-ok' : 'bg-control text-ink-soft',
                )}
              >
                {isDone ? <Check size={14} strokeWidth={2} /> : i + 1}
              </span>
              <span className="min-w-0 truncate">{stepLabel(step)}</span>
              {isDone ? <span className="sr-only">, done</span> : null}
            </>
          );
          const tile = cn(
            'flex h-11 min-w-0 items-center gap-2.5 rounded-control border px-3.5 text-sm no-underline transition-colors duration-200',
            isCurrent ? 'border-ink bg-ink font-medium text-white' : 'border-line bg-card text-ink-soft',
          );
          const linked = factory !== undefined && step !== 'start' && !isCurrent;
          return (
            <li key={step} className="min-w-0">
              {linked ? (
                <Link
                  to="/o/$org/new/$step"
                  params={{ org: org.slug, step }}
                  search={{ factory }}
                  className={cn(tile, 'hover:border-rule-strong hover:text-ink')}
                >
                  {body}
                </Link>
              ) : (
                <span aria-current={isCurrent ? 'step' : undefined} className={tile}>
                  {body}
                </span>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
