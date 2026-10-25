import { useQuery, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { Users } from 'lucide-react';
import { Tabs } from 'radix-ui';
import { useEffect, useRef, useState } from 'react';
import { boardQuery, touchesQuery } from '../../api/queries';
import type { Board, BoardStation, ItemCard, TouchHeadline } from '../../api/types';
import { AndonButton, LineStatus } from '../../shared/andon';
import { factoryTitle, itemKey, useFactory, useFactoryParams } from '../../shared/factory';
import { formatPercent, humanize } from '../../shared/format';
import { waitingOnYou } from '../../shared/item-state';
import { MoveDialog } from '../../shared/move-menu';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { factoryRoleNote, holds, reasons } from '../../shared/permissions';
import { perItem, stationRates } from '../../shared/touches';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { Meter } from '../../ui/hatch';
import { BoardCard, ExpediteCard, ExpediteRow } from './board-card';
import { CardDnd, DraggableCard, DropColumn } from './board-dnd';
import {
  budgetNote,
  columnCount,
  constraintNote,
  expediteUse,
  firstStation,
  lineSentence,
  stationMeter,
  stationNote,
  stationStat,
  wipText,
} from './stats';

/** Opens the Move menu for a card, with the station a drop chose; undefined: the caller may not move work. */
type OnMove = ((item: ItemCard, to?: string) => void) | undefined;

/** A card's Move, bound to the card. */
const moveOf = (onMove: OnMove, item: ItemCard) => (onMove ? () => onMove(item) : undefined);

const live = (it: ItemCard) => it.state !== 'done' && it.state !== 'cancelled';
const isExpedite = (it: ItemCard) => it.class_of_service === 'expedite' && live(it);

const constraintRing = 'border-amber shadow-[inset_0_3px_0_var(--color-amber)]';

/** A station's touches per finished item over the board's week; null when no item finished. */
type RateOf = (station: BoardStation) => number | null;

/** "0.38 touches per item" for a station, or nothing when no item finished. */
function stationTouches(rateOf: RateOf, s: BoardStation): string | null {
  const rate = rateOf(s);
  return rate === null ? null : `${perItem(rate, 2)} touches per item`;
}

function LineCard({ board, decides, rateOf }: { board: Board; decides: boolean; rateOf: RateOf }) {
  const sentence = lineSentence(board.line);
  const budget = budgetNote(board.line);

  return (
    <SectionCard
      title="The line"
      description={
        <>
          {sentence.text}
          {sentence.notEnforced ? (
            <>
              {' '}
              <Chip tone="quiet" className="align-[1px] text-muted">
                Not enforced yet
              </Chip>
            </>
          ) : null}
          {board.line.touches.finished > 0 ? (
            <span className="block">Touches per finished item show where people step in.</span>
          ) : null}
        </>
      }
      aside={<span className="text-sm whitespace-nowrap text-muted">{wipText(board.line)}</span>}
    >
      {budget ? <p className="m-0 -mt-1 text-sm text-rework-ink">{budget}</p> : null}
      {/* biome-ignore lint/a11y/noNoninteractiveTabindex: a scroll container must take focus so keyboards can scroll it (axe scrollable-region-focusable) */}
      <section tabIndex={0} aria-label="Stations in line order" className="-mx-1 overflow-x-auto px-1 pb-1">
        <ol
          className="m-0 grid list-none gap-2 p-0"
          style={{ gridTemplateColumns: `repeat(${board.stations.length}, minmax(132px, 1fr))` }}
        >
          {board.stations.map((s) => {
            const drum = s.id === board.constraint;
            return (
              <li
                key={s.id}
                className={cn(
                  'flex flex-col gap-2 rounded-control border border-hairline bg-surface p-3',
                  drum && constraintRing,
                )}
              >
                <div className="flex items-center justify-between gap-1.5">
                  <span className="truncate text-[15px] font-medium">{humanize(s.id)}</span>
                  {drum ? <Chip tone="constraint">Constraint</Chip> : null}
                </div>
                <Meter className="bg-shell" {...stationMeter(s)} />
                <span className={cn('text-sm', decides && waitingOnYou(s) > 0 && 'text-you')}>{stationStat(s)}</span>
                <span className="text-[13px] leading-snug text-muted">{stationNote(s, decides)}</span>
                <StationTouches rateOf={rateOf} station={s} />
              </li>
            );
          })}
        </ol>
      </section>
    </SectionCard>
  );
}

function StationTouches({ rateOf, station }: { rateOf: RateOf; station: BoardStation }) {
  const rate = rateOf(station);
  if (rate === null) return null;
  return (
    <span className="mt-auto border-t border-line pt-1.5 text-[13px] tabular-nums">
      <strong className="font-medium">{perItem(rate, 2)}</strong> touches per item
    </span>
  );
}

/** From 768 px, in the header: the week's touches per finished item, linking to the Floor's panel. */
function TouchesChip({ touches }: { touches: TouchHeadline }) {
  const params = useFactoryParams();
  return (
    <Link
      to="/o/$org/f/$factory"
      params={params}
      hash="touches"
      className="hidden h-10 items-center gap-2 rounded-control bg-surface px-3.5 text-sm text-ink no-underline shadow-raise transition-shadow duration-200 hover:shadow-lift md:inline-flex"
    >
      <Users size={16} strokeWidth={1.5} aria-hidden="true" />
      {touches.finished === 0 ? (
        <span>No items finished this week</span>
      ) : (
        <>
          <span className="tabular-nums">
            <strong className="font-medium">{perItem(touches.per_item_mean)}</strong> touches per item
            <span className="sr-only">, </span>
          </span>
          <span className="text-muted tabular-nums">{formatPercent(touches.touchless_rate)} touchless</span>
        </>
      )}
    </Link>
  );
}

/** Phones, above the picked station: the same figures and a link to where they happen. */
function PhoneTouches({ touches }: { touches: TouchHeadline }) {
  const params = useFactoryParams();
  if (touches.finished === 0) return null;
  return (
    <p className="m-0 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-ink-soft tabular-nums">
      <span>
        <strong className="font-medium">{perItem(touches.per_item_mean)}</strong> touches per item
      </span>
      <span>{formatPercent(touches.touchless_rate)} touchless</span>
      <Link
        to="/o/$org/f/$factory"
        params={params}
        hash="touches-summary"
        className="inline-flex min-h-11 items-center text-ink underline underline-offset-2 transition-colors duration-200 hover:text-run"
      >
        By station
      </Link>
    </p>
  );
}

function ExpediteLane({ line, items, onMove }: { line: Board['line']; items: ItemCard[]; onMove: OnMove }) {
  if (items.length === 0) return null;
  return (
    <section
      aria-labelledby="expedite-h"
      className="hidden flex-col gap-3 rounded-card border border-rework-line bg-rework-tint p-3 md:flex @[720px]:flex-row @[720px]:flex-wrap @[720px]:items-center @[720px]:gap-4 @[720px]:px-3.5"
    >
      <div className="@[720px]:w-[140px] @[720px]:shrink-0">
        <h2 id="expedite-h" className="m-0 text-[15px] font-medium text-rework-ink">
          Expedite lane
        </h2>
        <p className="m-0 mt-0.5 text-[13px] text-rework-ink">{expediteUse(line, items.length)}</p>
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-2 md:max-w-[680px]">
        {items.map((it) => (
          <ExpediteCard key={it.id} item={it} onMove={moveOf(onMove, it)} />
        ))}
      </div>
    </section>
  );
}

function Columns({ board, onMove }: { board: Board; onMove: OnMove }) {
  const columns = <ColumnGrid board={board} onMove={onMove} />;
  // Cards are dragged only by someone who may move them.
  return onMove ? (
    <CardDnd board={board} onDrop={onMove}>
      {columns}
    </CardDnd>
  ) : (
    columns
  );
}

function ColumnGrid({ board, onMove }: { board: Board; onMove: OnMove }) {
  const decides = holds(useFactory(), 'item:decide');
  return (
    <section aria-label="Stations" className="-mx-1 hidden overflow-x-auto px-1 pb-2 md:block">
      <div
        className="grid items-start gap-2"
        style={{ gridTemplateColumns: `repeat(${board.stations.length}, minmax(144px, 1fr))` }}
      >
        {board.stations.map((s) => {
          const drum = s.id === board.constraint;
          const items = s.items.filter((it) => !isExpedite(it));
          return (
            <DropColumn
              key={s.id}
              station={s}
              className={cn(
                'flex min-w-0 flex-col gap-2 rounded-card border border-line bg-card p-3',
                drum && constraintRing,
              )}
            >
              <div className="flex items-baseline justify-between gap-2 px-1 pt-0.5 pb-1">
                <h2 className="m-0 text-[15px] font-medium">{humanize(s.id)}</h2>
                <span
                  className={cn(
                    'text-[13px] whitespace-nowrap text-muted',
                    decides && waitingOnYou(s) > 0 && 'text-you',
                  )}
                >
                  {columnCount(s)}
                </span>
              </div>
              {items.length === 0 ? <p className="m-0 px-1 pb-1 text-[13px] text-muted">Empty</p> : null}
              {items.map((it) =>
                onMove ? (
                  <DraggableCard key={it.id} item={it}>
                    {(drag) => <BoardCard item={it} onMove={() => onMove(it)} drag={drag} />}
                  </DraggableCard>
                ) : (
                  <BoardCard key={it.id} item={it} />
                ),
              )}
            </DropColumn>
          );
        })}
      </div>
    </section>
  );
}

/** Phones: a station picker and the picked station's cards. */
function StationTabs({
  board,
  selected,
  expedite,
  onMove,
  rateOf,
}: {
  board: Board;
  selected: string;
  expedite: ItemCard[];
  onMove: OnMove;
  rateOf: RateOf;
}) {
  const navigate = useNavigate({ from: '/o/$org/f/$factory/board' });
  const list = useRef<HTMLDivElement>(null);

  // Keep the picked station visible in the scrolling picker.
  useEffect(() => {
    list.current
      ?.querySelector(`[data-station="${CSS.escape(selected)}"]`)
      ?.scrollIntoView({ block: 'nearest', inline: 'center' });
  }, [selected]);

  return (
    <Tabs.Root
      value={selected}
      onValueChange={(station) => navigate({ search: { station }, replace: true, resetScroll: false })}
      className="flex flex-col gap-3 md:hidden"
    >
      <Tabs.List ref={list} aria-label="Stations" className="-mx-4 flex gap-2 overflow-x-auto px-4 py-1">
        {board.stations.map((s) => {
          const drum = s.id === board.constraint;
          return (
            <Tabs.Trigger
              key={s.id}
              value={s.id}
              data-station={s.id}
              className={cn(
                'flex min-h-14 shrink-0 flex-col items-start justify-center gap-0.5 rounded-control border border-line bg-surface px-3 py-2 text-ink',
                'data-[state=active]:border-ink data-[state=active]:bg-ink data-[state=active]:text-white',
                drum && 'shadow-[inset_0_-3px_0_var(--color-amber)]',
              )}
            >
              <span className="text-sm font-medium">{humanize(s.id)}</span>
              <span className="text-xs opacity-80">{stationStat(s)}</span>
            </Tabs.Trigger>
          );
        })}
      </Tabs.List>
      {expedite.map((it) => (
        <ExpediteRow key={it.id} item={it} onMove={moveOf(onMove, it)} />
      ))}
      <PhoneTouches touches={board.line.touches} />
      {board.stations.map((s) => {
        const drum = s.id === board.constraint;
        const items = s.items.filter((it) => !isExpedite(it));
        return (
          <Tabs.Content key={s.id} value={s.id} className="outline-none">
            <section
              aria-labelledby={`st-${s.id}`}
              className={cn(
                'flex flex-col gap-2 rounded-card border border-line bg-card p-3.5',
                drum && constraintRing,
              )}
            >
              <div className="flex items-baseline justify-between gap-2">
                <h2 id={`st-${s.id}`} className="m-0 text-base font-medium">
                  {humanize(s.id)}
                </h2>
                <span className="text-[13px] text-muted">{stationStat(s)}</span>
              </div>
              <StationNote station={s} drum={drum} rateOf={rateOf} />
              {items.length === 0 ? <p className="m-0 text-sm text-muted">Nothing here right now.</p> : null}
              {items.map((it) => (
                <BoardCard key={it.id} item={it} onMove={moveOf(onMove, it)} compact />
              ))}
            </section>
          </Tabs.Content>
        );
      })}
    </Tabs.Root>
  );
}

/** On a phone, under the station's name: the constraint's load, then its touches per item. */
function StationNote({ station, drum, rateOf }: { station: BoardStation; drum: boolean; rateOf: RateOf }) {
  const touches = stationTouches(rateOf, station);
  const parts = [drum ? `${constraintNote(station)}.` : null, touches ? `${touches}.` : null].filter(Boolean);
  if (parts.length === 0) return null;
  return <p className="m-0 mb-1 text-[13px] text-ink-soft">{parts.join(' ')}</p>;
}

/** Puts focus back on a card after it moved (it is rendered in its new column by then). */
function focusCard(id: string) {
  requestAnimationFrame(() => {
    const cards = document.querySelectorAll<HTMLElement>(`[data-card="${CSS.escape(id)}"] a[href]`);
    const visible = [...cards].find((a) => a.offsetParent !== null);
    visible?.focus();
  });
}

export function BoardPage() {
  const factory = useFactory();
  const org = useOrg();
  const { data: board } = useSuspenseQuery(boardQuery(org.id, factory.id));
  // The board's window is the last 7 days, as the touch metrics' default range. The rates wait
  // for them (or go without when they fail); the line's headline is the board's own.
  const touches = useQuery(touchesQuery(org.id, factory.id)).data?.stats;
  const rates = touches ? stationRates(touches) : undefined;
  const rateOf: RateOf = (s) => (!rates || touches?.finished === 0 ? null : (rates.get(s.id) ?? 0));
  const search = useSearch({ from: '/o/$org/f/$factory/board' });
  const expedite = board.stations.flatMap((s) => s.items.filter(isExpedite));
  const selected = board.stations.find((s) => s.id === search.station)?.id ?? firstStation(board);
  const [moving, setMoving] = useState<{ item: ItemCard; to?: string | undefined } | null>(null);
  const onMove: OnMove = holds(factory, 'item:operate') ? (item, to) => setMoving({ item, to }) : undefined;
  const role = factoryRoleNote(factory.role, factory.name);

  return (
    <>
      <header className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5">
          <h1 className="m-0 text-[34px] leading-[1.05] font-normal tracking-display md:text-5xl">
            {factoryTitle(factory)} board
          </h1>
          {role ? <Chip tone="plain">{role}</Chip> : null}
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <TouchesChip touches={board.line.touches} />
          <span className="hidden h-10 max-w-[360px] items-center rounded-control bg-surface px-3.5 shadow-raise md:inline-flex">
            <LineStatus andon={board.line.andon} className="text-sm text-ink" />
          </span>
          <AndonButton factory={factory} andon={board.line.andon} className="hidden md:inline-flex" />
          <span className="md:hidden">
            <NewItemButton factory={factory} compact />
          </span>
          <span className="hidden md:inline-flex">
            <NewItemButton factory={factory} />
          </span>
        </div>
      </header>
      <div className="-mt-3 flex flex-wrap items-center justify-between gap-2 text-sm text-muted md:hidden">
        <LineStatus
          andon={board.line.andon}
          className="text-sm"
          suffix={
            <>
              , <span className="whitespace-nowrap">{wipText(board.line)}</span>
            </>
          }
          wrap
        />
        <AndonButton factory={factory} andon={board.line.andon} className="min-h-11" />
      </div>

      <div className="hidden md:block">
        <LineCard board={board} decides={holds(factory, 'item:decide')} rateOf={rateOf} />
      </div>
      <ExpediteLane line={board.line} items={expedite} onMove={onMove} />
      <Columns board={board} onMove={onMove} />
      <StationTabs board={board} selected={selected} expedite={expedite} onMove={onMove} rateOf={rateOf} />
      {onMove ? (
        <>
          <p className="m-0 hidden text-[13px] text-muted md:block">
            Drag a card, or focus it and press M to choose a station. Agents pull work; nobody assigns it.
          </p>
          <p className="m-0 text-[13px] text-muted md:hidden">
            Use the arrow on a card to move it; agents pull the next item themselves.
          </p>
        </>
      ) : (
        <p className="m-0 text-[13px] text-muted">{reasons.board(factory.name)}</p>
      )}
      {moving ? (
        <MoveDialog
          item={{ ...moving.item, key: itemKey(factory.name, moving.item.ref) }}
          factory={factory}
          open
          onOpenChange={(open) => {
            if (!open) setMoving(null);
          }}
          initialTo={moving.to}
          onMoved={(updated) => focusCard(updated.id)}
        />
      ) : null}
    </>
  );
}
