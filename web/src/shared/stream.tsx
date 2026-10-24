// Live updates: one stream subscription per open factory, within its organization. Each item
// event invalidates the queries it can change (invalidation.ts); invalidations are batched so a
// burst of tool calls causes one refetch.
import { useQueryClient } from '@tanstack/react-query';
import { createContext, type ReactNode, useContext, useEffect, useState } from 'react';
import { type StreamStatus, subscribeToFactory } from '../api/client';
import { itemInvalidations, resyncInvalidations } from './invalidation';

const StatusContext = createContext<StreamStatus>('connecting');

export function useStreamStatus(): StreamStatus {
  return useContext(StatusContext);
}

const batchMs = 300;

export function FactoryStream({ org, factory, children }: { org: string; factory: string; children: ReactNode }) {
  const client = useQueryClient();
  const [status, setStatus] = useState<StreamStatus>('connecting');

  useEffect(() => {
    const items = new Set<string>();
    let timer: ReturnType<typeof setTimeout> | undefined;

    const flush = () => {
      timer = undefined;
      for (const queryKey of itemInvalidations(org, factory, items)) client.invalidateQueries({ queryKey });
      items.clear();
    };

    const unsubscribe = subscribeToFactory(
      org,
      factory,
      (e) => {
        items.add(e.ref);
        items.add(e.item_id);
        timer ??= setTimeout(flush, batchMs);
      },
      () => {
        for (const queryKey of resyncInvalidations(org)) client.invalidateQueries({ queryKey });
      },
      setStatus,
    );

    return () => {
      unsubscribe();
      if (timer) clearTimeout(timer);
    };
  }, [client, org, factory]);

  return <StatusContext value={status}>{children}</StatusContext>;
}
