// The item page's stepper: one step per station of the factory, in the factory's order. Each step's
// time comes from the item's flow (its spans), its outcome from the decisions made at that station
// (the events), and the lead-time target from the flow's target_seconds.
import type { Item, ItemEvent, ItemFlow, ItemSpan, Station } from '../../api/types';
import { formatDuration, humanize } from '../../shared/format';
import type { Step } from '../../ui/stepper';
import { formatTarget } from './time';

export const noFlow: ItemFlow = {
  spans: [],
  target_seconds: null,
  rework_target: null,
  budget: { per_item_usd: null, max_turns_per_task: null },
  current_run: null,
};

/** A step with its detail line only when there is one. */
function step(name: string, state: Step['state'], detail: string | undefined): Step {
  return detail ? { name, state, detail } : { name, state };
}

/** An item's visits to one station: consecutive spans there, with the time spent. */
export function visitsAt(spans: readonly ItemSpan[], station: string, end: number) {
  let visits = 0;
  let spent = 0;
  spans.forEach((s, i) => {
    if (s.station !== station) return;
    if (spans[i - 1]?.station !== station) visits++;
    spent += Math.max(0, (s.left_at ? Date.parse(s.left_at) : end) - Date.parse(s.entered_at));
  });
  return { visits, spent };
}

/** What the decisions at a station came to: "accepted", "sent back 2×", or both. Undefined when none. */
export function outcomeAt(events: readonly ItemEvent[], station: string): string | undefined {
  const at = events.filter((e) => e.station === station);
  const accepted = at.some((e) => e.kind === 'approved' || e.kind === 'gate_approved');
  const backs = at.filter((e) => e.kind === 'rejected' || e.kind === 'gate_rejected').length;
  const parts = [backs > 0 ? `sent back ${backs}×` : null, accepted ? 'accepted' : null].filter(Boolean);
  return parts.length > 0 ? parts.join(', ') : undefined;
}

export interface StepsInput {
  item: Item;
  /** The factory's stations, in order (factory.stations). */
  stations: readonly Station[];
  events: readonly ItemEvent[];
  flow: ItemFlow;
  /** Now, for the time so far of an open span. */
  now: number;
}

/**
 * The stepper's steps. A visited station is done and says its time and outcome; the item's station is
 * the current step and says the time so far, and "waiting on you" when the item waits for a person.
 * An unvisited station shows its description. The done station says the lead time: the target and the
 * time so far, or the time so far without a target.
 */
export function stepsOf({ item, stations, events, flow, now }: StepsInput): Step[] {
  const end = item.done_at ? Date.parse(item.done_at) : now;
  const waits = item.state === 'waiting_human';
  return stations.map((s): Step => {
    const name = humanize(s.id);
    const current = s.id === item.station;
    const { visits, spent } = visitsAt(flow.spans, s.id, end);

    if (s.kind === 'done') {
      const elapsed = formatDuration(end - Date.parse(item.created_at));
      const detail = flow.target_seconds
        ? `Target ${formatTarget(flow.target_seconds, 'short')}, ${elapsed} so far`
        : `${elapsed} so far`;
      const state = item.state === 'done' ? 'done' : current ? 'current' : 'todo';
      return step(name, state, detail);
    }
    if (current) {
      const parts = [visits > 0 ? formatDuration(spent) : null, waits ? 'waiting on you' : null].filter(Boolean);
      return step(name, 'current', parts.join(', '));
    }
    if (visits > 0) {
      const outcome = outcomeAt(events, s.id);
      return step(name, 'done', [formatDuration(spent), outcome].filter(Boolean).join(', '));
    }
    return step(name, 'todo', s.description);
  });
}

/**
 * The phone's summary line under the stepper: the time so far at the item's station, the time in the
 * line, and the lead-time target when the flow has one, e.g. "2 h 12 min in Build, 3 h in the line of a
 * 48 h target".
 */
export function summaryOf({ item, flow, now }: Pick<StepsInput, 'item' | 'flow' | 'now'>): string {
  const end = item.done_at ? Date.parse(item.done_at) : now;
  const atStation = visitsAt(flow.spans, item.station, end).spent;
  const inLine = formatDuration(end - Date.parse(item.created_at));
  const target = flow.target_seconds ? ` of a ${formatTarget(flow.target_seconds, 'short')} target` : '';
  return `${formatDuration(atStation)} in ${humanize(item.station)}, ${inLine} in the line${target}`;
}
