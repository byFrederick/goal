// What the organization's stream invalidates (C §10.10). Pure, so the rules are tested without an
// EventSource.
import type { QueryKey } from '@tanstack/react-query';
import { keys } from '../api/queries';
import type { StreamEvent } from '../api/types';

/**
 * The factory's work keys an item event can change, named one by one: never the factory's bare
 * prefix, under which later phases keep a definition draft, versions and credentials that must not
 * refetch while agents work. E's keys (weekly … agent) are named here before their queries exist.
 */
const workKeys = ['board', 'metrics', 'items', 'weekly', 'lead-time', 'constraint', 'andon', 'agent'] as const;

/** Run progress: it changes a card on the board, not where work is or what waits on people. */
const runProgress = new Set(['tool_call', 'tool_result', 'agent_message']);

/**
 * The queries one item event touches: the organization's attention counts, inboxes and factory
 * overviews, the factory's work keys (only its board for run progress) and the item, by ref and
 * by id, all within the organization.
 */
export function invalidationsFor(org: string, event: StreamEvent): QueryKey[] {
  return [
    keys.attention(org),
    keys.inboxes(org),
    ...(runProgress.has(event.kind)
      ? [keys.board(org, event.factory_id)]
      : factoryWorkInvalidations(org, event.factory_id)),
    keys.item(org, event.ref),
    keys.item(org, event.item_id),
    keys.overviews(org),
  ];
}

/** Every work key of one factory (by id), for a change that moves its items: never its bare prefix. */
export function factoryWorkInvalidations(org: string, factory: string): QueryKey[] {
  return workKeys.map((name) => [...keys.factory(org, factory), name]);
}

/** Overviews are the dearest read; the stream refreshes them at most every few seconds. */
export function isOverviewKey(key: QueryKey): boolean {
  return key[0] === 'org' && key[2] === 'overview';
}

/** A resync may have missed anything of the organization: everything under it is re-read. */
export function resyncInvalidations(org: string): QueryKey[] {
  return [keys.org(org)];
}
