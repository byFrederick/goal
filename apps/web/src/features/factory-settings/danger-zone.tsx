// The Archive, Restore and Delete rows of a factory's Danger zone. Archive needs the factory paused and
// idle: a running factory offers "Pause first", which pauses, then archives. Each row shows on its own
// permission, both organization-level (the organization's `can`): Archive or Restore on factory:archive,
// Delete on factory:delete. Delete needs an archived factory and the typed name. Archive and restore keep
// the history; delete takes the factory's items, runs, events and versions.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useId, useState } from 'react';
import { toast } from 'sonner';
import { ApiError, archiveFactory, pauseFactory, purgeFactory } from '../../api/client';
import { defKeys, keys } from '../../api/queries';
import type { Factory, OrgSummary } from '../../api/types';
import { Button } from '../../ui/button';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { refreshFactory } from './refresh';
import { useRestoreFactory } from './restore';
import { ZoneRow } from './zone-row';

/** Why an action failed, in the zone's words: goald's detail ("2 items are being worked") when it gives one. */
function actionError(err: Error): string {
  if (!(err instanceof ApiError)) return 'goald is not answering. Try again once it is running.';
  return err.detail ?? err.title;
}

interface DangerZoneProps {
  org: OrgSummary;
  /** The factory as goald last answered it: paused and archived_at are what the zone reads. */
  factory: Factory;
  /** The caller may archive and restore it (factory:archive, org level). */
  archives: boolean;
  /** The caller may delete it (factory:delete, org level). */
  deletes: boolean;
}

export function DangerZone({ org, factory, archives, deletes }: DangerZoneProps) {
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
    <>
      {archives ? (
        archived ? (
          <ZoneRow
            title="Restore"
            description="Lists it in the sidebar and the Factories home again. It stays paused until you resume it."
            error={restore.error ? actionError(restore.error) : null}
            action={
              <Button variant="secondary" disabled={restore.isPending} onClick={() => restore.mutate()}>
                {restore.isPending ? 'Restoring…' : 'Restore'}
              </Button>
            }
          />
        ) : (
          <ZoneRow
            title="Archive"
            description="Hides it from the sidebar and keeps its history. You can restore it."
            action={
              <Button
                variant="secondary"
                onClick={() => {
                  archive.reset();
                  setArchiveOpen(true);
                }}
              >
                Archive
              </Button>
            }
          />
        )
      ) : null}

      {deletes ? (
        <ZoneRow
          title="Delete"
          descriptionId={reasonId}
          description={
            archived
              ? 'Its items, runs, events and versions go for good. You type the name to confirm.'
              : 'Archive the factory first'
          }
          error={purge.error ? actionError(purge.error) : null}
          action={
            <Button
              variant="danger"
              disabled={!archived}
              aria-describedby={reasonId}
              onClick={() => {
                purge.reset();
                setDeleteOpen(true);
              }}
            >
              Delete
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
          <p role="alert" className="m-0 text-secondary text-rework-ink">
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
          <p role="alert" className="m-0 text-secondary text-rework-ink">
            {actionError(purge.error)}
          </p>
        ) : null}
      </ConfirmDialog>
    </>
  );
}
