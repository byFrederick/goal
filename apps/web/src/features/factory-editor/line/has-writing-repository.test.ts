import { describe, expect, it } from 'vitest';
import { hasWritingRepository } from './line-editor';

describe('hasWritingRepository', () => {
  it('is true for the shorthand repository', () => {
    expect(hasWritingRepository({ repository: { url: 'https://github.com/acme/app' } })).toBe(true);
  });
  it('is true for an entry that writes, or has no access (write by default)', () => {
    expect(hasWritingRepository({ repositories: [{ name: 'app', url: 'https://github.com/acme/app', access: 'write' }] })).toBe(true);
    expect(hasWritingRepository({ repositories: [{ name: 'app', url: 'https://github.com/acme/app' } as never] })).toBe(true);
  });
  it('is false for read-only entries, entries without a URL, or none', () => {
    expect(hasWritingRepository({ repositories: [{ name: 'app', url: 'https://github.com/acme/app', access: 'read' }] })).toBe(false);
    expect(hasWritingRepository({ repositories: [{ name: 'app', url: '', access: 'write' }] })).toBe(false);
    expect(hasWritingRepository({})).toBe(false);
  });
});
