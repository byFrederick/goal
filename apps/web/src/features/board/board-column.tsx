// The desktop board's columns (from 768 px): one column per station, headed by its card (name, WIP
// meter and what the station says), then the expedite zone and the standard zone of cards. The
// constraint's column is amber-edged; the drum's column splits its standard zone into the buffer's
// Ready cards, "Next in" first, and the In progress cards. The phone keeps its station tabs (board-page).
//
// Every column's body starts at the same height. Each column spans the same rows of the stations grid
// with CSS subgrid: the header row, then the expedite row (when drawn). So the header cards share one
// height and the zones share one top edge. Each standard zone opens with a sub-header slot, one meta
// line tall (its height is the label's own line height, not a pixel offset): the drum's labels sit in
// it, and the other columns keep it empty, so their first cards line up. The slot and the rows are
// desktop only (md and up); the phone's tabs show one column at a time, and it keeps its own layout.
import type { Board, BoardStation, ItemCard, PartsSummary } from '../../api/types';
import { formatDuration, humanize, plural } from '../../shared/format';
import { waitingOnYou } from '../../shared/item-state';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { BoardCard, type CardDrag } from './board-card';
import { DraggableCard, DropColumn } from './board-dnd';
import { columnCount, isExpedite, stationNote } from './stats';

/** The constraint's edge: an amber border with a 1 px amber ring, as the picture draws it. */
export const constraintEdge = 'border-amber ring-1 ring-amber';

/** One card of the column; dragged on the desktop, moved by its Move button (and M) always. */
function ColumnCard({
  item,
  onMove,
  draggable,
  assemblyParts,
}: {
  item: ItemCard;
  onMove: ((item: ItemCard) => void) | undefined;
  draggable: boolean;
  assemblyParts: PartsSummary | undefined;
}) {
  const card = (drag: CardDrag | undefined) => (
    <BoardCard item={item} onMove={onMove && (() => onMove(item))} drag={drag} assemblyParts={assemblyParts} />
  );
  return draggable ? <DraggableCard item={item}>{card}</DraggableCard> : card(undefined);
}

/** The WIP meter: one segment per slot, in progress (run), waiting on you (you), free (hover). */
function WipMeter({ station }: { station: BoardStation }) {
  const limit = station.wip_limit;
  if (limit === null) return null;
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
            'block h-1.5 flex-1 rounded-chip',
            i < busy ? 'bg-run' : i < busy + waiting ? 'bg-you' : 'bg-hover',
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

/** A column's header card: the station's name, its count, the constraint chip, the WIP meter and the meta lines. */
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
    <header
      className={cn('flex flex-col gap-2 rounded-cell border border-line bg-card p-3', constraint && constraintEdge)}
    >
      <div className="flex items-center gap-2">
        <h2 className="m-0 min-w-0 truncate text-heading font-medium">{humanize(station.id)}</h2>
        {constraint ? <Chip tone="constraint">Constraint</Chip> : null}
        <span className="ml-auto whitespace-nowrap text-secondary font-medium text-ink-soft tabular-nums">
          {columnCount(station)}
        </span>
      </div>
      <WipMeter station={station} />
      <div className="flex items-baseline justify-between gap-2 text-meta text-muted">
        <span className="min-w-0 truncate">{left}</span>
        <span className="shrink-0 whitespace-nowrap tabular-nums">{right}</span>
      </div>
    </header>
  );
}

/** A zone's label row, as in the picture: "Ready, buffer 2 of 2" with "Next in", or "In progress". */
function ZoneLabel({ children, aside }: { children: string; aside?: string | undefined }) {
  return (
    <div className="flex items-center justify-between gap-2 px-1 text-meta font-medium text-muted">
      <span>{children}</span>
      {aside ? <span>{aside}</span> : null}
    </div>
  );
}

/** The sub-header slot a standard zone opens with on the desktop: empty and hidden from the screen readers, one line tall. */
function SubHeaderSlot() {
  return <div aria-hidden="true" className="hidden min-h-[1lh] px-1 text-meta md:block" />;
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
    />
  );

  const expedite = station.items.filter(isExpedite);
  const standard = station.items.filter((it) => !isExpedite(it));
  // The drum's column: the buffer's cards are Ready, the rest are in progress.
  const isDrum = board.line.release.drum === station.id;
  const ready = isDrum ? standard.filter((it) => it.in_buffer) : [];
  const working = isDrum ? standard.filter((it) => !it.in_buffer) : standard;
  const bufferLabel = `Ready, buffer ${board.line.release.in_buffer}${
    board.line.release.buffer !== null ? ` of ${board.line.release.buffer}` : ''
  }`;

  // The column takes the header row and the expedite row (if drawn) and the zone row of the stations grid.
  const rows = showExpedite ? 3 : 2;
  return (
    <DropColumn
      station={station}
      className="flex min-w-0 flex-col gap-2 md:grid md:grid-rows-subgrid"
      style={{ gridRow: `span ${rows} / span ${rows}` }}
    >
      <ColumnHeader station={station} constraint={constraint} decides={decides} finishedThisWeek={finishedThisWeek} />
      {showExpedite ? (
        <div
          className={cn(
            'flex min-h-10 flex-col gap-2 rounded-cell p-2 md:self-start',
            expedite.length > 0 && 'bg-amber-tint',
          )}
        >
          {expedite.map(card)}
        </div>
      ) : null}
      <div className="flex min-h-10 flex-col gap-2 rounded-cell bg-panel p-2 md:self-start">
        {isDrum ? (
          <>
            <ZoneLabel aside="Next in">{bufferLabel}</ZoneLabel>
            {ready.map(card)}
            <ZoneLabel>In progress</ZoneLabel>
          </>
        ) : (
          <SubHeaderSlot />
        )}
        {working.map(card)}
        {station.items.length === 0 ? <p className="m-0 px-1 py-2 text-meta text-muted md:py-0">Empty</p> : null}
      </div>
    </DropColumn>
  );
}
