import { Link } from '@tanstack/react-router';
import { ExternalLink, Users, Zap } from 'lucide-react';
import type { CSSProperties, KeyboardEvent, PointerEvent, Ref } from 'react';
import type { ItemCard } from '../../api/types';
import { refParam, useFactory, useFactoryParams } from '../../shared/factory';
import { formatAge, humanize, plural } from '../../shared/format';
import { pullRequestLabel, toneText } from '../../shared/item-state';
import { isMoveShortcut, MoveButton, moveShortcut } from '../../shared/move-menu';
import { holds } from '../../shared/permissions';
import { cn } from '../../ui/cn';
import { cardState, isExpedite } from './stats';

/** What the board's drag and drop attaches to a card (columns only; phones move by the button). */
export interface CardDrag {
  ref: Ref<HTMLElement>;
  onPointerDown?: ((e: PointerEvent) => void) | undefined;
  onKeyDown?: ((e: KeyboardEvent) => void) | undefined;
  style?: CSSProperties;
  dragging: boolean;
  /** The id of the drag instructions for screen readers, on the card's link. */
  describedBy?: string | undefined;
}

/** M on a focused card opens its Move menu; other keys go on to the drag, if any. */
export function moveKeys(onMove: (() => void) | undefined, drag?: CardDrag) {
  if (!onMove) return drag?.onKeyDown;
  return (e: KeyboardEvent) => {
    if (isMoveShortcut(e)) {
      e.preventDefault();
      onMove();
      return;
    }
    drag?.onKeyDown?.(e);
  };
}

/**
 * One work item on the board. The title is the link (stretched over the card); the Move button
 * and the pull request link sit above it so each stays separately focusable. M on the focused card
 * opens its Move menu; in columns the card can be dragged too. `compact` is the phone's list row:
 * the reference, type and age on one line, no chips, and the 44 px Move arrow beside the text.
 * Without `onMove` (the caller may not move work: no item:operate) there is no Move at all.
 * A live expedite item is the same card, tinted rust and labelled "Expedite" (not by color alone).
 */
export function BoardCard({
  item,
  onMove,
  drag,
  compact = false,
}: {
  item: ItemCard;
  onMove?: (() => void) | undefined;
  drag?: CardDrag | undefined;
  compact?: boolean;
}) {
  // A decision waits on "you" only for someone who decides here.
  const decides = holds(useFactory(), 'item:decide');
  const state = cardState(item, decides);
  const age = formatAge(item.age_seconds);
  const expedite = isExpedite(item);
  const expediteLabel = expedite ? <ExpediteMark /> : null;

  // Two lines at most: a done item's summary is an excerpt, and a card stays about as tall as its neighbours.
  const stateLine = (
    <span className={cn('line-clamp-2', compact ? 'text-[13px]' : 'text-xs', toneText[state.tone])}>{state.text}</span>
  );
  const touches = <TouchCount item={item} />;
  const prLink = <PrLink item={item} />;

  return (
    <article
      ref={drag?.ref}
      data-card={item.id}
      onPointerDown={drag?.onPointerDown}
      onKeyDown={moveKeys(onMove, drag)}
      style={drag?.style}
      className={cn(
        'relative flex rounded-control border bg-surface transition-[border-color,box-shadow,opacity] duration-200',
        'hover:border-faint hover:shadow-lift has-[a:focus-visible]:outline-2 has-[a:focus-visible]:outline-offset-2 has-[a:focus-visible]:outline-run',
        compact ? 'items-center gap-2 p-3' : 'flex-col gap-2 p-3',
        expedite
          ? 'border-rework-line bg-rework-tint'
          : item.in_buffer
            ? 'border-amber-line bg-amber-tint'
            : item.state === 'waiting_human' && decides
              ? 'border-you-line'
              : item.state === 'blocked'
                ? 'border-rework-line'
                : 'border-hairline',
        drag?.dragging && 'opacity-40',
      )}
    >
      {compact ? (
        <>
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            {expediteLabel}
            <span className="text-xs text-muted tabular-nums">
              {[item.ref, item.type ? humanize(item.type) : null, age].filter(Boolean).join(', ')}
            </span>
            <CardTitle item={item} compact describedBy={drag?.describedBy} movable={Boolean(onMove)} />
            <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1">
              {stateLine}
              {prLink}
              {touches}
            </div>
          </div>
          {onMove ? <MoveButton item={item} onMove={onMove} size="lg" /> : null}
        </>
      ) : (
        <>
          {expediteLabel}
          <div className="flex justify-between gap-2 text-xs text-muted tabular-nums">
            <span>{item.ref}</span>
            <span className="whitespace-nowrap">{age}</span>
          </div>
          <CardTitle item={item} describedBy={drag?.describedBy} movable={Boolean(onMove)} />
          <div className="flex items-end gap-2">
            <div className="flex min-w-0 flex-1 flex-wrap items-center justify-between gap-x-2 gap-y-1">
              {stateLine}
              {prLink}
              {touches}
            </div>
            {onMove ? <MoveButton item={item} onMove={onMove} className="-mr-1 -mb-1" /> : null}
          </div>
        </>
      )}
    </article>
  );
}

/** The expedite label: text with an icon, so the tint is not the only sign (WCAG 1.4.1). */
export function ExpediteMark() {
  return (
    <span className="inline-flex w-fit items-center gap-1 text-xs font-medium text-rework-ink">
      <Zap size={12} strokeWidth={1.75} aria-hidden="true" />
      Expedite
    </span>
  );
}

/** People who had to act on an item so far (spec 4 §4.9); an item nobody touched says nothing. */
export function TouchCount({ item }: { item: Pick<ItemCard, 'touch_count'> }) {
  if (item.touch_count <= 0) return null;
  return (
    <span className="inline-flex basis-full items-center gap-1 text-xs text-you-ink tabular-nums">
      <Users size={12} strokeWidth={1.75} aria-hidden="true" />
      {plural(item.touch_count, 'touch', 'touches')}
    </span>
  );
}

/** The item's pull request, in a new tab; nothing when it has none. */
export function PrLink({ item }: { item: Pick<ItemCard, 'pr_url'> }) {
  if (!item.pr_url) return null;
  return (
    <a
      href={item.pr_url}
      target="_blank"
      rel="noreferrer"
      className="relative z-10 inline-flex items-center gap-1 text-xs text-ink underline-offset-2 hover:text-run"
    >
      {pullRequestLabel(item.pr_url)}
      <ExternalLink size={12} strokeWidth={1.5} aria-hidden="true" />
      <span className="sr-only">(opens in a new tab)</span>
    </a>
  );
}

/**
 * The assembly a part belongs to, on a lane card: "Migrate billing › 4 of 6", linking to the
 * assembly's item page. The title may shrink to one line; the part count never does. Nothing when
 * the item is no part of an assembly.
 */
export function AssemblyLine({ item }: { item: Pick<ItemCard, 'assembly'> }) {
  const params = useFactoryParams();
  const assembly = item.assembly;
  if (!assembly) return null;
  return (
    <Link
      to="/o/$org/f/$factory/items/$ref"
      params={{ ...params, ref: refParam(assembly.ref) }}
      draggable={false}
      aria-label={`Part ${assembly.part} of ${assembly.parts} of ${assembly.title}`}
      className="relative z-10 flex min-w-0 items-center gap-1 text-xs text-muted no-underline hover:text-run"
    >
      <span className="min-w-0 truncate" title={assembly.title}>
        {assembly.title}
      </span>
      <span className="shrink-0 whitespace-nowrap">
        › {assembly.part} of {assembly.parts}
      </span>
    </Link>
  );
}

export function CardTitle({
  item,
  compact = false,
  describedBy,
  movable,
}: {
  item: ItemCard;
  compact?: boolean;
  describedBy?: string | undefined;
  /** M opens the card's Move menu. */
  movable: boolean;
}) {
  const params = useFactoryParams();
  return (
    <h3 className={cn('m-0 leading-snug font-medium', compact ? 'text-[15px]' : 'text-sm')}>
      <Link
        to="/o/$org/f/$factory/items/$ref"
        params={{ ...params, ref: refParam(item.ref) }}
        draggable={false}
        title={item.title}
        aria-keyshortcuts={movable ? moveShortcut : undefined}
        aria-describedby={describedBy}
        className="line-clamp-2 no-underline outline-none after:absolute after:inset-0 after:rounded-control"
      >
        {item.title}
      </Link>
    </h3>
  );
}
