// The board's filters and grouping as pure functions over the board response: what the Assembly,
// Agent role and Status choices offer, which cards they keep, and how a station's cards group by
// assembly. The choices live in the board route's search params (assembly, role, status, group),
// so a link reproduces the view. Nothing here reads a station or factory name.
import type { Board, BoardStation, ItemCard, PartsSummary } from '../../api/types';
import { itemStatus } from '../../shared/item-state';
import type { StatusKind } from '../../ui/status-icon';

export const statusChoices: { value: StatusKind; label: string }[] = [
  { value: 'working', label: 'Working' },
  { value: 'waiting', label: 'Waiting for another item' },
  { value: 'same-files', label: 'Waiting for same files' },
  { value: 'ready', label: 'Ready' },
];

export type BoardGroup = 'assembly';

/** The choices the filters and the grouping hold; an absent field is "Any" or "None". */
export interface BoardView {
  /** The search: a card matches by its title or its number ("#12" or "12"). */
  q?: string | undefined;
  type?: string | undefined;
  lane?: string | undefined;
  assembly?: string | undefined;
  role?: string | undefined;
  status?: StatusKind | undefined;
  group?: BoardGroup | undefined;
}

export interface AssemblyOption {
  id: string;
  title: string;
}

/** An open assembly, headed over its grouped parts: its ref, its title and where its parts stand. */
export interface OpenAssembly {
  id: string;
  ref: string;
  title: string;
  parts: PartsSummary;
}

/** The board's open assemblies by id. Only these get a header; the others keep their assembly line. */
export function openAssemblies(board: Board): Map<string, OpenAssembly> {
  const open = new Map<string, OpenAssembly>();
  for (const a of board.assemblies) {
    if (a.parts) open.set(a.id, { id: a.id, ref: a.ref, title: a.title, parts: a.parts });
  }
  return open;
}

function allItems(board: Board): ItemCard[] {
  return board.stations.flatMap((s) => s.items);
}

/** Each assembly on the board, by title: the open ones and those of the parts on the board. */
export function assemblyOptions(board: Board): AssemblyOption[] {
  const byId = new Map<string, AssemblyOption>();
  for (const a of board.assemblies) byId.set(a.id, { id: a.id, title: a.title });
  for (const it of allItems(board)) {
    if (it.assembly && !byId.has(it.assembly.id))
      byId.set(it.assembly.id, { id: it.assembly.id, title: it.assembly.title });
  }
  return [...byId.values()].sort((a, b) => a.title.localeCompare(b.title));
}

/** The distinct work types on the board's cards, by name. */
export function typeOptions(board: Board): string[] {
  const types = new Set(
    allItems(board)
      .map((it) => it.type)
      .filter((type): type is string => Boolean(type)),
  );
  return [...types].sort((a, b) => a.localeCompare(b));
}

/** The distinct lanes (the work's lane, not a station lane) on the board's cards, by name. */
export function laneOptions(board: Board): string[] {
  const lanes = new Set(
    allItems(board)
      .map((it) => it.lane)
      .filter((lane): lane is string => Boolean(lane)),
  );
  return [...lanes].sort((a, b) => a.localeCompare(b));
}

/** The distinct lane roles on the board, by name. */
export function roleOptions(board: Board): string[] {
  const roles = new Set(board.stations.flatMap((s) => (s.lanes ?? []).map((lane) => lane.role)));
  return [...roles].sort((a, b) => a.localeCompare(b));
}

/**
 * The view with the choices the board does not offer dropped: an assembly that left the board, a
 * role no lane has, an unknown status or group. The page shows all of them, never a filter it cannot show.
 */
export function knownView(board: Board, view: BoardView): BoardView {
  const assembly = assemblyOptions(board).some((o) => o.id === view.assembly) ? view.assembly : undefined;
  const type = typeOptions(board).includes(view.type ?? '') ? view.type : undefined;
  const lane = laneOptions(board).includes(view.lane ?? '') ? view.lane : undefined;
  const role = roleOptions(board).includes(view.role ?? '') ? view.role : undefined;
  const status = statusChoices.some((s) => s.value === view.status) ? view.status : undefined;
  const group = view.group === 'assembly' ? view.group : undefined;
  const q = view.q?.trim() ? view.q.trim() : undefined;
  return { q, type, lane, assembly, role, status, group };
}

/** Whether a search matches a card: its title, or its number with or without the "#". */
export function searchMatches(item: Pick<ItemCard, 'title' | 'number'>, q: string): boolean {
  const query = q.trim().toLowerCase();
  if (!query) return true;
  const number = query.startsWith('#') ? query.slice(1) : query;
  return item.title.toLowerCase().includes(query) || String(item.number) === number;
}

/** The role of the lane a card names in board_lane, within its station; undefined for an unclaimed card. */
function laneRole(station: BoardStation, item: ItemCard): string | undefined {
  return station.lanes?.find((lane) => lane.key === item.board_lane)?.role;
}

/** A card matches an assembly when it is the assembly itself or one of its parts. */
function matches(station: BoardStation, item: ItemCard, view: BoardView): boolean {
  if (view.q && !searchMatches(item, view.q)) return false;
  if (view.type && item.type !== view.type) return false;
  if (view.lane && item.lane !== view.lane) return false;
  if (view.assembly && item.id !== view.assembly && item.assembly?.id !== view.assembly) return false;
  if (view.role && laneRole(station, item) !== view.role) return false;
  if (view.status && itemStatus(item) !== view.status) return false;
  return true;
}

/**
 * The cards of one station grouped by assembly: each assembly's parts together in part order, the
 * assemblies in the order of their first part, then the cards of no assembly in their own order.
 * A lane keeps this order, so its working cards and waiting rows stay in it too.
 */
export function groupByAssembly(items: ItemCard[]): ItemCard[] {
  const groups = new Map<string, ItemCard[]>();
  const loose: ItemCard[] = [];
  for (const it of items) {
    const key = it.assembly?.id;
    if (key === undefined) {
      loose.push(it);
      continue;
    }
    const members = groups.get(key);
    if (members) members.push(it);
    else groups.set(key, [it]);
  }
  const grouped = [...groups.values()].flatMap((members) =>
    members.sort((a, b) => (a.assembly?.part ?? 0) - (b.assembly?.part ?? 0)),
  );
  return [...grouped, ...loose];
}

/** A run of consecutive cards of one assembly (undefined: of none), in the order a list keeps them. */
export interface Run {
  assembly: string | undefined;
  items: ItemCard[];
}

/** A list split into its runs of one assembly: a header goes over each run of an open assembly. */
export function runsOf(items: ItemCard[]): Run[] {
  const runs: Run[] = [];
  for (const it of items) {
    const assembly = it.assembly?.id;
    const last = runs.at(-1);
    if (last && last.assembly === assembly) last.items.push(it);
    else runs.push({ assembly, items: [it] });
  }
  return runs;
}

/**
 * The board as the view shows it: each station's cards that match, grouped when asked. Stations,
 * lanes and the line keep their numbers and their names, so only the cards change.
 */
export function arrangeBoard(board: Board, view: BoardView): Board {
  return {
    ...board,
    stations: board.stations.map((station) => {
      const items = station.items.filter((it) => matches(station, it, view));
      return { ...station, items: view.group === 'assembly' ? groupByAssembly(items) : items };
    }),
  };
}
