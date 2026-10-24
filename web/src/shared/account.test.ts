import { describe, expect, test } from 'vitest';
import { initialsOf, roleLine } from './account';

describe('initialsOf', () => {
  test.each([
    ['F. Cruz', 'FC'],
    ['a.ortiz', 'AO'],
    ['Jonas Lindqvist-Berg', 'JB'],
    ['Okafor', 'O'],
    ['Ünal Şahin', 'ÜŞ'],
    ['', '?'],
  ])('%s → %s', (name, initials) => {
    expect(initialsOf(name)).toBe(initials);
  });
});

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
