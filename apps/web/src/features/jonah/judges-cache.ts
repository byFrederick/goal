// What a judge save, flip or new judge changes: the judges of the factory, and the versions and the
// draft the publish made. A publish also arrives as a `definition` event; invalidating here shows it at once.
import type { QueryClient } from '@tanstack/react-query';
import { defKeys, keys } from '../../api/queries';

export function refreshAfterPublish(queryClient: QueryClient, org: string, factory: string): void {
  for (const queryKey of [keys.judges(org, factory), defKeys.versions(org, factory), defKeys.draft(org, factory)]) {
    void queryClient.invalidateQueries({ queryKey });
  }
}
