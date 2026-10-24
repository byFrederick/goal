import { describe, expect, test } from 'vitest';
import { checkSlug, slugFromName } from './org-rules';

describe('slugFromName', () => {
  test('lowercases the name and joins its words with dashes', () => {
    expect(slugFromName('Acme Two')).toBe('acme-two');
    expect(slugFromName('  North   Star, Inc. ')).toBe('north-star-inc');
  });

  test('drops accents and anything that is not a letter or digit', () => {
    expect(slugFromName('Ünïcode Läbs!')).toBe('unicode-labs');
    expect(slugFromName('R&D 42')).toBe('r-d-42');
  });

  test('keeps 40 characters at most and never ends with a dash', () => {
    expect(slugFromName(`${'a'.repeat(39)} b`)).toBe('a'.repeat(39));
    expect(slugFromName('x'.repeat(60))).toHaveLength(40);
  });

  test('is empty for a name with no letters or digits', () => {
    expect(slugFromName('— !')).toBe('');
  });

  test('suggests an address goald accepts for an ordinary name', () => {
    for (const name of ['Acme Two', 'Example Labs', 'R&D 42', `${'Long '.repeat(12)}name`]) {
      expect(checkSlug(slugFromName(name)), name).toBeNull();
    }
  });
});
