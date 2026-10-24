import { describe, expect, test } from 'vitest';
import { railFocusTarget } from './rail-focus';

describe('railFocusTarget on the vertical rail', () => {
  test('ArrowDown and ArrowUp move one item, stopping at the ends', () => {
    expect(railFocusTarget('ArrowDown', 0, 5, 'vertical')).toBe(1);
    expect(railFocusTarget('ArrowUp', 3, 5, 'vertical')).toBe(2);
    expect(railFocusTarget('ArrowDown', 4, 5, 'vertical')).toBe(4);
    expect(railFocusTarget('ArrowUp', 0, 5, 'vertical')).toBe(0);
  });

  test('Home and End go to the first and last item', () => {
    expect(railFocusTarget('Home', 3, 5, 'vertical')).toBe(0);
    expect(railFocusTarget('End', 1, 5, 'vertical')).toBe(4);
  });

  test('other keys, and the cross axis, leave focus to the browser', () => {
    expect(railFocusTarget('ArrowRight', 1, 5, 'vertical')).toBeNull();
    expect(railFocusTarget('Enter', 1, 5, 'vertical')).toBeNull();
    expect(railFocusTarget('Tab', 1, 5, 'vertical')).toBeNull();
  });

  test('a focus outside the items starts from the first', () => {
    expect(railFocusTarget('ArrowDown', -1, 5, 'vertical')).toBe(0);
    expect(railFocusTarget('ArrowDown', 0, 0, 'vertical')).toBeNull();
  });
});

describe('railFocusTarget on the phone top row', () => {
  test('ArrowRight and ArrowLeft move along the row', () => {
    expect(railFocusTarget('ArrowRight', 1, 5, 'horizontal')).toBe(2);
    expect(railFocusTarget('ArrowLeft', 1, 5, 'horizontal')).toBe(0);
    expect(railFocusTarget('ArrowDown', 1, 5, 'horizontal')).toBeNull();
  });
});
