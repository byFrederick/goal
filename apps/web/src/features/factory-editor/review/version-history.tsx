// Version rows (D §10.10, boards Define-Publish and New-Review): the draft, then the published
// versions with their note, when and by whom, "Running" on the one that runs, Compare (pressed
// while its comparison shows, `?compare=`) and Restore… (review decision 3: a pointer move, the
// numbers stay). A restore that would strand open items at stations the older version lacks asks
// where they go (`station_moves`); items being worked must finish first.
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError } from '../../../api/client';
import { restoreVersion } from '../../../api/definitions';
import { defKeys, keys, versionModelQuery } from '../../../api/queries';
import type { ItemsAtRemovedStations, Version } from '../../../api/types';
import { formatClock } from '../../../shared/format';
import { Button } from '../../../ui/button';
import { Chip } from '../../../ui/chip';
import { cn } from '../../../ui/cn';
import { ConfirmDialog } from '../../../ui/confirm-dialog';
import { Field, NativeSelect } from '../../../ui/field';
import { useSetSearch } from '../search-link';
import { type ComparePair, compareParam, publishedLine } from './review';
import { useEditorFacts } from './use-review';

/** What a version's Compare shows: it against the draft, else against the version before it. */
export function comparePairOf(number: number, hasDraft: boolean): ComparePair | undefined {
  if (hasDraft) return { from: String(number), to: 'draft' };
  return number > 1 ? { from: String(number - 1), to: String(number) } : undefined;
}

const rowClass = 'grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-3 gap-y-2 py-3';

export function DraftRow({ label, detail }: { label: string; detail: string }) {
  return (
    <li className={rowClass}>
      <Chip tone="plain" className="border-ink">
        Draft
      </Chip>
      <div className="min-w-0">
        <p className="m-0 text-body font-medium">{label}</p>
        <p className="m-0 text-secondary text-muted">{detail}</p>
      </div>
    </li>
  );
}

/** "Autosaved 14:32 by you" for the draft row. */
export function draftDetail(updatedAt: string | undefined): string {
  return updatedAt ? `Autosaved ${formatClock(updatedAt)} by you` : 'Not saved yet';
}

export function VersionRow({
  version,
  comparing,
  onRestore,
}: {
  version: Version;
  /** The `?compare=` shown now. */
  comparing: string | undefined;
  /** Restore…; absent for someone who may not publish (a view-only editor). */
  onRestore?: ((v: Version) => void) | undefined;
}) {
  const { hasDraft } = useEditorFacts();
  const setSearch = useSetSearch();
  const n = version.number ?? 0;
  const pair = comparePairOf(n, hasDraft);
  const pressed = pair !== undefined && comparing === compareParam(pair);
  const name = `v${n}`;
  return (
    <li className={cn(rowClass, 'border-t border-line')}>
      <Chip tone={version.current ? 'quiet' : 'plain'} className={cn(version.current && 'bg-ink text-white')}>
        {name}
      </Chip>
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-x-3 gap-y-2">
        <div className="min-w-0">
          <p className="m-0 min-w-0 break-words text-body font-medium">{version.message || 'No note'}</p>
          <p className="m-0 text-meta text-muted">{publishedLine(version.published_at, version.published_by)}</p>
        </div>
        <div className="flex items-center gap-2">
          {version.current ? <Chip tone="ok">Live</Chip> : null}
          {pair ? (
            <Button
              size="sm"
              variant="white"
              className={cn('h-11 md:h-8', pressed && 'border-ink')}
              aria-pressed={pressed}
              aria-label={pressed ? `Comparing ${name}` : `Compare ${name}`}
              onClick={() => setSearch({ compare: pressed ? undefined : compareParam(pair) })}
            >
              {pressed ? 'Comparing' : 'Compare'}
            </Button>
          ) : null}
          {version.current || !onRestore ? null : (
            <Button
              size="sm"
              variant="white"
              className="h-11 md:h-8"
              aria-haspopup="dialog"
              aria-label={`Restore ${name}…`}
              onClick={() => onRestore(version)}
            >
              Restore…
            </Button>
          )}
        </div>
      </div>
    </li>
  );
}

const stuck = (items: ItemsAtRemovedStations | undefined) => items?.in_progress ?? [];

/** Restore…: a confirmation, then the pointer move; open items at removed stations ask where they go. */
export function RestoreDialog({ version, onClose }: { version: Version | null; onClose: () => void }) {
  const { org, factory, current, hasDraft } = useEditorFacts();
  const client = useQueryClient();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | undefined>();
  const [items, setItems] = useState<ItemsAtRemovedStations | undefined>();
  const [moves, setMoves] = useState<Record<string, string>>({});
  const n = version?.number ?? 0;
  const model = useQuery({ ...versionModelQuery(org, factory, String(n)), enabled: items !== undefined && n > 0 });
  const stations = ((model.data?.model.line as { stations?: { id: string }[] } | undefined)?.stations ?? []).map(
    (s) => s.id,
  );
  const unmapped = [...new Set((items?.unmapped ?? []).map((i) => i.station))];
  const ready = unmapped.every((s) => moves[s]) && stuck(items).length === 0;

  const close = () => {
    setError(undefined);
    setItems(undefined);
    setMoves({});
    onClose();
  };

  const restore = async () => {
    if (!version || !current) return;
    setPending(true);
    setError(undefined);
    try {
      await restoreVersion(org, factory, String(n), {
        expected_current_version_id: current.id,
        ...(Object.keys(moves).length > 0 ? { station_moves: moves } : {}),
      });
      toast(`v${n} is running`);
      for (const key of [
        defKeys.factory(org, factory),
        defKeys.factories(org),
        defKeys.versions(org, factory),
        defKeys.versionsOf(org, factory),
        defKeys.draft(org, factory),
        defKeys.compares(org, factory),
        keys.board(org, factory),
      ]) {
        void client.invalidateQueries({ queryKey: key });
      }
      close();
    } catch (err) {
      if (err instanceof ApiError && err.code === 'items-at-removed-station') {
        setItems(err.problem.items as ItemsAtRemovedStations | undefined);
        setError(`Open items are at stations v${n} doesn’t have.`);
      } else if (err instanceof ApiError && err.code === 'draft-base-moved') {
        setError('Another version was published or restored meanwhile. Close this and look again.');
        void client.invalidateQueries({ queryKey: defKeys.factory(org, factory) });
      } else {
        setError(err instanceof ApiError ? (err.detail ?? err.title) : 'goald can’t be reached. Try again.');
      }
    } finally {
      setPending(false);
    }
  };

  return (
    <ConfirmDialog
      open={version !== null}
      onOpenChange={(open) => !open && close()}
      title={`Make v${n} the running version?`}
      description={`v${current?.number ?? ''} stops taking new runs${hasDraft ? '; your draft stays open' : ''}. Runs already started finish on v${current?.number ?? ''}.`}
      confirmLabel={`Restore v${n}`}
      pendingLabel="Restoring…"
      pending={pending || (items !== undefined && !ready)}
      onConfirm={() => void restore()}
    >
      {error ? (
        <p role="alert" className="m-0 text-body text-rework-ink">
          {error}
        </p>
      ) : null}
      {stuck(items).length > 0 ? (
        <ul className="m-0 flex list-disc flex-col gap-1 pl-5 text-body">
          {stuck(items).map((i) => (
            <li key={i.id}>
              #{i.number} is being worked at {i.station}: let its run finish or stop it first.
            </li>
          ))}
        </ul>
      ) : null}
      {unmapped.length > 0 ? (
        <fieldset className="m-0 flex flex-col gap-3 border-0 p-0">
          <legend className="mb-2 text-body font-medium">Where do these items go?</legend>
          {unmapped.map((station) => (
            <Field key={station} label={`Items at ${station}`}>
              <NativeSelect
                value={moves[station] ?? ''}
                onChange={(e) => setMoves({ ...moves, [station]: e.target.value })}
              >
                <option value="" disabled>
                  Choose a station of v{n}…
                </option>
                {stations.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </NativeSelect>
            </Field>
          ))}
        </fieldset>
      ) : null}
    </ConfirmDialog>
  );
}
