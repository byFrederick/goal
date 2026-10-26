// "Changes from v8" (D §10.10, boards Define-Publish and New-Review): B's comparison of two
// versions, "3 changes in 2 sections", one tab per changed file, and each file's unified diff
// computed here from the two immutable files, its hunks headed by B's change labels. Lines say
// "+" or "−" and, to screen readers, Added or Removed.
import { useQuery } from '@tanstack/react-query';
import { useId, useState } from 'react';
import { versionFileQuery } from '../../../api/queries';
import type { Comparison, FileChange } from '../../../api/types';
import { plural } from '../../../shared/format';
import { SectionCard } from '../../../ui/card';
import { cn } from '../../../ui/cn';
import { type ComparePair, type Hunk, hunksOf } from './review';
import { useComparison, useEditorFacts } from './use-review';

/** "Changes from v8", "Changes from the draft's start", "Changes from v7 to v8". */
function title(comparison: Comparison | undefined, pair: ComparePair, fromName?: string): string {
  if (fromName) return `Changes from ${fromName}`;
  const from = comparison?.from.number;
  const to = comparison?.to.number;
  const fromText = from ? `v${from}` : pair.from === 'source' ? 'where the draft started' : pair.from;
  if (pair.to === 'draft') return `Changes from ${fromText}`;
  return `Changes from ${fromText} to ${to ? `v${to}` : pair.to}`;
}

export function ChangesCard({
  pair,
  fromName,
  countsOnly = false,
}: {
  pair: ComparePair;
  /** How to name the starting side ("the Cost review template"). */
  fromName?: string | undefined;
  /** A blank start: the counts, no diff. */
  countsOnly?: boolean;
}) {
  const { data: comparison, isError } = useComparison(pair);
  const files = (comparison?.files ?? []).filter((f) => f.op !== 'unchanged');
  const [chosen, setChosen] = useState<string | undefined>();
  const open = files.find((f) => f.path === chosen) ?? files[0];
  const id = useId();
  const counts = comparison?.counts;

  return (
    <SectionCard
      title={title(comparison, pair, fromName)}
      aside={
        counts ? (
          <span className="pt-1 text-sm text-muted">
            {counts.changes === 0
              ? 'No changes'
              : `${plural(counts.changes, 'change')} in ${plural(Math.max(1, counts.sections), 'section')}`}
          </span>
        ) : null
      }
    >
      {isError ? (
        <p className="m-0 text-sm text-rework-ink">The comparison could not load.</p>
      ) : !comparison ? (
        <div aria-busy="true" className="h-40 rounded-control bg-control/60">
          <span className="sr-only">Loading the changes</span>
        </div>
      ) : files.length === 0 || countsOnly ? null : (
        <div className="flex min-w-0 flex-col gap-3">
          {files.length > 1 ? (
            <div role="tablist" aria-label="Changed files" className="-mb-px flex overflow-x-auto border-b border-line">
              {files.map((f) => (
                <button
                  key={f.path}
                  type="button"
                  role="tab"
                  id={`${id}-${f.path}`}
                  aria-selected={f === open}
                  aria-controls={`${id}-panel`}
                  tabIndex={f === open ? 0 : -1}
                  onClick={() => setChosen(f.path)}
                  className={cn(
                    'h-11 shrink-0 border-b-2 px-3 font-mono text-[13px] whitespace-nowrap transition-colors duration-200 md:h-10',
                    f === open ? 'border-ink text-ink' : 'border-transparent text-muted hover:text-ink',
                  )}
                >
                  {f.path}
                </button>
              ))}
            </div>
          ) : (
            <p className="m-0 border-b-2 border-ink pb-2 font-mono text-[13px] self-start">{open?.path}</p>
          )}
          {open ? (
            <div
              id={`${id}-panel`}
              {...(files.length > 1 ? { role: 'tabpanel', 'aria-labelledby': `${id}-${open.path}` } : {})}
            >
              <FileDiff file={open} pair={pair} changes={comparison.changes} />
            </div>
          ) : null}
        </div>
      )}
    </SectionCard>
  );
}

function FileDiff({ file, pair, changes }: { file: FileChange; pair: ComparePair; changes: Comparison['changes'] }) {
  const { org, factory } = useEditorFacts();
  const before = useQuery({
    ...versionFileQuery(org, factory, pair.from, file.path, file.from_sha256 ?? ''),
    enabled: Boolean(file.from_sha256),
  });
  const after = useQuery({
    ...versionFileQuery(org, factory, pair.to, file.path, file.to_sha256 ?? ''),
    enabled: Boolean(file.to_sha256),
  });
  const loading = (file.from_sha256 && !before.data) || (file.to_sha256 && !after.data);
  if (before.isError || after.isError) return <p className="m-0 text-sm text-rework-ink">This file could not load.</p>;
  if (loading) {
    return (
      <div aria-busy="true" className="h-32 rounded-control bg-control/60">
        <span className="sr-only">Loading {file.path}</span>
      </div>
    );
  }
  const binary = [before.data?.text, after.data?.text].some((t) => t?.includes('\u0000'));
  if (binary) return <p className="m-0 text-sm text-muted">A binary file {file.op}.</p>;
  const hunks = hunksOf(before.data?.text ?? '', after.data?.text ?? '', file.path === 'factory.yaml' ? changes : []);
  return <DiffView hunks={hunks} label={`Changes to ${file.path}`} />;
}

const mark = { context: ' ', added: '+', removed: '−' } as const;
const spoken = { context: '', added: 'Added: ', removed: 'Removed: ' } as const;

export function DiffView({ hunks, label }: { hunks: Hunk[]; label: string }) {
  if (hunks.length === 0) return <p className="m-0 text-sm text-muted">No line changes.</p>;
  return (
    <section
      aria-label={label}
      // biome-ignore lint/a11y/noNoninteractiveTabindex: the diff scrolls sideways; the keyboard scrolls it
      tabIndex={0}
      className="overflow-x-auto rounded-control border border-line bg-surface py-1.5 font-mono text-[13px] leading-[1.75]"
    >
      <table className="w-full border-collapse">
        {hunks.map((h, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: hunks are replaced whole
          <tbody key={i}>
            {h.header ? (
              <tr>
                <td aria-hidden="true" className="w-10" />
                <td aria-hidden="true" className="w-5" />
                <th scope="rowgroup" className="pr-3 text-left font-normal whitespace-pre text-muted">
                  @@ {h.header} @@
                </th>
              </tr>
            ) : i > 0 ? (
              <tr>
                <td colSpan={3} className="h-2 border-t border-dashed border-line" />
              </tr>
            ) : null}
            {h.lines.map((l, j) => (
              <tr
                // biome-ignore lint/suspicious/noArrayIndexKey: a line's place in its hunk
                key={j}
                className={cn(l.kind === 'added' && 'bg-ok-tint', l.kind === 'removed' && 'bg-rework-tint')}
              >
                <td className="w-10 pr-2 text-right align-top text-muted select-none">{l.number ?? ''}</td>
                <td
                  aria-hidden="true"
                  className={cn(
                    'w-5 align-top select-none',
                    l.kind === 'added' && 'text-ok-ink',
                    l.kind === 'removed' && 'text-rework-ink',
                  )}
                >
                  {mark[l.kind]}
                </td>
                <td className="pr-3 align-top whitespace-pre">
                  <span className="sr-only">{spoken[l.kind]}</span>
                  {l.text || ' '}
                </td>
              </tr>
            ))}
          </tbody>
        ))}
      </table>
    </section>
  );
}
