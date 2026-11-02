import type { ReactNode } from 'react';

/** For the sentence under a page's h1. */
export const descriptionClass = 'm-0 text-[15px] leading-normal text-muted';
/** For a section's heading (h2, h3 or legend) on a page: 14 px at 600, from the `--text-heading` token. */
export const sectionHeadingClass = 'm-0 text-heading';
/** For the element holding a page's actions. */
export const actionsClass = 'flex flex-wrap gap-2';

/**
 * A page's header (C §10.11): the h1 (the page-title style at every width),
 * an optional description, and its actions at the right, wrapping below on narrow screens. The
 * description and the actions are passed as elements (styled with `descriptionClass`, `actionsClass`) so a page can show one only on phones or only from 768 px.
 */
export function PageHeader({
  title,
  description,
  actions,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-x-4 gap-y-3">
      <div className="flex min-w-0 flex-col gap-1 md:gap-2">
        <h1 className="m-0 text-page-title tracking-display">{title}</h1>
        {description}
      </div>
      {actions}
    </header>
  );
}
