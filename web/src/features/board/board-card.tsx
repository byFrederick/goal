import { Link } from '@tanstack/react-router';
import { ExternalLink, Zap } from 'lucide-react';
import type { CSSProperties, KeyboardEvent, PointerEvent, Ref } from 'react';
import type { ItemCard } from '../../api/types';
import { refParam, useFactory, useFactoryParams } from '../../shared/factory';
import { formatAge, formatUsd, humanize } from '../../shared/format';
import { pullRequestLabel, toneText } from '../../shared/item-state';
import { isMoveShortcut, MoveButton, moveShortcut } from '../../shared/move-menu';
import { holds } from '../../shared/permissions';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { agentCopy, cardState } from './stats';

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
function moveKeys(onMove: (() => void) | undefined, drag?: CardDrag) {
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

  const stateLine = <span className={cn(compact ? 'text-[13px]' : 'text-xs', toneText[state.tone])}>{state.text}</span>;
  const prLink = item.pr_url ? (
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
  ) : null;

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
        item.in_buffer
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
            <span className="text-xs text-muted tabular-nums">
              {[item.ref, item.type ? humanize(item.type) : null, age].filter(Boolean).join(', ')}
            </span>
            <CardTitle item={item} compact describedBy={drag?.describedBy} movable={Boolean(onMove)} />
            <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1">
              {stateLine}
              {prLink}
            </div>
          </div>
          {onMove ? <MoveButton item={item} onMove={onMove} size="lg" /> : null}
        </>
      ) : (
        <>
          <div className="flex justify-between gap-2 text-xs text-muted tabular-nums">
            <span>{item.ref}</span>
            <span className="whitespace-nowrap">{age}</span>
          </div>
          <CardTitle item={item} describedBy={drag?.describedBy} movable={Boolean(onMove)} />
          <div className="flex flex-wrap items-center gap-1.5">
            {item.type ? <Chip tone="quiet">{humanize(item.type)}</Chip> : null}
            {item.lane ? <Chip tone="quiet">{item.lane}</Chip> : null}
          </div>
          <div className="flex items-end gap-2">
            <div className="flex min-w-0 flex-1 flex-wrap items-center justify-between gap-x-2 gap-y-1">
              {stateLine}
              {prLink}
            </div>
            {onMove ? <MoveButton item={item} onMove={onMove} className="-mr-1 -mb-1" /> : null}
          </div>
        </>
      )}
    </article>
  );
}

function CardTitle({
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
        aria-keyshortcuts={movable ? moveShortcut : undefined}
        aria-describedby={describedBy}
        className="no-underline outline-none after:absolute after:inset-0 after:rounded-control"
      >
        {item.title}
      </Link>
    </h3>
  );
}

/** "investigator-2 on turn 14", "investigator-2", or where the item is when nobody works it. */
function expediteProgress(item: ItemCard): string {
  if (item.state !== 'in_progress' || !item.agent) return `at ${humanize(item.station)}`;
  return item.turn ? `${agentCopy(item)} on turn ${item.turn}` : agentCopy(item);
}

/** An expedited item on a phone, under the station picker: the lane's name, its title, who works it. */
export function ExpediteRow({ item, onMove }: { item: ItemCard; onMove?: (() => void) | undefined }) {
  const params = useFactoryParams();
  const who = item.state === 'in_progress' && item.agent ? agentCopy(item) : humanize(item.station);
  return (
    <article
      data-card={item.id}
      onKeyDown={moveKeys(onMove)}
      className="relative grid grid-cols-[36px_minmax(0,1fr)_auto] items-center gap-3 rounded-card border border-rework-line bg-rework-tint p-3 has-[a:focus-visible]:outline-2 has-[a:focus-visible]:outline-offset-2 has-[a:focus-visible]:outline-run"
    >
      <span
        aria-hidden="true"
        className="flex size-9 items-center justify-center rounded-control bg-surface text-rework"
      >
        <Zap size={16} strokeWidth={1.5} />
      </span>
      <span className="min-w-0">
        <span className="block text-xs font-medium text-rework-ink">Expedite lane</span>
        <Link
          to="/o/$org/f/$factory/items/$ref"
          params={{ ...params, ref: refParam(item.ref) }}
          aria-keyshortcuts={onMove ? moveShortcut : undefined}
          className="block text-[15px] leading-snug font-medium no-underline outline-none after:absolute after:inset-0 after:rounded-card"
        >
          {item.title}
        </Link>
        <span className="block text-[13px] text-muted">
          {item.ref}, {who}, {formatAge(item.age_seconds)}
        </span>
      </span>
      {onMove ? <MoveButton item={item} onMove={onMove} size="lg" /> : null}
    </article>
  );
}

/** An expedited item: it skips the queue, so it rides in its own lane above the columns. */
export function ExpediteCard({ item, onMove }: { item: ItemCard; onMove?: (() => void) | undefined }) {
  const params = useFactoryParams();
  return (
    <article
      data-card={item.id}
      onKeyDown={moveKeys(onMove)}
      className="relative grid grid-cols-[36px_minmax(0,1fr)_auto] items-center gap-3 rounded-control border border-rework-line bg-surface p-3 transition-shadow duration-200 hover:shadow-lift has-[a:focus-visible]:outline-2 has-[a:focus-visible]:outline-offset-2 has-[a:focus-visible]:outline-run md:grid-cols-[36px_minmax(0,1fr)_auto_auto]"
    >
      <span
        aria-hidden="true"
        className="flex size-9 items-center justify-center rounded-control bg-rework-tint text-rework"
      >
        <Zap size={16} strokeWidth={1.5} />
      </span>
      <span className="min-w-0">
        <Link
          to="/o/$org/f/$factory/items/$ref"
          params={{ ...params, ref: refParam(item.ref) }}
          aria-keyshortcuts={onMove ? moveShortcut : undefined}
          className="block text-[15px] leading-snug font-medium no-underline outline-none after:absolute after:inset-0 after:rounded-control"
        >
          {item.title}
        </Link>
        <span className="mt-0.5 block text-[13px] text-muted">
          {item.ref}
          {item.type ? `, ${item.type}` : ''}
          {item.lane ? ` ${item.lane}` : ''}, {expediteProgress(item)}
        </span>
      </span>
      <span className="col-start-2 row-start-2 text-sm whitespace-nowrap text-muted md:col-start-auto md:row-start-auto">
        {formatAge(item.age_seconds)}, {formatUsd(item.cost_usd)}
      </span>
      {onMove ? (
        <MoveButton
          item={item}
          onMove={onMove}
          className="col-start-3 row-start-1 size-11 md:col-start-auto md:row-start-auto md:size-8"
        />
      ) : null}
    </article>
  );
}
