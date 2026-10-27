// The banner above every page of an archived factory (spec 2.2-b): when and by whom it was archived,
// that it takes no work, and Restore for whoever may archive or restore (factory:archive on the
// organization's `can`). Nothing else is drawn for an active factory. The write controls on those
// pages are off by holds() (shared/permissions.ts), which is where the rule lives.
import { Archive } from 'lucide-react';
import { ApiError } from '../api/client';
import { useRestoreFactory } from '../features/factory-settings/restore';
import { useFactory } from '../shared/factory';
import { formatDay } from '../shared/format';
import { useOrg, useOrgHolds } from '../shared/org';
import { reasons } from '../shared/permissions';
import { Button } from '../ui/button';

export function ArchivedBanner() {
  const org = useOrg();
  const factory = useFactory();
  const mayRestore = useOrgHolds('factory:archive');
  const restore = useRestoreFactory(org, factory);
  if (!factory.archived_at) return null;

  const when = formatDay(factory.archived_at);
  const who = factory.archived_by?.name ?? 'someone';
  const error = restore.error instanceof ApiError ? (restore.error.detail ?? restore.error.title) : null;

  return (
    <div className="m-0 flex flex-col gap-2 rounded-control border border-line bg-card px-4 py-2.5 text-sm leading-snug">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="m-0 flex min-w-0 items-start gap-2.5">
          <Archive size={16} strokeWidth={1.5} aria-hidden="true" className="mt-0.5 shrink-0 text-muted" />
          <span className="font-medium text-ink">{reasons.archived(when, who)}</span>
        </p>
        {mayRestore ? (
          <Button
            variant="raised"
            className="h-11 border border-line shadow-none md:h-10"
            disabled={restore.isPending}
            onClick={() => restore.mutate()}
          >
            {restore.isPending ? 'Restoring…' : 'Restore'}
          </Button>
        ) : null}
      </div>
      {error ? (
        <p role="alert" className="m-0 text-sm text-rework-ink">
          {error}
        </p>
      ) : null}
    </div>
  );
}
