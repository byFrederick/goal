import { Link } from '@tanstack/react-router';
import { ExternalLink, Users, Zap } from 'lucide-react';
import type { CSSProperties, KeyboardEvent, PointerEvent, Ref } from 'react';
import type { ItemCard, PartsSummary } from '../../api/types';
import { refParam, useFactory, useFactoryParams } from '../../shared/factory';
import { formatAge, formatUsd, humanize, plural } from '../../shared/format';
import { itemStatus, pullRequestLabel, toneText } from '../../shared/item-state';
import { isMoveShortcut, MoveButton, moveShortcut } from '../../shared/move-menu';
import { holds } from '../../shared/permissions';
import { WaitLine } from '../../shared/waiting-for';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import type { OpenAssembly } from './board-filters';
import { cardState, isExpedite, waitsOn } from './stats';

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
  assemblyParts,
}: {
  item: ItemCard;
  onMove?: (() => void) | undefined;
  drag?: CardDrag | undefined;
  compact?: boolean;
  /** The assembly's parts summary from the board: the pips of the assembly line (desktop cards). */
  assemblyParts?: PartsSummary | undefined;
}) {
  // A decision waits on "you" only for someone who decides here.
  const decides = holds(useFactory(), 'item:decide');
  const state = cardState(item, decides);
  const age = formatAge(item.age_seconds);
  const expedite = isExpedite(item);
  const expediteLabel = expedite ? <ExpediteMark /> : null;

  // A done card has no state line: its summary is on the item page. A wait is one line (its full list
  // is the tooltip); any other state line is two lines at most, so a card stays about as tall as its neighbours.
  const wait = waitsOn(item);
  const stateLine = compact ? (
    item.state === 'done' ? null : wait ? (
      <WaitLine waiting={wait} status={itemStatus(item)} className="text-secondary" />
    ) : (
      <span className={cn('line-clamp-2 text-secondary', toneText[state.tone])}>{state.text}</span>
    )
  ) : (
    <DeskState item={item} state={state} decides={decides} wait={wait} />
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
        'relative flex rounded-control border bg-card transition-[border-color,box-shadow,opacity] duration-200',
        'hover:border-muted has-[a:focus-visible]:outline-2 has-[a:focus-visible]:outline-offset-2 has-[a:focus-visible]:outline-run',
        compact ? 'items-center gap-2 p-3' : 'flex-col gap-2 rounded-cell p-3',
        // The desktop card keeps its border plain: an expedite item is tinted by its column's zone.
        !compact
          ? item.state === 'waiting_human' && decides
            ? 'border-you ring-1 ring-you'
            : 'border-line'
          : expedite
            ? 'border-rework-tint bg-rework-tint'
            : item.in_buffer
              ? 'border-amber-tint bg-amber-tint'
              : item.state === 'waiting_human' && decides
                ? 'border-you-tint'
                : item.state === 'blocked'
                  ? 'border-rework-tint'
                  : 'border-line',
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
          <div className="flex min-w-0 flex-wrap items-center gap-2 text-meta text-muted tabular-nums">
            <span>{item.ref}</span>
            {item.type ? <Chip tone="plain">{humanize(item.type)}</Chip> : null}
            {item.lane ? <Chip tone="plain">{humanize(item.lane)}</Chip> : null}
            <span className="ml-auto whitespace-nowrap">{age}</span>
          </div>
          {expediteLabel}
          <CardTitle item={item} describedBy={drag?.describedBy} movable={Boolean(onMove)} />
          {item.assembly ? <AssemblyLine item={item} parts={assemblyParts} /> : null}
          {item.agent ? <AgentLine item={item} /> : null}
          <div className="flex items-end gap-2">
            <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-2 gap-y-1">
              {stateLine}
              {prLink}
              {touches}
            </div>
            {onMove ? <MoveButton item={item} onMove={onMove} size="lg" className="-mr-1 -mb-1 md:size-8" /> : null}
          </div>
        </>
      )}
    </article>
  );
}

/** The role mark's letters: "Infra engineer" → "IE". Decorative; the role says the same. */
export function roleMark(role: string): string {
  return role
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0])
    .join('')
    .toUpperCase();
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
export function AssemblyLine({
  item,
  parts,
}: {
  item: Pick<ItemCard, 'assembly'>;
  /** The assembly's parts from the board: one pip per part, the merged ones filled (desktop cards). */
  parts?: PartsSummary | undefined;
}) {
  const params = useFactoryParams();
  const assembly = item.assembly;
  if (!assembly) return null;
  return (
    <Link
      to="/o/$org/f/$factory/items/$ref"
      params={{ ...params, ref: refParam(assembly.ref) }}
      draggable={false}
      aria-label={`Part ${assembly.part} of ${assembly.parts} of ${assembly.title}`}
      className={cn(
        'relative z-10 flex min-w-0 items-center no-underline hover:text-run',
        parts ? 'gap-2 text-meta font-medium text-ok-ink' : 'gap-1 text-xs text-muted',
      )}
    >
      <span className="min-w-0 truncate" title={assembly.title}>
        {assembly.title}
      </span>
      {parts ? <PartPips total={parts.total} merged={parts.merged} /> : null}
      <span className="shrink-0 whitespace-nowrap">
        › {assembly.part} of {assembly.parts}
      </span>
    </Link>
  );
}

/** One pip per part of the assembly; the merged parts are filled. Decorative: the count says the same. */
function PartPips({ total, merged }: { total: number; merged: number }) {
  return (
    <span aria-hidden="true" className="flex shrink-0 gap-1">
      {Array.from({ length: total }, (_, i) => (
        <i
          // biome-ignore lint/suspicious/noArrayIndexKey: the pips are fixed slots by position, never reordered
          key={i}
          className={cn('block h-1.5 w-2 rounded-chip', i < merged ? 'bg-ok' : 'bg-ok-tint')}
        />
      ))}
    </span>
  );
}

/** The state line of a desktop card: a chip for a decision or a rework, a pulsing run line, else muted text. */
function DeskState({
  item,
  state,
  decides,
  wait,
}: {
  item: ItemCard;
  state: { text: string; tone: keyof typeof toneText };
  decides: boolean;
  wait: ItemCard['waiting'];
}) {
  if (item.state === 'done') return null;
  if (wait) return <WaitLine waiting={wait} status={itemStatus(item)} className="text-meta" />;
  if (item.state === 'waiting_human' && decides) return <Chip tone="you">{state.text}</Chip>;
  if (state.tone === 'rework') return <Chip tone="rework">{state.text}</Chip>;
  if (item.state === 'in_progress') {
    return (
      <span className="flex min-w-0 items-center gap-2 text-meta text-run-ink">
        <span
          aria-hidden="true"
          className="size-1.5 shrink-0 rounded-full bg-run animate-pulse-dot motion-reduce:animate-none"
        />
        <span className="line-clamp-2">{state.text}</span>
      </span>
    );
  }
  return <span className={cn('line-clamp-2 text-meta', toneText[state.tone])}>{state.text}</span>;
}

/** The agent working the item now: its role mark (the run colour for every role), its name and its cost. */
function AgentLine({ item }: { item: ItemCard }) {
  return (
    <div className="flex min-w-0 items-center gap-2 text-meta text-ink-soft">
      <span
        aria-hidden="true"
        className="flex size-5 shrink-0 items-center justify-center rounded-full bg-run text-meta font-bold text-white"
      >
        {roleMark(item.agent ?? '')}
      </span>
      <span className="min-w-0 truncate">{item.agent}</span>
      <span className="ml-auto shrink-0 whitespace-nowrap text-muted tabular-nums">{formatUsd(item.cost_usd)}</span>
    </div>
  );
}

/**
 * An open assembly's header over its grouped parts: "Migrate billing · 3 of 5 merged", linking to the
 * assembly's item page. It is no card: it counts nowhere and cannot be dragged. The phone's is 44 px tall.
 */
export function AssemblyHeader({ assembly, compact }: { assembly: OpenAssembly; compact: boolean }) {
  const params = useFactoryParams();
  return (
    <h3 className="m-0 flex min-w-0 px-1 text-secondary font-medium">
      <Link
        to="/o/$org/f/$factory/items/$ref"
        params={{ ...params, ref: refParam(assembly.ref) }}
        draggable={false}
        className={cn(
          'flex min-w-0 flex-1 items-center gap-2 rounded-control text-ink no-underline hover:text-run',
          'outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-run',
          compact ? 'min-h-11' : 'min-h-8',
        )}
      >
        <span className="min-w-0 truncate" title={assembly.title}>
          {assembly.title}
        </span>
        <span className="shrink-0 whitespace-nowrap text-muted tabular-nums">
          · {assembly.parts.merged} of {assembly.parts.total} merged
        </span>
      </Link>
    </h3>
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
    <h3 className={cn('m-0 leading-snug font-medium', compact ? 'text-heading' : 'text-secondary')}>
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
