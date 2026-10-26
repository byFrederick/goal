import type { QueryClient } from '@tanstack/react-query';
import { defKeys, keys } from '../../api/queries';

/**
 * After a factory changed (paused, archived, restored or deleted): the organization's factory list,
 * the factory's own detail and the rail's badges and dots, which the other factories' squares read too.
 */
export function refreshFactory(client: QueryClient, org: string, factory: string): Promise<void> {
  return Promise.all([
    client.invalidateQueries({ queryKey: keys.factories(org) }),
    client.invalidateQueries({ queryKey: defKeys.factory(org, factory) }),
    client.invalidateQueries({ queryKey: keys.attention(org) }),
    client.invalidateQueries({ queryKey: keys.overviews(org) }),
  ]).then(() => undefined);
}
