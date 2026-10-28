// The Pause row of the Danger zone: Pause or Resume the factory (pauseFactory, resumeFactory). It shows
// with factory:manage on the factory's `can`, and not for an archived factory (Restore starts it paused).
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError, pauseFactory, resumeFactory } from '../../api/client';
import { defKeys } from '../../api/queries';
import type { Factory, OrgSummary } from '../../api/types';
import { Button } from '../../ui/button';
import { refreshFactory } from './refresh';
import { ZoneRow } from './zone-row';

export function PauseRow({ org, factory }: { org: OrgSummary; factory: Factory }) {
  const client = useQueryClient();
  const [error, setError] = useState<string | null>(null);

  const toggle = useMutation({
    mutationFn: () => (factory.paused ? resumeFactory(org.id, factory.id) : pauseFactory(org.id, factory.id)),
    onMutate: () => setError(null),
    onSuccess: (answer) => {
      client.setQueryData(defKeys.factory(org.id, factory.id), answer);
      toast.success(answer.paused ? `Paused ${factory.name}` : `Resumed ${factory.name}`);
    },
    onError: (err) => {
      setError(
        err instanceof ApiError ? (err.detail ?? err.title) : 'goald is not answering. Try again once it is running.',
      );
    },
    onSettled: () => refreshFactory(client, org.id, factory.id),
  });

  return (
    <ZoneRow
      title="Pause the factory"
      description={
        factory.paused
          ? 'It takes no work. Resume starts it again.'
          : 'Running items finish their run; nothing new starts until you resume.'
      }
      error={error}
      action={
        <Button variant="secondary" disabled={toggle.isPending} onClick={() => toggle.mutate()}>
          {factory.paused ? (toggle.isPending ? 'Resuming…' : 'Resume') : toggle.isPending ? 'Pausing…' : 'Pause'}
        </Button>
      }
    />
  );
}
