import { describe, expect, it } from 'vitest';
import { buildCacheClass, buildCacheLabel } from './build-cache';

describe('build cache label', () => {
  it('names each outcome and nothing for null', () => {
    expect(buildCacheLabel('hit')).toBe('Cache hit');
    expect(buildCacheLabel('miss')).toBe('Cache miss');
    expect(buildCacheLabel('off')).toBe('Cache off');
    expect(buildCacheLabel('error')).toBe('Cache error');
    expect(buildCacheLabel(null)).toBeNull();
  });

  it('colours only the error', () => {
    expect(buildCacheClass('error')).toBe('text-rework-ink');
    for (const o of ['hit', 'miss', 'off'] as const) expect(buildCacheClass(o)).toBe('text-muted');
  });
});
