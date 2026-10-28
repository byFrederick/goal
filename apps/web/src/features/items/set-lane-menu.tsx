// The Set lane menu of the Items bulk bar: a dialog that lists the lanes the running version's
// stations route by (the sorted union of their route keys, so there is no free text), then sets
// each checked item's lane with one request per item. A refusal of one item does not stop the
// others: each refused item gets its own toast, with the reason from its problem type.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { type FormEvent, useId, useRef, useState } from 'react';
import { toast } from 'sonner';
import { setItemLane } from '../../api/client';
import { keys } from '../../api/queries';
import type { Factory, Item } from '../../api/types';
import { useRunningModel } from '../../shared/definition/agent-cards';
import type { Model } from '../../shared/definition/model';
import { humanize } from '../../shared/format';
import { factoryWorkInvalidations } from '../../shared/invalidation';
import { useOrg } from '../../shared/org';
import type { Movable } from '../../shared/move-menu';
import { Button } from '../../ui/button';
import { Dialog, DialogClose, DialogContent } from '../../ui/dialog';
import { cn } from '../../ui/cn';
import { laneRefusalOf } from './lane-refusals';

/** The lanes the line's stations route by: the sorted union of every station's route keys. */
export function routedLanesOf(model: Model): string[] {
  const lanes = new Set<string>();
  for (const station of model.line.stations ?? []) {
    for (const lane of Object.keys(station.route ?? {})) lanes.add(lane);
  }
  return [...lanes].sort();
}

/** One item's answer from goald: its updated item, or the refusal. */
type LaneResult = { item: Movable; updated: Item; error: null } | { item: Movable; updated: null; error: Error };

/** Sets the lane of each item, one request per item, and reports each refusal. */
function useSetLane(items: Movable[], factory: Factory, onDone: () => void) {
  const client = useQueryClient();
  const org = useOrg();
  return useMutation({
    mutationFn: (lane: string): Promise<LaneResult[]> =>
      Promise.all(
        items.map(async (item): Promise<LaneResult> => {
          try {
            return { item, updated: await setItemLane(org.id, item.key, lane), error: null };
          } catch (err) {
            return { item, updated: null, error: err instanceof Error ? err : new Error(String(err)) };
          }
        }),
      ),
    onSuccess: (results, lane) => {
      for (const result of results) {
        if (result.updated) client.setQueryData(keys.item(org.id, result.item.key), result.updated);
        else toast.error(`Could not set the lane of ${result.item.ref}`, { description: laneRefusalOf(result.error) });
      }
      const changed = results.flatMap((r) => (r.updated ? [r.item.ref] : []));
      if (changed.length === 1) toast.success(`Set the lane of ${changed[0]} to ${humanize(lane)}.`);
      else if (changed.length > 1) toast.success(`Set the lane of ${changed.length} items to ${humanize(lane)}.`);
      onDone();
    },
    onSettled: () =>
      Promise.all([
        ...factoryWorkInvalidations(org.id, factory.id).map((queryKey) => client.invalidateQueries({ queryKey })),
        ...items.map((item) => client.invalidateQueries({ queryKey: keys.item(org.id, item.key) })),
      ]),
  });
}

interface DialogProps {
  /** The checked items, in list order. */
  items: Movable[];
  factory: Factory;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** The Set lane menu: a dialog with the routed lanes to choose from (Radix: focus trapped, Escape closes). */
export function SetLaneDialog({ items, factory, open, onOpenChange }: DialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {open ? <SetLaneForm items={items} factory={factory} onDone={() => onOpenChange(false)} /> : null}
    </Dialog>
  );
}

function SetLaneForm({ items, factory, onDone }: { items: Movable[]; factory: Factory; onDone: () => void }) {
  const id = useId();
  const model = useRunningModel();
  const lanes = model.data ? routedLanesOf(model.data) : [];
  const [selected, setSelected] = useState('');
  const first = useRef<HTMLInputElement>(null);
  const setLane = useSetLane(items, factory, onDone);
  const subject = items.length === 1 ? (items[0]?.ref ?? '') : `${items.length} items`;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!selected) return;
    setLane.mutate(selected);
  };

  return (
    <DialogContent
      title={`Set lane for ${subject}`}
      description="Choose the lane. Only the lanes the line's stations route by are listed."
      initialFocus={first}
    >
      {model.isPending ? (
        <div aria-busy="true" className="min-h-20">
          <p className="sr-only">Loading</p>
        </div>
      ) : model.isError ? (
        <div className="flex flex-col items-start gap-3">
          <p className="m-0 text-body text-rework">The lanes could not be read. Try again.</p>
          <Button variant="secondary" onClick={() => void model.refetch()}>
            Try again
          </Button>
        </div>
      ) : lanes.length === 0 ? (
        <p className="m-0 text-body text-muted">No station routes items by lane, so there is no lane to set.</p>
      ) : (
        <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
          <fieldset className="m-0 flex flex-col gap-1.5 border-0 p-0">
            <legend className="mb-1.5 p-0 text-secondary text-muted">Lane</legend>
            {lanes.map((lane, index) => {
              const checked = lane === selected;
              return (
                <label
                  key={lane}
                  className={cn(
                    'flex min-h-11 cursor-pointer items-center gap-3 rounded-control border px-3 py-2 transition-colors',
                    'has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-run',
                    checked ? 'border-ink bg-card' : 'border-line bg-card hover:border-muted',
                  )}
                >
                  <input
                    ref={index === 0 ? first : undefined}
                    type="radio"
                    name={`${id}-lane`}
                    value={lane}
                    checked={checked}
                    onChange={() => setSelected(lane)}
                    className="size-4 shrink-0 accent-ink outline-none"
                  />
                  <span className="text-body font-medium">{humanize(lane)}</span>
                </label>
              );
            })}
          </fieldset>
          <div className="flex flex-wrap justify-end gap-2">
            <DialogClose asChild>
              <Button variant="secondary">Cancel</Button>
            </DialogClose>
            <Button type="submit" variant="primary" disabled={!selected || setLane.isPending}>
              {setLane.isPending ? 'Setting…' : `Set lane to ${selected ? humanize(selected) : '…'}`}
            </Button>
          </div>
        </form>
      )}
    </DialogContent>
  );
}
