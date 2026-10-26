// What the board says about stations and cards, in words, from the board's statistics. Derived
// only from station kinds and the API's numbers, never from station or factory names.
import type { Board, BoardLine, BoardStation, ItemCard } from '../../api/types';
import { formatDuration, formatPercent, formatUsd, formatUsdCompact, humanize, plural } from '../../shared/format';
import { describeState, type StateLabel, waitingOnYou } from '../../shared/item-state';
import type { HatchTone } from '../../ui/hatch';

/** "3 of 3, 2 ready", "2 of 5", "7 waiting", "4 today": a station's load. */
export function stationStat(s: BoardStation): string {
  switch (s.kind) {
    case 'agent': {
      const ready = s.waiting > 0 ? `, ${s.waiting} ready` : '';
      return `${s.in_progress}${s.wip_limit !== null ? ` of ${s.wip_limit}` : ' busy'}${ready}`;
    }
    case 'human': {
      const n = waitingOnYou(s);
      return s.wip_limit !== null ? `${n} of ${s.wip_limit}` : `${n} waiting`;
    }
    case 'queue':
      return `${s.waiting} waiting`;
    case 'done':
      return s.done_today !== null ? `${s.done_today} today` : `${s.items.length} done`;
  }
}

/** A column's count: "3 of 3" slots in use, "2 of 5" waiting on you, "7" waiting, "4 today". */
export function columnCount(s: BoardStation): string {
  if (s.kind === 'agent' && s.wip_limit !== null) return `${s.in_progress} of ${s.wip_limit}`;
  if (s.kind === 'queue') return `${s.waiting}`;
  return s.kind === 'agent' ? `${s.in_progress}` : stationStat(s);
}

/**
 * A station tile's meter: an agent station's slots in use (rust when its gate sends work back),
 * what waits on you at a human station; queues and done stations have nothing in progress.
 */
export function stationMeter(s: BoardStation): { value: number; max: number; tone: HatchTone } {
  switch (s.kind) {
    case 'agent':
      return {
        value: s.in_progress,
        max: s.wip_limit ?? Math.max(1, s.in_progress),
        tone: s.send_back_ratio ? 'rework' : 'run',
      };
    case 'human': {
      const n = waitingOnYou(s);
      return { value: n, max: s.wip_limit ?? Math.max(1, n), tone: 'run' };
    }
    default:
      return { value: 0, max: 1, tone: 'run' };
  }
}

/** ", queue growing" when the queue moves; nothing while it holds steady. */
function trend(s: BoardStation): string {
  return s.queue_trend === 'growing' || s.queue_trend === 'shrinking' ? `, queue ${s.queue_trend}` : '';
}

/**
 * The line under a station's load: how long the oldest item has waited in a queue, how busy an
 * agent station has been (its idle share when mostly idle) or how often its gate sends work back,
 * whether a human station waits on you (on its approvers, for someone who does not decide).
 */
export function stationNote(s: BoardStation, decides = true): string {
  switch (s.kind) {
    case 'queue':
      return s.oldest_waiting_seconds !== null ? `Oldest ${formatDuration(s.oldest_waiting_seconds * 1000)}` : 'Empty';
    case 'agent':
      if (s.send_back_ratio !== null) return `Sends back ${formatPercent(s.send_back_ratio)}`;
      if (s.busy_ratio === null) return s.agents.length > 0 ? s.agents.join(', ') : 'No agents';
      return s.busy_ratio < 0.5
        ? `Idle ${formatPercent(1 - s.busy_ratio)} of the time`
        : `Busy ${formatPercent(s.busy_ratio)}${trend(s)}`;
    case 'human':
      if (waitingOnYou(s) === 0) return 'Nothing waiting';
      return decides ? 'Waiting on you' : 'Waiting on approvers';
    case 'done':
      return '';
  }
}

/** The phone's sentence under the tabs for the constraint: "Build is the constraint: 2 of 6 busy, 3 waiting". */
export function constraintSentence(s: BoardStation): string {
  const busy = s.wip_limit !== null ? `${s.in_progress} of ${s.wip_limit} busy` : `${s.in_progress} busy`;
  return `${humanize(s.id)} is the constraint: ${busy}, ${s.waiting} waiting`;
}

/**
 * The line's sentence: under drum-buffer-rope with a buffer, the rope ("New work enters only when
 * Investigate's buffer drops below 2"), which goald does not enforce yet; otherwise which station
 * paces the line.
 */
export function lineSentence(line: BoardLine): { text: string; notEnforced: boolean } {
  const { policy, drum, buffer } = line.release;
  if (policy === 'drum-buffer-rope' && drum && buffer) {
    return { text: `New work enters only when ${humanize(drum)}’s buffer drops below ${buffer}`, notEnforced: true };
  }
  return {
    text: drum
      ? `${humanize(drum)} is the constraint; the line moves at its pace.`
      : 'No station holds the line back right now.',
    notEnforced: false,
  };
}

/** "WIP 9 of 15": work in progress at the stations with a limit, against their limits. */
/**
 * The station the phone board opens on: the first, in line order, with an item waiting on a
 * person, else with a blocked item (what needs attention), else the constraint, else the first.
 */
export function firstStation(board: Pick<Board, 'stations' | 'constraint'>): string {
  const withState = (state: ItemCard['state']) =>
    board.stations.find((s) => s.items.some((it) => it.state === state))?.id;
  return withState('waiting_human') ?? withState('blocked') ?? board.constraint ?? board.stations[0]?.id ?? '';
}

export function wipText(line: BoardLine): string {
  return `WIP ${line.wip.in_progress}${line.wip.limit !== null ? ` of ${line.wip.limit}` : ''}`;
}

/** A live expedite item: it skips the queue, so it is tinted in the station it is at. */
export function isExpedite(item: Pick<ItemCard, 'class_of_service' | 'state'>): boolean {
  return item.class_of_service === 'expedite' && item.state !== 'done' && item.state !== 'cancelled';
}

/** "1 of 1 in use": the expedite lane against its limit; `shown` items when the line has no lane. */
export function expediteUse(line: BoardLine, shown: number): string {
  const lane = line.expedite;
  if (!lane) return `${plural(shown, 'item')} skipping the queue`;
  return lane.limit !== null ? `${lane.in_use} of ${lane.limit} in use` : `${lane.in_use} in use`;
}

/** "Expedite 1 of 1 in use" for the line header; null when the line has no lane and no expedite item is on the board. */
export function expediteHeader(line: BoardLine, shown: number): string | null {
  if (!line.expedite && shown === 0) return null;
  return `Expedite ${expediteUse(line, shown)}`;
}

/** What the line says once its weekly budget is used up; null while there is room. */
export function budgetNote(line: BoardLine): string | null {
  const b = line.budget;
  if (!b?.exhausted) return null;
  return `Weekly budget used up: ${formatUsd(b.spent_usd)} of ${formatUsdCompact(b.weekly_usd)}. No new agent work starts until the week’s spend falls below it.`;
}

/** A working item's doing line: "Working", with its turn once the agent has taken one. */
export function doingText(item: Pick<ItemCard, 'turn'>): string {
  return item.turn ? `Working, turn ${item.turn}` : 'Working';
}

/** One segment of a plan bar: a todo done, the one in hand, or a todo still to do. */
export type PlanSegment = 'done' | 'current' | 'todo';

/**
 * A plan bar's segments, one per todo: the first `done` are done (`done` clamped to the total), the
 * next is current while the plan has one left, the rest are todo. No segments when the plan lists none.
 */
export function planSegments(plan: NonNullable<ItemCard['plan_steps']>): PlanSegment[] {
  const total = Math.max(0, plan.total);
  const done = Math.min(Math.max(0, plan.done), total);
  return Array.from({ length: total }, (_, i): PlanSegment => {
    if (i < done) return 'done';
    return i === done ? 'current' : 'todo';
  });
}

const times = ['once', 'twice'];

/** The most characters of a done item's summary a card shows; the item page shows all of it. */
const excerptLength = 100;

/**
 * The first line of a summary with its markdown markers (headings, bullets, numbering, quotes,
 * bold and code marks) taken off, or null when no line has words in it.
 */
function summaryLine(summary: string): string | null {
  for (const line of summary.split('\n')) {
    const text = line
      .replace(/^(?:#{1,6}(?:\s+|$)|[-*+](?:\s+|$)|>\s*|\d+[.)](?:\s+|$))+/, '')
      .replace(/\*\*|`/g, '')
      .trim();
    if (text) return text;
  }
  return null;
}

/**
 * A short excerpt of a done item's summary for its card: the first line, cut at a word boundary to
 * at most 100 characters and ended with '…' when cut. Null when the summary has no words.
 */
export function summaryExcerpt(summary: string): string | null {
  const line = summaryLine(summary);
  if (line === null) return null;
  const chars = Array.from(line);
  if (chars.length <= excerptLength) return line;
  const head = chars.slice(0, excerptLength - 1).join('');
  const lastSpace = head.search(/\s\S*$/);
  return `${(lastSpace > 0 ? head.slice(0, lastSpace) : head).trimEnd()}…`;
}

/**
 * A card's state line: the doing line while worked (what the run is doing now, else "Working", with
 * its turn), "Needs your OK", work waiting in the drum's buffer or held back by the rope, how often
 * work was sent back, what a done item became (an excerpt of its summary); otherwise the item's state.
 */
export function cardState(
  item: Pick<
    ItemCard,
    'state' | 'agent' | 'turn' | 'rework_count' | 'summary' | 'in_buffer' | 'rope_held' | 'activity' | 'waiting'
  >,
  decides = true,
): StateLabel {
  switch (item.state) {
    case 'in_progress':
      return { text: item.activity || doingText(item), tone: 'run' };
    case 'waiting_human':
      return decides ? { text: 'Needs your OK', tone: 'you' } : { text: 'Waiting on an approver', tone: 'muted' };
    case 'done':
      return { text: summaryExcerpt(item.summary ?? '') ?? 'Done', tone: 'ok' };
    case 'queued':
    case 'ready':
      if (item.waiting) return waitingReason(item.waiting);
      if (item.in_buffer) return { text: 'Ready in buffer', tone: 'muted' };
      if (item.rope_held) return { text: 'Waiting for buffer room', tone: 'muted' };
      if (item.rework_count > 0) {
        return { text: `Sent back ${times[item.rework_count - 1] ?? `${item.rework_count} times`}`, tone: 'rework' };
      }
      if (item.state === 'ready') return { text: 'Review approved, checks passed', tone: 'ok' };
      break;
  }
  return describeState(item);
}

/**
 * Why a queued or ready item waits on another item, as the API gives its refs: "Starts after sre#3
 * merges" for an item it needs, "Waits for sre#3: same files (internal/api/**)" for one whose files
 * overlap its own.
 */
function waitingReason(waiting: NonNullable<ItemCard['waiting']>): StateLabel {
  const refs = waiting.items.join(', ');
  if (waiting.kind === 'files') {
    return { text: `Waits for ${refs}: same files (${waiting.files.join(', ')})`, tone: 'rework' };
  }
  const merge = waiting.items.length > 1 ? 'merge' : 'merges';
  return { text: `Starts after ${refs} ${merge}`, tone: 'muted' };
}
