import { type ReactNode, useId } from 'react';
import { cn } from '../../ui/cn';

/**
 * One card of the organization settings (canvas "Org-Settings"): a heading over its rows, with the
 * card's border and 14 px corners. `id` is the anchor "On this page" links to.
 */
export function SettingsSection({
  id,
  title,
  tone,
  children,
}: {
  id: string;
  title: string;
  tone?: 'danger';
  children: ReactNode;
}) {
  const headingId = useId();
  return (
    <section
      id={id}
      aria-labelledby={headingId}
      className={cn('overflow-hidden rounded-card border border-rule', tone === 'danger' && 'border-rework/40')}
    >
      <h2 id={headingId} className={cn('m-0 px-5 pt-4 pb-1 text-heading', tone === 'danger' && 'text-rework-ink')}>
        {title}
      </h2>
      <div className="flex min-w-0 flex-col [&>:first-child]:border-t-0">{children}</div>
    </section>
  );
}

/** The ids a row's help and error get, so the control can point at them. */
export const rowIds = (id: string) => ({ help: `${id}-help`, error: `${id}-error` });

/** The `aria-describedby` for a control in a row: its help and its error, when there are any. */
export function describedBy(id: string, parts: { help?: boolean; error?: string | undefined }): string | undefined {
  const ids = [parts.help ? rowIds(id).help : null, parts.error ? rowIds(id).error : null];
  return ids.filter(Boolean).join(' ') || undefined;
}

/**
 * One row of a section: its label and help at the left, the control at the right. Stacked below
 * 768 px. With `id`, the label names the control with that id.
 */
export function SettingsRow({
  id,
  label,
  help,
  error,
  children,
}: {
  id?: string;
  label: string;
  help?: ReactNode;
  error?: string | undefined;
  children: ReactNode;
}) {
  const title = 'text-body font-semibold text-ink';
  return (
    <div className="grid gap-2 border-t border-rule px-5 py-4 md:grid-cols-[minmax(0,220px)_minmax(0,1fr)] md:items-center md:gap-4">
      <div className="flex min-w-0 flex-col gap-1">
        {id ? (
          <label htmlFor={id} className={title}>
            {label}
          </label>
        ) : (
          <span className={title}>{label}</span>
        )}
        {help ? (
          <span id={id ? rowIds(id).help : undefined} className="text-secondary text-muted">
            {help}
          </span>
        ) : null}
      </div>
      <div className="flex min-w-0 flex-col gap-2">
        {children}
        {error && id ? (
          <p id={rowIds(id).error} className="m-0 text-secondary text-rework-ink">
            {error}
          </p>
        ) : null}
      </div>
    </div>
  );
}
