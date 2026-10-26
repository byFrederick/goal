// What an item's state means to the person looking at it, and which decisions they can take.
// Derived only from the API's kinds and states, never from station or factory names.
import type { ItemCard, ItemState, Station } from '../api/types';
import { humanize } from './format';

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
 * (`decides`: the factory's item:decide); anyone else sees it waiting on an approver. `agent` is
 * the agent's name; it reads as its role ("Infra engineer working").
 */
export function describeState(
  item: Pick<ItemCard, 'state' | 'agent' | 'rework_count'>,
  opts: { atConstraint?: boolean; decides?: boolean } = {},
): StateLabel {
  switch (item.state) {
    case 'in_progress':
      return { text: item.agent ? `${humanize(item.agent)} working` : 'In progress', tone: 'run' };
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
    case 'assembly':
      return { text: 'Assembly', tone: 'muted' };
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
  assembly: 'Assembly',
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

/**
 * The pull requests a station output names: its pull_request (or the older pr_url) and its
 * pull_requests list (spec 4 §4.7: an item may open several, in several repositories), each once.
 */
export function pullRequestsIn(output: unknown): string[] {
  if (!output || typeof output !== 'object') return [];
  const o = output as Record<string, unknown>;
  const list = Array.isArray(o.pull_requests) ? o.pull_requests.map(urlOf) : [];
  return [...new Set([urlOf(o.pull_request) ?? urlOf(o.pr_url), ...list].filter((u): u is string => u !== null))];
}

/**
 * Every pull request an item's outputs name, as the merge station merges them: station by station
 * in line order, each once (the item endpoint has none; a card's pull_requests are these at a merge station).
 */
export function pullRequestsOf(outputs: Record<string, unknown>, lineOrder: string[]): string[] {
  const rank = (s: string) => (lineOrder.includes(s) ? lineOrder.indexOf(s) : lineOrder.length);
  const stations = Object.keys(outputs).sort((a, b) => rank(a) - rank(b));
  return [...new Set(stations.flatMap((s) => pullRequestsIn(outputs[s])))];
}

/**
 * "PR #41" from a GitHub pull request URL, "MR !3" from a GitLab merge request's, with the
 * repository when asked ("acme/web PR #41"); "Pull request" for any other URL.
 */
export function pullRequestLabel(url: string, withRepo = false): string {
  const github = url.match(/^https?:\/\/[^/]+\/(.+?)\/pull\/(\d+)/);
  if (github) return `${withRepo ? `${github[1]} ` : ''}PR #${github[2]}`;
  const gitlab = url.match(/^https?:\/\/[^/]+\/(.+?)\/-\/merge_requests\/(\d+)/);
  if (gitlab) return `${withRepo ? `${gitlab[1]} ` : ''}MR !${gitlab[2]}`;
  return 'Pull request';
}
