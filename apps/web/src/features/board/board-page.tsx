import { useSuspenseQuery } from '@tanstack/react-query';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { ChevronDown, SlidersHorizontal } from 'lucide-react';
import { Tabs } from 'radix-ui';
import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { boardQuery } from '../../api/queries';
import type { Board, Factory, ItemCard } from '../../api/types';
import { usePhone } from '../../app/phone-chrome';
import { TopBarSlot } from '../../app/top-bar';
import { LineStatus } from '../../shared/andon';
import { itemKey, useFactory } from '../../shared/factory';
import { humanize } from '../../shared/format';
import { MoveDialog } from '../../shared/move-menu';
import { NeedsYouLink } from '../../shared/needs-you-link';
import { NewItemButton } from '../../shared/new-item';
import { useOrg } from '../../shared/org';
import { factoryRoleNote, holds, reasons } from '../../shared/permissions';
import { useArrivals } from '../../ui/arrivals';
import { Chip } from '../../ui/chip';
import { cn } from '../../ui/cn';
import { Menu, MenuChoice, MenuContent, MenuRadioGroup, MenuTrigger } from '../../ui/menu';
import { SegmentedControl, type SegmentedOption } from '../../ui/segmented-control';
import { StationColumn } from './board-column';
import { CardDnd } from './board-dnd';
import {
  type AssemblyOption,
  arrangeBoard,
  assemblyOptions,
  type BoardView,
  knownView,
  laneOptions,
  roleOptions,
  statusChoices,
  typeOptions,
} from './board-filters';
import { BoardTopBarActions, SearchItems } from './board-header';
import { useCardMoves } from './card-move';
import { columnCount, expediteHeader, expediteUse, firstStation, isExpedite } from './stats';

/** Opens the Move menu for a card, with the station a drop chose; undefined: the caller may not move work. */
type OnMove = ((item: ItemCard, to?: string) => void) | undefined;

/** The expedite zone is drawn when the line has an expedite lane or an expedite item is on the board. */
function drawsExpedite(board: Board): boolean {
  return expediteHeader(board.line, board.stations.flatMap((s) => s.items).filter(isExpedite).length) !== null;
}

/** The expedite row's caption: "Expedite, 1 of 1 in use, jumps every queue"; null when the line draws no row. */
function expediteCaption(board: Board): string | null {
  const shown = board.stations.flatMap((s) => s.items).filter(isExpedite).length;
  return expediteHeader(board.line, shown) === null
    ? null
    : `Expedite, ${expediteUse(board.line, shown)}, jumps every queue`;
}

interface ColumnsProps {
  board: Board;
  onMove: OnMove;
  decides: boolean;
  finishedThisWeek: number;
  arrived: (id: string) => boolean;
}

function Columns({ board, onMove, decides, finishedThisWeek, arrived }: ColumnsProps) {
  const columns = (
    <DesktopColumns
      board={board}
      onMove={onMove}
      decides={decides}
      finishedThisWeek={finishedThisWeek}
      arrived={arrived}
    />
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

/** From 768 px: the stations side by side, each a StationColumn; the first one's expedite row carries the lane's caption. */
function DesktopColumns({ board, onMove, decides, finishedThisWeek, arrived }: ColumnsProps) {
  const showExpedite = drawsExpedite(board);
  return (
    <section aria-label="Stations" className="-mx-1 hidden overflow-x-auto px-1 pb-2 md:block">
      <div
        className="grid items-start gap-x-3"
        style={{ gridTemplateColumns: `repeat(${board.stations.length}, minmax(176px, 1fr))` }}
      >
        {board.stations.map((s, i) => (
          <StationColumn
            key={s.id}
            board={board}
            station={s}
            showExpedite={showExpedite}
            expediteCaption={i === 0 ? (expediteCaption(board) ?? undefined) : undefined}
            decides={decides}
            onMove={onMove}
            draggable={Boolean(onMove)}
            finishedThisWeek={finishedThisWeek}
            arrived={arrived}
          />
        ))}
      </div>
    </section>
  );
}

/**
 * Phones: a row of one-line station tabs, the picked station's column below them. The column is the
 * desktop's (its header, its expedite zone, the drum's Ready and In progress zones and its cards), with
 * no drag: a card moves by its 44 px Move button.
 */
function StationTabs({
  board,
  selected,
  onMove,
  decides,
  finishedThisWeek,
  arrived,
}: ColumnsProps & { selected: string }) {
  const navigate = useNavigate({ from: '/o/$org/f/$factory/board' });
  const list = useRef<HTMLDivElement>(null);
  const showExpedite = drawsExpedite(board);

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
        {board.stations.map((s) => (
          <Tabs.Trigger
            key={s.id}
            value={s.id}
            data-station={s.id}
            className={cn(
              'flex min-h-11 shrink-0 items-center gap-2 whitespace-nowrap rounded-dialog border border-line bg-card px-4 text-sm font-medium text-ink-soft',
              'transition-[background-color,border-color,color] duration-(--duration-fast) ease-(--ease-out) motion-reduce:transition-colors',
              'data-[state=active]:border-transparent data-[state=active]:bg-hover data-[state=active]:text-ink',
            )}
          >
            {s.id === board.constraint ? (
              <span aria-hidden="true" className="size-2 shrink-0 rounded-full bg-amber" />
            ) : null}
            {humanize(s.id)}
            <span className="tabular-nums opacity-80 transition-opacity duration-(--duration-quick) ease-(--ease-out) motion-reduce:transition-none">
              {columnCount(s)}
            </span>
          </Tabs.Trigger>
        ))}
      </Tabs.List>
      {board.stations.map((s) => (
        <Tabs.Content key={s.id} value={s.id} className="outline-none">
          <StationColumn
            board={board}
            station={s}
            showExpedite={showExpedite}
            expediteCaption={expediteCaption(board) ?? undefined}
            decides={decides}
            onMove={onMove}
            draggable={false}
            finishedThisWeek={finishedThisWeek}
            arrived={arrived}
          />
        </Tabs.Content>
      ))}
    </Tabs.Root>
  );
}

const groupChoices: SegmentedOption<'none' | 'assembly'>[] = [
  { value: 'none', label: 'None' },
  { value: 'assembly', label: 'Assembly' },
];

/** The value a filter menu shows for "any": no filter in the URL. */
const any = '*';

/** A small dropdown button (design: .fbtn): 28 px from 768 px, 44 px on a phone; the chosen value shows in it. */
const filterButton =
  'inline-flex min-h-11 items-center gap-2 rounded-control border border-rule bg-card px-3 text-secondary font-medium text-ink-soft transition-colors duration-(--duration-quick) hover:border-muted hover:text-ink md:min-h-7';

/** One filter: a button naming the filter and its choice, opening a radio list of what the board offers. */
function FilterMenu({
  name,
  value,
  choices,
  onChange,
  phoneOnly = false,
}: {
  name: string;
  value: string | undefined;
  choices: { value: string; label: string }[];
  onChange: (value: string | undefined) => void;
  /** Status only the phone's station tabs use; from 768 px the columns do not take it. */
  phoneOnly?: boolean;
}) {
  const shown = choices.find((c) => c.value === value)?.label;
  return (
    <div className={cn('min-w-0 max-w-full', phoneOnly && 'md:hidden')}>
      <Menu>
        <MenuTrigger asChild>
          <button type="button" className={cn(filterButton, 'max-w-full')}>
            <span className="truncate">{shown ? `${name}: ${shown}` : name}</span>
            <ChevronDown size={12} strokeWidth={1.75} aria-hidden="true" className="shrink-0 text-muted" />
          </button>
        </MenuTrigger>
        <MenuContent side="bottom" className="w-60">
          <MenuRadioGroup
            value={shown ? (value ?? any) : any}
            onValueChange={(v) => onChange(v === any ? undefined : v)}
          >
            <MenuChoice value={any}>{`Any ${name.toLowerCase()}`}</MenuChoice>
            {choices.map((c) => (
              <MenuChoice key={c.value} value={c.value}>
                {c.label}
              </MenuChoice>
            ))}
          </MenuRadioGroup>
        </MenuContent>
      </Menu>
    </div>
  );
}

/**
 * The filters over the columns and the phone's station tabs: Assembly, Agent role and Status, then
 * Group by. Each choice is a URL search param; "Any" and "None" are the absent param. From 768 px the
 * search is on the title row (board-header.tsx), so the row keeps it only on a phone.
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
    <fieldset className="m-0 flex min-w-0 flex-wrap items-center gap-2 border-0 p-0">
      <legend className="sr-only">Filter and group the cards</legend>
      {phone ? <SearchItems value={view.q ?? ''} onChange={(q) => onChange({ q: q || undefined })} /> : null}
      <FilterMenu
        name="Work type"
        value={view.type}
        onChange={(type) => onChange({ type })}
        choices={typeOptions(board).map((type) => ({ value: type, label: humanize(type) }))}
      />
      <FilterMenu
        name="Lane"
        value={view.lane}
        onChange={(lane) => onChange({ lane })}
        choices={laneOptions(board).map((lane) => ({ value: lane, label: humanize(lane) }))}
      />
      <FilterMenu
        name="Assembly"
        value={view.assembly}
        onChange={(assembly) => onChange({ assembly })}
        choices={assemblies.map((a) => ({ value: a.id, label: a.title }))}
      />
      <FilterMenu
        name="Agent role"
        value={view.role}
        onChange={(role) => onChange({ role })}
        choices={roleOptions(board).map((role) => ({ value: role, label: role }))}
      />
      <FilterMenu
        name="Status"
        phoneOnly
        value={view.status}
        onChange={(v) => onChange({ status: statusChoices.find((s) => s.value === v)?.value })}
        choices={statusChoices}
      />
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

/** Phones: the top bar's Filter button, which shows or hides the filters row, and New item. */
function PhoneBoardActions({
  factory,
  open,
  controls,
  onToggle,
}: {
  factory: Factory;
  open: boolean;
  controls: string;
  onToggle: () => void;
}) {
  return (
    <>
      <button
        type="button"
        aria-label="Filter"
        aria-expanded={open}
        aria-controls={open ? controls : undefined}
        onClick={onToggle}
        className="flex size-11 flex-none items-center justify-center rounded-control text-ink-soft hover:bg-hover hover:text-ink"
      >
        <SlidersHorizontal size={18} strokeWidth={1.75} aria-hidden="true" />
      </button>
      <NewItemButton factory={factory} compact />
    </>
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
  const { data: fetched } = useSuspenseQuery(boardQuery(org.id, factory.id));
  // A card that changes column or lane travels there (card-move.ts).
  const board = useCardMoves(fetched);
  const search = useSearch({ from: '/o/$org/f/$factory/board' });
  const navigate = useNavigate({ from: '/o/$org/f/$factory/board' });
  // The filters and the grouping: the URL's choices, those the board offers only.
  const view = knownView(board, search);
  const shown = arrangeBoard(board, view);
  // Cards new to the board since it first had cards rise in; the ones it opened with, and a filter's returns, do not.
  const arrived = useArrivals(board.stations.flatMap((s) => s.items.map((it) => it.id)));
  const changeView = (change: BoardView) =>
    navigate({ search: (prev) => ({ ...prev, ...change }), replace: true, resetScroll: false });
  const selected = board.stations.find((s) => s.id === search.station)?.id ?? firstStation(board);
  const [moving, setMoving] = useState<{ item: ItemCard; to?: string | undefined } | null>(null);
  const onMove: OnMove = holds(factory, 'item:operate') ? (item, to) => setMoving({ item, to }) : undefined;
  const role = factoryRoleNote(factory.role, factory.name);
  // A phone keeps its filters row behind the top bar's Filter button; from 768 px the top bar holds Pull andon and New item.
  const phone = usePhone();
  const [filtersOpen, setFiltersOpen] = useState(false);
  const filtersId = useId();
  const actions = useMemo(
    () =>
      phone ? (
        <PhoneBoardActions
          factory={factory}
          open={filtersOpen}
          controls={filtersId}
          onToggle={() => setFiltersOpen((open) => !open)}
        />
      ) : (
        <BoardTopBarActions
          factory={factory}
          andon={board.line.andon}
          q={view.q ?? ''}
          onSearch={(q) =>
            navigate({ search: (prev) => ({ ...prev, q: q || undefined }), replace: true, resetScroll: false })
          }
        />
      ),
    [phone, factory, board.line.andon, filtersOpen, filtersId, view.q, navigate],
  );
  const filters = <FilterRow board={board} view={view} phone={phone} onChange={changeView} />;

  return (
    <>
      <TopBarSlot actions={actions} />
      {phone ? null : (
        <header className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
            <h1 className="m-0 text-page-title tracking-display">Board</h1>
            {role ? <Chip tone="plain">{role}</Chip> : null}
            <span role="status">
              <Chip tone={board.line.andon ? 'rework' : 'ok'} dot size="lg">
                {board.line.andon ? `Line stopped: ${board.line.andon.reason}` : 'Line running'}
              </Chip>
            </span>
          </div>
          {filters}
          <div className="ml-auto flex min-w-0 items-center gap-2">
            <NeedsYouLink factory={factory} />
          </div>
        </header>
      )}
      {phone && board.line.andon ? (
        <div className="text-sm text-muted">
          <LineStatus andon={board.line.andon} />
        </div>
      ) : null}
      {phone && filtersOpen ? <div id={filtersId}>{filters}</div> : null}
      <Columns
        board={shown}
        onMove={onMove}
        decides={holds(factory, 'item:decide')}
        finishedThisWeek={board.line.touches.finished}
        arrived={arrived}
      />
      <StationTabs
        board={shown}
        selected={selected}
        onMove={onMove}
        decides={holds(factory, 'item:decide')}
        finishedThisWeek={board.line.touches.finished}
        arrived={arrived}
      />
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
          items={[{ ...moving.item, key: itemKey(factory.name, moving.item.ref) }]}
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
