// The board's columns (from 768 px, and the phone's picked station): one column per station, headed by a
// row (name, count, WIP meter and what the station says), then the expedite zone and the standard zone of
// cards. On the desktop each column is a subgrid of the board's rows, so headers, the Expedite boxes and cards line up. The drum's column lists its In progress cards first and the buffer's Ready cards below them, dimmed;
// every column's In progress group ends in "Show N more" past a few cards. The columns have no fixed heights:
// a body starts directly under its header.
import { Zap } from 'lucide-react';
import { Fragment, useState } from 'react';
import type { Board, BoardStation, ItemCard, PartsSummary } from '../../api/types';
import { formatDuration, humanize, plural } from '../../shared/format';
import { waitingOnYou } from '../../shared/item-state';
import { cn } from '../../ui/cn';
import { BoardCard, type CardDrag } from './board-card';
import { DraggableCard, DropColumn } from './board-dnd';
import { columnCount, columnGroups, columnLimit, isExpedite, stationNote, visibleCards } from './stats';

/** One card of the column; dragged on the desktop, moved by its Move button (and M) always. */
function ColumnCard({
  item,
  onMove,
  draggable,
  assemblyParts,
  arrived,
}: {
  item: ItemCard;
  onMove: ((item: ItemCard) => void) | undefined;
  draggable: boolean;
  assemblyParts: PartsSummary | undefined;
  arrived: boolean;
}) {
  const card = (drag: CardDrag | undefined) => (
    <BoardCard
      item={item}
      onMove={onMove && (() => onMove(item))}
      drag={drag}
      assemblyParts={assemblyParts}
      arrived={arrived}
    />
  );
  return draggable ? <DraggableCard item={item}>{card}</DraggableCard> : card(undefined);
}

/** The WIP meter: one 4 px segment per slot, in progress (ink), waiting on you (orange), free (rule). */
function WipMeter({ station }: { station: BoardStation }) {
  const limit = station.wip_limit;
  // No limit (Inbox, Done): an empty row of the meter's height, so every header is as tall as the others.
  if (limit === null) return <span aria-hidden="true" data-meter="reserved" className="block h-1" />;
  const busy = Math.min(station.in_progress, limit);
  const waiting = Math.min(waitingOnYou(station), limit - busy);
  const label = `${station.in_progress} of ${limit} in progress`;
  return (
    <span role="img" aria-label={label} className="flex gap-1">
      {Array.from({ length: limit }, (_, i) => (
        <i
          // biome-ignore lint/suspicious/noArrayIndexKey: the segments are fixed slots by position, never reordered
          key={i}
          aria-hidden="true"
          className={cn(
            'block h-1 flex-1 rounded-chip',
            i < busy ? 'bg-ink' : i < busy + waiting ? 'bg-you' : 'bg-line',
          )}
        />
      ))}
    </span>
  );
}

/** The header card's two meta lines: what the station holds, what it says of its load. */
function meta(station: BoardStation, decides: boolean, finishedThisWeek: number): [string, string] {
  switch (station.kind) {
    case 'queue':
      return [
        station.oldest_waiting_seconds !== null
          ? `Queue, oldest ${formatDuration(station.oldest_waiting_seconds * 1000)}`
          : 'Queue, empty',
        'No limit',
      ];
    case 'done':
      return ['Merged pull requests', `${finishedThisWeek} this week`];
    case 'agent':
      return [plural(station.agents.length, 'agent'), stationNote(station, decides)];
    case 'human':
      return [plural(station.agents.length, 'approver'), stationNote(station, decides)];
  }
}

/** A column's header row: the station's name, the constraint's marker, its count, the WIP meter and the meta line. No card. */
function ColumnHeader({
  station,
  constraint,
  decides,
  finishedThisWeek,
}: {
  station: BoardStation;
  constraint: boolean;
  decides: boolean;
  finishedThisWeek: number;
}) {
  const [left, right] = meta(station, decides, finishedThisWeek);
  return (
    <header className="flex flex-col gap-2 px-1">
      <div className="flex min-h-6 items-center gap-2">
        <h2 className="m-0 min-w-0 truncate text-heading font-semibold" title={humanize(station.id)}>
          {humanize(station.id)}
        </h2>
        {constraint ? (
          <span className="inline-flex items-center gap-2 text-meta font-medium text-ink-soft">
            <i aria-hidden="true" className="block size-1.5 rounded-none bg-amber" />
            Constraint
          </span>
        ) : null}
        <span className="ml-auto shrink-0 whitespace-nowrap text-secondary font-medium text-muted tabular-nums">
          {columnCount(station)}
        </span>
      </div>
      <WipMeter station={station} />
      <div className="flex flex-wrap items-baseline justify-between gap-x-2 text-meta text-muted">
        <span className="min-w-0 max-w-full truncate" title={left}>
          {left}
        </span>
        <span className="min-w-0 max-w-full whitespace-nowrap tabular-nums">{right}</span>
      </div>
    </header>
  );
}

/** A zone's label row, as in the picture: "Ready, buffer 2 of 2" with "Next in", or "In progress". */
function ZoneLabel({ children, aside }: { children: string; aside?: string | undefined }) {
  return (
    <div className="flex items-center justify-between gap-2 px-1 text-meta font-medium text-muted">
      <span className="min-w-0 truncate" title={children}>
        {children}
      </span>
      {aside ? <span className="shrink-0 whitespace-nowrap">{aside}</span> : null}
    </div>
  );
}

/** The lane's label: the lightning icon and "Expedite", and nothing else. */
export function ExpediteLabel({ className }: { className?: string }) {
  return (
    <span className={cn('flex items-center gap-2 px-1 text-secondary font-medium text-amber-ink', className)}>
      <Zap size={14} strokeWidth={1.75} aria-hidden="true" className="shrink-0" />
      Expedite
    </span>
  );
}

export interface StationColumnProps {
  board: Board;
  station: BoardStation;
  /** The expedite zone is drawn (the line has an expedite lane, or an expedite item is on the board). */
  showExpedite: boolean;
  decides: boolean;
  onMove: ((item: ItemCard) => void) | undefined;
  draggable: boolean;
  /** Items the line finished this week: the Done column's "this week" count. */
  finishedThisWeek: number;
  /**
   * The desktop's board grid: the column is a subgrid of its rows (header, expedite, cards), and the expedite
   * lane is the board's one band behind every column. It is the column's 1-based grid column: the band fills
   * its row in every column, so auto-placement would push the columns into new tracks. Without it (the phone)
   * the column draws its own lane.
   */
  inBoard?: number | undefined;
  /** A test for the cards that arrived after the board first had cards. */
  arrived?: ((id: string) => boolean) | undefined;
}

/** One desktop column: its header, its expedite zone, and its standard zone (the drum's split into buffer and work). */
export function StationColumn({
  board,
  station,
  showExpedite,
  decides,
  onMove,
  draggable,
  finishedThisWeek,
  inBoard,
  arrived,
}: StationColumnProps) {
  const constraint = station.id === board.constraint;
  const partsOf = (id: string | undefined) => board.assemblies.find((a) => a.id === id)?.parts ?? undefined;
  const card = (item: ItemCard) => (
    <ColumnCard
      key={item.id}
      item={item}
      onMove={onMove}
      draggable={draggable}
      assemblyParts={partsOf(item.assembly?.id)}
      arrived={arrived?.(item.id) ?? false}
    />
  );

  const expedite = station.items.filter(isExpedite);
  const standardAll = station.items.filter((it) => !isExpedite(it));
  // The inbox's items that may not start yet wait under "Later", soonest first (ADR 0040).
  const later = standardAll
    .filter((it) => it.start)
    .toSorted((a, b) => (a.start?.at ?? '').localeCompare(b.start?.at ?? ''));
  const standard = standardAll.filter((it) => !it.start);
  // The drum's column: In progress cards first, then the buffer's Ready cards, dimmed.
  const isDrum = board.line.release.drum === station.id;
  const [expanded, setExpanded] = useState(false);
  const bufferLabel = `Ready, buffer ${board.line.release.in_buffer}${
    board.line.release.buffer !== null ? ` of ${board.line.release.buffer}` : ''
  }`;

  return (
    <DropColumn
      station={station}
      className={cn('min-w-0', inBoard !== undefined ? 'grid grid-cols-1 grid-rows-subgrid' : 'flex flex-col gap-2')}
      {...(inBoard !== undefined
        ? { style: { gridColumn: inBoard, gridRow: `1 / span ${showExpedite ? 3 : 2}` } }
        : {})}
    >
      <ColumnHeader station={station} constraint={constraint} decides={decides} finishedThisWeek={finishedThisWeek} />
      {showExpedite ? (
        <fieldset
          data-zone="expedite"
          aria-label="Expedite lane"
          className="m-0 flex min-h-11 min-w-0 flex-col gap-2 rounded-cell border-0 bg-amber-tint p-2 transition-colors duration-(--duration-quick)"
        >
          {inBoard === undefined || inBoard === 1 ? <ExpediteLabel /> : null}
          {expedite.map(card)}
        </fieldset>
      ) : null}
      <div className="flex min-h-10 w-full min-w-0 flex-col gap-2 self-start rounded-cell bg-panel p-2">
        {columnGroups(standard, isDrum).map((group) => {
          const shown = group.kind === 'in-progress' ? visibleCards(group.items, expanded) : group.items;
          const hidden = group.items.length - shown.length;
          return (
            <Fragment key={group.kind}>
              {isDrum ? (
                group.kind === 'ready' ? (
                  <ZoneLabel aside="Next in">{bufferLabel}</ZoneLabel>
                ) : (
                  <ZoneLabel>In progress</ZoneLabel>
                )
              ) : null}
              {group.kind === 'ready' ? (
                <div className="flex min-w-0 flex-col gap-2 opacity-70" data-group="ready">
                  {shown.map(card)}
                </div>
              ) : (
                shown.map(card)
              )}
              {group.kind === 'in-progress' && group.items.length > columnLimit ? (
                <button
                  type="button"
                  aria-expanded={expanded}
                  onClick={() => setExpanded((open) => !open)}
                  className="press min-h-11 self-start rounded-chip px-1 text-secondary font-medium text-link hover:text-run-ink md:min-h-6"
                >
                  {expanded ? 'Show fewer' : `Show ${hidden} more`}
                </button>
              ) : null}
            </Fragment>
          );
        })}
        {later.length > 0 ? (
          <section aria-labelledby={`later-${station.id}`} className="flex min-w-0 flex-col gap-2">
            <div className="flex items-center justify-between gap-2 px-1 text-meta font-medium text-muted">
              <h3 id={`later-${station.id}`} className="m-0 text-meta font-medium">
                Later
              </h3>
              <span className="tabular-nums">{later.length}</span>
            </div>
            {later.map(card)}
          </section>
        ) : null}
        {station.items.length === 0 ? <p className="m-0 px-1 py-2 text-meta text-muted md:py-0">Empty</p> : null}
      </div>
    </DropColumn>
  );
}
