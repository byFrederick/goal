import { type ComponentProps, type ReactNode, useId } from 'react';
import { cn } from './cn';

interface SectionCardProps extends Omit<ComponentProps<'section'>, 'title'> {
  title: ReactNode;
  description?: ReactNode;
  /** Right side of the header: a link, a count or a menu. */
  aside?: ReactNode;
  headingLevel?: 'h2' | 'h3';
}

/** A titled card (card #F6F7F5, 12px radius) labelled by its heading. */
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

/** A label/value row with a top rule, as in the mockups' stat lists. */
export function StatRow({ label, children, className }: { label: ReactNode; children: ReactNode; className?: string }) {
  return (
    <div
      className={cn(
        'flex items-center justify-between gap-3 border-t border-line py-2.5 text-[15px] tabular-nums',
        className,
      )}
    >
      <span className="text-muted">{label}</span>
      <span className="text-right">{children}</span>
    </div>
  );
}
