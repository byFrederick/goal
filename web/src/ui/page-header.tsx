import type { ReactNode } from 'react';

/** For the eyebrow above a page's h1 (the organization on a phone): pass a `<p>` with it. */
export const eyebrowClass = 'm-0 text-[13px] text-muted';
/** For the sentence under a page's h1. */
export const descriptionClass = 'm-0 text-[15px] leading-normal text-muted';
/** For the element holding a page's actions. */
export const actionsClass = 'flex flex-wrap gap-2';

/**
 * A page's header (C §10.11): an optional eyebrow, the h1 (34 px on phones, 48 px from 768 px),
 * an optional description, and its actions at the right, wrapping below on narrow screens. The
 * eyebrow, the description and the actions are passed as elements (styled with `eyebrowClass`,
 * `descriptionClass`, `actionsClass`) so a page can show one only on phones or only from 768 px.
 */
export function PageHeader({
  title,
  eyebrow,
  description,
  actions,
}: {
  title: ReactNode;
  eyebrow?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-x-4 gap-y-3">
      <div className="flex min-w-0 flex-col gap-1 md:gap-2">
        {eyebrow}
        <h1 className="m-0 text-[34px] leading-[1.05] font-normal tracking-display md:text-5xl">{title}</h1>
        {description}
      </div>
      {actions}
    </header>
  );
}
