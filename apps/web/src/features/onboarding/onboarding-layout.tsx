// The layout every onboarding step shares (ADR 0032; docs/design/screens/get-started/org.jpg): the
// Goal mark and "Save and finish later" on top, the eight steps as a stepper, the step's form on the
// left with Back and the primary action at its bottom, and on the right the dark panel "Your factory,
// as you build it". The panel's slots are filled by the steps; until then they show dashed placeholders.
import { Link } from '@tanstack/react-router';
import type { ReactNode } from 'react';
import type { OrgSummary } from '../../api/types';
import { useSignOut } from '../../shared/account';
import { cn } from '../../ui/cn';
import { Wordmark } from '../../ui/logo';
import { type OnboardingStepKey, onboardingSteps, stepIndex } from './steps';

/** What the steps show in the panel. Each slot falls back to its dashed placeholder. */
export interface PreviewSlots {
  /** The organization card, first in the grid. */
  organization?: ReactNode;
  /** The card beside it. */
  second?: ReactNode;
  /** The line area across the grid. */
  line?: ReactNode;
  /** Three cards in a row under the line; the step passes three elements. */
  cards?: ReactNode;
}

interface OnboardingLayoutProps {
  step: OnboardingStepKey;
  /** The organization the person is in, if they have one yet: "Save and finish later" goes to it. */
  org?: OrgSummary | undefined;
  /** Bottom left: the Back link, when the step has one. */
  back?: ReactNode | undefined;
  /** Bottom right: the primary action. */
  action?: ReactNode | undefined;
  preview?: PreviewSlots | undefined;
  /** The step's form, above the footer. */
  children?: ReactNode;
}

/** The text style of "Save and finish later", for the link and for the button that signs out. */
export const saveLaterClass =
  'rounded-control font-control text-secondary text-ink-soft no-underline hover:text-ink focus-visible:outline-2 focus-visible:outline-run';

export function OnboardingLayout({ step, org, back, action, preview, children }: OnboardingLayoutProps) {
  return (
    <div className="grid min-h-dvh grid-cols-1 bg-shell text-ink xl:grid-cols-2">
      <main id="main" className="flex min-w-0 flex-col gap-7 px-4 pt-6 pb-8 md:px-8 xl:px-16 xl:pt-7">
        <div className="flex items-center gap-4">
          <Wordmark />
          <div className="ml-auto">
            <SaveAndFinishLater org={org} />
          </div>
        </div>
        <Stepper step={step} />
        <div className="flex max-w-xl flex-1 flex-col gap-6">{children}</div>
        <footer className="flex max-w-xl items-center gap-2">
          {back}
          <div className="ml-auto">{action}</div>
        </footer>
      </main>
      <Panel preview={preview} />
    </div>
  );
}

/** "Save and finish later": the organization's home, or sign-out while there is no organization yet. */
function SaveAndFinishLater({ org }: { org: OrgSummary | undefined }) {
  const { signOut } = useSignOut();
  if (org) {
    return (
      <Link to="/o/$org" params={{ org: org.slug }} className={saveLaterClass}>
        Save and finish later
      </Link>
    );
  }
  return (
    <button
      type="button"
      aria-busy={signOut.isPending || undefined}
      onClick={() => {
        if (!signOut.isPending) signOut.mutate();
      }}
      className={cn(saveLaterClass, 'cursor-pointer bg-transparent p-0')}
    >
      Save and finish later
    </button>
  );
}

/** The eight steps: a bar and a name each. Done steps are lighter, the current one cobalt and bold. */
function Stepper({ step }: { step: OnboardingStepKey }) {
  const current = stepIndex(step);
  return (
    <ol
      aria-label={`Setup, step ${current + 1} of ${onboardingSteps.length}`}
      className="m-0 flex max-w-xl list-none gap-2 overflow-x-auto p-0"
    >
      {onboardingSteps.map((s, i) => {
        const isCurrent = i === current;
        const done = i < current;
        return (
          <li
            key={s.key}
            aria-current={isCurrent ? 'step' : undefined}
            className={cn(
              'flex flex-auto flex-col gap-2 text-meta font-control whitespace-nowrap',
              isCurrent ? 'text-ink' : done ? 'text-ink-soft' : 'text-muted',
            )}
          >
            <span
              aria-hidden="true"
              className={cn('block h-1 rounded-full', isCurrent ? 'bg-run' : done ? 'bg-run-soft' : 'bg-line')}
            />
            {s.label}
          </li>
        );
      })}
    </ol>
  );
}

function Panel({ preview }: { preview: PreviewSlots | undefined }) {
  return (
    <aside
      aria-label="Preview of your factory"
      className="flex min-w-0 flex-col gap-3 bg-andon px-6 py-7 text-white md:px-9"
    >
      <div className="flex items-center justify-between gap-4 text-secondary text-andon-text">
        <h2 className="m-0 font-control text-body text-white">Your factory, as you build it</h2>
        <span>Updates as you go</span>
      </div>
      <div className="grid grid-cols-2 gap-3">
        {preview?.organization ?? <PreviewPlaceholder className="min-h-26" />}
        {preview?.second ?? (
          <PreviewPlaceholder className="justify-start py-4">
            <span className="block h-2.5 w-2/5 rounded-full bg-andon-line" />
            <span className="block h-2.5 w-7/10 rounded-full bg-andon-line" />
          </PreviewPlaceholder>
        )}
      </div>
      {preview?.line ?? (
        <PreviewPlaceholder className="min-h-42 items-center justify-center text-center">
          <span className="text-secondary text-andon-text">The line appears when you pick a template</span>
        </PreviewPlaceholder>
      )}
      <div className="grid grid-cols-3 gap-3">
        {preview?.cards ?? (
          <>
            <PreviewPlaceholder className="min-h-37.5" />
            <PreviewPlaceholder className="min-h-37.5" />
            <PreviewPlaceholder className="min-h-37.5" />
          </>
        )}
      </div>
    </aside>
  );
}

/** A card of the panel. `active` marks the one the step is working on (the organization, in step 1). */
export function PreviewCard({
  label,
  value,
  detail,
  active = false,
}: {
  label: string;
  value: string;
  detail?: string;
  active?: boolean;
}) {
  return (
    <div
      className={cn(
        'flex min-w-0 flex-col gap-2 rounded-card border border-andon-line bg-andon-raised p-4',
        active && 'border-run-soft ring-4 ring-run-soft/25',
      )}
    >
      <span className="text-meta text-andon-text">{label}</span>
      <span className="font-control text-heading break-words text-white">{value}</span>
      {detail ? <span className="text-secondary break-all text-andon-text">{detail}</span> : null}
    </div>
  );
}

/** An empty slot: a dashed outline the step's card replaces. */
export function PreviewPlaceholder({ className, children }: { className?: string; children?: ReactNode }) {
  return (
    <div
      className={cn('flex min-w-0 flex-col gap-2 rounded-card border border-dashed border-andon-line p-4', className)}
    >
      {children}
    </div>
  );
}
