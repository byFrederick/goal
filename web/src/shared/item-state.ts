// What an item's state means to the person looking at it, and which decisions they can take.
// Derived only from the API's kinds and states, never from station or factory names.
import type { ItemCard, ItemState, Station } from '../api/types';

export type Tone = 'run' | 'you' | 'rework' | 'muted' | 'ok';

/** Text colour for a tone. */
export const toneText: Record<Tone, string> = {
  run: 'text-run',
  you: 'text-you',
  rework: 'text-rework',
  muted: 'text-muted',
  ok: 'text-ok',
};

export interface StateLabel {
  text: string;
  tone: Tone;
}

/**
 * An item's state in words. A decision waits on "you" only for someone who decides there
 * (`decides`: the factory's item:decide); anyone else sees it waiting on an approver.
 */
export function describeState(
  item: Pick<ItemCard, 'state' | 'agent' | 'rework_count'>,
  opts: { atConstraint?: boolean; decides?: boolean } = {},
): StateLabel {
  switch (item.state) {
    case 'in_progress':
      return { text: item.agent ? `${item.agent} working` : 'In progress', tone: 'run' };
    case 'waiting_human':
      return opts.decides === false
        ? { text: 'Waiting on an approver', tone: 'muted' }
        : { text: 'Waiting on you', tone: 'you' };
    case 'blocked':
      return { text: 'Blocked, needs a retry', tone: 'rework' };
    case 'ready':
      return { text: opts.atConstraint ? 'Ready in buffer' : 'Ready', tone: 'muted' };
    case 'queued':
      return {
        text: item.rework_count > 0 ? 'Sent back, waiting' : 'Waiting',
        tone: item.rework_count > 0 ? 'rework' : 'muted',
      };
    case 'done':
      return { text: 'Done', tone: 'ok' };
    case 'cancelled':
      return { text: 'Cancelled', tone: 'muted' };
  }
}

export const stateNames: Record<ItemState, string> = {
  queued: 'Waiting',
  ready: 'Ready',
  in_progress: 'In progress',
  waiting_human: 'Waiting on you',
  blocked: 'Blocked',
  done: 'Done',
  cancelled: 'Cancelled',
};

export interface Decisions {
  approve: boolean;
  reject: boolean;
  /** Merge the pull request, then approve (goald does both). */
  merge: boolean;
  retry: boolean;
}

/**
 * The decisions a person can take on an item. A human station whose action is merge merges the
 * pull request instead of approving (goald approves after the merge).
 */
export function decisionsFor(
  item: { state: ItemState },
  station: Pick<Station, 'kind' | 'action'> | undefined,
): Decisions {
  const deciding = item.state === 'waiting_human' && station?.kind === 'human';
  const merging = station?.action === 'merge';
  return {
    approve: deciding && !merging,
    merge: deciding && merging,
    reject: deciding,
    retry: item.state === 'blocked',
  };
}

export interface Controls {
  /** Stop it where it is until a retry (blocked, with a paused event). */
  pause: boolean;
  /** Send it back to its station's rework target with a reason. */
  sendBack: boolean;
  /** Put it at another station of the line. */
  move: boolean;
}

/**
 * A person's controls of an open item's flow (goald stops a run in progress for each): pause what
 * is not blocked already, send back what waits, works or is blocked when its station has a rework
 * target (Item.flow.rework_target), move anything open.
 */
export function controlsFor(item: { state: ItemState }, reworkTarget: string | null | undefined): Controls {
  const open = item.state !== 'done' && item.state !== 'cancelled';
  return {
    pause: open && item.state !== 'blocked',
    sendBack: Boolean(reworkTarget) && ['waiting_human', 'in_progress', 'blocked'].includes(item.state),
    move: open,
  };
}

/** Whether a blocked item was paused by a person (its latest block is a paused event): Resume, not Retry. */
export function pausedNow(state: ItemState, events: readonly { kind: string }[]): boolean {
  if (state !== 'blocked') return false;
  return events.findLast((e) => e.kind === 'paused' || e.kind === 'blocked')?.kind === 'paused';
}

/** Items waiting on a person at a station (the board's waiting count covers queued and ready only). */
export function waitingOnYou(station: { kind: string; items: Pick<ItemCard, 'state'>[] }): number {
  return station.kind === 'human' ? station.items.filter((it) => it.state === 'waiting_human').length : 0;
}

function urlOf(v: unknown): string | null {
  if (typeof v === 'string' && /^https?:\/\//.test(v)) return v;
  if (v && typeof v === 'object') {
    const o = v as Record<string, unknown>;
    return urlOf(o.html_url) ?? urlOf(o.url);
  }
  return null;
}

/** The pull request a station output names (its pull_request field), or null. */
export function pullRequestIn(output: unknown): string | null {
  if (!output || typeof output !== 'object') return null;
  const o = output as Record<string, unknown>;
  return urlOf(o.pull_request) ?? urlOf(o.pr_url);
}

/**
 * The pull request an item links to: the latest one its station outputs name, in line order
 * (the item endpoint has no pr_url; cards do).
 */
export function pullRequestOf(outputs: Record<string, unknown>, lineOrder: string[]): string | null {
  const stations = Object.keys(outputs).sort((a, b) => lineOrder.indexOf(b) - lineOrder.indexOf(a));
  for (const s of stations) {
    const pr = pullRequestIn(outputs[s]);
    if (pr) return pr;
  }
  return null;
}

/** "#41" from a GitHub pull request URL, or "Pull request" when it is not one. */
export function pullRequestLabel(url: string): string {
  const m = url.match(/\/pull\/(\d+)/);
  return m ? `PR #${m[1]}` : 'Pull request';
}
