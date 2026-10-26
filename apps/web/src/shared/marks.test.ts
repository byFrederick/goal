import { describe, expect, test } from 'vitest';
import { factoryMark, orgMark } from './marks';

describe('orgMark', () => {
  test('uses the short mark the organization chose', () => {
    expect(orgMark({ name: 'Acme', mark: 'AC' })).toBe('AC');
  });

  test('takes the initials of the first two words without one', () => {
    expect(orgMark({ name: 'Example Labs', mark: '' })).toBe('EL');
    expect(orgMark({ name: 'north  star works', mark: ' ' })).toBe('NS');
  });

  test('takes the first two letters, title-cased, of a one-word name', () => {
    expect(orgMark({ name: 'acme', mark: '' })).toBe('Ac');
    expect(orgMark({ name: 'X', mark: '' })).toBe('X');
  });
});

describe('factoryMark', () => {
  const named = (...names: string[]) => names.map((name) => ({ name }));

  test('is the first letter, upper-cased', () => {
    expect(factoryMark({ name: 'goal' }, named('goal', 'sre'))).toBe('G');
  });

  test('takes two letters when a factory earlier by name has the same first letter', () => {
    const all = named('support', 'sre', 'goal');
    expect(factoryMark({ name: 'sre' }, all)).toBe('S');
    expect(factoryMark({ name: 'support' }, all)).toBe('Su');
  });

  test('keeps one letter for the first of several factories with that letter', () => {
    expect(factoryMark({ name: 'alpha' }, named('alpha', 'atlas', 'apex'))).toBe('A');
    expect(factoryMark({ name: 'atlas' }, named('alpha', 'atlas', 'apex'))).toBe('At');
  });
});
