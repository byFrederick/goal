// What the stream invalidates. Pure, so the rules are tested without an EventSource.
import type { QueryKey } from '@tanstack/react-query';
import { keys } from '../api/queries';

/**
 * The queries a burst of item events of one factory touches: the factory's work (board,
 * metrics, lists) and each item seen, by ref and by id, all within the organization.
 */
export function itemInvalidations(org: string, factory: string, items: Iterable<string>): QueryKey[] {
  return [keys.factory(org, factory), ...[...items].map((item) => keys.item(org, item))];
}

/** A resync may have missed anything of the organization: everything under it is re-read. */
export function resyncInvalidations(org: string): QueryKey[] {
  return [keys.org(org)];
}
