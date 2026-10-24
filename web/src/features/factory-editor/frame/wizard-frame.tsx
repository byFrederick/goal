// The New factory wizard's frame (D §10.3, canvas New-*): the header (breadcrumb "Factories / New
// factory", the heading with the draft's chip and where it came from, "Draft saved 14:32",
// Visual | YAML as pressed buttons and Save and exit), and the footer (Cancel or Back, "Step 2 of
// 6", Continue to the next step), sticky at the bottom on a phone.
import { Link } from '@tanstack/react-router';
import { Check, CircleAlert, Code, LoaderCircle, SlidersHorizontal } from 'lucide-react';
import type { ReactNode } from 'react';
import { formatClock } from '../../../shared/format';
import { useOrg } from '../../../shared/org';
import { Button } from '../../../ui/button';
import { cn } from '../../../ui/cn';
import type { SessionState } from '../draft/session';
import { type WizardStep, wizardSteps } from '../wizard-progress';
import { saveSentence } from './state';

export function WizardHeader({
  title,
  chip,
  description,
  actions,
}: {
  title: string;
  chip?: ReactNode;
  description: ReactNode;
  actions: ReactNode;
}) {
  const org = useOrg();
  return (
    <header className="flex flex-col gap-2.5">
      <nav aria-label="Breadcrumb" className="text-sm text-muted">
        <ol className="m-0 flex list-none gap-1.5 p-0">
          <li>
            <Link
              to="/o/$org"
              params={{ org: org.slug }}
              className="text-muted underline underline-offset-3 hover:text-run"
            >
              Factories
            </Link>
          </li>
          <li aria-hidden="true">/</li>
          <li aria-current="page">New factory</li>
        </ol>
      </nav>
      <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
        <div className="flex min-w-0 max-w-[72ch] flex-col gap-2">
          <div className="flex min-w-0 flex-wrap items-center gap-3">
            <h1
              translate="no"
              className="m-0 min-w-0 text-[34px] leading-[1.05] font-normal tracking-display break-words md:text-[44px]"
            >
              {title}
            </h1>
            {chip}
          </div>
          <p className="m-0 text-[15px] leading-normal text-ink-soft">{description}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">{actions}</div>
      </div>
    </header>
  );
}

const viewItem =
  'inline-flex h-[38px] items-center gap-1.5 rounded-chip px-3 text-sm text-ink-soft transition-colors duration-200 md:h-[34px]';

/**
 * Visual | YAML in the wizard: a pressed-button group (the boards' toggle). YAML says why it is
 * unavailable when it is (`yamlUnavailable`).
 */
export function WizardViewToggle({
  yaml,
  onChange,
  yamlUnavailable,
}: {
  yaml: boolean;
  onChange: (yaml: boolean) => void;
  yamlUnavailable?: string | undefined;
}) {
  const current = 'bg-surface font-medium text-ink shadow-raise';
  return (
    <fieldset aria-label="Editor view" className="m-0 inline-flex gap-0.5 rounded-control border-0 bg-control p-[3px]">
      <button
        type="button"
        aria-pressed={!yaml}
        onClick={() => onChange(false)}
        className={cn(viewItem, 'cursor-pointer', !yaml ? current : 'hover:bg-shell hover:text-ink')}
      >
        <SlidersHorizontal size={15} strokeWidth={1.5} aria-hidden="true" />
        Visual
      </button>
      <button
        type="button"
        aria-pressed={yaml}
        aria-disabled={yamlUnavailable ? true : undefined}
        title={yamlUnavailable}
        onClick={() => (yamlUnavailable ? undefined : onChange(true))}
        className={cn(
          viewItem,
          yamlUnavailable ? 'cursor-not-allowed text-muted' : 'cursor-pointer',
          yaml ? current : !yamlUnavailable && 'hover:bg-shell hover:text-ink',
        )}
      >
        <Code size={15} strokeWidth={1.5} aria-hidden="true" />
        YAML
        {yamlUnavailable ? <span className="sr-only">, {yamlUnavailable}</span> : null}
      </button>
    </fieldset>
  );
}

/** "Draft saved 14:32" beside the wizard's actions: how saving goes, in the status line's words when it doesn't. */
export function DraftSaved({
  session,
  onRetry,
  onResolve,
}: {
  session: Pick<SessionState, 'status' | 'savedAt' | 'retryAt' | 'error'>;
  onRetry: () => void;
  onResolve: () => void;
}) {
  const trouble = !['saved', 'saving'].includes(session.status);
  const text =
    session.status === 'saved'
      ? session.savedAt
        ? `Draft saved ${formatClock(session.savedAt)}`
        : null
      : saveSentence({ ...session, retryAt: null, now: Date.now() });
  return (
    <div className="flex flex-wrap items-center gap-2 text-sm text-muted">
      <p role="status" className={cn('m-0 inline-flex items-center gap-1.5', trouble && 'text-rework-ink')}>
        {text ? (
          <>
            {session.status === 'saved' ? (
              <Check size={16} strokeWidth={1.8} aria-hidden="true" className="text-ok" />
            ) : session.status === 'saving' ? (
              <LoaderCircle size={16} strokeWidth={1.8} aria-hidden="true" className="motion-safe:animate-spin" />
            ) : (
              <CircleAlert size={16} strokeWidth={1.8} aria-hidden="true" className="text-rework" />
            )}
            {text}
          </>
        ) : null}
      </p>
      {session.status === 'offline' || session.status === 'retrying' || session.status === 'failed' ? (
        <Button size="sm" variant="white" onClick={onRetry} className="h-11 md:h-8">
          {session.status === 'failed' ? 'Try again' : 'Retry now'}
        </Button>
      ) : null}
      {session.status === 'conflict' ? (
        <Button size="sm" variant="white" onClick={onResolve} className="h-11 md:h-8">
          Resolve…
        </Button>
      ) : null}
    </div>
  );
}

/**
 * The wizard's footer: what goes back (Cancel, Back, Delete…) on the left, the step in the
 * middle, the way forward on the right. On a phone it stays at the bottom of the screen.
 */
export function WizardFooter({ step, start, end }: { step: WizardStep; start: ReactNode; end?: ReactNode }) {
  return (
    <footer
      className={cn(
        'mt-2 grid grid-cols-[1fr_auto] items-center gap-3 border-t border-rule pt-4 md:grid-cols-[1fr_auto_1fr] md:pt-5',
        'max-md:sticky max-md:bottom-0 max-md:z-20 max-md:-mx-4 max-md:-mb-8 max-md:bg-shell max-md:px-4 max-md:pb-[calc(12px+env(safe-area-inset-bottom))]',
      )}
    >
      <div className="flex min-w-0 flex-wrap items-center gap-2">{start}</div>
      <p className="m-0 hidden text-center text-sm text-muted md:block">
        Step {wizardSteps.indexOf(step) + 1} of {wizardSteps.length}
      </p>
      <div className="flex justify-end">{end}</div>
    </footer>
  );
}
