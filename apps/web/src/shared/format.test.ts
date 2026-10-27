import { describe, expect, it } from 'vitest';
import { formatHours, ordinal } from './format';

describe('formatHours', () => {
  it('says a duration in hours adaptively, as the Item page does', () => {
    expect(formatHours(114 / 3600)).toBe('1 min'); // a p85 of 114 s, not "0.0 h"
    expect(formatHours(30 / 3600)).toBe('30 s');
    expect(formatHours(1.5)).toBe('1 h 30 min');
    expect(formatHours(6)).toBe('6 h');
    expect(formatHours(50)).toBe('2 d 2 h');
  });
});

describe('ordinal', () => {
  it('says a number as its ordinal, teens included', () => {
    expect(ordinal(1)).toBe('1st');
    expect(ordinal(2)).toBe('2nd');
    expect(ordinal(3)).toBe('3rd');
    expect(ordinal(4)).toBe('4th');
    expect(ordinal(7)).toBe('7th');
    expect(ordinal(11)).toBe('11th');
    expect(ordinal(12)).toBe('12th');
    expect(ordinal(13)).toBe('13th');
    expect(ordinal(21)).toBe('21st');
    expect(ordinal(112)).toBe('112th');
  });
});
