import { describe, expect, test } from 'vitest';
import { roleLine } from './account';

describe('roleLine', () => {
  test('names the role in the organization', () => {
    expect(roleLine('owner', 'Acme')).toBe('Owner of Acme');
    expect(roleLine('member', 'Example Labs')).toBe('Member of Example Labs');
  });

  test('says nothing without a role (sign-in off, an API key)', () => {
    expect(roleLine(undefined, 'Acme')).toBeUndefined();
    expect(roleLine(null, 'Acme')).toBeUndefined();
  });
});
