// The Pause row of the Danger zone: Pause or Resume the factory (pauseFactory, resumeFactory). It shows
// with factory:manage on the factory's `can`, and not for an archived factory (Restore starts it paused).
// The sidebar's search runs the same change through useTogglePause.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError, pauseFactory, resumeFactory } from '../../api/client';
import { defKeys } from '../../api/queries';
import type { Factory, OrgSummary } from '../../api/types';
import { Button } from '../../ui/button';
import { refreshFactory } from './refresh';
import { ZoneRow } from './zone-row';

/** Pauses the factory it is given, or resumes it when it is paused; a refusal is passed to `reportError`. */
export function useTogglePause(org: OrgSummary, reportError: (message: string) => void) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (factory: Factory) =>
      factory.paused ? resumeFactory(org.id, factory.id) : pauseFactory(org.id, factory.id),
    onSuccess: (answer, factory) => {
      client.setQueryData(defKeys.factory(org.id, factory.id), answer);
      toast.success(answer.paused ? `Paused ${factory.name}` : `Resumed ${factory.name}`);
    },
    onError: (err) =>
      reportError(
        err instanceof ApiError ? (err.detail ?? err.title) : 'goald is not answering. Try again once it is running.',
      ),
    onSettled: (_answer, _err, factory) => refreshFactory(client, org.id, factory.id),
  });
}

export function PauseRow({ org, factory }: { org: OrgSummary; factory: Factory }) {
  const [error, setError] = useState<string | null>(null);
  const toggle = useTogglePause(org, setError);

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
        <Button
          variant="secondary"
          disabled={toggle.isPending}
          onClick={() => {
            setError(null);
            toggle.mutate(factory);
          }}
        >
          {factory.paused ? (toggle.isPending ? 'Resuming…' : 'Resume') : toggle.isPending ? 'Pausing…' : 'Pause'}
        </Button>
      }
    />
  );
}
