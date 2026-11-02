// A settings group: one flat, always-open section of the page under its title. Its fields wait in the Skeleton until the draft model is adopted, and they
// are read-only for a caller without definition:edit. The footer sits outside that read-only fieldset: a
// control that is not available yet keeps its tooltip reachable by keyboard.
import type { ReactNode } from 'react';
import { Skeleton } from '../../ui/skeleton';
import { useDraft, useViewOnly } from '../factory-editor/draft/use-draft';

interface SettingsGroupProps {
  id: string;
  title: string;
  /** The editable fields. */
  children: ReactNode;
  /** Controls outside the read-only fieldset. */
  footer?: ReactNode;
}

const placeholder = (
  <div className="flex flex-col gap-4">
    <div className="h-4 w-32 rounded-control bg-shell" />
    <div className="h-10 rounded-control bg-shell" />
    <div className="h-10 rounded-control bg-shell" />
  </div>
);

export function SettingsGroup({ id, title, children, footer }: SettingsGroupProps) {
  const ready = useDraft((s) => s.ready);
  const viewOnly = useViewOnly();
  return (
    <section id={id} aria-labelledby={`${id}-title`} className="rounded-card border border-line bg-card">
      <h2 id={`${id}-title`} className="m-0 px-4 pt-4 text-base font-medium md:px-5 md:text-lg">
        {title}
      </h2>
      <div className="@container flex min-w-0 flex-col gap-5 px-4 pt-4 pb-5 md:px-5">
        {/* The fields mount with the adopted draft: the editor's field marks read its edits, which exist only then. */}
        <Skeleton ready={ready} placeholder={placeholder}>
          {ready ? (
            <div className="flex flex-col gap-5">
              <fieldset disabled={viewOnly} className="m-0 flex min-w-0 flex-col gap-5 border-0 p-0">
                {children}
              </fieldset>
              {footer}
            </div>
          ) : (
            <div />
          )}
        </Skeleton>
      </div>
    </section>
  );
}
