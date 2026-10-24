// The status line under the editor's header (D §10.3, role="status", polite): what the draft is
// based on and what runs, how saving goes ("Autosaved 14:32", "Saving…", "Not saved: … Retrying
// in 5 s" with Retry now), the changes (→ Review) and the first problem (→ its section and
// entity), and "Updated elsewhere at 14:35" for a while after another tab or the CLI saved.
import { Link } from '@tanstack/react-router';
import { Check, CircleAlert, LoaderCircle, TriangleAlert } from 'lucide-react';
import { useEffect, useState } from 'react';
import type { DefinitionProblem } from '../../../api/types';
import type { Model } from '../../../shared/definition/model';
import { useFactoryParams } from '../../../shared/factory';
import { formatClock, plural } from '../../../shared/format';
import { Button } from '../../../ui/button';
import type { SessionState } from '../draft/session';
import { baseSentence, elsewhereMs, placeSearch, problemPlace, saveSentence } from './state';

interface StatusLineProps {
  session: Pick<
    SessionState,
    'status' | 'savedAt' | 'retryAt' | 'error' | 'updatedElsewhereAt' | 'hasDraft' | 'discarded'
  >;
  base: { number: number } | null | undefined;
  current: { number: number } | null | undefined;
  /** "3 changes in 2 sections"; null without changes. */
  changes: string | null;
  problemCount: number;
  firstProblem: DefinitionProblem | undefined;
  model: Model;
  onRetry: () => void;
  onResolve: () => void;
  onRestart: () => void;
}

/** The time now, ticking every second while `active`. */
function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [active]);
  return now;
}

const linkClass = 'text-ink underline underline-offset-3 transition-colors duration-200 hover:text-run';

export function StatusLine({
  session,
  base,
  current,
  changes,
  problemCount,
  firstProblem,
  model,
  onRetry,
  onResolve,
  onRestart,
}: StatusLineProps) {
  const params = useFactoryParams();
  const elsewhereAt = session.updatedElsewhereAt ? Date.parse(session.updatedElsewhereAt) : null;
  const now = useNow(session.retryAt !== null || (elsewhereAt !== null && Date.now() - elsewhereAt < elsewhereMs));

  const based = baseSentence({ hasDraft: session.hasDraft, base, current });
  const saving = { ...session, error: session.error, now };
  // The countdown is shown, not spoken: the live region would announce every second.
  const discarded = 'Not saved: this draft was discarded in another tab or the CLI.';
  const shown = session.discarded ? discarded : saveSentence(saving);
  const spoken = session.discarded ? discarded : saveSentence({ ...saving, retryAt: null });
  const elsewhere =
    elsewhereAt !== null && now - elsewhereAt < elsewhereMs && session.updatedElsewhereAt
      ? `Updated elsewhere at ${formatClock(session.updatedElsewhereAt, new Date(now))}.`
      : null;
  const trouble = session.discarded || ['offline', 'retrying', 'failed', 'conflict', 'held'].includes(session.status);
  const place = firstProblem ? problemPlace(firstProblem, model) : undefined;

  return (
    <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5 text-sm text-muted">
      <p role="status" className="m-0 flex flex-wrap items-center gap-x-2.5 gap-y-1">
        <StatusIcon status={session.discarded ? 'failed' : session.status} />
        <span>{based.text}</span>
        {shown ? (
          <>
            <span aria-hidden="true" className={trouble ? 'text-rework-ink' : undefined}>
              {shown}
            </span>
            <span className="sr-only">{spoken}</span>
          </>
        ) : null}
        {elsewhere ? <span>{elsewhere}</span> : null}
      </p>
      {based.differs ? (
        <Link
          to="/o/$org/f/$factory/definition/$section"
          params={{ ...params, section: 'review' }}
          search={{ compare: 'current..draft' }}
          className={linkClass}
        >
          Review the differences
        </Link>
      ) : null}
      {changes ? (
        <Link
          to="/o/$org/f/$factory/definition/$section"
          params={{ ...params, section: 'review' }}
          className={linkClass}
        >
          {changes}
        </Link>
      ) : null}
      {problemCount > 0 && place ? (
        <Link
          to="/o/$org/f/$factory/definition/$section"
          params={{ ...params, section: place.section }}
          search={placeSearch(place)}
          className="inline-flex items-center gap-1 text-rework-ink underline underline-offset-3 transition-colors duration-200 hover:text-rework"
        >
          <TriangleAlert size={14} strokeWidth={1.75} aria-hidden="true" />
          {plural(problemCount, 'problem')}
        </Link>
      ) : null}
      {session.status === 'offline' || session.status === 'retrying' || session.status === 'failed' ? (
        <Button size="sm" variant="white" onClick={onRetry} className="h-11 md:h-8">
          {session.status === 'failed' ? 'Try again' : 'Retry now'}
        </Button>
      ) : null}
      {session.discarded ? (
        <Button size="sm" variant="white" onClick={onRestart} className="h-11 md:h-8">
          Start a new draft
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

function StatusIcon({ status }: { status: SessionState['status'] }) {
  if (status === 'saved') return <Check size={16} strokeWidth={1.8} aria-hidden="true" className="text-ok" />;
  if (status === 'saving')
    return <LoaderCircle size={16} strokeWidth={1.8} aria-hidden="true" className="motion-safe:animate-spin" />;
  return <CircleAlert size={16} strokeWidth={1.8} aria-hidden="true" className="text-rework" />;
}
