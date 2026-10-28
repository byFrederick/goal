// The decisions an entry takes. A Decision: approve (or merge, where the entry's action is merge) and
// send back with a reason, on the same endpoints as the item page. An Approval: approve the tool call,
// with Always allow when offered, or deny it with a note (ADR 0031). Either way the item's changes
// invalidate the inbox, so the entry leaves the page once it is decided.
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { approveItem, decideToolApproval, mergeItem, rejectItem } from '../../api/client';
import { keys } from '../../api/queries';
import type { InboxEntry, Item, ItemCard } from '../../api/types';
import { itemChangeInvalidations } from '../../shared/item-actions';
import { useOrg } from '../../shared/org';

/** A decided entry: the item it is about and the factory it is in. */
export interface DecidedEntry {
  entry: InboxEntry & { item: ItemCard };
}

export type EntryDecision =
  | { kind: 'approve' }
  | { kind: 'merge' }
  | { kind: 'reject'; reason: string }
  | { kind: 'tool_approve'; always_allow: boolean }
  | { kind: 'tool_deny'; reason: string };

/** The one action the entry offers: merge where its station merges, else approve. */
export function decisionOf(entry: InboxEntry): 'approve' | 'merge' {
  return entry.action === 'merge' ? 'merge' : 'approve';
}

/**
 * Whether the Always allow checkbox is offered for an Approval: the API offers it for this call
 * (always_allow_offered), and the caller may manage connections (connection:manage), which the
 * API checks again.
 */
export function alwaysAllowOffered(entry: InboxEntry, canManageConnections: boolean): boolean {
  return canManageConnections && entry.tool_approval?.always_allow_offered === true;
}

/** Runs an entry's decision: the item's approve, merge or reject, or the tool call's decision, with a toast. */
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
        case 'tool_approve':
          return decideToolApproval(org.id, item, {
            decision: 'approve',
            always_allow: decision.always_allow,
          });
        case 'tool_deny':
          return decideToolApproval(org.id, item, { decision: 'deny', note: decision.reason });
      }
    },
    onSuccess: (updated, { entry, decision }) => {
      client.setQueryData(keys.item(org.id, entry.item.id), updated);
      const ref = entry.item.ref;
      const said = {
        approve: `Approved ${ref}.`,
        merge: `Merged ${ref}.`,
        reject: `Sent ${ref} back.`,
        tool_approve:
          decision.kind === 'tool_approve' && decision.always_allow
            ? `Approved the call on ${ref}. ${entry.tool_approval?.agent ?? 'The agent'} may make it without asking again.`
            : `Approved the call on ${ref}.`,
        tool_deny: `Denied the call on ${ref}.`,
      }[decision.kind];
      toast.success(said);
    },
    onError: (err, { entry, decision }) => {
      const verb = {
        approve: 'approve',
        merge: 'merge',
        reject: 'send back',
        tool_approve: 'approve',
        tool_deny: 'deny',
      }[decision.kind];
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
