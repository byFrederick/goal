// The stream's batcher: collects the keys an event can change and invalidates them together. It
// never aborts a refetch that is already running (goald would redo the work), and a hidden page
// refetches nothing until it is shown again.
import type { QueryClient, QueryKey } from '@tanstack/react-query';
import { heldByMove } from './invalidation';
import type { PageSource } from './stream-presence';

/** Collects keys and invalidates them together once `delay` has passed since the first. */
export function batcher(client: QueryClient, delay: number, page: PageSource) {
  const pending = new Map<string, QueryKey>();
  let timer: ReturnType<typeof setTimeout> | undefined;

  const schedule = () => {
    timer ??= setTimeout(flush, delay);
  };
  function flush() {
    clearTimeout(timer);
    timer = undefined;
    // A hidden page collects keys; the return to it flushes them.
    if (page.hidden()) return;
    const keys = [...pending.values()];
    pending.clear();
    for (const queryKey of keys) {
      if (heldByMove(client, queryKey)) continue;
      if (client.isFetching({ queryKey }) > 0) {
        // The running request may answer from before the event: mark the data stale without
        // refetching or cancelling, and let the next batch catch up once the fetch has ended.
        void client.invalidateQueries({ queryKey, refetchType: 'none' });
        pending.set(JSON.stringify(queryKey), queryKey);
      } else {
        void client.invalidateQueries({ queryKey }, { cancelRefetch: false });
      }
    }
    if (pending.size > 0) schedule();
  }
  const stopShow = page.onShow(() => {
    if (pending.size > 0) flush();
  });

  return {
    add(queryKey: QueryKey) {
      pending.set(JSON.stringify(queryKey), queryKey);
      schedule();
    },
    cancel() {
      clearTimeout(timer);
      timer = undefined;
      stopShow();
    },
  };
}
