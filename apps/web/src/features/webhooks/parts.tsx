// The parts the Webhooks screens share: a factory's chip, the card frame, the status chip and the skeletons.
import type { ReactNode } from 'react';
import type { Factory } from '../../api/types';
import { FactoryBadge } from '../../app/factory-badge';
import { factoryTitle } from '../../shared/factory';
import { factoryMark } from '../../shared/marks';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { InfoTip } from '../../ui/info-tip';
import type { WebhookStatus } from './words';

export const cardClass = 'flex min-w-0 flex-col rounded-card border border-line bg-card';

/** A card with its heading, an optional ⓘ and an optional action at the far end. */
export function Card({
  title,
  tip,
  aside,
  children,
  className,
}: {
  title: string;
  tip?: { about: string; text: string } | undefined;
  aside?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section aria-label={title} className={cn(cardClass, className)}>
      <header className="flex min-h-12 items-center gap-1 border-b border-line px-4 py-2">
        <h2 className="m-0 text-heading">{title}</h2>
        {tip ? <InfoTip about={tip.about}>{tip.text}</InfoTip> : null}
        {aside ? <div className="ms-auto flex items-center gap-2 text-secondary text-muted">{aside}</div> : null}
      </header>
      {children}
    </section>
  );
}

/** A factory by its letter badge and its name; the opening's id may name a factory that is gone, which shows nothing. */
export function FactoryChip({ id, factories, name }: { id: string; factories: readonly Factory[]; name?: string }) {
  const factory = factories.find((f) => f.id === id);
  if (!factory) return name ? <span className="truncate text-secondary text-ink">{name}</span> : null;
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5 text-secondary text-ink">
      <FactoryBadge mark={factoryMark(factory, factories)} />
      <span className="truncate">{factoryTitle(factory)}</span>
    </span>
  );
}

export function StatusChip({ status }: { status: WebhookStatus }) {
  return (
    <Chip tone="quiet" dot className="gap-1.5">
      {status.label}
    </Chip>
  );
}

const block = 'rounded-control bg-hover';

/** The list's placeholder: the heading, and a card of rows. */
export function WebhooksShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <div className={cn(block, 'h-7 w-40 max-w-full')} />
      <div className={cn(cardClass, 'p-0')}>
        <div className="border-b border-line px-4 py-3">
          <div className={cn(block, 'h-5 w-28')} />
        </div>
        {['a', 'b', 'c', 'd'].map((key) => (
          <div key={key} className="flex items-center gap-4 border-t border-line px-4 py-3 first:border-t-0">
            <div className={cn(block, 'h-9 w-64 max-w-full')} />
            <div className={cn(block, 'ms-auto h-5.5 w-32 max-md:hidden')} />
            <div className={cn(block, 'h-5.5 w-24')} />
          </div>
        ))}
      </div>
    </div>
  );
}

/** One webhook's placeholder: the title, then the Connection and Deliveries cards beside Open to and Set it up. */
export function WebhookShape() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-5">
      <div className={cn(block, 'h-7 w-56 max-w-full')} />
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <div className="flex min-w-0 flex-col gap-5">
          <div className={cn(cardClass, 'gap-3 p-4')}>
            <div className={cn(block, 'h-4 w-24')} />
            <div className={cn(block, 'h-8 w-full')} />
            <div className={cn(block, 'h-5 w-64 max-w-full')} />
            <div className={cn(block, 'h-5 w-48 max-w-full')} />
          </div>
          <div className={cn(cardClass, 'gap-3 p-4')}>
            <div className={cn(block, 'h-4 w-24')} />
            <div className={cn(block, 'h-12 w-full')} />
            <div className={cn(block, 'h-12 w-full')} />
            <div className={cn(block, 'h-12 w-full')} />
          </div>
        </div>
        <div className="flex min-w-0 flex-col gap-5">
          <div className={cn(cardClass, 'gap-3 p-4')}>
            <div className={cn(block, 'h-4 w-24')} />
            <div className={cn(block, 'h-8 w-full')} />
            <div className={cn(block, 'h-8 w-full')} />
          </div>
          <div className={cn(cardClass, 'gap-3 p-4')}>
            <div className={cn(block, 'h-4 w-40')} />
            <div className={cn(block, 'h-5 w-full')} />
            <div className={cn(block, 'h-5 w-3/4')} />
          </div>
        </div>
      </div>
    </div>
  );
}
