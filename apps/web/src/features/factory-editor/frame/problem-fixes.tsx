// B's problems at a model pointer with their quick fixes (spec 4 §4.4: "Use claude", "Keep
// aws-prod", "Remove runtime.image"): the message, then a button per fix, which applies the fix's
// patch to the draft as one edit. View only, the messages alone. The YAML view (yaml/panels.tsx)
// shows the same buttons through QuickFixes and useApplyFix, so both places apply a fix one way.
import { TriangleAlert } from 'lucide-react';
import { useState } from 'react';
import { ApiError } from '../../../api/client';
import type { DefinitionProblem, ProblemFix } from '../../../api/types';
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

/** Why a fix did not apply, in the words the editor uses for a refused save. */
function refusal(err: unknown): string {
  return err instanceof ApiError ? (err.detail ?? err.title) : 'goald can’t be reached';
}

/**
 * Applies a quick fix to the draft. What is typed is saved first, so the fix patches what goald
 * holds; then the fix's patch is applied to the sections' saved values (ops.ts). `pending` is the
 * fix being applied, and `error` says why the last one did not apply; the draft is then unchanged.
 */
export function useApplyFix() {
  const session = useDraftSession();
  const [pending, setPending] = useState<ProblemFix | null>(null);
  const [error, setError] = useState<string | null>(null);
  const apply = async (fix: ProblemFix) => {
    setPending(fix);
    setError(null);
    try {
      await session.flush();
      const refused = session.getSnapshot().error;
      if (refused) {
        setError(refusal(refused));
        return;
      }
      const s = session.getSnapshot().sections;
      const model = {
        basics: s.basics.local,
        line: s.line.local,
        agents: s.agents.local,
        connections: s.connections.local,
        review: s.review.local,
      };
      session.edit(applyFix(model, fix));
    } catch (err) {
      setError(refusal(err));
    } finally {
      setPending(null);
    }
  };
  return { apply, pending, error };
}

/**
 * A problem's quick fixes as buttons, with the reason the last one did not apply. Nothing without
 * fixes, and nothing in view only. `dense` is the line block's size; elsewhere a touch target.
 */
export function QuickFixes({
  fixes,
  dense = false,
  className,
}: {
  fixes: ProblemFix[];
  dense?: boolean;
  className?: string;
}) {
  const viewOnly = useViewOnly();
  const { apply, pending, error } = useApplyFix();
  if (fixes.length === 0 || viewOnly) return null;
  return (
    <span className={cn('flex flex-wrap items-center gap-2', className)}>
      {fixes.map((f) => (
        <Button
          key={f.label}
          variant="white"
          size="sm"
          className={dense ? 'h-6 px-2 text-meta' : 'h-11 md:h-8'}
          disabled={pending !== null}
          aria-busy={pending === f ? true : undefined}
          onClick={() => void apply(f)}
        >
          {f.label}
        </Button>
      ))}
      {error ? (
        <span role="alert" className="text-meta text-rework-ink">
          {error}
        </span>
      ) : null}
    </span>
  );
}

export function ProblemItem({ problem: p }: { problem: DefinitionProblem }) {
  const warning = p.severity !== 'error';
  return (
    <li className="flex flex-col gap-2 text-secondary leading-snug">
      <span className={cn('flex items-start gap-2', warning ? 'text-ink-soft' : 'text-rework-ink')}>
        <TriangleAlert size={14} strokeWidth={1.75} aria-hidden="true" className="shrink-0" />
        <span>
          <span className="sr-only">{warning ? 'Warning: ' : 'Problem: '}</span>
          {p.message}
        </span>
      </span>
      <QuickFixes fixes={p.fixes} className="pl-5" />
    </li>
  );
}
