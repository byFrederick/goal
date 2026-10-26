import { ChevronRight } from 'lucide-react';
import { Slot } from 'radix-ui';
import type { ReactElement, ReactNode } from 'react';
import { cn } from './cn';

const tiles = {
  /** Waiting on a person: violet. */
  you: 'bg-you-tint text-you',
  /** Anything else: concrete. */
  plain: 'bg-control text-ink-soft',
} as const;

interface ListRowProps {
  /** The tile's icon (decorative: the title says what the row is). */
  icon: ReactNode;
  tone?: keyof typeof tiles;
  title: ReactNode;
  sub?: ReactNode;
  /** Right of the text from 480 px of room, under it below: a chip, an age. */
  aside?: ReactNode;
  /** The far end: a chevron by default; "Later" for a row that goes nowhere yet. */
  end?: ReactNode;
  /**
   * The row's own element, without children: a router `<Link …/>`, an `<a href …/>`, a
   * `<button …/>`, or a `<span aria-disabled="true"/>`. The row's content goes inside it.
   */
  children: ReactElement;
}

/**
 * A row of a list inside a card (C §10.11): an icon tile, a title with a line under it, an aside
 * and a chevron, with a rule above. The whole row is the element passed as `children`, so a link
 * row is one target. Put rows in an `@container` (the list) so they lay out by its width.
 */
export function ListRow({ icon, tone = 'you', title, sub, aside, end, children }: ListRowProps) {
  return (
    <Slot.Root
      className={cn(
        'grid grid-cols-[36px_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 border-t border-line px-1 py-3 text-ink no-underline transition-colors duration-200',
        "[grid-template-areas:'tile_body_end'_'tile_aside_end']",
        "@[480px]:grid-cols-[36px_minmax(0,1fr)_auto_auto] @[480px]:[grid-template-areas:'tile_body_aside_end']",
        '[a&]:hover:bg-shell [button&]:hover:bg-shell',
      )}
    >
      <span
        aria-hidden="true"
        className={cn('flex size-9 items-center justify-center rounded-control [grid-area:tile]', tiles[tone])}
      >
        {icon}
      </span>
      <span className="min-w-0 [grid-area:body]">
        <span className="block text-[15px] leading-snug font-medium break-words">{title}</span>
        {sub ? <span className="mt-0.5 block text-[13px] leading-snug text-muted">{sub}</span> : null}
      </span>
      {aside ? <span className="flex items-center gap-2 [grid-area:aside]">{aside}</span> : null}
      <span className="flex justify-end text-muted [grid-area:end]">
        {end ?? <ChevronRight size={14} strokeWidth={1.5} aria-hidden="true" />}
      </span>
      <Slot.Slottable>{children}</Slot.Slottable>
    </Slot.Root>
  );
}
