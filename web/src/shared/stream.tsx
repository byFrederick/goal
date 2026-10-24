// Live updates: one stream subscription per open organization, mounted by the /o/$org layout
// (C §10.10). Each item event invalidates the queries it can change (invalidation.ts); they are
// batched so a burst of tool calls costs one refetch, and the dear factory overviews at most once
// every few seconds. Switching organization closes the old stream and opens the new one.
import { type QueryClient, type QueryKey, useQueryClient } from '@tanstack/react-query';
import { createContext, type ReactNode, use, useEffect, useState } from 'react';
import { type StreamStatus, subscribeToOrg } from '../api/client';
import {
  definitionInvalidations,
  factoryInvalidations,
  invalidationsFor,
  isOverviewKey,
  resyncInvalidations,
} from './invalidation';

const StatusContext = createContext<StreamStatus>('connecting');

export function useStreamStatus(): StreamStatus {
  return use(StatusContext);
}

/**
 * The queries other event names invalidate, by name. Later phases register theirs here (B
 * `definition`, phase 4 `credential`, `approval`, `connection`); names nobody registered are
 * ignored, so new events never break an older client. E's `factory` events arrive typed through
 * the client's `onFactory` (factoryInvalidations).
 */
export const streamHandlers: Readonly<Record<string, (org: string, data: unknown) => QueryKey[]>> = {
  definition: definitionInvalidations,
};

const batchMs = 300;
const overviewMs = 5_000;

/** Collects keys and invalidates them together once `delay` has passed since the first. */
function batcher(client: QueryClient, delay: number) {
  const pending = new Map<string, QueryKey>();
  let timer: ReturnType<typeof setTimeout> | undefined;
  const flush = () => {
    timer = undefined;
    for (const queryKey of pending.values()) client.invalidateQueries({ queryKey });
    pending.clear();
  };
  return {
    add(queryKey: QueryKey) {
      pending.set(JSON.stringify(queryKey), queryKey);
      timer ??= setTimeout(flush, delay);
    },
    cancel() {
      if (timer) clearTimeout(timer);
    },
  };
}

export function OrgStream({ org, children }: { org: string; children: ReactNode }) {
  const client = useQueryClient();
  const [status, setStatus] = useState<StreamStatus>('connecting');

  useEffect(() => {
    const work = batcher(client, batchMs);
    const overviews = batcher(client, overviewMs);
    const invalidate = (queryKeys: QueryKey[]) => {
      for (const key of queryKeys) (isOverviewKey(key) ? overviews : work).add(key);
    };

    const unsubscribe = subscribeToOrg(org, {
      onItem: (event) => invalidate(invalidationsFor(org, event)),
      onFactory: (event) => invalidate(factoryInvalidations(org, event)),
      events: Object.keys(streamHandlers),
      onEvent: (name, data) => invalidate(streamHandlers[name]?.(org, data) ?? []),
      onResync: () => {
        for (const queryKey of resyncInvalidations(org)) client.invalidateQueries({ queryKey });
      },
      onStatus: setStatus,
    });

    return () => {
      unsubscribe();
      work.cancel();
      overviews.cancel();
    };
  }, [client, org]);

  return <StatusContext value={status}>{children}</StatusContext>;
}
