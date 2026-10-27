import { type ComponentProps, type ReactNode, useId } from 'react';
import { cn } from './cn';

interface SectionCardProps extends Omit<ComponentProps<'section'>, 'title'> {
  title: ReactNode;
  description?: ReactNode;
  /** Right side of the header: a link, a count or a menu. */
  aside?: ReactNode;
  headingLevel?: 'h2' | 'h3';
}

/** A titled card (the card token, 14px radius) labelled by its heading. */
export function SectionCard({
  title,
  description,
  aside,
  headingLevel: H = 'h2',
  className,
  children,
  ...props
}: SectionCardProps) {
  const id = useId();
  return (
    <section
      aria-labelledby={id}
      className={cn('flex min-w-0 flex-col gap-4 rounded-card border border-line bg-card p-4 md:p-5', className)}
      {...props}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <H id={id} className="m-0 text-base font-medium md:text-lg">
            {title}
          </H>
          {description ? <p className="m-0 mt-1 text-sm text-muted">{description}</p> : null}
        </div>
        {aside}
      </div>
      {children}
    </section>
  );
}

/**
 * Stats on a white panel with hairlines between the cells (a `dl` of `Stat`s): two columns by
 * default, as a card's 2×2 grid; pass `grid-cols-3` for a row of three.
 */
export function StatGrid({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <dl
      className={cn(
        'm-0 grid grid-cols-2 gap-px overflow-hidden rounded-control border border-line bg-line',
        className,
      )}
    >
      {children}
    </dl>
  );
}

/** One cell of a `StatGrid`: its label (`dt`) and the `dd`s the caller passes. */
export function Stat({ label, className, children }: { label: ReactNode; className?: string; children: ReactNode }) {
  return (
    <div className={cn('flex min-w-0 flex-col gap-1 bg-card px-4 py-3', className)}>
      <dt className="text-secondary text-muted">{label}</dt>
      {children}
    </div>
  );
}

/** A label/value row with a top rule, as in the mockups' stat lists. */
export function StatRow({ label, children, className }: { label: ReactNode; children: ReactNode; className?: string }) {
  return (
    <div
      className={cn(
        'flex items-center justify-between gap-3 border-t border-line py-3 text-body tabular-nums',
        className,
      )}
    >
      <span className="text-muted">{label}</span>
      <span className="text-right">{children}</span>
    </div>
  );
}
