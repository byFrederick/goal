import { QueryClient, QueryObserver } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { keys } from '../api/queries';
import { batcher } from './stream-batch';
import type { PageSource } from './stream-presence';

function fakePage() {
  let hidden = false;
  const listeners = new Set<(fromCache: boolean) => void>();
  const page: PageSource = {
    hidden: () => hidden,
    onShow(l) {
      listeners.add(l);
      return () => listeners.delete(l);
    },
  };
  return {
    page,
    hide: () => {
      hidden = true;
    },
    show: () => {
      hidden = false;
      for (const l of [...listeners]) l(false);
    },
    listeners,
  };
}

const key = ['org', 'acme', 'inbox'] as const;

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 60_000 } } });
  const signals: AbortSignal[] = [];
  const finishers: Array<() => void> = [];
  const queryFn = vi.fn(
    ({ signal }: { signal: AbortSignal }) =>
      new Promise<number>((resolve) => {
        signals.push(signal);
        finishers.push(() => resolve(1));
      }),
  );
  // An observer makes the query active, so an invalidation refetches it.
  const observer = new QueryObserver(client, { queryKey: key, queryFn });
  observer.subscribe(() => {});
  return { client, queryFn, signals, finishers };
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe('the stream batcher', () => {
  test('does not cancel a fetch in flight and refetches the key once after it ends', async () => {
    const { client, queryFn, signals, finishers } = setup();
    const fake = fakePage();
    const batch = batcher(client, 300, fake.page);
    expect(queryFn).toHaveBeenCalledTimes(1);

    batch.add(key);
    await vi.advanceTimersByTimeAsync(300);
    expect(signals[0]?.aborted).toBe(false);
    expect(queryFn).toHaveBeenCalledTimes(1);

    finishers[0]?.();
    await vi.advanceTimersByTimeAsync(300);
    expect(queryFn).toHaveBeenCalledTimes(2);
    expect(signals[0]?.aborted).toBe(false);
    finishers[1]?.();
    await vi.advanceTimersByTimeAsync(1_000);
    expect(queryFn).toHaveBeenCalledTimes(2);
    batch.cancel();
  });

  test('collects keys while hidden and fetches each once on show', async () => {
    const { client, queryFn, finishers } = setup();
    const fake = fakePage();
    finishers[0]?.();
    await vi.advanceTimersByTimeAsync(0);
    expect(queryFn).toHaveBeenCalledTimes(1);

    const batch = batcher(client, 300, fake.page);
    fake.hide();
    batch.add(key);
    batch.add(key);
    batch.add(key);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(queryFn).toHaveBeenCalledTimes(1);

    fake.show();
    await vi.advanceTimersByTimeAsync(0);
    expect(queryFn).toHaveBeenCalledTimes(2);
    batch.cancel();
  });

  test('skips a key whose board move is in flight, and cancel removes the show listener', async () => {
    const client = new QueryClient();
    const fake = fakePage();
    const board = keys.board('acme', 'goal');
    const spy = vi.spyOn(client, 'invalidateQueries');
    void client
      .getMutationCache()
      .build(client, { mutationKey: keys.move('acme', 'goal'), mutationFn: () => new Promise<void>(() => {}) })
      .execute(undefined);
    const batch = batcher(client, 300, fake.page);
    batch.add(board);
    await vi.advanceTimersByTimeAsync(300);
    expect(spy).not.toHaveBeenCalled();

    expect(fake.listeners.size).toBe(1);
    batch.cancel();
    expect(fake.listeners.size).toBe(0);
  });
});
