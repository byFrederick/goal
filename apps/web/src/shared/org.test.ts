// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';
import { lastFactory, rememberFactory } from './org';

const factories = [
  { id: 'software-id', archived_at: null },
  { id: 'sre-id', archived_at: null },
  { id: 'old-id', archived_at: '2026-10-01T00:00:00Z' },
];

afterEach(() => localStorage.clear());

describe('lastFactory', () => {
  it('returns the factory remembered for the organization', () => {
    rememberFactory('org-1', 'sre-id');
    expect(lastFactory('org-1', factories)?.id).toBe('sre-id');
  });

  it('is kept per organization', () => {
    rememberFactory('org-2', 'sre-id');
    expect(lastFactory('org-1', factories)).toBeUndefined();
  });

  it('ignores a remembered factory that is no longer listed', () => {
    rememberFactory('org-1', 'gone-id');
    expect(lastFactory('org-1', factories)).toBeUndefined();
  });

  it('ignores a remembered factory that is archived', () => {
    rememberFactory('org-1', 'old-id');
    expect(lastFactory('org-1', factories)).toBeUndefined();
  });

  it('is undefined when nothing was remembered', () => {
    expect(lastFactory('org-1', factories)).toBeUndefined();
  });
});
