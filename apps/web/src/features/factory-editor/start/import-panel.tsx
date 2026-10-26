// Import YAML (D §10.11): paste a factory.yaml into the code editor, or upload one or a whole
// definition folder (≤ 500 files, 5 MiB). Every change is checked with B's
// POST /definitions/validate (debounced, latest wins; Check runs it at once): errors that stop it
// from parsing or matching the schema are listed by line and keep Continue disabled; anything
// else (a missing prompt file, an unknown agent) is listed as fixable in the next steps.
import { useMutation } from '@tanstack/react-query';
import { Check, FileUp, FolderUp } from 'lucide-react';
import { type ChangeEvent, useEffect, useId, useRef, useState } from 'react';
import { ApiError } from '../../../api/client';
import { validateDefinition } from '../../../api/definitions';
import type { DefinitionProblem, FileMap } from '../../../api/types';
import { plural } from '../../../shared/format';
import { useOrg } from '../../../shared/org';
import { Button } from '../../../ui/button';
import { CodeEditor } from '../yaml/code-editor';
import { type ImportVerdict, importVerdict } from './start-choice';
import { filesFromUpload } from './upload';

const yamlFile = 'factory.yaml';
const checkDelayMs = 600;

export interface ImportResult {
  files: FileMap;
  verdict: ImportVerdict;
}

const textOf = (files: FileMap) => {
  const f = files[yamlFile];
  return f && 'text' in f ? f.text : '';
};

export function ImportPanel({ onResult }: { onResult: (result: ImportResult | undefined) => void }) {
  const org = useOrg();
  const headingId = useId();
  const [files, setFiles] = useState<FileMap>({});
  const [checked, setChecked] = useState<ImportResult>();
  const [uploadError, setUploadError] = useState<string>();
  const latest = useRef(files);
  const report = useRef(onResult);
  report.current = onResult;

  const check = useMutation({
    mutationFn: (f: FileMap) => validateDefinition(org.id, f),
    onSuccess: (validation, f) => {
      if (f !== latest.current) return;
      const result = { files: f, verdict: importVerdict(validation) };
      setChecked(result);
      report.current(result);
    },
  });
  const run = check.mutate;

  const text = textOf(files);
  useEffect(() => {
    latest.current = files;
    setChecked(undefined);
    report.current(undefined);
    if (textOf(files).trim() === '') return;
    const t = setTimeout(() => run(files), checkDelayMs);
    return () => clearTimeout(t);
  }, [files, run]);

  const upload = async (e: ChangeEvent<HTMLInputElement>) => {
    const chosen = [...(e.target.files ?? [])];
    e.target.value = '';
    if (chosen.length === 0) return;
    const uploaded = await Promise.all(
      chosen.map(async (f) => ({ path: f.webkitRelativePath || f.name, bytes: new Uint8Array(await f.arrayBuffer()) })),
    );
    const result = filesFromUpload(uploaded);
    if ('error' in result) {
      setUploadError(result.error);
      return;
    }
    setUploadError(undefined);
    setFiles(result.files);
  };

  const others = Object.keys(files).filter((p) => p !== yamlFile).length;
  const verdict = checked?.verdict;
  const failed = check.error && !check.isPending ? check.error : null;
  const uploadButton =
    'inline-flex h-11 cursor-pointer items-center gap-2 rounded-control border border-line bg-surface px-3.5 text-sm font-medium text-ink transition-colors duration-200 hover:bg-card has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-run md:h-10';

  return (
    <section
      aria-labelledby={headingId}
      className="flex min-w-0 flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 id={headingId} className="m-0 text-xl font-medium tracking-[-0.01em]">
            factory.yaml
          </h2>
          <p className="m-0 mt-1 text-sm text-muted">
            Paste it below, or upload it or its whole folder (up to 500 files, 5 MiB).
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <label className={uploadButton}>
            <FileUp size={16} strokeWidth={1.5} aria-hidden="true" />
            Upload a file
            <input type="file" accept=".yaml,.yml,text/yaml" onChange={(e) => void upload(e)} className="sr-only" />
          </label>
          <label className={uploadButton}>
            <FolderUp size={16} strokeWidth={1.5} aria-hidden="true" />
            Upload a folder
            <input
              type="file"
              {...{ webkitdirectory: '' }}
              multiple
              onChange={(e) => void upload(e)}
              className="sr-only"
            />
          </label>
        </div>
      </div>

      {uploadError ? (
        <p role="alert" className="m-0 rounded-control border border-rework-line bg-rework-tint px-3 py-2.5 text-sm">
          {uploadError}
        </p>
      ) : null}

      <div className="overflow-hidden rounded-control border border-rule bg-surface">
        <CodeEditor
          value={text}
          onChange={(t) => setFiles((prev) => ({ ...prev, [yamlFile]: { text: t } }))}
          language="yaml"
          label="factory.yaml to import"
          minLines={12}
          maxLines={24}
        />
      </div>
      {others > 0 ? (
        <p className="m-0 text-[13px] text-muted">With {plural(others, 'other file')} from the folder.</p>
      ) : null}

      <div className="flex flex-wrap items-center gap-3">
        <Button
          variant="white"
          className="h-11 md:h-10"
          disabled={text.trim() === '' || check.isPending}
          onClick={() => run(files)}
        >
          {check.isPending ? 'Checking…' : 'Check'}
        </Button>
        <p role="status" className="m-0 text-sm text-muted">
          {verdict?.ok ? (
            <span className="inline-flex items-center gap-1.5 text-ok-ink">
              <Check size={16} strokeWidth={1.8} aria-hidden="true" />
              It parses and matches the schema.
            </span>
          ) : verdict ? (
            `${plural(verdict.blocking.length, 'problem')} to fix before you continue.`
          ) : text.trim() === '' ? (
            'Nothing to check yet.'
          ) : null}
        </p>
      </div>

      {failed ? (
        <p role="alert" className="m-0 text-sm text-rework-ink">
          Not checked: {failed instanceof ApiError ? (failed.detail ?? failed.title) : 'goald can’t be reached'}.
        </p>
      ) : null}
      {verdict && verdict.blocking.length > 0 ? (
        <ProblemList title="Fix these before you continue" problems={verdict.blocking} tone="rework" />
      ) : null}
      {verdict?.ok && verdict.later.length > 0 ? (
        <ProblemList title="You can fix these in the next steps" problems={verdict.later} tone="muted" />
      ) : null}
    </section>
  );
}

function ProblemList({
  title,
  problems,
  tone,
}: {
  title: string;
  problems: readonly DefinitionProblem[];
  tone: 'rework' | 'muted';
}) {
  const id = useId();
  return (
    <div className="flex flex-col gap-2">
      <h3 id={id} className={tone === 'rework' ? 'm-0 text-sm font-medium text-rework-ink' : 'm-0 text-sm font-medium'}>
        {title}
      </h3>
      <ul aria-labelledby={id} className="m-0 flex list-none flex-col gap-1.5 p-0 text-sm">
        {problems.map((p) => (
          <li key={`${p.code}:${p.pointer ?? ''}:${p.line}:${p.column}:${p.message}`} className="leading-snug">
            {p.line > 0 ? <span className="font-mono text-[13px] text-muted">Line {p.line}: </span> : null}
            {p.message}
          </li>
        ))}
      </ul>
    </div>
  );
}
