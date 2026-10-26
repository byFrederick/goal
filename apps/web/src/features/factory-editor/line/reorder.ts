// Reordering the line's stations (D §10.6): the ↑ ↓ buttons, Alt+ArrowUp/Down and a drop all go
// through moveStation; this says where a step up or down lands, whether it is allowed (Done stays
// last and nothing passes it) and what the live region says afterwards.
import type { ModelStation } from '../../../shared/definition/model';

export type Direction = -1 | 1;

/** The index a station lands on one step up (-1) or down (1); undefined when it cannot move. */
export function stepTarget(stations: readonly ModelStation[], id: string, dir: Direction): number | undefined {
  const from = stations.findIndex((s) => s.id === id);
  if (from < 0 || stations[from]?.kind === 'done') return undefined;
  const to = from + dir;
  const last = stations.at(-1)?.kind === 'done' ? stations.length - 2 : stations.length - 1;
  return to < 0 || to > last ? undefined : to;
}

/** The index a dragged station lands on when dropped on another row; undefined when not allowed. */
export function dropTarget(stations: readonly ModelStation[], id: string, over: string): number | undefined {
  const from = stations.findIndex((s) => s.id === id);
  const to = stations.findIndex((s) => s.id === over);
  if (from < 0 || to < 0 || from === to || stations[from]?.kind === 'done') return undefined;
  const last = stations.at(-1)?.kind === 'done' ? stations.length - 2 : stations.length - 1;
  return to > last ? undefined : to;
}

/** Why a step is not possible, for the disabled button's description; undefined when it is. */
export function stepBlocked(stations: readonly ModelStation[], id: string, dir: Direction): string | undefined {
  if (stepTarget(stations, id, dir) !== undefined) return undefined;
  const at = stations.findIndex((s) => s.id === id);
  if (stations[at]?.kind === 'done') return 'Done stays last';
  if (dir === -1) return 'Already first';
  return stations[at + 1]?.kind === 'done' ? 'Nothing moves past Done' : 'Already last';
}

/** "Build moved to position 3 of 7". */
export function movedMessage(name: string, index: number, total: number): string {
  return `${name} moved to position ${index + 1} of ${total}`;
}
