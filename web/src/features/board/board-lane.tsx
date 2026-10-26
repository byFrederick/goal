// One station's body on the board, split into lanes: one lane per agent role at an agent station,
// one person lane at a human station. A lane holds the work its role is doing as cards (in progress,
// or waiting on a person) and the items queued for it as waiting rows. A card names its lane in
// board_lane; one that matches no lane sits in an untitled lane at the foot. A queue or done
// station has no lanes and lists its items as cards, as before.
import type { ReactNode } from 'react';
import type { BoardLane, BoardStation, ItemCard } from '../../api/types';
import { useFactory } from '../../shared/factory';
import { formatAge } from '../../shared/format';
import { itemStatus, toneText } from '../../shared/item-state';
import { MoveButton } from '../../shared/move-menu';
import { holds } from '../../shared/permissions';
import { cn } from '../../ui/cn';
import { StatusIcon } from '../../ui/status-icon';
import {
  AssemblyLine,
  BoardCard,
  type CardDrag,
  CardTitle,
  ExpediteMark,
  moveKeys,
  PrLink,
  TouchCount,
} from './board-card';
import { DraggableCard } from './board-dnd';
import { cardState, isExpedite, type PlanSegment, planSegments } from './stats';

/** Opens the Move menu for an item. */
type OnMove = ((item: ItemCard) => void) | undefined;

/** A card's drag, when the board lets people drag: the card is a draggable only then. */
function Movable({
  item,
  draggable,
  children,
}: {
  item: ItemCard;
  draggable: boolean;
  children: (drag: CardDrag | undefined) => ReactNode;
}) {
  return draggable ? <DraggableCard item={item}>{children}</DraggableCard> : children(undefined);
}

/**
 * A station's items. Without lanes (a queue or done station) they are cards, as before; with lanes
 * they are grouped under their lane, and any item no lane claims at the foot.
 */
export function StationBody({
  station,
  onMove,
  draggable,
  compact = false,
}: {
  station: BoardStation;
  onMove: OnMove;
  /** Cards can be dragged (the desktop columns); the phone moves by the Move button only. */
  draggable: boolean;
  /** The phone's layout: 44 px Move buttons and the list rows, in lanes and without them. */
  compact?: boolean;
}) {
  const lanes = station.lanes ?? [];
  if (lanes.length === 0) {
    return station.items.map((it) => (
      <Movable key={it.id} item={it} draggable={draggable}>
        {(drag) => <BoardCard item={it} onMove={onMove && (() => onMove(it))} drag={drag} compact={compact} />}
      </Movable>
    ));
  }

  const keys = new Set(lanes.map((lane) => lane.key));
  const unclaimed = station.items.filter((it) => !it.board_lane || !keys.has(it.board_lane));
  return (
    <>
      {lanes.map((lane) => (
        <Lane
          key={lane.key}
          lane={lane}
          items={station.items.filter((it) => it.board_lane === lane.key)}
          onMove={onMove}
          draggable={draggable}
          compact={compact}
        />
      ))}
      {unclaimed.length > 0 ? <Lane items={unclaimed} onMove={onMove} draggable={draggable} compact={compact} /> : null}
    </>
  );
}

/** A lane: its header, then its work cards and waiting rows, or "Nothing to do". */
function Lane({
  lane,
  items,
  onMove,
  draggable,
  compact,
}: {
  /** Undefined for the untitled lane of cards no lane claims. */
  lane?: BoardLane;
  items: ItemCard[];
  onMove: OnMove;
  draggable: boolean;
  compact: boolean;
}) {
  const decides = holds(useFactory(), 'item:decide');
  const working = items.filter(
    (it) => it.state === 'in_progress' || (lane?.kind === 'person' && it.state === 'waiting_human'),
  );
  const waiting = items.filter((it) => !working.includes(it));
  return (
    <section
      aria-label={lane?.role}
      className="flex flex-col gap-2 border-t border-line pt-2 first:border-t-0 first:pt-0"
    >
      {lane ? <LaneHeader lane={lane} /> : null}
      {items.length === 0 ? <p className="m-0 px-1 pb-1 text-[13px] text-muted">Nothing to do</p> : null}
      {working.map((it) => (
        <Movable key={it.id} item={it} draggable={draggable}>
          {(drag) => (
            <WorkCard item={it} decides={decides} onMove={onMove && (() => onMove(it))} drag={drag} compact={compact} />
          )}
        </Movable>
      ))}
      {waiting.map((it) => (
        <Movable key={it.id} item={it} draggable={draggable}>
          {(drag) => (
            <WaitingRow
              item={it}
              decides={decides}
              onMove={onMove && (() => onMove(it))}
              drag={drag}
              compact={compact}
            />
          )}
        </Movable>
      ))}
    </section>
  );
}

/** The role mark's letters: "Infra engineer" → "IE". Decorative; the role says the same. */
function roleMark(role: string): string {
  return role
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0])
    .join('')
    .toUpperCase();
}

/** The lane's role mark, role, model and slot boxes (filled: busy). A person lane has no slots. */
function LaneHeader({ lane }: { lane: BoardLane }) {
  const hasSlots = lane.kind === 'agent' && lane.slots !== null;
  return (
    <header className="flex min-w-0 items-center gap-2 px-1">
      <span
        aria-hidden="true"
        className="grid size-7 shrink-0 place-items-center rounded-control border border-rule bg-surface text-[11px] font-medium text-ink-soft"
      >
        {roleMark(lane.role)}
      </span>
      <div className="flex min-w-0 flex-1 flex-col">
        <h3 className="m-0 truncate text-[13px] font-medium" title={lane.role}>
          {lane.role}
        </h3>
        {lane.model_label ? (
          <span className="truncate text-xs text-muted" title={lane.model_label}>
            {lane.model_label}
          </span>
        ) : null}
      </div>
      {hasSlots ? <Slots slots={lane.slots ?? 0} busy={lane.busy} /> : null}
    </header>
  );
}

/** One box a slot: filled while the lane's agent holds it, outlined while free. */
function Slots({ slots, busy }: { slots: number; busy: number }) {
  const filled = Math.min(busy, slots);
  return (
    <span className="flex shrink-0 items-center gap-1">
      <span className="sr-only">
        {filled} of {slots} slots busy
      </span>
      {Array.from({ length: slots }, (_, i) => (
        <span
          // biome-ignore lint/suspicious/noArrayIndexKey: the boxes are fixed by the slot count and never reorder
          key={i}
          aria-hidden="true"
          className={cn('size-2.5 rounded-[2px] border', i < filled ? 'border-run bg-run' : 'border-rule-strong')}
        />
      ))}
    </span>
  );
}

/** A work card: an item being worked (or waiting on a person at a person lane), with its doing line. */
function WorkCard({
  item,
  decides,
  onMove,
  drag,
  compact,
}: {
  item: ItemCard;
  decides: boolean;
  onMove: (() => void) | undefined;
  drag: CardDrag | undefined;
  compact: boolean;
}) {
  const expedite = isExpedite(item);
  const doing = cardState(item, decides);
  return (
    <article
      ref={drag?.ref}
      data-card={item.id}
      onPointerDown={drag?.onPointerDown}
      onKeyDown={moveKeys(onMove, drag)}
      style={drag?.style}
      className={cn(
        'relative flex flex-col gap-2 rounded-control border bg-surface p-3 transition-[border-color,box-shadow,opacity] duration-200',
        'hover:border-faint hover:shadow-lift has-[a:focus-visible]:outline-2 has-[a:focus-visible]:outline-offset-2 has-[a:focus-visible]:outline-run',
        expedite ? 'border-rework-line bg-rework-tint' : 'border-hairline',
        drag?.dragging && 'opacity-40',
      )}
    >
      {expedite ? <ExpediteMark /> : null}
      <div className="flex justify-between gap-2 text-xs text-muted tabular-nums">
        <span>{item.ref}</span>
        <span className="whitespace-nowrap">{formatAge(item.age_seconds)}</span>
      </div>
      <AssemblyLine item={item} />
      <CardTitle item={item} compact={compact} describedBy={drag?.describedBy} movable={Boolean(onMove)} />
      <div className="flex items-end gap-2">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <span className={cn('flex min-w-0 items-center gap-1.5 text-xs', toneText[doing.tone])} title={doing.text}>
            <StatusIcon status="working" />
            <span className="truncate">{doing.text}</span>
          </span>
          <PlanBar plan={item.plan_steps} />
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <PrLink item={item} />
            <TouchCount item={item} />
          </div>
        </div>
        {onMove ? (
          <MoveButton
            item={item}
            onMove={onMove}
            size={compact ? 'lg' : 'sm'}
            className={compact ? '' : '-mr-1 -mb-1'}
          />
        ) : null}
      </div>
    </article>
  );
}

/**
 * A run's plan as one segment per todo, the way its slot boxes read: done segments filled, the one
 * in hand hatched, the rest outlined. Nothing when the run has listed no todos (the API sends null).
 */
function PlanBar({ plan }: { plan: ItemCard['plan_steps'] }) {
  if (!plan) return null;
  const segments = planSegments(plan);
  if (segments.length === 0) return null;
  const done = segments.filter((s) => s === 'done').length;
  return (
    <div role="img" aria-label={`${done} of ${segments.length} steps`} className="flex min-w-0 gap-0.5">
      {segments.map((state, i) => (
        <span
          // biome-ignore lint/suspicious/noArrayIndexKey: the segments are fixed by the todo count and never reorder
          key={i}
          data-state={state}
          className={cn('h-1.5 min-w-0 flex-1 rounded-[2px] border', planTones[state])}
        />
      ))}
    </div>
  );
}

const planTones: Record<PlanSegment, string> = {
  done: 'border-run bg-run',
  current: 'border-run bg-run-hatch',
  todo: 'border-rule-strong bg-surface',
};

/** A waiting row: an item queued for the lane, its title and the reason it waits, with its status icon. */
function WaitingRow({
  item,
  decides,
  onMove,
  drag,
  compact,
}: {
  item: ItemCard;
  decides: boolean;
  onMove: (() => void) | undefined;
  drag: CardDrag | undefined;
  compact: boolean;
}) {
  const reason = cardState(item, decides);
  const status = itemStatus(item);
  const expedite = isExpedite(item);
  return (
    <article
      ref={drag?.ref}
      data-card={item.id}
      onPointerDown={drag?.onPointerDown}
      onKeyDown={moveKeys(onMove, drag)}
      style={drag?.style}
      className={cn(
        'relative flex items-center gap-2 rounded-control border bg-surface px-3 py-2 transition-[border-color,box-shadow,opacity] duration-200',
        'hover:border-faint hover:shadow-lift has-[a:focus-visible]:outline-2 has-[a:focus-visible]:outline-offset-2 has-[a:focus-visible]:outline-run',
        expedite
          ? 'border-rework-line bg-rework-tint'
          : item.state === 'blocked'
            ? 'border-rework-line'
            : 'border-hairline',
        drag?.dragging && 'opacity-40',
      )}
    >
      {expedite ? <ExpediteMark /> : null}
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-xs text-muted tabular-nums">{item.ref}</span>
        <AssemblyLine item={item} />
        <CardTitle item={item} compact={compact} describedBy={drag?.describedBy} movable={Boolean(onMove)} />
        <span className={cn('flex min-w-0 items-center gap-1.5 text-xs', toneText[reason.tone])} title={reason.text}>
          {status ? <StatusIcon status={status} /> : null}
          <span className="truncate">{reason.text}</span>
        </span>
      </div>
      {onMove ? <MoveButton item={item} onMove={onMove} size={compact ? 'lg' : 'sm'} /> : null}
    </article>
  );
}
