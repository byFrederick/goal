// The Board's stations as the line draws them (ui/line.tsx). One mapping serves the Floor's line and Home's line, so
// both say the same about a station: what waits, its busy or send-back share, its agent slots and its constraint.
import type { Board, BoardLane, BoardStation } from '../../api/types';
import { formatAge, formatPercent, humanize } from '../../shared/format';
import type { LineStation } from '../../ui/line';

/** The stations of a board, from intake to done, with the constraint and the Done count filled in. */
export function lineStations(
  board: Board,
  constraint: string | null,
  doneThisWeek: number | null,
  merges = false,
): LineStation[] {
  // The drum is the station the buffer feeds; it only means something under drum-buffer-rope.
  const drum = board.line.release.policy === 'drum-buffer-rope' ? board.line.release.drum : null;
  return board.stations.map((s) => {
    const done = s.kind === 'done';
    const slots = slotsOf(s);
    return {
      id: s.id,
      name: humanize(s.id),
      count: done ? (s.done_today ?? 0) : s.waiting + s.in_progress,
      load: done ? null : loadOf(s),
      constraint: !done && s.id === constraint,
      done,
      waits: waitsOf(s, drum, doneThisWeek, merges),
      slots: slots?.slots ?? null,
      slotsLabel: slots?.label,
      share: busyOf(s),
    };
  });
}

/** The busy share as the bar's load, or null where the station has none. */
function loadOf(s: BoardStation): number | null {
  return s.busy_ratio;
}

/** What waits at the station, in the words the line uses; one line each. Done says "merged" only on a line with code. */
export function waitsOf(s: BoardStation, drum: string | null, doneThisWeek: number | null, merges = false): string[] {
  if (s.kind === 'done') {
    return [
      s.done_today !== null ? `${s.done_today} ${merges ? 'merged' : 'done'} today` : null,
      doneThisWeek !== null ? `${doneThisWeek} this week` : null,
    ].filter((line): line is string => line !== null);
  }
  if (s.kind === 'queue') {
    return [
      `${s.waiting} waiting`,
      s.oldest_waiting_seconds !== null ? `Oldest ${formatAge(s.oldest_waiting_seconds)}` : null,
    ].filter((line): line is string => line !== null);
  }
  if (s.kind === 'human') return [`${s.in_progress} waiting on you`];
  if (s.id === drum) return [`${s.waiting} ready in buffer`];
  return [`${s.in_progress} in progress`];
}

/** "Busy 94%" where the station has a busy share, else "Sends back 14%" where it has a send-back share. */
export function busyOf(s: BoardStation): string | null {
  if (s.busy_ratio !== null) return `Busy ${formatPercent(s.busy_ratio)}`;
  if (s.send_back_ratio !== null) return `Sends back ${formatPercent(s.send_back_ratio)}`;
  return null;
}

/**
 * The station's agent slots: busy ones are working, waiting ones are items waiting on a person, and the rest are
 * free. Waiting is capped at the slots that are not busy, so the squares never outnumber the slots. Named by the
 * role, never by a copy.
 */
export function slotsOf(
  s: BoardStation,
): { slots: { total: number; busy: number; waiting: number }; label: string } | null {
  const agents: BoardLane[] = (s.lanes ?? []).filter((lane) => lane.kind === 'agent');
  const total = agents.find((lane) => lane.slots !== null)?.slots;
  const role = agents[0]?.role;
  if (!total || !role) return null;

  const busy = Math.min(
    agents.reduce((n, lane) => n + lane.busy, 0),
    total,
  );
  const waiting = Math.min(s.items.filter((it) => it.state === 'waiting_human').length, total - busy);
  const free = total - busy - waiting;
  return {
    slots: { total, busy, waiting },
    label: `${role}: ${busy} working, ${waiting} waiting on you, ${free} free`,
  };
}
