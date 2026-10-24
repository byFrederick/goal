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
