// B's problems at a model pointer with their quick fixes (spec 4 §4.4: "Use claude", "Keep
// aws-prod", "Remove runtime.image"): the message, then a button per fix, which applies the fix's
// patch to the draft as one edit. View only, the messages alone.
import { TriangleAlert } from 'lucide-react';
import type { DefinitionProblem } from '../../../api/types';
import { Button } from '../../../ui/button';
import { cn } from '../../../ui/cn';
import { applyFix } from '../draft/ops';
import { useDraftSession, useViewOnly } from '../draft/use-draft';
import { useProblems } from './use-editor';

export function ProblemFixes({
  pointer,
  exact = false,
  id,
  className,
}: {
  pointer: string;
  /** Only the problems at the pointer itself, not inside it. */
  exact?: boolean;
  /** For the control the problems describe (aria-describedby). */
  id?: string;
  className?: string;
}) {
  const all = useProblems(pointer);
  const problems = exact ? all.filter((p) => p.pointer === pointer) : all;
  if (problems.length === 0) return null;
  return (
    <ul id={id} className={cn('m-0 flex list-none flex-col gap-2 p-0', className)}>
      {problems.map((p) => (
        <ProblemItem key={`${p.code}${p.pointer ?? ''}${p.message}`} problem={p} />
      ))}
    </ul>
  );
}

export function ProblemItem({ problem: p }: { problem: DefinitionProblem }) {
  const session = useDraftSession();
  const viewOnly = useViewOnly();
  const warning = p.severity !== 'error';
  const fix = (index: number) => {
    const f = p.fixes[index];
    if (!f) return;
    const s = session.getSnapshot().sections;
    const model = {
      basics: s.basics.local,
      line: s.line.local,
      agents: s.agents.local,
      connections: s.connections.local,
      review: s.review.local,
    };
    session.edit(applyFix(model, f));
  };
  return (
    <li className="flex flex-col gap-1.5 text-[13px] leading-snug">
      <span className={cn('flex items-start gap-1.5', warning ? 'text-ink-soft' : 'text-rework-ink')}>
        <TriangleAlert size={14} strokeWidth={1.75} aria-hidden="true" className="mt-0.5 shrink-0" />
        <span>
          <span className="sr-only">{warning ? 'Warning: ' : 'Problem: '}</span>
          {p.message}
        </span>
      </span>
      {p.fixes.length > 0 && !viewOnly ? (
        <span className="flex flex-wrap gap-1.5 pl-5">
          {p.fixes.map((f, i) => (
            <Button key={f.label} variant="white" size="sm" className="h-11 md:h-8" onClick={() => fix(i)}>
              {f.label}
            </Button>
          ))}
        </span>
      ) : null}
    </li>
  );
}
