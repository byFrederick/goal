// Where a person can move an item, and what goald does with it there, from the line alone (kinds
// and order, never station names): the Move menu's choices and the board's optimistic update.
import type { Board, ItemState, Station } from '../api/types';

type LineStation = Pick<Station, 'id' | 'kind'>;

export interface MoveChoice extends LineStation {
  /** Upstream of the item: the move counts as rework and needs a reason. */
  upstream: boolean;
}

/** Every station of the line but the item's own and done stations (work finishes by a decision). */
export function moveChoices(stations: LineStation[], current: string): MoveChoice[] {
  const at = stations.findIndex((s) => s.id === current);
  return stations
    .map((s, i) => ({ id: s.id, kind: s.kind, upstream: at >= 0 && i < at }))
    .filter((s) => s.id !== current && s.kind !== 'done');
}

/**
 * The choices for several items moved together: every station one of them could go to, in line
 * order. A station counts as upstream when it is upstream of any of the items.
 */
export function moveChoicesFor(stations: LineStation[], currents: string[]): MoveChoice[] {
  const byId = new Map<string, MoveChoice>();
  for (const current of new Set(currents)) {
    for (const choice of moveChoices(stations, current)) {
      const seen = byId.get(choice.id);
      byId.set(choice.id, seen ? { ...seen, upstream: seen.upstream || choice.upstream } : choice);
    }
  }
  return stations.flatMap((s) => byId.get(s.id) ?? []);
}

/** The state an item arrives in at a station of this kind. */
export function arrivalState(kind: Station['kind']): ItemState {
  return kind === 'human' ? 'waiting_human' : 'queued';
}

/** The board with one card moved to station `to`, as goald will answer it; the input is unchanged. */
export function movedOnBoard(board: Board, id: string, to: string, stations: LineStation[]): Board {
  const card = board.stations.flatMap((s) => s.items).find((it) => it.id === id);
  const target = stations.find((s) => s.id === to);
  if (!card || !target) return board;

  const upstream = moveChoices(stations, card.station).find((c) => c.id === to)?.upstream ?? false;
  const moved = {
    ...card,
    station: to,
    state: arrivalState(target.kind),
    rework_count: card.rework_count + (upstream ? 1 : 0),
    agent: null,
  };

  return {
    ...board,
    stations: board.stations.map((s) => {
      const items = s.items.filter((it) => it.id !== id);
      return { ...s, items: s.id === to ? [...items, moved] : items };
    }),
  };
}
