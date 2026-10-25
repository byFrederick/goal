// The inbox drawer's groups (E §10.2): what waits on a person, grouped by what they do about it, one
// row per entry, oldest first. Pure, so the grouping and each row's action are tested without a DOM.
// Expedite entries wait on no person and the board already shows them, so they are left out; a kind
// this build does not know is an Open row under Needs a decision.
import type { InboxEntry, ItemCard } from '../api/types';
import { humanize } from './format';

export type InboxGroupId = 'ready' | 'decide' | 'blocked';

/** The one action a row offers; `open` leads to the item's page or to the board. */
export type InboxAction = 'merge' | 'approve' | 'retry' | 'open';

export interface InboxRow {
  /** The entry's id: unique within the inbox. */
  key: string;
  entry: InboxEntry;
  /** The item the entry is about, or null for an entry that is about no item. */
  item: ItemCard | null;
  action: InboxAction;
  /** The row's title: the item's, or what the entry is when it is about no item. */
  title: string;
  /** The status icon: check for ready to merge, clock for a decision or a blocked row. */
  status: 'ready' | 'waiting';
  /** Why it waits, in one line. */
  why: string;
  /** Where the row leads: the item's page, or the board (andon, rework and entries about no item). */
  to: 'item' | 'board';
}

export interface InboxGroup {
  id: InboxGroupId;
  title: string;
  rows: InboxRow[];
}

const titles: Record<InboxGroupId, string> = {
  ready: 'Ready to merge',
  decide: 'Needs a decision',
  blocked: 'Blocked',
};

const order: InboxGroupId[] = ['ready', 'decide', 'blocked'];

type Placed = Pick<InboxRow, 'action' | 'status' | 'why' | 'to'> & { group: InboxGroupId };

/** Where an entry goes, its action and its words; null for expedite, which waits on no person. */
function place(e: InboxEntry): Placed | null {
  const item = e.item;
  const station = humanize(e.station ?? item?.station ?? '');
  switch (e.kind) {
    case 'expedite':
      return null;
    case 'decision':
      if (!item)
        return { group: 'decide', action: 'open', status: 'waiting', why: `Waiting at ${station}`, to: 'board' };
      if (e.action === 'merge') {
        return { group: 'ready', action: 'merge', status: 'ready', why: `Ready to merge at ${station}`, to: 'item' };
      }
      return {
        group: 'decide',
        action: 'approve',
        status: 'waiting',
        why: `Waiting for your decision at ${station}`,
        to: 'item',
      };
    case 'blocked': {
      const reason = e.blocked?.reason ? `: ${e.blocked.reason}` : '';
      const why = `${e.blocked?.paused ? 'Paused' : 'Blocked'} at ${station}${reason}`;
      return item
        ? { group: 'blocked', action: 'retry', status: 'waiting', why, to: 'item' }
        : { group: 'blocked', action: 'open', status: 'waiting', why, to: 'board' };
    }
    case 'andon':
      return {
        group: 'blocked',
        action: 'open',
        status: 'waiting',
        why: `Line stopped: ${e.andon?.reason ?? 'the andon cord is pulled'}`,
        to: 'board',
      };
    case 'rework_warning':
      return {
        group: 'decide',
        action: 'open',
        status: 'waiting',
        why: e.rework?.top_reason ? humanize(e.rework.top_reason) : 'Rework is rising',
        to: 'board',
      };
    default:
      return {
        group: 'decide',
        action: 'open',
        status: 'waiting',
        why: [humanize(e.kind), station].filter(Boolean).join(' at '),
        to: item ? 'item' : 'board',
      };
  }
}

/** The row's title: the item's, or what the entry is when it is about no item. */
function titleOf(e: InboxEntry): string {
  if (e.item) return e.item.title;
  switch (e.kind) {
    case 'andon':
      return 'Line stopped';
    case 'rework_warning':
      return e.rework
        ? `${humanize(e.rework.station)} sent back ${e.rework.rejected} of the last ${e.rework.verdicts}`
        : 'Rework is rising';
    default:
      return humanize(e.kind);
  }
}

/**
 * The inbox's groups in their order (Ready to merge, Needs a decision, Blocked), each with its rows
 * oldest first. Groups with no rows are left out.
 */
export function inboxGroups(entries: readonly InboxEntry[]): InboxGroup[] {
  const byGroup = new Map<InboxGroupId, InboxRow[]>();
  for (const e of entries) {
    const placed = place(e);
    if (!placed) continue;
    const { group, ...words } = placed;
    const rows = byGroup.get(group) ?? [];
    rows.push({ key: e.id, entry: e, item: e.item, title: titleOf(e), ...words });
    byGroup.set(group, rows);
  }
  return order.flatMap((id) => {
    const rows = byGroup.get(id);
    if (!rows) return [];
    rows.sort((a, b) => Date.parse(a.entry.since) - Date.parse(b.entry.since));
    return [{ id, title: titles[id], rows }];
  });
}

/** How many rows the groups hold: the count the Inbox button shows. */
export function inboxCount(groups: readonly InboxGroup[]): number {
  return groups.reduce((n, g) => n + g.rows.length, 0);
}
