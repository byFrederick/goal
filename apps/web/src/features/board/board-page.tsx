import { useQuery, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { Users } from 'lucide-react';
import { Tabs } from 'radix-ui';
import { type ReactNode, useEffect, useId, useRef, useState } from 'react';
import { boardQuery, touchesQuery } from '../../api/queries';
import type { Board, BoardStation, ItemCard, TouchHeadline } from '../../api/types';
import { AndonButton, LineStatus } from '../../shared/andon';
import { factoryTitle, itemKey, useFactory, useFactoryParams } from '../../shared/factory';
import { formatPercent, humanize } from '../../shared/format';
import { InboxButton } from '../../shared/inbox-drawer';
import { waitingOnYou } from '../../shared/item-state';
import { MoveDialog } from '../../shared/move-menu';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { factoryRoleNote, holds, reasons } from '../../shared/permissions';
import { perItem, stationRates } from '../../shared/touches';
import { SectionCard } from '../../ui/card';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { NativeSelect } from '../../ui/field';
import { Meter } from '../../ui/hatch';
import { SegmentedControl, type SegmentedOption } from '../../ui/segmented-control';
import { StatusIcon } from '../../ui/status-icon';
import { CardDnd, DropColumn } from './board-dnd';
import {
  type AssemblyOption,
  arrangeBoard,
  assemblyOptions,
  type BoardView,
  knownView,
  openAssemblies,
  roleOptions,
  statusChoices,
} from './board-filters';
import { type OpenAssemblies, StationBody } from './board-lane';
import {
  budgetNote,
  columnCount,
  constraintSentence,
  expediteHeader,
  firstStation,
  isExpedite,
  lineSentence,
  stationMeter,
  stationNote,
  stationStat,
  wipText,
} from './stats';

/** Opens the Move menu for a card, with the station a drop chose; undefined: the caller may not move work. */
type OnMove = ((item: ItemCard, to?: string) => void) | undefined;

const constraintRing = 'border-amber shadow-[inset_0_3px_0_var(--color-amber)]';

/** A station's touches per finished item over the board's week; null when no item finished. */
type RateOf = (station: BoardStation) => number | null;

/** "0.38 touches per item" for a station, or nothing when no item finished. */
function stationTouches(rateOf: RateOf, s: BoardStation): string | null {
  const rate = rateOf(s);
  return rate === null ? null : `${perItem(rate, 2)} touches per item`;
}

function LineCard({
  board,
  decides,
  rateOf,
  expedite,
}: {
  board: Board;
  decides: boolean;
  rateOf: RateOf;
  /** The expedite count for the header, null when there is none to show. */
  expedite: string | null;
}) {
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
      aside={
        <span className="flex flex-wrap justify-end gap-x-3 text-sm text-muted">
          <span className="whitespace-nowrap">{wipText(board.line)}</span>
          {expedite ? <span className="whitespace-nowrap">{expedite}</span> : null}
        </span>
      }
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

/** Phones, in the status line: the link to where the touches happen, a 44 px target beside the line. */
function PhoneTouchesLink() {
  const params = useFactoryParams();
  return (
    <Link
      to="/o/$org/f/$factory"
      params={params}
      hash="touches-summary"
      className="inline-flex min-h-11 shrink-0 items-center text-ink underline underline-offset-2 transition-colors duration-200 hover:text-run"
    >
      By station
    </Link>
  );
}

function Columns({ board, onMove, headers }: { board: Board; onMove: OnMove; headers: OpenAssemblies | undefined }) {
  const columns = <ColumnGrid board={board} onMove={onMove} headers={headers} />;
  // Cards are dragged only by someone who may move them.
  return onMove ? (
    <CardDnd board={board} onDrop={onMove}>
      {columns}
    </CardDnd>
  ) : (
    columns
  );
}

function ColumnGrid({ board, onMove, headers }: { board: Board; onMove: OnMove; headers: OpenAssemblies | undefined }) {
  const decides = holds(useFactory(), 'item:decide');
  return (
    <section aria-label="Stations" className="-mx-1 hidden overflow-x-auto px-1 pb-2 md:block">
      <div
        className="grid items-start gap-2"
        style={{ gridTemplateColumns: `repeat(${board.stations.length}, minmax(144px, 1fr))` }}
      >
        {board.stations.map((s) => {
          const drum = s.id === board.constraint;
          return (
            <DropColumn
              key={s.id}
              station={s}
              className={cn(
                'flex min-w-0 flex-col gap-2 rounded-card border border-line bg-card p-3',
                drum && constraintRing,
              )}
            >
              {s.kind === 'queue' ? null : (
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
              )}
              {!s.lanes?.length && s.items.length === 0 ? (
                <p className="m-0 px-1 pb-1 text-[13px] text-muted">Empty</p>
              ) : null}
              <StationBody station={s} onMove={onMove} draggable={Boolean(onMove)} headers={headers} />
            </DropColumn>
          );
        })}
      </div>
    </section>
  );
}

/** Phones: a station picker, the constraint's sentence, and the picked station's cards. */
function StationTabs({
  board,
  selected,
  onMove,
  rateOf,
  headers,
}: {
  board: Board;
  selected: string;
  onMove: OnMove;
  rateOf: RateOf;
  headers: OpenAssemblies | undefined;
}) {
  const navigate = useNavigate({ from: '/o/$org/f/$factory/board' });
  const list = useRef<HTMLDivElement>(null);
  const picked = board.stations.find((s) => s.id === selected);
  const sentence = picked && picked.id === board.constraint ? constraintSentence(picked) : null;

  // Keep the picked station visible in the scrolling picker.
  useEffect(() => {
    list.current
      ?.querySelector(`[data-station="${CSS.escape(selected)}"]`)
      ?.scrollIntoView({ block: 'nearest', inline: 'center' });
  }, [selected]);

  return (
    <Tabs.Root
      value={selected}
      onValueChange={(station) =>
        navigate({ search: (prev) => ({ ...prev, station }), replace: true, resetScroll: false })
      }
      className="flex flex-col gap-3 md:hidden"
    >
      <Tabs.List ref={list} aria-label="Stations" className="-mx-4 flex gap-2 overflow-x-auto px-4 py-1">
        {board.stations.map((s) => {
          const drum = s.id === board.constraint;
          const load = stationStat(s);
          return (
            <Tabs.Trigger
              key={s.id}
              value={s.id}
              data-station={s.id}
              className={cn(
                'flex min-h-14 w-36 shrink-0 flex-col items-start justify-center gap-0.5 rounded-control border border-line bg-surface px-3 py-2 text-ink',
                'data-[state=active]:border-ink data-[state=active]:bg-ink data-[state=active]:text-white',
                drum && 'shadow-[inset_0_-3px_0_var(--color-amber)]',
              )}
            >
              <span className="block w-full truncate text-sm font-medium" title={humanize(s.id)}>
                {humanize(s.id)}
              </span>
              <span className="block w-full truncate text-xs opacity-80" title={load}>
                {load}
              </span>
            </Tabs.Trigger>
          );
        })}
      </Tabs.List>
      {sentence ? (
        <p className="m-0 truncate text-sm text-ink-soft" title={sentence}>
          {sentence}
        </p>
      ) : null}
      {board.stations.map((s) => {
        const drum = s.id === board.constraint;
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
              <StationNote station={s} rateOf={rateOf} />
              {!s.lanes?.length && s.items.length === 0 ? (
                <p className="m-0 text-sm text-muted">Nothing here right now.</p>
              ) : null}
              <StationBody station={s} onMove={onMove} draggable={false} compact headers={headers} />
            </section>
          </Tabs.Content>
        );
      })}
    </Tabs.Root>
  );
}

/** On a phone, under the station's name: its touches per item, when one finished. */
function StationNote({ station, rateOf }: { station: BoardStation; rateOf: RateOf }) {
  const touches = stationTouches(rateOf, station);
  if (!touches) return null;
  return <p className="m-0 mb-1 text-[13px] text-ink-soft">{touches}.</p>;
}

const groupChoices: SegmentedOption<'none' | 'assembly'>[] = [
  { value: 'none', label: 'None' },
  { value: 'assembly', label: 'Assembly' },
];

/** A filter: a labelled native select (44 px on phones), with the status icon of its choice beside it. */
function FilterSelect({
  label,
  value,
  onChange,
  icon,
  children,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  icon?: ReactNode;
  children: ReactNode;
}) {
  const id = useId();
  return (
    <div className="flex min-w-0 max-w-full items-center gap-2 text-sm text-muted">
      <label htmlFor={id} className="shrink-0">
        {label}
      </label>
      <NativeSelect
        id={id}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="w-44 max-w-full truncate"
      >
        {children}
      </NativeSelect>
      {icon}
    </div>
  );
}

/**
 * The filters over the columns and the phone's station tabs: Assembly, Agent role and Status, then
 * Group by. Each choice is a URL search param; "Any" and "None" are the absent param.
 */
function FilterRow({
  board,
  view,
  onChange,
}: {
  board: Board;
  view: BoardView;
  onChange: (change: BoardView) => void;
}) {
  const assemblies: AssemblyOption[] = assemblyOptions(board);
  return (
    <fieldset className="m-0 flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2 border-0 p-0">
      <legend className="sr-only">Filter and group the cards</legend>
      <FilterSelect
        label="Assembly"
        value={view.assembly ?? ''}
        onChange={(v) => onChange({ assembly: v || undefined })}
      >
        <option value="">Any</option>
        {assemblies.map((a) => (
          <option key={a.id} value={a.id} title={a.title}>
            {a.title}
          </option>
        ))}
      </FilterSelect>
      <FilterSelect label="Agent role" value={view.role ?? ''} onChange={(v) => onChange({ role: v || undefined })}>
        <option value="">Any</option>
        {roleOptions(board).map((role) => (
          <option key={role} value={role}>
            {role}
          </option>
        ))}
      </FilterSelect>
      <FilterSelect
        label="Status"
        value={view.status ?? ''}
        onChange={(v) => onChange({ status: statusChoices.find((s) => s.value === v)?.value })}
        icon={view.status ? <StatusIcon status={view.status} /> : null}
      >
        <option value="">Any</option>
        {statusChoices.map((s) => (
          <option key={s.value} value={s.value}>
            {s.label}
          </option>
        ))}
      </FilterSelect>
      <SegmentedControl
        label="Group by"
        options={groupChoices}
        value={view.group ?? 'none'}
        onChange={(group) => onChange({ group: group === 'assembly' ? 'assembly' : undefined })}
      />
    </fieldset>
  );
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
  const navigate = useNavigate({ from: '/o/$org/f/$factory/board' });
  // The filters and the grouping: the URL's choices, those the board offers only.
  const view = knownView(board, search);
  const shown = arrangeBoard(board, view);
  const changeView = (change: BoardView) =>
    navigate({ search: (prev) => ({ ...prev, ...change }), replace: true, resetScroll: false });
  // Grouped by assembly, each open assembly heads its parts; ungrouped, the parts keep their assembly line.
  const headers = view.group === 'assembly' ? openAssemblies(board) : undefined;
  const expedite = expediteHeader(board.line, board.stations.flatMap((s) => s.items).filter(isExpedite).length);
  // The phone's status line: the line, its WIP and the week's touches per item, in one truncated line.
  const headline = board.line.touches;
  const touchesHeadline = `${perItem(headline.per_item_mean)} touches per item`;
  const phoneLine = [
    board.line.andon ? `Line stopped: ${board.line.andon.reason}` : 'Line running',
    wipText(board.line),
    headline.finished > 0 ? touchesHeadline : null,
  ]
    .filter(Boolean)
    .join(', ');
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
          <InboxButton factory={factory} />
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
      <div className="-mt-3 flex min-w-0 items-center gap-2 text-sm text-muted md:hidden">
        <LineStatus
          andon={board.line.andon}
          className="min-w-0 flex-1"
          title={phoneLine}
          suffix={
            <>
              , <span className="whitespace-nowrap">{wipText(board.line)}</span>
              {headline.finished > 0 ? (
                <>
                  , <span className="whitespace-nowrap">{touchesHeadline}</span>
                </>
              ) : null}
            </>
          }
        />
        {headline.finished > 0 ? <PhoneTouchesLink /> : null}
        <AndonButton factory={factory} andon={board.line.andon} className="min-h-11" />
      </div>

      <div className="hidden md:block">
        <LineCard board={board} decides={holds(factory, 'item:decide')} rateOf={rateOf} expedite={expedite} />
      </div>
      <FilterRow board={board} view={view} onChange={changeView} />
      <Columns board={shown} onMove={onMove} headers={headers} />
      <StationTabs board={shown} selected={selected} onMove={onMove} rateOf={rateOf} headers={headers} />
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
