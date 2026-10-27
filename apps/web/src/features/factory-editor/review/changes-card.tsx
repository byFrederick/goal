// "Changes from v8" (D §10.10, boards Define-Publish and New-Review): B's comparison of two
// versions, grouped by section (one block per section, each change a line in it), then each changed
// file's unified diff computed here from the two immutable files. Lines say "+" or "−" and, to
// screen readers, Added or Removed.
import { useQuery } from '@tanstack/react-query';
import { versionFileQuery } from '../../../api/queries';
import type { Change, Comparison, FileChange } from '../../../api/types';
import { Chip } from '../../../ui/chip';
import { cn } from '../../../ui/cn';
import { type ComparePair, type Hunk, hunksOf, sectionName } from './review';
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

/** The changes of one section, in the order B lists them. */
function bySection(changes: readonly Change[]): [string, Change[]][] {
  const groups = new Map<string, Change[]>();
  for (const c of changes) groups.set(c.section, [...(groups.get(c.section) ?? []), c]);
  return [...groups];
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
  const changes = comparison?.changes ?? [];

  return (
    <section aria-label={title(comparison, pair, fromName)} className="flex min-w-0 flex-col gap-4">
      {isError ? (
        <p className="m-0 text-secondary text-rework-ink">The comparison could not load.</p>
      ) : !comparison ? (
        <div aria-busy="true">
          <span className="sr-only">Loading</span>
        </div>
      ) : countsOnly ? null : changes.length === 0 && files.length === 0 ? (
        <p className="m-0 text-secondary text-muted">No changes</p>
      ) : (
        <>
          {bySection(changes).map(([section, list]) => (
            <div key={section} className="flex min-w-0 flex-col rounded-card border border-line bg-card">
              <div className="border-b border-line px-4 py-3">
                <Chip tone="plain">{sectionName(section)}</Chip>
              </div>
              <ul className="m-0 flex list-none flex-col p-0">
                {list.map((c, i) => (
                  <li key={c.summary} className={cn('px-4 py-3 text-heading', i > 0 && 'border-t border-line')}>
                    {c.summary}
                  </li>
                ))}
              </ul>
            </div>
          ))}
          {files.map((f) => (
            <div key={f.path} className="flex min-w-0 flex-col rounded-card border border-line bg-card">
              <div className="flex min-w-0 items-center gap-3 border-b border-line px-4 py-3">
                <Chip tone="plain">File</Chip>
                <p className="m-0 min-w-0 truncate font-mono text-secondary">{f.path}</p>
              </div>
              <div className="p-4">
                <FileDiff file={f} pair={pair} changes={changes} />
              </div>
            </div>
          ))}
        </>
      )}
    </section>
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
  if (before.isError || after.isError)
    return <p className="m-0 text-secondary text-rework-ink">This file could not load.</p>;
  if (loading) {
    return (
      <div aria-busy="true">
        <span className="sr-only">Loading {file.path}</span>
      </div>
    );
  }
  const binary = [before.data?.text, after.data?.text].some((t) => t?.includes('\u0000'));
  if (binary) return <p className="m-0 text-secondary text-muted">A binary file {file.op}.</p>;
  const hunks = hunksOf(before.data?.text ?? '', after.data?.text ?? '', file.path === 'factory.yaml' ? changes : []);
  return <DiffView hunks={hunks} label={`Changes to ${file.path}`} />;
}

const mark = { context: ' ', added: '+', removed: '−' } as const;
const spoken = { context: '', added: 'Added: ', removed: 'Removed: ' } as const;

export function DiffView({ hunks, label }: { hunks: Hunk[]; label: string }) {
  if (hunks.length === 0) return <p className="m-0 text-secondary text-muted">No line changes.</p>;
  return (
    <section
      aria-label={label}
      // biome-ignore lint/a11y/noNoninteractiveTabindex: the diff scrolls sideways; the keyboard scrolls it
      tabIndex={0}
      className="overflow-x-auto rounded-control border border-line bg-card py-2 font-mono text-secondary leading-relaxed"
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
