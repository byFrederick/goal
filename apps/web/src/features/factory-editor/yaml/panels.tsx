// The YAML view's right column (D §10.9): Problems (B's problems with where they are, their fixes
// and Open in Visual; a location moves the cursor there), Changes from the base version (B's
// compare, with line links) and the "Same draft, two views" note; plus the block a problem gets
// under its line in Monaco.
import { Info, Minus, MoveVertical, Plus, TriangleAlert } from 'lucide-react';
import type { Change, Comparison, DefinitionProblem, ProblemFix } from '../../../api/types';
import type { Model } from '../../../shared/definition/model';
import { Button } from '../../../ui/button';
import { SectionCard } from '../../../ui/card';
import { cn } from '../../../ui/cn';
import { problemPlace } from '../frame/state';
import { sectionInfo } from '../sections';
import { linesLabel } from './blocks';
import { factoryFile } from './files';

/** "connections › browser", "line › build", "basics". */
export function problemWhere(problem: DefinitionProblem, model: Model): string {
  const place = problemPlace(problem, model);
  const entity = 'station' in place ? (place.station ?? place.agent ?? place.connection) : undefined;
  const section = problem.pointer || problem.section !== 'files' ? place.section : 'files';
  return entity ? `${section} › ${entity}` : section;
}

/** "Line 26, connections › browser"; a file other than factory.yaml is named. */
export function problemLocation(problem: DefinitionProblem, model: Model): string {
  const file = problem.file && problem.file !== factoryFile ? `${problem.file}, ` : '';
  const line = problem.line > 0 ? `${file}${file ? 'line' : 'Line'} ${problem.line}, ` : file;
  return `${line}${problemWhere(problem, model)}`;
}

/** The fixes a section save can apply here; a publish fix (station moves) belongs to Review. */
const sectionFixes = (p: DefinitionProblem) => p.fixes.filter((f) => f.section !== 'publish');

const severityText = { error: 'Error', warning: 'Warning', info: 'Note' } as const;

function ProblemIcon({ severity }: { severity: DefinitionProblem['severity'] }) {
  return severity === 'info' ? (
    <Info size={16} strokeWidth={1.75} aria-hidden="true" className="mt-0.5 shrink-0 text-run" />
  ) : (
    <TriangleAlert size={16} strokeWidth={1.75} aria-hidden="true" className="mt-0.5 shrink-0 text-rework" />
  );
}

interface ProblemActions {
  onFix: (fix: ProblemFix) => void;
  onVisual: (problem: DefinitionProblem) => void;
}

function Fixes({
  problem,
  onFix,
  onVisual,
  compact,
  pointerOnly,
}: { problem: DefinitionProblem; compact?: boolean; pointerOnly?: boolean } & ProblemActions) {
  // In the editor's blocks the buttons are for the pointer: Monaco hides that layer from screen
  // readers, and the Problems panel offers the same fixes to the keyboard.
  const tab = pointerOnly ? { tabIndex: -1 } : {};
  const fixes = sectionFixes(problem);
  return (
    <div className="flex flex-wrap gap-2">
      {fixes.map((fix) => (
        <Button key={fix.label} size="sm" variant="white" className="h-11 md:h-8" onClick={() => onFix(fix)} {...tab}>
          {fix.label}
        </Button>
      ))}
      {problem.pointer && !compact ? (
        <Button size="sm" variant="secondary" className="h-11 md:h-8" onClick={() => onVisual(problem)} {...tab}>
          Open in Visual
        </Button>
      ) : null}
    </div>
  );
}

/** Under a line of the editor: its problems, their fixes and Open in Visual (board Define-YAML). */
export function ProblemBlock({ problems, ...actions }: { problems: DefinitionProblem[] } & ProblemActions) {
  return (
    <div className="flex flex-col gap-3 rounded-control border border-rework-line bg-surface p-3 shadow-raise">
      {problems.map((p) => (
        <div key={`${p.code}${p.pointer ?? ''}${p.message}`} className="flex flex-col gap-2.5">
          <p className="m-0 flex gap-2 text-sm leading-normal text-rework-ink">
            <ProblemIcon severity={p.severity} />
            <span>
              <span className="font-medium">
                Line {p.line}, {severityText[p.severity].toLowerCase()}:
              </span>{' '}
              {p.message}
            </span>
          </p>
          <Fixes problem={p} {...actions} pointerOnly />
        </div>
      ))}
    </div>
  );
}

interface ProblemsPanelProps extends ProblemActions {
  problems: readonly DefinitionProblem[];
  model: Model;
  /** Moves the cursor to the problem (opening its file), and focus to the editor. */
  onGo: (problem: DefinitionProblem) => void;
}

export function ProblemsPanel({ problems, model, onGo, ...actions }: ProblemsPanelProps) {
  const counted = problems.filter((p) => p.severity !== 'info').length;
  return (
    <SectionCard
      title={
        <>
          Problems <span className={cn('tabular-nums', counted > 0 ? 'text-rework-ink' : 'text-muted')}>{counted}</span>
        </>
      }
      className={cn(counted > 0 && 'border-rework-line bg-rework-tint/40')}
    >
      {problems.length === 0 ? (
        <p className="m-0 text-sm text-muted">No problems. Checked as you type.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col gap-4 p-0">
          {problems.map((p) => (
            <li key={`${p.code}${p.pointer ?? ''}${p.file ?? ''}${p.line}${p.message}`} className="flex gap-2">
              <ProblemIcon severity={p.severity} />
              <div className="flex min-w-0 flex-col gap-2 text-sm leading-normal">
                <span className="sr-only">{severityText[p.severity]}: </span>
                {p.line > 0 ? (
                  <button
                    type="button"
                    onClick={() => onGo(p)}
                    className={cn(
                      'self-start text-left underline underline-offset-3 transition-colors duration-200',
                      p.severity === 'info' ? 'text-ink hover:text-run' : 'text-rework-ink hover:text-rework',
                    )}
                  >
                    {problemLocation(p, model)}
                  </button>
                ) : (
                  <span className="font-medium">{problemLocation(p, model)}</span>
                )}
                <span>{p.message}</span>
                <Fixes problem={p} {...actions} compact={p.line > 0} />
              </div>
            </li>
          ))}
        </ul>
      )}
    </SectionCard>
  );
}

const opIcons = { added: Plus, changed: Plus, removed: Minus, moved: MoveVertical } as const;
const opText = { added: 'Added', changed: 'Changed', removed: 'Removed', moved: 'Moved' } as const;

/** "Connections", "Line", "Files". */
const sectionName = (section: string) =>
  section in sectionInfo ? sectionInfo[section as keyof typeof sectionInfo].label : 'Files';

interface ChangesPanelProps {
  comparison: Comparison | undefined;
  /** "v8"; null for a draft that started from a template, a clone or an import. */
  base: number | null;
  /** Moves the cursor to a line of factory.yaml. */
  onLine: (line: number) => void;
}

export function ChangesPanel({ comparison, base, onLine }: ChangesPanelProps) {
  const changes = comparison?.changes ?? [];
  return (
    <SectionCard title={base !== null ? `Changes from v${base}` : 'Changes since the draft started'}>
      {comparison === undefined ? (
        <p className="m-0 text-sm text-muted">Your first change opens a draft; its changes show here.</p>
      ) : changes.length === 0 ? (
        <p className="m-0 text-sm text-muted">No changes yet.</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {changes.map((c, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: changes have no id; the list is replaced whole
            <ChangeRow key={i} change={c} onLine={onLine} first={i === 0} />
          ))}
        </ul>
      )}
    </SectionCard>
  );
}

function ChangeRow({ change, onLine, first }: { change: Change; onLine: (line: number) => void; first: boolean }) {
  const Icon = opIcons[change.op];
  const at = change.op === 'removed' ? 0 : (change.to_lines[0] ?? 0);
  return (
    <li className={cn('flex gap-2.5 py-3 text-sm leading-normal', !first && 'border-t border-line')}>
      <span
        aria-hidden="true"
        className={cn(
          'mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-chip',
          change.op === 'removed' ? 'bg-rework-tint text-rework-ink' : 'bg-ok-tint text-ok-ink',
        )}
      >
        <Icon size={12} strokeWidth={2} />
      </span>
      <span>
        <span className="sr-only">{opText[change.op]}: </span>
        {at > 0 ? (
          <>
            <button
              type="button"
              onClick={() => onLine(at)}
              className="underline underline-offset-3 transition-colors duration-200 hover:text-run"
            >
              {linesLabel(change.to_lines)}
            </button>
            ,{' '}
          </>
        ) : null}
        {sectionName(change.section)}: {change.summary}
      </span>
    </li>
  );
}

export function TwoViewsNote() {
  return (
    <SectionCard title="Same draft, two views">
      <p className="m-0 text-sm leading-relaxed text-muted">
        Edits here show up in Visual at once, and the other way round. Switching keeps your place: from a line of a
        station you land on that station in Line.
      </p>
      <p className="m-0 text-sm leading-relaxed text-muted">Comments and key order are kept when Visual saves.</p>
    </SectionCard>
  );
}
