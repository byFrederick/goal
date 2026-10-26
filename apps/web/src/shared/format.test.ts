import { describe, expect, it } from 'vitest';
import { formatHours } from './format';

describe('formatHours', () => {
  it('says a duration in hours adaptively, as the Item page does', () => {
    expect(formatHours(114 / 3600)).toBe('1 min'); // a p85 of 114 s, not "0.0 h"
    expect(formatHours(30 / 3600)).toBe('30 s');
    expect(formatHours(1.5)).toBe('1 h 30 min');
    expect(formatHours(6)).toBe('6 h');
    expect(formatHours(50)).toBe('2 d 2 h');
  });
});
