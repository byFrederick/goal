// Live updates: one stream subscription per open factory. Each item event invalidates the queries it
// can change; invalidations are batched so a burst of tool calls causes one refetch.
import { useQueryClient } from '@tanstack/react-query';
import { createContext, type ReactNode, useContext, useEffect, useState } from 'react';
import { type StreamStatus, subscribeToFactory } from '../api/client';
import { keys } from '../api/queries';

const StatusContext = createContext<StreamStatus>('connecting');

export function useStreamStatus(): StreamStatus {
  return useContext(StatusContext);
}

const batchMs = 300;

export function FactoryStream({ factory, children }: { factory: string; children: ReactNode }) {
  const client = useQueryClient();
  const [status, setStatus] = useState<StreamStatus>('connecting');

  useEffect(() => {
    const items = new Set<string>();
    let timer: ReturnType<typeof setTimeout> | undefined;

    const flush = () => {
      timer = undefined;
      client.invalidateQueries({ queryKey: keys.factory(factory) });
      for (const item of items) client.invalidateQueries({ queryKey: keys.item(item) });
      items.clear();
    };

    const unsubscribe = subscribeToFactory(
      factory,
      (e) => {
        items.add(e.ref);
        items.add(e.item_id);
        timer ??= setTimeout(flush, batchMs);
      },
      () => client.invalidateQueries(),
      setStatus,
    );

    return () => {
      unsubscribe();
      if (timer) clearTimeout(timer);
    };
  }, [client, factory]);

  return <StatusContext value={status}>{children}</StatusContext>;
}
