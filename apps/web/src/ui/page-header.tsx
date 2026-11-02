import type { ReactNode } from 'react';

/** For the sentence under a page's h1. */
export const descriptionClass = 'm-0 text-[15px] leading-normal text-muted';
/** For a section's heading (h2, h3 or legend) on a page: 14 px at 600, from the `--text-heading` token. */
export const sectionHeadingClass = 'm-0 text-heading';

/**
 * A page's header (C §10.11): the h1 (the page-title style at every width),
 * and an optional description, passed as an element (styled with `descriptionClass`) so a page can show it only on phones or only from 768 px. A page's actions sit in the top bar (`TopBarSlot`).
 */
export function PageHeader({ title, description }: { title: ReactNode; description?: ReactNode }) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-x-4 gap-y-3">
      <div className="flex min-w-0 flex-col gap-1 md:gap-2">
        <h1 className="m-0 text-page-title tracking-display">{title}</h1>
        {description}
      </div>
    </header>
  );
}
