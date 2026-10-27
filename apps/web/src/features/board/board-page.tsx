import { useQuery, useSuspenseQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { Tabs } from 'radix-ui';
import { type ReactNode, useEffect, useId, useMemo, useRef, useState } from 'react';
import { boardQuery, touchesQuery } from '../../api/queries';
import type { Board, BoardStation, ItemCard } from '../../api/types';
import { usePhone } from '../../app/phone-chrome';
import { TopBarSlot } from '../../app/top-bar';
import { AndonButton, LineStatus } from '../../shared/andon';
import { factoryTitle, itemKey, useFactory, useFactoryParams } from '../../shared/factory';
import { humanize } from '../../shared/format';
import { InboxButton } from '../../shared/inbox-drawer';
import { MoveDialog } from '../../shared/move-menu';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { factoryRoleNote, holds, reasons } from '../../shared/permissions';
import { perItem, stationRates } from '../../shared/touches';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { NativeSelect } from '../../ui/field';
import { SegmentedControl, type SegmentedOption } from '../../ui/segmented-control';
import { StatusIcon } from '../../ui/status-icon';
import { StationColumn } from './board-column';
import { CardDnd } from './board-dnd';
import { BoardTopBar, SearchItems } from './board-top-bar';
import {
  type AssemblyOption,
  arrangeBoard,
  assemblyOptions,
  type BoardView,
  knownView,
  laneOptions,
  openAssemblies,
  roleOptions,
  statusChoices,
  typeOptions,
} from './board-filters';
import { type OpenAssemblies, StationBody } from './board-lane';
import {
  constraintSentence,
  expediteHeader,
  expediteUse,
  firstStation,
  isExpedite,
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

function Columns({
  board,
  onMove,
  decides,
  finishedThisWeek,
}: {
  board: Board;
  onMove: OnMove;
  decides: boolean;
  finishedThisWeek: number;
}) {
  const columns = (
    <DesktopColumns board={board} onMove={onMove} decides={decides} finishedThisWeek={finishedThisWeek} />
  );
  // Cards are dragged only by someone who may move them.
  return onMove ? (
    <CardDnd board={board} onDrop={onMove}>
      {columns}
    </CardDnd>
  ) : (
    columns
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

/** From 768 px: the stations side by side, each a StationColumn, with the expedite lane's caption above them. */
function DesktopColumns({
  board,
  onMove,
  decides,
  finishedThisWeek,
}: {
  board: Board;
  onMove: OnMove;
  decides: boolean;
  finishedThisWeek: number;
}) {
  const expedite = expediteHeader(board.line, board.stations.flatMap((s) => s.items).filter(isExpedite).length);
  // The expedite zone is drawn when the line has a lane or an expedite item is on the board.
  const showExpedite = expedite !== null;
  return (
    <section aria-label="Stations" className="-mx-1 hidden overflow-x-auto px-1 pb-2 md:block">
      {showExpedite ? (
        <p className="m-0 mb-2 flex items-center gap-2 text-secondary text-muted">
          <Chip tone="expedite">Expedite</Chip>
          <span>
            {expediteUse(board.line, board.stations.flatMap((s) => s.items).filter(isExpedite).length)}, jumps every
            queue
          </span>
        </p>
      ) : null}
      <div
        className="grid items-start gap-3"
        style={{ gridTemplateColumns: `repeat(${board.stations.length}, minmax(176px, 1fr))` }}
      >
        {board.stations.map((s) => (
          <StationColumn
            key={s.id}
            board={board}
            station={s}
            showExpedite={showExpedite}
            decides={decides}
            onMove={onMove}
            draggable={Boolean(onMove)}
            finishedThisWeek={finishedThisWeek}
          />
        ))}
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
                'flex min-h-14 w-36 shrink-0 flex-col items-start justify-center gap-0.5 rounded-control border border-line bg-card px-3 py-2 text-ink',
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
                <span className="text-secondary text-muted">{stationStat(s)}</span>
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
  return <p className="m-0 mb-1 text-secondary text-ink-soft">{touches}.</p>;
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
  phoneOnly = false,
  children,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  icon?: ReactNode;
  /** Status and Group by only the phone's station tabs use; from 768 px the columns do not take them. */
  phoneOnly?: boolean;
  children: ReactNode;
}) {
  const id = useId();
  return (
    <div className={cn('flex min-w-0 max-w-full items-center gap-2 text-sm text-muted', phoneOnly && 'md:hidden')}>
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
 * Group by. Each choice is a URL search param; "Any" and "None" are the absent param. From 768 px the
 * search is in the top bar (board-top-bar.tsx), so the row keeps it only on a phone.
 */
function FilterRow({
  board,
  view,
  phone,
  onChange,
}: {
  board: Board;
  view: BoardView;
  phone: boolean;
  onChange: (change: BoardView) => void;
}) {
  const assemblies: AssemblyOption[] = assemblyOptions(board);
  return (
    <fieldset className="m-0 flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2 border-0 p-0">
      <legend className="sr-only">Filter and group the cards</legend>
      {phone ? (
        <SearchItems value={view.q ?? ''} onChange={(q) => onChange({ q: q || undefined })} />
      ) : null}
      <FilterSelect label="Work type" value={view.type ?? ''} onChange={(v) => onChange({ type: v || undefined })}>
        <option value="">Any</option>
        {typeOptions(board).map((type) => (
          <option key={type} value={type}>
            {humanize(type)}
          </option>
        ))}
      </FilterSelect>
      <FilterSelect label="Lane" value={view.lane ?? ''} onChange={(v) => onChange({ lane: v || undefined })}>
        <option value="">Any</option>
        {laneOptions(board).map((lane) => (
          <option key={lane} value={lane}>
            {humanize(lane)}
          </option>
        ))}
      </FilterSelect>
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
        phoneOnly
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
      <div className="md:hidden">
        <SegmentedControl
          label="Group by"
          options={groupChoices}
          value={view.group ?? 'none'}
          onChange={(group) => onChange({ group: group === 'assembly' ? 'assembly' : undefined })}
        />
      </div>
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
  // From 768 px the actions and the search are in the top bar; a phone keeps its own header.
  const phone = usePhone();
  const actions = useMemo(
    () => (phone ? undefined : <BoardTopBar factory={factory} andon={board.line.andon} />),
    [phone, factory, board.line.andon],
  );
  const filters = <FilterRow board={board} view={view} phone={phone} onChange={changeView} />;

  return (
    <>
      <TopBarSlot actions={actions} />
      <header className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5">
          <h1 className="m-0 text-page-title tracking-display">{phone ? `${factoryTitle(factory)} board` : 'Board'}</h1>
          {role ? <Chip tone="plain">{role}</Chip> : null}
          {phone ? null : (
            <span role="status">
              <Chip tone={board.line.andon ? 'rework' : 'ok'} dot size="lg">
                {board.line.andon ? `Line stopped: ${board.line.andon.reason}` : 'Line running'}
              </Chip>
            </span>
          )}
        </div>
        <div className="ml-auto flex min-w-0 flex-wrap items-center gap-2">
          <InboxButton factory={factory} />
          {phone ? <NewItemButton factory={factory} compact /> : null}
          {phone ? null : filters}
        </div>
      </header>
      {phone ? (
        <>
          <div className="-mt-3 flex min-w-0 items-center gap-2 text-sm text-muted">
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
          {filters}
        </>
      ) : null}
      <Columns
        board={shown}
        onMove={onMove}
        decides={holds(factory, 'item:decide')}
        finishedThisWeek={board.line.touches.finished}
      />
      <StationTabs board={shown} selected={selected} onMove={onMove} rateOf={rateOf} headers={headers} />
      {onMove ? (
        <>
          <p className="m-0 hidden text-secondary text-muted md:block">
            Drag a card, or focus it and press M to choose a station. Agents pull work; nobody assigns it.
          </p>
          <p className="m-0 text-secondary text-muted md:hidden">
            Use the arrow on a card to move it; agents pull the next item themselves.
          </p>
        </>
      ) : (
        <p className="m-0 text-secondary text-muted">{reasons.board(factory.name)}</p>
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
