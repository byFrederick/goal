import { describe, expect, test } from 'vitest';
import { legacyPath, legacyTarget } from './legacy';

describe('legacyTarget', () => {
  test('splits the factory from the rest of an old /f/ path', () => {
    expect(legacyTarget('goal')).toEqual({ factory: 'goal', rest: '' });
    expect(legacyTarget('goal/board')).toEqual({ factory: 'goal', rest: '/board' });
    expect(legacyTarget('goal/items/12')).toEqual({ factory: 'goal', rest: '/items/12' });
  });

  test('ignores stray slashes', () => {
    expect(legacyTarget('/goal//items/')).toEqual({ factory: 'goal', rest: '/items' });
  });

  test('has no target without a factory', () => {
    expect(legacyTarget('')).toBeNull();
    expect(legacyTarget('/')).toBeNull();
  });
});

describe('legacyPath', () => {
  test('re-homes the path under the organization, keeping the rest', () => {
    expect(legacyPath('acme', { factory: 'goal', rest: '/items/12' })).toBe('/o/acme/f/goal/items/12');
    expect(legacyPath('acme', { factory: 'goal', rest: '' })).toBe('/o/acme/f/goal');
  });

  test('encodes every segment it puts in the path', () => {
    expect(legacyPath('acme', { factory: 'a b', rest: '/items/goal#12' })).toBe('/o/acme/f/a%20b/items/goal%2312');
  });
});
