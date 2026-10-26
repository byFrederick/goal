// Restore an archived factory (factory:archive, spec 2.2-b): the Danger zone, the Archived list on
// the Factories home and the banner on an archived factory's pages all use this one mutation, so
// they answer and refresh the same way. The factory comes back paused.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { restoreFactory } from '../../api/client';
import { defKeys } from '../../api/queries';
import type { Factory, OrgSummary } from '../../api/types';
import { refreshFactory } from './refresh';

export function useRestoreFactory(org: OrgSummary, factory: Factory) {
  const client = useQueryClient();
  const detailKey = defKeys.factory(org.id, factory.id);
  return useMutation({
    mutationFn: () => restoreFactory(org.id, factory.id),
    onSuccess: (answer) => {
      client.setQueryData(detailKey, answer);
      toast.success(`Restored ${factory.name}`, {
        description: 'It is listed again and stays paused until you resume it.',
      });
    },
    onSettled: () => refreshFactory(client, org.id, factory.id),
  });
}
