import { useSuspenseQuery } from '@tanstack/react-query';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { Tabs } from 'radix-ui';
import { useEffect, useRef } from 'react';
import { boardQuery } from '../../api/queries';
import type { Board, ItemCard } from '../../api/types';
import { factoryTitle, useFactory } from '../../shared/factory';
import { humanize, plural } from '../../shared/format';
import { waitingOnYou } from '../../shared/item-state';
import { LiveStatus } from '../../shared/live-status';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { Meter } from '../../ui/hatch';
import { BoardCard, ExpediteCard, ExpediteRow } from './board-card';
import { columnCount, constraintNote, stationMeter, stationNote, stationStat } from './stats';

const live = (it: ItemCard) => it.state !== 'done' && it.state !== 'cancelled';
const isExpedite = (it: ItemCard) => it.class_of_service === 'expedite' && live(it);

const constraintRing = 'border-amber shadow-[inset_0_3px_0_var(--color-amber)]';

function LineCard({ board }: { board: Board }) {
  const wip = board.stations.reduce((n, s) => n + s.in_progress, 0);
  const limit = board.stations.reduce((n, s) => n + (s.wip_limit ?? 0), 0);
  const constraint = board.stations.find((s) => s.id === board.constraint);

  return (
    <SectionCard
      title="The line"
      description={
        constraint
          ? `${humanize(constraint.id)} is the constraint; the line moves at its pace.`
          : 'No station holds the line back right now.'
      }
      aside={
        <span className="text-sm whitespace-nowrap text-muted">
          WIP {wip}
          {limit > 0 ? ` of ${limit}` : ''}
        </span>
      }
    >
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
                <span className={cn('text-sm', waitingOnYou(s) > 0 && 'text-you')}>{stationStat(s)}</span>
                <span className="text-[13px] leading-snug text-muted">{stationNote(s)}</span>
              </li>
            );
          })}
        </ol>
      </section>
    </SectionCard>
  );
}

function ExpediteLane({ items }: { items: ItemCard[] }) {
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
        <p className="m-0 mt-0.5 text-[13px] text-rework-ink">{plural(items.length, 'item')} skipping the queue</p>
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-2 md:max-w-[680px]">
        {items.map((it) => (
          <ExpediteCard key={it.id} item={it} />
        ))}
      </div>
    </section>
  );
}

function Columns({ board }: { board: Board }) {
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
            <section
              key={s.id}
              aria-label={humanize(s.id)}
              className={cn(
                'flex min-w-0 flex-col gap-2 rounded-card border border-line bg-card p-3',
                drum && constraintRing,
              )}
            >
              <div className="flex items-baseline justify-between gap-2 px-1 pt-0.5 pb-1">
                <h2 className="m-0 text-[15px] font-medium">{humanize(s.id)}</h2>
                <span className={cn('text-[13px] whitespace-nowrap text-muted', waitingOnYou(s) > 0 && 'text-you')}>
                  {columnCount(s)}
                </span>
              </div>
              {items.length === 0 ? <p className="m-0 px-1 pb-1 text-[13px] text-muted">Empty</p> : null}
              {items.map((it) => (
                <BoardCard key={it.id} item={it} atConstraint={drum} />
              ))}
            </section>
          );
        })}
      </div>
    </section>
  );
}

/** Phones: a station picker and the picked station's cards. */
function StationTabs({ board, selected, expedite }: { board: Board; selected: string; expedite: ItemCard[] }) {
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
        <ExpediteRow key={it.id} item={it} />
      ))}
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
              {drum ? <p className="m-0 mb-1 text-[13px] text-ink-soft">{constraintNote(s)}</p> : null}
              {items.length === 0 ? <p className="m-0 text-sm text-muted">Nothing here right now.</p> : null}
              {items.map((it) => (
                <BoardCard key={it.id} item={it} atConstraint={drum} compact />
              ))}
            </section>
          </Tabs.Content>
        );
      })}
    </Tabs.Root>
  );
}

export function BoardPage() {
  const factory = useFactory();
  const org = useOrg();
  const { data: board } = useSuspenseQuery(boardQuery(org.id, factory.id));
  const search = useSearch({ from: '/o/$org/f/$factory/board' });
  const expedite = board.stations.flatMap((s) => s.items.filter(isExpedite));
  const wip = board.stations.reduce((n, s) => n + s.in_progress, 0);
  const selected =
    board.stations.find((s) => s.id === search.station)?.id ??
    board.constraint ??
    board.stations.find((s) => s.items.length > 0)?.id ??
    board.stations[0]?.id ??
    '';

  return (
    <>
      <header className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <h1 className="m-0 text-[34px] leading-[1.05] font-normal tracking-display md:text-5xl">
          {factoryTitle(factory)} board
        </h1>
        <div className="flex items-center gap-2">
          <span className="hidden h-10 items-center rounded-control bg-surface px-3.5 shadow-raise md:inline-flex">
            <LiveStatus className="text-sm text-ink" />
          </span>
          <span className="md:hidden">
            <NewItemButton factory={factory} compact />
          </span>
          <span className="hidden md:inline-flex">
            <NewItemButton factory={factory} />
          </span>
        </div>
      </header>
      <div className="-mt-3 flex items-center gap-1 text-sm text-muted md:hidden">
        <LiveStatus className="text-sm" suffix={`, WIP ${wip}`} />
      </div>

      <div className="hidden md:block">
        <LineCard board={board} />
      </div>
      <ExpediteLane items={expedite} />
      <Columns board={board} />
      <StationTabs board={board} selected={selected} expedite={expedite} />
      <p className="m-0 text-[13px] text-muted">Agents pull work when they have room; nobody assigns it.</p>
    </>
  );
}
