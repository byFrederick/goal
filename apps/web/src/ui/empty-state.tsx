import type { ReactNode } from 'react';

/**
 * What a screen shows when it has nothing yet (MASTER.md, Components: Empty states): one line that says what is
 * missing and what to do, and the one action that does it. No box, no illustration, no icon tile. The `icon`
 * prop is still accepted so callers compile, but it is not drawn; the screens that pass one are restyled by their
 * own parts.
 */
export function EmptyState({
  title,
  actions,
  children,
}: {
  /** @deprecated Not drawn: MASTER.md forbids icon tiles in empty states. */
  icon?: ReactNode;
  title: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className="flex max-w-xl flex-col items-start gap-3">
      <h2 className="m-0 text-body font-medium text-ink">{title}</h2>
      {children ? <div className="text-body text-muted">{children}</div> : null}
      {actions ? <div className="flex flex-wrap gap-2">{actions}</div> : null}
    </div>
  );
}
