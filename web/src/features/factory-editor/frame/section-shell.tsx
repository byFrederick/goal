// The card every section renders in (D §10.3): its h2 and description, an optional aside (a
// section's own action, "Add station"), then its body. Moving between sections focuses the h2
// (route-focus.ts follows `data-route-focus` when the page's h1 stays). EditedMark is the
// "Edited · Undo" beside a field; LaterSection stands in for a section a later PR builds.
import { type ReactNode, useId } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../../api/client';
import type { Section } from '../../../shared/definition/model';
import { cn } from '../../../ui/cn';
import { useDraftSession } from '../draft/use-draft';
import { type EditorSection, sectionInfo } from '../sections';
import { useEdited } from './use-editor';

export function SectionShell({
  section,
  description,
  descriptionId,
  aside,
  className,
  children,
}: {
  section: EditorSection;
  /** The sentence under the h2, when the section says more than its standard one. */
  description?: ReactNode;
  /** The description's id, for a control it describes (the Line's reorder instructions). */
  descriptionId?: string;
  /** Right of the heading: the section's own action. */
  aside?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  const id = useId();
  const info = sectionInfo[section];
  return (
    <section
      aria-labelledby={id}
      className={cn('flex min-w-0 flex-col gap-5 rounded-card border border-line bg-card p-4 md:p-5', className)}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2
            id={id}
            data-route-focus=""
            tabIndex={-1}
            className="m-0 text-xl font-medium tracking-[-0.01em] outline-none"
          >
            {info.label}
          </h2>
          <p id={descriptionId} className="m-0 mt-1 max-w-[72ch] text-sm leading-normal text-muted">
            {description ?? info.description}
          </p>
        </div>
        {aside}
      </div>
      {children}
    </section>
  );
}

/** SectionShell's className for a section without a card of its own (its list and detail are the cards). */
export const bareSection = 'gap-3.5 rounded-none border-0 bg-transparent p-0 md:p-0';

/** A section whose editor arrives with a later PR: what to use until then. */
export function LaterSection({ section }: { section: EditorSection }) {
  return (
    <SectionShell section={section}>
      <p className="m-0 rounded-control border border-dashed border-rule px-4 py-3.5 text-sm leading-normal text-muted">
        Editing {sectionInfo[section].label.toLowerCase()} here arrives later. Until then, change it in a working copy:{' '}
        <code className="font-mono text-[13px] text-ink">goal factory pull</code>, edit,{' '}
        <code className="font-mono text-[13px] text-ink">goal factory push</code>.
      </p>
    </SectionShell>
  );
}

/**
 * "Edited · Undo" beside a field the draft changed against the version it is based on: Undo puts
 * the base's value back through goald (B's revert), not the session's undo.
 */
export function EditedMark({ section, pointer, label }: { section: Section; pointer: string; label: string }) {
  const session = useDraftSession();
  const pointers = useEdited(pointer);
  if (pointers.length === 0) return null;

  const undo = () =>
    session.revert(section, pointers).catch((err: unknown) => {
      toast.error(`Not undone: ${err instanceof ApiError ? (err.detail ?? err.title) : 'goald can’t be reached'}`);
    });

  return (
    <span className="inline-flex items-center gap-2 text-[13px] font-normal">
      <span className="inline-flex h-[22px] items-center rounded-chip bg-ok-tint px-[7px] text-xs font-medium text-ok-ink">
        Edited
      </span>
      <button
        type="button"
        onClick={() => void undo()}
        aria-label={`Undo the change to ${label}`}
        className="inline-flex min-h-6 items-center rounded-chip px-1 text-ink underline underline-offset-3 transition-colors duration-200 hover:text-run"
      >
        Undo
      </button>
    </span>
  );
}
