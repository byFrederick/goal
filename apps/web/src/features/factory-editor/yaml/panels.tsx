// The YAML view's panels (screens build/definition): the Problems panel under the editor (B's
// problems with where each is, "Checked as you type, the same checks as publish"), the Outline
// column beside it, and the block a problem gets under its line in Monaco. A quick fix ("Use
// large") is applied from the block under its line, and from the Problems panel on phones, where
// the editor is a textarea with no blocks (frame/problem-fixes.tsx).
import { CircleAlert, Info } from 'lucide-react';
import type { DefinitionProblem } from '../../../api/types';
import { plural } from '../../../shared/format';
import { cn } from '../../../ui/cn';
import { QuickFixes } from '../frame/problem-fixes';
import { usePhoneEditor } from './code-editor';
import { factoryFile } from './files';
import type { OutlineItem } from './outline';

/** "factory.yaml, line 13": where a problem is, in the words the panel and the editor share. */
export function problemLocation(problem: DefinitionProblem): string {
  const file = problem.file ?? factoryFile;
  return problem.line > 0 ? `${file}, line ${problem.line}` : file;
}

/** The fixes a section save could apply; a publish fix (station moves) belongs to Review. */
const fixesOf = (p: DefinitionProblem) => p.fixes.filter((f) => f.section !== 'publish');

function ProblemIcon({ severity }: { severity: DefinitionProblem['severity'] }) {
  return severity === 'info' ? (
    <Info size={14} strokeWidth={1.75} aria-hidden="true" className="mt-px shrink-0 text-run" />
  ) : (
    <CircleAlert size={14} strokeWidth={1.75} aria-hidden="true" className="mt-px shrink-0 text-rework-ink" />
  );
}

const severityText = { error: 'Error', warning: 'Warning', info: 'Note' } as const;

/** Under a line of the editor: its problems, each with its quick fixes. */
export function ProblemBlock({ problems }: { problems: DefinitionProblem[] }) {
  return (
    <div className="flex flex-col gap-2 rounded-control border border-rework-tint bg-card px-3 py-2">
      {problems.map((p) => (
        <div key={`${p.code}${p.pointer ?? ''}${p.message}`} className="flex flex-wrap items-center gap-2">
          <p className="m-0 flex items-center gap-2 text-secondary text-rework-ink">
            <ProblemIcon severity={p.severity} />
            <span>{p.message}</span>
          </p>
          <QuickFixes fixes={fixesOf(p)} dense />
        </div>
      ))}
    </div>
  );
}

interface ProblemsPanelProps {
  problems: readonly DefinitionProblem[];
  /** Moves the cursor to the problem (opening its file), and focus to the editor. */
  onGo: (problem: DefinitionProblem) => void;
  headingId: string;
}

export function ProblemsPanel({ problems, onGo, headingId }: ProblemsPanelProps) {
  // On a phone the editor has no blocks under the lines, so the row carries the quick fixes.
  const phone = usePhoneEditor();
  const counted = problems.filter((p) => p.severity !== 'info').length;
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-2 border-t border-line bg-panel px-4 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <h3 id={headingId} className="m-0 text-secondary font-control">
          Problems
        </h3>
        <span
          className={cn(
            'rounded-chip px-1 text-meta font-medium tabular-nums',
            counted > 0 ? 'bg-rework text-white' : 'bg-hover text-muted',
          )}
        >
          {counted}
        </span>
        <span className="ml-auto text-meta text-muted">Checked as you type, the same checks as publish</span>
      </div>
      {problems.length === 0 ? (
        <p className="m-0 text-secondary text-muted">No problems.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col gap-2 p-0">
          {problems.map((p) => (
            <li key={`${p.code}${p.pointer ?? ''}${p.file ?? ''}${p.line}${p.message}`} className="flex gap-2">
              <ProblemIcon severity={p.severity} />
              <div className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-3 gap-y-1 text-secondary">
                <span className="sr-only">{severityText[p.severity]}: </span>
                <span className="min-w-0 flex-1">{p.message}</span>
                {p.line > 0 ? (
                  <button
                    type="button"
                    onClick={() => onGo(p)}
                    className="text-run underline underline-offset-3 hover:text-run-ink"
                  >
                    {problemLocation(p)}
                  </button>
                ) : (
                  <span className="text-muted">{problemLocation(p)}</span>
                )}
                {phone ? <QuickFixes fixes={fixesOf(p)} /> : null}
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/** Indentation by depth: top-level keys flush, nested ones stepped in. */
const depthIndent = ['pl-2', 'pl-6', 'pl-10'] as const;

/** The Outline column (screens build/definition): the keys of factory.yaml, each a jump to its line. */
export function Outline({ items, onGo }: { items: readonly OutlineItem[]; onGo: (line: number) => void }) {
  return (
    <aside
      aria-label="Outline"
      className="hidden min-w-0 flex-col gap-px border-l border-line bg-shell p-3 @[1024px]:flex"
    >
      <p className="m-0 p-2 text-secondary font-control">Outline</p>
      <ul className="m-0 flex list-none flex-col gap-px p-0">
        {items.map((item) => (
          <li key={item.line}>
            <button
              type="button"
              onClick={() => onGo(item.line)}
              className={cn(
                'flex h-7 w-full min-w-0 items-center gap-2 rounded-chip pr-2 text-left text-secondary text-ink-soft hover:bg-hover hover:text-ink',
                depthIndent[item.depth] ?? 'pl-10',
              )}
            >
              <span className="truncate">{item.key}</span>
              {item.problems > 0 ? (
                <span className="ml-auto shrink-0 text-meta font-control text-rework-ink">
                  {plural(item.problems, 'problem')}
                </span>
              ) : item.count !== undefined ? (
                <span className="ml-auto shrink-0 text-meta text-muted tabular-nums">{item.count}</span>
              ) : null}
            </button>
          </li>
        ))}
      </ul>
    </aside>
  );
}
