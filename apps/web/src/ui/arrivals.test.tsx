// @vitest-environment jsdom
import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { useArrivals } from './arrivals';

describe('useArrivals', () => {
  it('treats the rows a list opens with as already there', () => {
    const { result } = renderHook(({ keys }) => useArrivals(keys), { initialProps: { keys: ['a', 'b'] } });
    expect(result.current('a')).toBe(false);
    expect(result.current('b')).toBe(false);
  });

  it('marks a row added after the list had rows as arrived, and keeps it so', () => {
    const { result, rerender } = renderHook(({ keys }) => useArrivals(keys), {
      initialProps: { keys: ['a'] },
    });
    rerender({ keys: ['a', 'new'] });
    expect(result.current('new')).toBe(true);
    expect(result.current('a')).toBe(false);

    rerender({ keys: ['a', 'new', 'newer'] });
    expect(result.current('new')).toBe(true);
    expect(result.current('newer')).toBe(true);
  });

  it('treats the first rows a list gets, after it was empty, as its baseline', () => {
    const { result, rerender } = renderHook(({ keys }) => useArrivals(keys), {
      initialProps: { keys: [] as string[] },
    });
    rerender({ keys: ['first'] });
    expect(result.current('first')).toBe(false);
  });
});
