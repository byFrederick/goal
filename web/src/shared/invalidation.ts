// What the organization's stream invalidates (C §10.10). Pure, so the rules are tested without an
// EventSource.
import type { QueryClient, QueryKey } from '@tanstack/react-query';
import { savedHere } from '../api/definitions';
import { defKeys, keys } from '../api/queries';
import type { DefinitionEvent, FactoryEvent, StreamEvent } from '../api/types';

/**
 * The factory's work keys an item event can change, named one by one: never the factory's bare
 * prefix, under which later phases keep a definition draft, versions and credentials that must not
 * refetch while agents work. E's keys (weekly … agent) are named here before their queries exist.
 */
const workKeys = [
  'board',
  'metrics',
  'items',
  'weekly',
  'lead-time',
  'constraint',
  'touches',
  'andon',
  'agent',
] as const;

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

/**
 * The queries one `factory` event touches (E §10.3): a line stopped or restarted by its andon
 * changes the factory's work keys (board, andon, …), the organization's attention and inboxes and
 * the factory overviews' health; no item changed.
 */
export function factoryInvalidations(org: string, event: FactoryEvent): QueryKey[] {
  return [
    keys.attention(org),
    keys.inboxes(org),
    ...factoryWorkInvalidations(org, event.factory_id),
    keys.overviews(org),
  ];
}

/**
 * Whether the stream should leave a key alone for now: a factory's board while a move of one of
 * its items is in flight. A refetch then could answer with the card where it was and undo the
 * optimistic move until the move's own event; the move refetches the board when it settles.
 */
export function heldByMove(client: QueryClient, key: QueryKey): boolean {
  if (key[0] !== 'org' || key[2] !== 'factory' || key[4] !== 'board' || key.length !== 5) return false;
  return client.isMutating({ mutationKey: keys.move(String(key[1]), String(key[3])) }) > 0;
}

/** Overviews are the dearest read; the stream refreshes them at most every few seconds. */
export function isOverviewKey(key: QueryKey): boolean {
  return key[0] === 'org' && key[2] === 'overview';
}

function isDefinitionEvent(data: unknown): data is DefinitionEvent {
  const e = data as Partial<DefinitionEvent> | null;
  return typeof e?.factory_id === 'string' && typeof e.kind === 'string';
}

/**
 * The queries one `definition` event touches (B §10):
 * - `draft`, a draft opened, saved, discarded or published: the draft, its model and files, the
 *   factory (its draft chip) and the factory list; nothing when this tab saved that revision of
 *   that draft itself (the event is its echo), and the versions too once the version is published;
 * - `current`, the version that runs moved or the factory was paused or resumed: the factory and
 *   the list, its versions, draft (based on, running), comparisons, board and overviews;
 * - `factory`, a factory created or deleted: the factory and the list, attention and overviews.
 */
export function definitionInvalidations(org: string, data: unknown): QueryKey[] {
  if (!isDefinitionEvent(data)) return [];

  const f = data.factory_id;
  const factory = [defKeys.factories(org), defKeys.factory(org, f)];

  switch (data.kind) {
    case 'draft':
      if (data.revision !== undefined && savedHere(f, data.version_id, data.revision)) return [];
      return [
        ...factory,
        defKeys.draft(org, f),
        ...(data.number !== undefined ? [defKeys.versions(org, f), defKeys.versionsOf(org, f)] : []),
      ];
    case 'current':
      return [
        ...factory,
        defKeys.versions(org, f),
        defKeys.versionsOf(org, f),
        defKeys.draft(org, f),
        defKeys.compares(org, f),
        keys.board(org, f),
        keys.overviews(org),
      ];
    case 'factory':
      return [...factory, keys.attention(org), keys.overviews(org)];
    default:
      return [];
  }
}

/** A resync may have missed anything of the organization: everything under it is re-read. */
export function resyncInvalidations(org: string): QueryKey[] {
  return [keys.org(org)];
}
