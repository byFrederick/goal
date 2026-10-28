// The bulk actions of the Items list (MASTER.md "Items"): a navy bar that shows while rows are
// checked. Move opens the shared Move menu for every checked item; Pause pauses each one with its
// own request; Set lane opens the lane menu, which sets each one's lane. A refusal names its item
// and the others still go through.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { ArrowRight, Pause } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';
import { pauseItem } from '../../api/client';
import { keys } from '../../api/queries';
import type { Factory, Item, ItemCard } from '../../api/types';
import { itemKey } from '../../shared/factory';
import { factoryWorkInvalidations } from '../../shared/invalidation';
import { type Movable, MoveDialog } from '../../shared/move-menu';
import { useOrg } from '../../shared/org';
import { cn } from '../../ui/cn';
import { SetLaneDialog } from './set-lane-menu';

/** The checked items as the Move menu takes them. */
export function movableOf(items: ItemCard[], factory: Factory): Movable[] {
  return items.map((item) => ({
    key: itemKey(factory.name, item.ref),
    id: item.id,
    ref: item.ref,
    title: item.title,
    station: item.station,
    state: item.state,
  }));
}

type PauseResult = { item: Movable; updated: Item; error: null } | { item: Movable; updated: null; error: Error };

function usePauseMany(factory: Factory) {
  const client = useQueryClient();
  const org = useOrg();
  return useMutation({
    mutationFn: (items: Movable[]): Promise<PauseResult[]> =>
      Promise.all(
        items.map(async (item): Promise<PauseResult> => {
          try {
            return { item, updated: await pauseItem(org.id, item.key), error: null };
          } catch (err) {
            return { item, updated: null, error: err instanceof Error ? err : new Error(String(err)) };
          }
        }),
      ),
    onSuccess: (results) => {
      for (const result of results) {
        if (result.updated) client.setQueryData(keys.item(org.id, result.item.key), result.updated);
        else toast.error(`Could not pause ${result.item.ref}`, { description: result.error.message });
      }
      const paused = results.flatMap((r) => (r.updated ? [r.item.ref] : []));
      if (paused.length === 1) toast.success(`Paused ${paused[0]}.`);
      else if (paused.length > 1) toast.success(`Paused ${paused.length} items.`);
    },
    onSettled: () =>
      Promise.all(
        factoryWorkInvalidations(org.id, factory.id).map((queryKey) => client.invalidateQueries({ queryKey })),
      ),
  });
}

/** Buttons on the navy bar: 44 px on a phone, 32 px from 768 px. */
const barButton =
  'inline-flex min-h-11 items-center gap-2 rounded-control border border-andon-line px-3 text-secondary text-white transition-colors hover:bg-andon-raised disabled:opacity-50 md:min-h-8';

export function BulkActions({
  items,
  factory,
  canOperate,
  onClear,
}: {
  /** The checked items, in list order. */
  items: ItemCard[];
  factory: Factory;
  /** Whether the person may move, pause and set the lane of items; without it, only Clear shows. */
  canOperate: boolean;
  onClear: () => void;
}) {
  const [moving, setMoving] = useState(false);
  const [settingLane, setSettingLane] = useState(false);
  const pause = usePauseMany(factory);
  const movable = movableOf(items, factory);

  return (
    <section
      aria-label="Bulk actions"
      className="flex flex-wrap items-center gap-2 rounded-card bg-andon px-4 py-3 text-white"
    >
      <span className="mr-2 text-body font-semibold">{items.length} selected</span>
      {canOperate ? (
        <>
          <button type="button" className={barButton} onClick={() => setMoving(true)}>
            <ArrowRight size={16} strokeWidth={1.75} aria-hidden="true" />
            Move
          </button>
          <button type="button" className={barButton} disabled={pause.isPending} onClick={() => pause.mutate(movable)}>
            <Pause size={16} strokeWidth={1.75} aria-hidden="true" />
            Pause
          </button>
          <button type="button" className={barButton} onClick={() => setSettingLane(true)}>
            Set lane
          </button>
        </>
      ) : null}
      <button type="button" onClick={onClear} className={cn(barButton, 'ml-auto border-transparent')}>
        Clear
      </button>
      {settingLane ? (
        <SetLaneDialog
          items={movable}
          factory={factory}
          open
          onOpenChange={(open) => {
            if (!open) setSettingLane(false);
          }}
        />
      ) : null}
      {moving ? (
        <MoveDialog
          items={movable}
          factory={factory}
          open
          onOpenChange={(open) => {
            if (!open) setMoving(false);
          }}
          onMoved={onClear}
        />
      ) : null}
    </section>
  );
}
