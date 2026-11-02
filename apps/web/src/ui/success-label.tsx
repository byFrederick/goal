// A button's label that swaps to a check once its action succeeded (MOTION.md, Approve, publish, merge succeed):
// the check grows in and the past-tense label rises in beside it. Set `done` only after the call resolved, never
// optimistically. Under reduced motion both appear without animation.
import { Check } from 'lucide-react';
import type { ReactNode } from 'react';

/** How long a row, card or dialog stays in view after its action succeeded: the swap, then a beat to read it. */
export const successHoldMs = 900;

export function SuccessLabel({
  done,
  doneLabel,
  children,
}: {
  done: boolean;
  /** The past-tense label shown beside the check: "Approved", "Merged". */
  doneLabel: string;
  /** The idle label. */
  children: ReactNode;
}) {
  if (!done) return <>{children}</>;
  return (
    <>
      <Check size={16} strokeWidth={2} aria-hidden="true" className="motion-safe:animate-success-check" />
      <span key="done" role="status" className="motion-safe:animate-text-states-swap">
        {doneLabel}
      </span>
    </>
  );
}
