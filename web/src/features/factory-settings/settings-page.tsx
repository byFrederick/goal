// A factory's settings (C §10.9): Pause or Resume, and the Danger zone. Pause needs factory:manage on
// the factory's `can`, and is left out for an archived factory (Restore starts it paused). The Danger
// zone reads the organization's `can` (factory:archive, factory:delete), since those permissions are
// organization-level. The factory is the detail goald answers, so archived and paused stay current
// after an action; the rail's factory list leaves archived factories out.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError, pauseFactory, resumeFactory } from '../../api/client';
import { defKeys, factoryQuery } from '../../api/queries';
import type { Factory, OrgSummary } from '../../api/types';
import { useFactory } from '../../shared/factory';
import { useOrg, useOrgHolds } from '../../shared/org';
import { holds } from '../../shared/permissions';
import { Button } from '../../ui/button';
import { descriptionClass, PageHeader } from '../../ui/page-header';
import { SettingsSection } from '../org-settings/section';
import { DangerZone } from './danger-zone';
import { refreshFactory } from './refresh';

export function FactorySettingsPage() {
  const org = useOrg();
  const listed = useFactory();
  // The detail answers archived and paused as they are now; the list stands in until it loads.
  const factory = useQuery(factoryQuery(org.id, listed.id)).data ?? listed;
  const mayArchive = useOrgHolds('factory:archive');
  const mayDelete = useOrgHolds('factory:delete');

  return (
    <div className="flex max-w-[980px] flex-col">
      <PageHeader
        title="Factory settings"
        description={<p className={descriptionClass}>Settings for {factory.name}.</p>}
      />
      {holds(factory, 'factory:manage') && !factory.archived_at ? <PauseSection org={org} factory={factory} /> : null}
      {mayArchive ? <DangerZone org={org} factory={factory} deletes={mayDelete} /> : null}
    </div>
  );
}

function PauseSection({ org, factory }: { org: OrgSummary; factory: Factory }) {
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
    <SettingsSection
      title="Work"
      lede="A paused factory takes no work. Runs in progress finish, and Resume starts it again."
    >
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-card border border-line bg-card p-4">
        <p className="m-0 flex min-w-0 flex-col">
          <span className="text-[15px] font-medium">{factory.paused ? 'Paused' : 'Running'}</span>
          <span className="text-sm text-muted">
            {factory.paused ? 'It takes no work.' : 'It takes work from its queue.'}
          </span>
        </p>
        <Button
          variant="raised"
          className="h-11 border border-line shadow-none md:h-10"
          disabled={toggle.isPending}
          onClick={() => toggle.mutate()}
        >
          {factory.paused
            ? toggle.isPending
              ? 'Resuming…'
              : 'Resume factory'
            : toggle.isPending
              ? 'Pausing…'
              : 'Pause factory'}
        </Button>
      </div>
      {error ? (
        <p role="alert" className="m-0 text-sm text-rework-ink">
          {error}
        </p>
      ) : null}
    </SettingsSection>
  );
}
