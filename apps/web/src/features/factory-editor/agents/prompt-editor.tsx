// An agent's prompt (D §10.7): the file at `prompt.path`, Markdown, edited in the code editor
// (Monaco, loaded lazily; a textarea on phones) and saved as a file 800 ms after typing stops or
// when the editor loses focus. Preview renders it with react-markdown, without raw HTML. The
// word count follows the text as it is typed. "Insert" placeholders are hidden in 2.2 (review
// decision 5).
import { useQuery } from '@tanstack/react-query';
import { Suspense, useEffect, useId, useState } from 'react';
import { ApiError } from '../../../api/client';
import { draftFileQuery, versionFileQuery } from '../../../api/queries';
import type { ModelAgent } from '../../../shared/definition/model';
import { cleanPath } from '../../../shared/definition/place';
import { lazyRetrying } from '../../../shared/lazy';
import { Button } from '../../../ui/button';
import { SegmentedControl } from '../../../ui/segmented-control';
import { useDraft, useDraftSession, useSection, useViewOnly } from '../draft/use-draft';
import { useProblemText } from '../frame/use-editor';
import { CodeEditor, LinedText } from '../yaml/code-editor';

const MarkdownPreview = lazyRetrying(() => import('./markdown-preview').then((m) => m.default));

export interface PromptText {
  /** The prompt file's path in the draft ("agents/x.md"); undefined when the agent has none. */
  path: string | undefined;
  /** What the editor shows: unsaved typing, else the file as loaded. */
  text: string | undefined;
  /** goald's refusal to load it (`file-not-found` for a missing file). */
  error: ApiError | undefined;
  /** The text for an edit, read first when the session hasn't it (and adopted, so the edit's undo can write it back). */
  load: () => Promise<string>;
}

/**
 * The text of an agent's prompt file: the draft's file, or the running version's while no draft
 * is open (the first edit opens one with the same file). Loaded files are adopted by the session,
 * which keeps unsaved typing over a reload.
 */
export function usePromptText(agent: string): PromptText {
  const session = useDraftSession();
  const prompt = useSection('agents').agents?.find((a) => a.name === agent)?.prompt;
  const path = prompt?.path ? cleanPath(prompt.path) : undefined;
  const hasDraft = useDraft((s) => s.hasDraft);
  const typed = useDraft((s) => (path ? s.files.get(path)?.text : undefined));

  const draftFile = draftFileQuery(session.org, session.factory, path ?? '');
  const versionFile = versionFileQuery(session.org, session.factory, 'current', path ?? '', prompt?.sha256 ?? '');
  const fromDraft = useQuery({ ...draftFile, enabled: hasDraft && path !== undefined });
  const fromVersion = useQuery({
    ...versionFile,
    enabled: !hasDraft && path !== undefined && prompt?.sha256 !== undefined,
  });
  const loaded = hasDraft ? fromDraft : fromVersion;

  useEffect(() => {
    if (path && loaded.data) session.adoptFile(path, loaded.data);
  }, [path, loaded.data, session]);

  const text = typed ?? loaded.data?.text;
  return {
    path,
    text,
    error: loaded.error instanceof ApiError ? loaded.error : undefined,
    load: () => (path ? session.fileText(path) : Promise.resolve('')),
  };
}

const words = (text: string) => text.split(/\s+/).filter(Boolean).length;

type View = 'write' | 'preview';

export function PromptEditor({ agent, index }: { agent: ModelAgent; index: number }) {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const headingId = useId();
  const [view, setView] = useState<View>('write');
  const { path, text, error } = usePromptText(agent.name);
  const problem = useProblemText(`/agents/agents/${index}/prompt`);
  const count = text !== undefined ? words(text) : agent.prompt?.words;

  const body = () => {
    if (!path) {
      return (
        <p className="m-0 p-3 text-sm text-muted">
          This agent has no prompt file. Name one in YAML (prompt: ./agents/…).
        </p>
      );
    }
    if (text === undefined && error?.code === 'file-not-found') {
      return (
        <div className="flex flex-wrap items-center gap-3 p-3 text-sm">
          <span className="text-rework-ink">{path} isn’t in the draft.</span>
          {viewOnly ? null : (
            <Button variant="white" size="sm" onClick={() => session.editFile(path, `# ${agent.name}\n`)}>
              Create it
            </Button>
          )}
        </div>
      );
    }
    if (text === undefined) return <LinedText text="" minLines={8} />;
    if (view === 'preview') {
      return (
        <Suspense fallback={<p className="m-0 p-3 text-sm text-muted">Loading the preview…</p>}>
          <MarkdownPreview text={text} />
        </Suspense>
      );
    }
    return (
      <CodeEditor
        value={text}
        language="markdown"
        label={`Prompt of ${agent.name}`}
        onChange={(next) => session.editFile(path, next)}
        onBlur={() => void session.flush()}
        readOnly={viewOnly}
        minLines={12}
        maxLines={28}
      />
    );
  };

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex items-baseline justify-between gap-3">
        <h4 id={headingId} className="m-0 text-[15px] font-medium">
          Prompt
        </h4>
        <span className="text-[13px] text-muted">
          Markdown{count !== undefined ? `, ${count === 1 ? '1 word' : `${count} words`}` : ''}
        </span>
      </div>
      <div className="overflow-hidden rounded-control border border-rule bg-surface has-[textarea:focus-visible]:outline-2 has-[textarea:focus-visible]:outline-offset-2 has-[textarea:focus-visible]:outline-run has-[.monaco-editor.focused]:outline-2 has-[.monaco-editor.focused]:outline-offset-2 has-[.monaco-editor.focused]:outline-run">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-line bg-card px-2 py-1.5">
          <SegmentedControl
            label="Prompt view"
            value={view}
            onChange={setView}
            options={[
              { value: 'write', label: 'Write' },
              { value: 'preview', label: 'Preview' },
            ]}
          />
          {path ? (
            <span translate="no" className="truncate font-mono text-xs text-muted">
              {path}
            </span>
          ) : null}
        </div>
        {body()}
      </div>
      {problem ? <p className="m-0 text-[13px] leading-snug text-rework-ink">{problem}</p> : null}
    </div>
  );
}
