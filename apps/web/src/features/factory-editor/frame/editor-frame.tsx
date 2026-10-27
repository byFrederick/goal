// The Definition editor's frame (D §10.3, screens build/definition): the header (the breadcrumb with
// the Live chip, the Form | YAML toggle in Form, Versions, Review changes and Publish), the draft
// banner (the change count, the problem sentence, Review changes and Discard draft), the Ask bar
// (not built yet), the status line, the section tiles in Form, then the section (children), and the
// dialogs a draft can need: discard, conflicts, a discard elsewhere and the unsaved-changes guard.
// It reads the draft session and B's draft; sections render inside it through `section-views.tsx`.
// View only (no definition:edit, canvas Define-View): the running version's chip, a banner saying who
// edits (and whose draft is open), no draft banner, no Discard, no Review or Publish, no status line.
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { ChevronRight, Clock } from 'lucide-react';
import { type ReactNode, useEffect, useId, useState } from 'react';
import { draftQuery, factoryQuery } from '../../../api/queries';
import type { Draft } from '../../../api/types';
import { type Model, sections } from '../../../shared/definition/model';
import { useFactory, useFactoryParams } from '../../../shared/factory';
import { plural } from '../../../shared/format';
import { useOrg } from '../../../shared/org';
import { reasons } from '../../../shared/permissions';
import { Button } from '../../../ui/button';
import { Chip } from '../../../ui/chip';
import { ViewOnlyBanner } from '../../../ui/view-only-banner';
import type { DraftSession, SessionState } from '../draft/session';
import { useDraft, useDraftOpen, useDraftSession } from '../draft/use-draft';
import type { EditorSection } from '../sections';
import { AskBar } from '../yaml/ask-bar';
import { ConflictDialog, DiscardedDialog, FileConflictDialog } from './conflict-dialog';
import { DiscardDialog } from './discard-dialog';
import { DraftBanner } from './draft-banner';
import { SectionTiles } from './section-tiles';
import { changesSentence } from './state';
import { StatusLine } from './status-line';
import { UnsavedGuard } from './unsaved-guard';
import { useValidation } from './use-editor';
import { ViewToggle } from './view-toggle';

interface FrameProps {
  /** The section the route shows. */
  section: EditorSection;
  /** The route asks for the YAML view (`?mode=yaml`). */
  yaml: boolean;
  children: ReactNode;
}

/** The draft's counts: the session's after its own save (fresher), else B's draft as last read. */
function counts(s: SessionState, draft: Draft | undefined) {
  if (!s.hasDraft) return { summaries: [], changeCount: 0, problemCount: 0 };
  if (s.lastSaved > (draft?.revision ?? 0) && s.summaries.length > 0) {
    return { summaries: s.summaries, changeCount: s.changeCount, problemCount: s.problemCount };
  }
  return {
    summaries: draft?.sections ?? [],
    changeCount: draft?.change_count ?? 0,
    problemCount: draft?.problem_count ?? 0,
  };
}

const modelOfSession = (s: SessionState): Model =>
  Object.fromEntries(sections.map((k) => [k, s.sections[k].local])) as unknown as Model;

/** Ctrl/Cmd+Z (and Shift for redo) outside text fields steps through the session's edits. */
export function useUndoKeys(session: DraftSession): void {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.altKey || e.key.toLowerCase() !== 'z') return;
      const t = e.target as HTMLElement | null;
      if (t?.closest('input, textarea, select, [contenteditable="true"], .monaco-editor')) return;
      if (e.shiftKey ? session.redo() : session.undo()) e.preventDefault();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [session]);
}

export function DefinitionFrame({ section, yaml, children }: FrameProps) {
  const org = useOrg();
  const factory = useFactory();
  const params = useFactoryParams();
  const navigate = useNavigate();
  const session = useDraftSession();
  const s = useDraft((state) => state);
  const open = useDraftOpen(session);
  const draft = useQuery({ ...draftQuery(org.id, factory.id), enabled: open === true }).data;
  const detail = useQuery(factoryQuery(org.id, factory.id)).data;
  const validation = useValidation();
  useUndoKeys(session);

  const [discardOpen, setDiscardOpen] = useState(false);
  const [conflictOpen, setConflictOpen] = useState(false);
  // A new conflict opens its dialog; closing it leaves "Resolve…" in the status line.
  const [lastStatus, setLastStatus] = useState(s.status);
  if (s.status !== lastStatus) {
    setLastStatus(s.status);
    if (s.status === 'conflict') setConflictOpen(true);
  }

  const { summaries, changeCount, problemCount } = counts(s, draft);
  const current = detail?.current ?? draft?.current ?? null;
  const base = s.hasDraft ? (draft?.base ?? null) : null;
  const changes = changesSentence(changeCount, summaries);
  // Nothing to publish (no draft, or a draft of a running factory without changes): Review and
  // publish is then not the page's primary action.
  const unchanged = !s.hasDraft || (current !== null && changeCount === 0);
  const unchangedId = useId();
  const blockedId = useId();
  const firstProblem = validation?.problems.find((p) => p.severity !== 'info');
  const hasFileConflicts = Object.keys(s.fileConflicts).length > 0;
  const viewOnly = session.readOnly;
  // The running version, the page's chip (the draft has its own banner).
  const chip = current ? `Live: v${current.number}` : null;
  // Someone else's draft, while a viewer sees the running version.
  const elsewhere = viewOnly && current && detail?.draft ? detail.draft : null;

  const publishNumber = draft?.next_number ?? (current ? current.number + 1 : 1);
  const canPublish = !viewOnly && !unchanged && problemCount === 0;

  return (
    <>
      <header className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-3">
          <nav aria-label="Breadcrumb" className="flex min-w-0 flex-wrap items-center gap-2">
            <span
              aria-hidden="true"
              className="inline-flex size-5 shrink-0 items-center justify-center rounded-chip bg-run text-meta font-control text-white"
            >
              {factory.name.charAt(0).toUpperCase()}
            </span>
            <Link to="/o/$org/f/$factory" params={params} className="text-secondary text-ink-soft hover:text-ink">
              {factory.name}
            </Link>
            <ChevronRight size={12} strokeWidth={1.5} aria-hidden="true" className="text-muted" />
            <h1 className="m-0 text-heading font-control text-ink">Definition</h1>
            {chip ? <Chip tone="plain">{chip}</Chip> : null}
          </nav>
          <div className="flex flex-wrap items-center gap-2">
            {yaml ? null : <ViewToggle section={section} yaml={false} />}
            <Button asChild variant="white" className="h-11 md:h-10">
              <Link to="/o/$org/f/$factory/definition/$section" params={{ ...params, section: 'versions' }}>
                <Clock size={16} strokeWidth={1.5} aria-hidden="true" />
                Versions
              </Link>
            </Button>
            {viewOnly ? null : (
              <>
                <Button asChild variant="white" className="h-11 md:h-10">
                  <Link
                    to="/o/$org/f/$factory/definition/$section"
                    params={{ ...params, section: 'review' }}
                    aria-describedby={unchanged ? unchangedId : undefined}
                  >
                    Review changes
                  </Link>
                </Button>
                {canPublish ? (
                  <Button asChild variant="primary" className="h-11 md:h-10">
                    <Link to="/o/$org/f/$factory/definition/$section" params={{ ...params, section: 'review' }}>
                      Publish v{publishNumber}
                    </Link>
                  </Button>
                ) : (
                  <Button
                    variant="primary"
                    className="h-11 opacity-50 md:h-10"
                    disabled
                    aria-describedby={unchanged ? unchangedId : blockedId}
                  >
                    Publish v{publishNumber}
                  </Button>
                )}
                <span id={unchangedId} className="sr-only">
                  No changes yet
                </span>
                <span id={blockedId} className="sr-only">
                  {problemCount > 0
                    ? `Fix ${plural(problemCount, 'problem')} before you publish`
                    : 'Nothing to publish'}
                </span>
              </>
            )}
          </div>
        </div>
        {viewOnly ? (
          <ViewOnlyBanner
            detail={
              elsewhere ? `${elsewhere.updated_by.name} is working on a draft of v${(current?.number ?? 0) + 1}.` : null
            }
          >
            {reasons.definition(factory.name)}
          </ViewOnlyBanner>
        ) : null}
        {!viewOnly && s.hasDraft && draft ? (
          <DraftBanner
            next={publishNumber}
            current={current?.number ?? null}
            changeCount={changeCount}
            problemCount={problemCount}
          >
            <Button asChild variant="white" size="sm">
              <Link to="/o/$org/f/$factory/definition/$section" params={{ ...params, section: 'review' }}>
                Review changes
              </Link>
            </Button>
            {current ? (
              <Button variant="white" size="sm" aria-haspopup="dialog" onClick={() => setDiscardOpen(true)}>
                Discard draft
              </Button>
            ) : null}
          </DraftBanner>
        ) : null}
        {viewOnly ? null : <AskBar />}
        {viewOnly ? null : (
          <StatusLine
            session={{ ...s, savedAt: s.savedAt ?? (s.hasDraft ? (draft?.updated_at ?? null) : null) }}
            base={base}
            current={current}
            changes={changes}
            problemCount={problemCount}
            firstProblem={firstProblem}
            model={modelOfSession(s)}
            onRetry={() => session.retryNow()}
            onResolve={() => setConflictOpen(true)}
            onRestart={() => session.restart()}
          />
        )}
      </header>

      {yaml ? null : (
        <SectionTiles
          current={section}
          summaries={summaries}
          changeCount={changeCount}
          problemCount={problemCount}
          hasDraft={s.hasDraft}
          yaml={false}
          parseError={s.parseError}
          viewOnly={viewOnly}
        />
      )}

      {children}

      <DiscardDialog
        open={discardOpen}
        onOpenChange={setDiscardOpen}
        next={draft?.next_number ?? (current ? current.number + 1 : 1)}
        current={current?.number ?? null}
        changes={changes}
        changeCount={changeCount}
      />
      <ConflictDialog open={conflictOpen && !hasFileConflicts} onOpenChange={setConflictOpen} />
      <FileConflictDialog open={conflictOpen && hasFileConflicts} onOpenChange={setConflictOpen} />
      <DiscardedDialog onLeave={() => void navigate({ to: '/o/$org/f/$factory', params, ignoreBlocker: true })} />
      <UnsavedGuard />
    </>
  );
}
