// The Definition editor's frame (D §10.3, canvas Define*): the header (breadcrumb, "Definition"
// with the draft's chip, Visual | YAML, Versions, Discard draft…, Review and publish), the status
// line, the section tiles, then the section (children), and the dialogs a draft can need: discard,
// conflicts, a discard elsewhere and the unsaved-changes guard. It reads the draft session and
// B's draft; sections render inside it through `section-views.tsx`.
import { useQuery } from '@tanstack/react-query';
import { Link, useLinkProps, useNavigate, useSearch } from '@tanstack/react-router';
import { ChevronRight, Clock, Code, SlidersHorizontal } from 'lucide-react';
import { type ReactNode, useEffect, useState } from 'react';
import { draftQuery, factoryQuery } from '../../../api/queries';
import type { Draft } from '../../../api/types';
import { laterText } from '../../../app/screens';
import { type Model, sections } from '../../../shared/definition/model';
import { useFactory, useFactoryParams } from '../../../shared/factory';
import { useOrg } from '../../../shared/org';
import { Button } from '../../../ui/button';
import { Chip } from '../../../ui/chip';
import { cn } from '../../../ui/cn';
import type { DraftSession, SessionState } from '../draft/session';
import { useDraft, useDraftSession } from '../draft/use-draft';
import { yamlView } from '../section-views';
import { type EditorSection, validateDefinitionSearch } from '../sections';
import { ConflictDialog, DiscardedDialog, FileConflictDialog } from './conflict-dialog';
import { DiscardDialog } from './discard-dialog';
import { SectionTiles } from './section-tiles';
import { changesSentence } from './state';
import { StatusLine } from './status-line';
import { UnsavedGuard } from './unsaved-guard';
import { useValidation } from './use-editor';

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
function useUndoKeys(session: DraftSession): void {
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
  const draft = useQuery(draftQuery(org.id, factory.id)).data;
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
  const firstProblem = validation?.problems.find((p) => p.severity !== 'info');
  const hasFileConflicts = Object.keys(s.fileConflicts).length > 0;
  const chip =
    s.hasDraft && draft
      ? draft.base || current
        ? `Draft of v${draft.next_number}`
        : 'Draft'
      : current
        ? `v${current.number}`
        : null;

  return (
    <>
      <header className="flex flex-col gap-2.5">
        <nav aria-label="Breadcrumb" className="hidden text-sm text-muted md:block">
          <ol className="m-0 flex list-none gap-1.5 p-0">
            <li>
              <Link to="/o/$org" params={{ org: params.org }} className="text-muted hover:text-run">
                {org.name}
              </Link>
            </li>
            <li aria-hidden="true">/</li>
            <li>
              <Link to="/o/$org/f/$factory" params={params} className="text-muted hover:text-run">
                {factory.name}
              </Link>
            </li>
            <li aria-hidden="true">/</li>
            <li aria-current="page">Definition</li>
          </ol>
        </nav>
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-3">
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="m-0 text-[34px] leading-[1.05] font-normal tracking-display md:text-[44px]">Definition</h1>
            {chip ? (
              <Chip tone="plain" className={cn(s.hasDraft && 'border-ink')}>
                {chip}
              </Chip>
            ) : null}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <ViewToggle section={section} yaml={yaml} />
            <Button asChild variant="white" className="h-11 md:h-10">
              <Link to="/o/$org/f/$factory/definition/$section" params={{ ...params, section: 'versions' }}>
                <Clock size={16} strokeWidth={1.5} aria-hidden="true" />
                Versions
              </Link>
            </Button>
            {s.hasDraft && current ? (
              <Button
                variant="white"
                className="h-11 md:h-10"
                aria-haspopup="dialog"
                onClick={() => setDiscardOpen(true)}
              >
                Discard draft…
              </Button>
            ) : null}
            <Button asChild variant="primary" className="h-11 md:h-10">
              <Link to="/o/$org/f/$factory/definition/$section" params={{ ...params, section: 'review' }}>
                Review and publish
                <ChevronRight size={14} strokeWidth={1.75} aria-hidden="true" />
              </Link>
            </Button>
          </div>
        </div>
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
      </header>

      <SectionTiles
        current={section}
        summaries={summaries}
        changeCount={changeCount}
        problemCount={problemCount}
        hasDraft={s.hasDraft}
      />

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

const viewItem =
  'inline-flex h-[38px] items-center gap-1.5 rounded-chip px-3 text-sm text-ink-soft no-underline transition-colors duration-200 md:h-[34px]';

/** Visual | YAML: two links, the current one aria-current="page"; YAML is Later until D10. */
function ViewToggle({ section, yaml }: { section: EditorSection; yaml: boolean }) {
  const params = useFactoryParams();
  const showYaml = yaml && yamlView !== null;
  // The entity stays across views; the YAML view's own place (file, line) does not.
  const { mode: _mode, file: _file, line: _line, ...entity } = validateDefinitionSearch(useSearch({ strict: false }));
  const { 'aria-current': _visualPage, ...visual } = useLinkProps({
    to: '/o/$org/f/$factory/definition/$section',
    params: { ...params, section },
    search: entity,
  });
  const { 'aria-current': _yamlPage, ...yamlLink } = useLinkProps({
    to: '/o/$org/f/$factory/definition/$section',
    params: { ...params, section },
    search: { ...entity, mode: 'yaml' },
  });
  const currentClass = 'bg-surface font-medium text-ink shadow-raise';

  return (
    <nav aria-label="Editor view" className="inline-flex gap-0.5 rounded-control bg-control p-[3px]">
      <a
        {...visual}
        aria-current={showYaml ? undefined : 'page'}
        className={cn(viewItem, !showYaml ? currentClass : 'hover:bg-shell hover:text-ink')}
      >
        <SlidersHorizontal size={15} strokeWidth={1.5} aria-hidden="true" />
        Visual
      </a>
      {yamlView ? (
        <a
          {...yamlLink}
          aria-current={showYaml ? 'page' : undefined}
          className={cn(viewItem, showYaml ? currentClass : 'hover:bg-shell hover:text-ink')}
        >
          <Code size={15} strokeWidth={1.5} aria-hidden="true" />
          YAML
        </a>
      ) : (
        <span aria-disabled="true" className={cn(viewItem, 'cursor-default text-muted')}>
          <Code size={15} strokeWidth={1.5} aria-hidden="true" />
          YAML
          <span className="sr-only">, {laterText}</span>
        </span>
      )}
    </nav>
  );
}
