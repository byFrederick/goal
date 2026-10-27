import type { ReactNode } from 'react';

/**
 * What a screen shows when it has nothing yet (C §10.11): an icon, a title saying so, what to do
 * about it, and the actions that do it.
 */
export function EmptyState({
  icon,
  title,
  actions,
  children,
}: {
  icon?: ReactNode;
  title: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className="flex max-w-xl flex-col items-start gap-3 rounded-card border border-line bg-card p-5">
      {icon ? (
        <span
          aria-hidden="true"
          className="flex size-10 items-center justify-center rounded-control border border-rule bg-card text-ink-soft"
        >
          {icon}
        </span>
      ) : null}
      <h2 className="m-0 text-xl font-medium">{title}</h2>
      {children ? <div className="text-[15px] leading-relaxed text-muted">{children}</div> : null}
      {actions ? <div className="flex flex-wrap gap-2">{actions}</div> : null}
    </div>
  );
}
