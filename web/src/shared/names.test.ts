import { describe, expect, test } from 'vitest';
import { displayName, hasName, initialsOf } from './names';

const email = 'a.ortiz@example.com';

describe('initialsOf', () => {
  test.each([
    ['F. Cruz', 'FC'],
    ['a.ortiz', 'AO'],
    ['Jonas Lindqvist-Berg', 'JB'],
    ['Okafor', 'O'],
    ['Ünal Şahin', 'ÜŞ'],
  ])('%s → %s', (name, initials) => {
    expect(initialsOf({ name, email })).toBe(initials);
  });

  // Signed up by code, a person has no name until they choose one: their email's initials.
  test.each([
    ['', 'a.ortiz@example.com', 'AO'],
    ['   ', 'okafor@example.com', 'O'],
    ['', 'f_cruz+goal@example.com', 'FG'],
    ['', '', '?'],
  ])('no name, %j and %s → %s', (name, address, initials) => {
    expect(initialsOf({ name, email: address })).toBe(initials);
  });
});

describe('displayName', () => {
  test('is the name, trimmed', () => {
    expect(displayName({ name: ' A. Ortiz ', email })).toBe('A. Ortiz');
    expect(hasName({ name: ' A. Ortiz ', email })).toBe(true);
  });

  test('is the email while the person has no name', () => {
    expect(displayName({ name: '', email })).toBe(email);
    expect(displayName({ name: '  ', email })).toBe(email);
    expect(hasName({ name: '  ', email })).toBe(false);
  });
});
