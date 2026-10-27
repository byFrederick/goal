// The two decisions a Decision entry takes: approve (or merge, where the entry's action is merge) and
// send back with a reason. They call the same endpoints as the item page, and the item's changes
// invalidate the inbox, so the entry leaves the page once it is decided.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { approveItem, mergeItem, rejectItem } from '../../api/client';
import { keys } from '../../api/queries';
import type { InboxEntry, Item, ItemCard } from '../../api/types';
import { itemChangeInvalidations } from '../../shared/item-actions';
import { useOrg } from '../../shared/org';

/** A decided entry: the item it is about and the factory it is in. */
export interface DecidedEntry {
  entry: InboxEntry & { item: ItemCard };
}

export type EntryDecision = { kind: 'approve' } | { kind: 'merge' } | { kind: 'reject'; reason: string };

/** The one action the entry offers: merge where its station merges, else approve. */
export function decisionOf(entry: InboxEntry): 'approve' | 'merge' {
  return entry.action === 'merge' ? 'merge' : 'approve';
}

/** Runs a Decision entry's decision: approveItem, mergeItem or rejectItem, with a toast for the result. */
export function useEntryDecision() {
  const client = useQueryClient();
  const org = useOrg();
  return useMutation({
    mutationFn: ({ entry, decision }: DecidedEntry & { decision: EntryDecision }): Promise<Item> => {
      const item = entry.item.id;
      switch (decision.kind) {
        case 'approve':
          return approveItem(org.id, item);
        case 'merge':
          return mergeItem(org.id, item);
        case 'reject':
          return rejectItem(org.id, item, decision.reason);
      }
    },
    onSuccess: (updated, { entry, decision }) => {
      client.setQueryData(keys.item(org.id, entry.item.id), updated);
      const ref = entry.item.ref;
      const said = {
        approve: `Approved ${ref}.`,
        merge: `Merged ${ref}.`,
        reject: `Sent ${ref} back.`,
      }[decision.kind];
      toast.success(said);
    },
    onError: (err, { entry, decision }) => {
      const verb = { approve: 'approve', merge: 'merge', reject: 'send back' }[decision.kind];
      toast.error(`Could not ${verb} ${entry.item.ref}`, { description: err.message });
    },
    onSettled: (_data, _err, { entry }) =>
      Promise.all(
        itemChangeInvalidations(org.id, entry.factory.id, entry.item.id).map((queryKey) =>
          client.invalidateQueries({ queryKey }),
        ),
      ),
  });
}
