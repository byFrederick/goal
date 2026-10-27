// Danger zone of a factory (canvas "Org-Settings", as the organization's): Archive, or Restore once
// archived, and Delete for good, which needs an archived factory and the typed name. Archive needs
// the factory paused and idle: a running factory offers "Pause first", which pauses, then archives.
// Shown only to people with factory:archive on the organization (its `can`); Delete only with
// factory:delete. Archive and restore keep the history; delete takes the factory's items, runs,
// events and versions.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { type ReactNode, useId, useState } from 'react';
import { toast } from 'sonner';
import { ApiError, archiveFactory, pauseFactory, purgeFactory } from '../../api/client';
import { defKeys, keys } from '../../api/queries';
import type { Factory, OrgSummary } from '../../api/types';
import { Button } from '../../ui/button';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { SettingsSection } from '../org-settings/section';
import { refreshFactory } from './refresh';
import { useRestoreFactory } from './restore';

/** Why an action failed, in the zone's words: goald's detail ("2 items are being worked") when it gives one. */
function actionError(err: Error): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. Try again once it is running.';
  return err.detail ?? err.title;
}

function ZoneRow({
  title,
  description,
  descriptionId,
  action,
  error,
}: {
  title: string;
  description: ReactNode;
  descriptionId?: string;
  action: ReactNode;
  error?: string | null;
}) {
  return (
    <div className="flex flex-col gap-3 rounded-card border border-line bg-card p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="m-0 flex min-w-0 flex-col">
          <span className="text-[15px] font-medium">{title}</span>
          <span id={descriptionId} className="text-sm text-muted">
            {description}
          </span>
        </p>
        {action}
      </div>
      {error ? (
        <p role="alert" className="m-0 text-sm text-rework-ink">
          {error}
        </p>
      ) : null}
    </div>
  );
}

interface DangerZoneProps {
  org: OrgSummary;
  /** The factory as goald last answered it: paused and archived_at are what the zone reads. */
  factory: Factory;
  /** The caller may delete it (factory:delete, org level). */
  deletes: boolean;
}

export function DangerZone({ org, factory, deletes }: DangerZoneProps) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const [archiveOpen, setArchiveOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const reasonId = useId();
  const archived = Boolean(factory.archived_at);
  const detailKey = defKeys.factory(org.id, factory.id);

  const archive = useMutation({
    // Pause first when asked: the answer is the factory's new state, kept for the dialog's label.
    mutationFn: async ({ pauseFirst }: { pauseFirst: boolean }) => {
      if (pauseFirst) client.setQueryData(detailKey, await pauseFactory(org.id, factory.id));
      return archiveFactory(org.id, factory.id);
    },
    onSuccess: (answer) => {
      client.setQueryData(detailKey, answer);
      setArchiveOpen(false);
      toast.success(`Archived ${factory.name}`, { description: 'It stays readable, paused. Restore brings it back.' });
    },
    onSettled: () => refreshFactory(client, org.id, factory.id),
  });

  const restore = useRestoreFactory(org, factory);

  const purge = useMutation({
    mutationFn: () => purgeFactory(org.id, factory.id),
    onSuccess: async () => {
      setDeleteOpen(false);
      // Leave the factory's page before its queries go, so nothing on it refetches a deleted factory.
      await navigate({ to: '/o/$org', params: { org: org.slug }, replace: true });
      client.removeQueries({ queryKey: keys.factory(org.id, factory.id) });
      toast.success(`Deleted ${factory.name}`, {
        description: 'Its items, runs, events and versions are gone. The audit history stays.',
      });
    },
    onSettled: () => refreshFactory(client, org.id, factory.id),
  });

  // A running factory cannot be archived: goald answers factory-running, and the dialog offers to pause it first.
  const needsPause = !factory.paused || (archive.error instanceof ApiError && archive.error.code === 'factory-running');

  return (
    <SettingsSection
      title="Danger zone"
      lede="Archiving can be undone with Restore. Deleting can’t be undone from here."
    >
      {archived ? (
        <ZoneRow
          title={`Restore ${factory.name}`}
          description="Lists it in the sidebar and the Factories home again. It stays paused until you resume it."
          error={restore.error ? actionError(restore.error) : null}
          action={
            <Button
              variant="raised"
              className="h-11 border border-line shadow-none md:h-10"
              disabled={restore.isPending}
              onClick={() => restore.mutate()}
            >
              {restore.isPending ? 'Restoring…' : 'Restore factory'}
            </Button>
          }
        />
      ) : (
        <ZoneRow
          title={`Archive ${factory.name}`}
          description="Stops its work and takes it off the sidebar and the Factories home. It stays readable, with its items and versions."
          action={
            <Button
              variant="raised"
              className="h-11 border border-line shadow-none md:h-10"
              onClick={() => {
                archive.reset();
                setArchiveOpen(true);
              }}
            >
              Archive factory…
            </Button>
          }
        />
      )}

      {deletes ? (
        <ZoneRow
          title={`Delete ${factory.name}`}
          descriptionId={reasonId}
          description={
            archived
              ? 'Its items, runs, events and versions go for good. You type the name to confirm.'
              : 'Archive the factory first'
          }
          error={purge.error ? actionError(purge.error) : null}
          action={
            <Button
              variant="raised"
              className="h-11 border border-line shadow-none md:h-10"
              disabled={!archived}
              aria-describedby={reasonId}
              onClick={() => {
                purge.reset();
                setDeleteOpen(true);
              }}
            >
              Delete factory…
            </Button>
          }
        />
      ) : null}

      <ConfirmDialog
        open={archiveOpen}
        onOpenChange={setArchiveOpen}
        title={`Archive ${factory.name}?`}
        description={
          needsPause
            ? 'It is running. Pause first stops it taking work, then archives it: it leaves the sidebar and the Factories home and stays readable, with its items and versions.'
            : 'It stops taking work and leaves the sidebar and the Factories home. It stays readable, with its items and versions.'
        }
        confirmLabel={needsPause ? 'Pause first' : 'Archive factory'}
        pendingLabel={needsPause ? 'Pausing…' : 'Archiving…'}
        pending={archive.isPending}
        onConfirm={() => archive.mutate({ pauseFirst: needsPause })}
      >
        {archive.error ? (
          <p role="alert" className="m-0 text-sm text-rework-ink">
            {actionError(archive.error)}
          </p>
        ) : null}
      </ConfirmDialog>

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={`Delete ${factory.name}?`}
        description="Its items, runs, events and versions go too. The audit history stays."
        confirmLabel="Delete factory"
        pendingLabel="Deleting…"
        tone="danger"
        confirmText={factory.name}
        pending={purge.isPending}
        onConfirm={() => purge.mutate()}
      >
        {purge.error ? (
          <p role="alert" className="m-0 text-sm text-rework-ink">
            {actionError(purge.error)}
          </p>
        ) : null}
      </ConfirmDialog>
    </SettingsSection>
  );
}
