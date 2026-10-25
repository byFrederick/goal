import { describe, expect, it, vi } from 'vitest';
import { retryImport } from './lazy';

describe('retryImport', () => {
  it('asks again after a failed load and resolves with the module', async () => {
    const load = vi
      .fn<() => Promise<string>>()
      .mockRejectedValueOnce(new TypeError('Failed to fetch dynamically imported module'))
      .mockResolvedValueOnce('module');
    await expect(retryImport(load)).resolves.toBe('module');
    expect(load).toHaveBeenCalledTimes(2);
  });

  it('waits before each new try, longer each time, so a dropped connection can come back', async () => {
    vi.useFakeTimers();
    try {
      const load = vi
        .fn<() => Promise<string>>()
        .mockRejectedValueOnce(new Error('first'))
        .mockRejectedValueOnce(new Error('second'))
        .mockResolvedValueOnce('module');
      const done = retryImport(load, 3);
      await vi.advanceTimersByTimeAsync(299);
      expect(load).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(1);
      expect(load).toHaveBeenCalledTimes(2);
      await vi.advanceTimersByTimeAsync(999);
      expect(load).toHaveBeenCalledTimes(2);
      await vi.advanceTimersByTimeAsync(1);
      await expect(done).resolves.toBe('module');
    } finally {
      vi.useRealTimers();
    }
  });

  it('gives up after the last try with its error', async () => {
    const load = vi
      .fn<() => Promise<string>>()
      .mockRejectedValueOnce(new Error('first'))
      .mockRejectedValueOnce(new Error('second'))
      .mockRejectedValueOnce(new Error('third'));
    await expect(retryImport(load, 3)).rejects.toThrow('third');
    expect(load).toHaveBeenCalledTimes(3);
  });
});
