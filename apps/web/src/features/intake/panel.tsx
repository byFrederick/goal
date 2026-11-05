// One card of the Intake page: its title with an ⓘ for what it is, an aside on the right (a count, a link or a
// control), and its body. Explanations live in the ⓘ, never in a paragraph (MASTER.md).
import type { ReactNode } from 'react';
import { useId } from 'react';
import { InfoTip } from '../../ui/info-tip';

export function Panel({
  title,
  info,
  aside,
  children,
}: {
  title: string;
  /** What the card is, shown behind its ⓘ. */
  info: string;
  aside?: ReactNode;
  children: ReactNode;
}) {
  const id = useId();
  return (
    <section aria-labelledby={id} className="flex min-w-0 flex-col rounded-card border border-line bg-card">
      <header className="flex min-h-12 items-center gap-1 px-5 pt-3 pb-2">
        <h2 id={id} className="m-0 text-heading">
          {title}
        </h2>
        <InfoTip about={title}>{info}</InfoTip>
        {aside ? <div className="ml-auto flex items-center gap-2 text-secondary text-muted">{aside}</div> : null}
      </header>
      {children}
    </section>
  );
}

/** A row of a panel's list: hairline above all but the first. */
export const rowClass =
  'grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 border-t border-line px-5 py-3 first:border-t-0 md:grid-cols-[8rem_minmax(0,1fr)_auto]';
