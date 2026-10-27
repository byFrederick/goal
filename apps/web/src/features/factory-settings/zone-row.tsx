// One row of the Danger zone: what the action is and does on the left, the action at the right, and the
// row's own error under it. Rows stack on phones and sit in three columns from 768 px.
import type { ReactNode } from 'react';

export function ZoneRow({
  title,
  description,
  descriptionId,
  action,
  error,
}: {
  title: string;
  description: ReactNode;
  descriptionId?: string;
  action: ReactNode;
  error?: string | null;
}) {
  return (
    <div className="flex flex-col gap-2 border-t border-line pt-4 md:grid md:grid-cols-[200px_minmax(0,1fr)_auto] md:items-center md:gap-x-5">
      <span className="font-control text-body">{title}</span>
      <span id={descriptionId} className="text-secondary text-muted">
        {description}
      </span>
      <div>{action}</div>
      {error ? (
        <p role="alert" className="col-span-full m-0 text-secondary text-rework-ink">
          {error}
        </p>
      ) : null}
    </div>
  );
}
