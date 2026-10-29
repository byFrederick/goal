import { describe, expect, it } from 'vitest';
import {
  clampWidth,
  previewWidth,
  resizeFromKey,
  settle,
  sidebarDefault,
  sidebarMax,
  sidebarMin,
} from './sidebar-width';

describe('clampWidth', () => {
  it.each([
    [215, 216],
    [216, 216],
    [256.4, 256],
    [400, 400],
    [401, 400],
    [Number.NaN, 256],
    [Number.POSITIVE_INFINITY, 256],
  ])('clamps %s to %s', (px, width) => {
    expect(clampWidth(px)).toBe(width);
  });
});

describe('settle: a release at the snap point', () => {
  it('folds to the rail below 160 and keeps the width it had', () => {
    expect(settle(159, 300)).toEqual({ collapsed: true, width: 300 });
  });

  it('does not fold at 160, which is clamped to the minimum', () => {
    expect(settle(160, 300)).toEqual({ collapsed: false, width: 216 });
  });

  it('clamps 215 and 216 to the minimum, and 400 and 401 to the maximum', () => {
    expect(settle(215, 256)).toEqual({ collapsed: false, width: 216 });
    expect(settle(216, 256)).toEqual({ collapsed: false, width: 216 });
    expect(settle(400, 256)).toEqual({ collapsed: false, width: 400 });
    expect(settle(401, 256)).toEqual({ collapsed: false, width: 400 });
  });

  it('expands to the default for NaN', () => {
    expect(settle(Number.NaN, 300)).toEqual({ collapsed: false, width: sidebarDefault });
  });
});

describe('previewWidth', () => {
  it('follows the pointer between the rail and the widest sidebar', () => {
    expect(previewWidth(120)).toBe(120);
    expect(previewWidth(10)).toBe(60);
    expect(previewWidth(900)).toBe(sidebarMax);
  });

  it('falls back to the default for NaN', () => {
    expect(previewWidth(Number.NaN)).toBe(sidebarDefault);
  });
});

describe('resizeFromKey', () => {
  const expanded = (width: number) => ({ collapsed: false, width });

  it('steps the width by 16 px with ArrowRight and ArrowLeft', () => {
    const once = resizeFromKey(expanded(256), 'ArrowRight');
    expect(once).toEqual(expanded(272));
    expect(resizeFromKey(once ?? expanded(0), 'ArrowRight')).toEqual(expanded(288));
    expect(resizeFromKey(expanded(256), 'ArrowLeft')).toEqual(expanded(240));
  });

  it('stops at the maximum and the minimum', () => {
    expect(resizeFromKey(expanded(400), 'ArrowRight')).toEqual(expanded(400));
    expect(resizeFromKey(expanded(216), 'ArrowLeft')).toEqual(expanded(216));
  });

  it('expands the rail at its saved width with ArrowRight and ignores ArrowLeft', () => {
    expect(resizeFromKey({ collapsed: true, width: 300 }, 'ArrowRight')).toEqual(expanded(300));
    expect(resizeFromKey({ collapsed: true, width: 300 }, 'ArrowLeft')).toBeNull();
  });

  it('collapses with Home, sets 400 with End and resets to 256 with Enter', () => {
    expect(resizeFromKey(expanded(300), 'Home')).toEqual({ collapsed: true, width: 300 });
    expect(resizeFromKey(expanded(300), 'End')).toEqual(expanded(400));
    expect(resizeFromKey({ collapsed: true, width: 300 }, 'Enter')).toEqual(expanded(256));
  });

  it('ignores other keys', () => {
    expect(resizeFromKey(expanded(256), 'a')).toBeNull();
    expect(sidebarMin).toBe(216);
  });
});
