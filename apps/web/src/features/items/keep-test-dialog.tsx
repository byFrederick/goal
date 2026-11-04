// Keep as a test (ADR 0039 decision 12, docs/design/screens/daily-work/teardown.html): keeps this item as a test at
// one of the stations it passed the first time. goald decides whether it did (409 not-passed-first-time); the
// dialog offers the stations whose runs here look clean and says goald's answer when it refuses.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { toast } from 'sonner';
import { ApiError, keepTest } from '../../api/client';
import { keys } from '../../api/queries';
import { humanize } from '../../shared/format';
import { useOrg } from '../../shared/org';
import { Callout } from '../../ui/callout';
import { ConfirmDialog } from '../../ui/confirm-dialog';
import { Field } from '../../ui/field';
import { Select } from '../../ui/select';

/** What Keep as a test says when it is refused, by the API's code. */
export function keepFailure(err: unknown, station: string, number: number): string {
  const code = err instanceof ApiError ? err.code : undefined;
  if (code === 'not-passed-first-time') return `#${number} did not pass ${humanize(station)} the first time.`;
  if (code === 'kept-test-exists') return `This item is already kept as a test at ${humanize(station)}.`;
  if (code === 'item-not-found') return 'This item is gone.';
  return 'goald is not answering. Try again in a moment.';
}

export function KeepTestDialog({
  open,
  onOpenChange,
  factoryId,
  number,
  stations,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  factoryId: string;
  number: number;
  /** The stations the item's runs here make candidates, in the line's order. */
  stations: readonly string[];
}) {
  const org = useOrg();
  const queryClient = useQueryClient();
  const [chosen, setChosen] = useState<string | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);
  const station = chosen ?? stations[0] ?? '';
  const keep = useMutation({
    mutationFn: () => keepTest(org.id, factoryId, { item: number, station }),
    onSuccess: async () => {
      toast.success(`Kept #${number} as a test at ${humanize(station)}.`);
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: keys.jonah(org.id, factoryId) });
    },
    onError: (err) => setError(keepFailure(err, station, number)),
  });
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          setError(null);
          setChosen(undefined);
        }
        onOpenChange(next);
      }}
      title="Keep as a test"
      description={`Goal reruns #${number} when a change reaches the station you pick.`}
      confirmLabel="Keep as a test"
      pendingLabel="Keeping…"
      pending={keep.isPending}
      onConfirm={() => {
        setError(null);
        keep.mutate();
      }}
    >
      <Field label="Station">
        <Select
          value={station}
          onValueChange={setChosen}
          options={stations.map((s) => ({ value: s, label: humanize(s) }))}
        />
      </Field>
      {error ? (
        <div role="alert">
          <Callout tone="rework">{error}</Callout>
        </div>
      ) : null}
    </ConfirmDialog>
  );
}
